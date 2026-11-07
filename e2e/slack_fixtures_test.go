package e2e

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"
)

// Contract: slack-mcp-contract.md.

const (
	_slackClientID = "fake-slack-client-1.1"
	_slackAnswer   = "slack-answer-42"
)

// _slackScopes are the user scopes the fake's protected-resource metadata lists.
var _slackScopes = []string{"search:read.public", "channels:history", "chat:write", "users:read"}

type slackCode struct{ challenge, redirect string }

// fakeSlack is Slack's MCP server and its authorization server on one origin, as Slack serves
// them: the 401 names the protected-resource metadata (with scopes_supported), the auth server
// metadata has no registration_endpoint, the token endpoint answers HTTP 200 with {"ok":...} and
// refresh tokens are single use. A public client sends no client_secret and no Authorization
// header to the token endpoint; the fake reports either with t.Errorf. Every code and token it
// issues is unique. Its MCP tool `answer` answers "slack-answer-42" to the latest access token.
type fakeSlack struct {
	t   *testing.T
	tag string
	srv *httptest.Server

	mu         sync.Mutex
	ttl        int  // expires_in of the tokens a code grant issues
	refreshTTL int  // expires_in of the tokens a refresh issues
	reject     bool // answer every refresh {"ok":false,"error":"invalid_refresh_token"}
	issued     int
	codes      map[string]slackCode
	authorizes []url.Values
	exchanges  []url.Values
	refreshes  []url.Values
	registers  int // requests to /register
	access     string
	refresh    string
	bad        int // MCP requests with a wrong Authorization header
	called     int // tools/call requests with the right one
}

func newFakeSlack(t *testing.T) *fakeSlack {
	t.Helper()
	s := &fakeSlack{
		t: t, tag: strings.ReplaceAll(t.Name(), "/", "-"), ttl: 43200, refreshTTL: 43200, codes: map[string]slackCode{},
	}
	s.srv = httptest.NewServer(http.HandlerFunc(s.serve))
	t.Cleanup(s.srv.Close)
	return s
}

func (s *fakeSlack) url() string { return s.srv.URL + "/mcp" }

func (s *fakeSlack) serve(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/mcp":
		s.serveMCP(w, r)
	case "/.well-known/oauth-protected-resource":
		writeJSON(w, 200, map[string]any{"resource": s.url(), "authorization_servers": []string{s.srv.URL}, "scopes_supported": _slackScopes})
	case "/.well-known/oauth-authorization-server":
		writeJSON(w, 200, map[string]any{
			"issuer": s.srv.URL, "authorization_endpoint": s.srv.URL + "/oauth/v2_user/authorize", "token_endpoint": s.srv.URL + "/api/oauth.v2.user.access",
			"code_challenge_methods_supported": []string{"S256"}, "response_types_supported": []string{"code"},
			"grant_types_supported": []string{"authorization_code", "refresh_token"}, "token_endpoint_auth_methods_supported": []string{"none"},
		})
	case "/register":
		s.mu.Lock()
		s.registers++
		s.mu.Unlock()
		w.WriteHeader(http.StatusNotFound)
	case "/oauth/v2_user/authorize":
		s.authorize(w, r)
	case "/api/oauth.v2.user.access":
		s.token(w, r)
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func (s *fakeSlack) serveMCP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	header := r.Header.Get("Authorization")
	s.mu.Lock()
	if s.access == "" || header != "Bearer "+s.access {
		if header != "" {
			s.bad++
		}
		s.mu.Unlock()
		w.Header().Set("WWW-Authenticate", `Bearer resource_metadata="`+s.srv.URL+`/.well-known/oauth-protected-resource"`)
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	s.mu.Unlock()
	var req struct {
		ID     json.RawMessage `json:"id"`
		Method string          `json:"method"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	if len(req.ID) == 0 {
		w.WriteHeader(http.StatusAccepted)
		return
	}
	var result any = map[string]any{}
	switch req.Method {
	case "initialize":
		w.Header().Set("Mcp-Session-Id", "fake-session")
		result = map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{"tools": map[string]any{}},
			"serverInfo": map[string]string{"name": "fake-slack", "version": "0"}}
	case "tools/list":
		result = map[string]any{"tools": []any{map[string]any{"name": "answer", "description": "Answers a fixed text.",
			"inputSchema": map[string]any{"type": "object", "properties": map[string]any{}}}}}
	case "tools/call":
		s.mu.Lock()
		s.called++
		s.mu.Unlock()
		result = map[string]any{"content": []map[string]string{{"type": "text", "text": _slackAnswer}}}
	}
	writeJSON(w, 200, map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result})
}

func (s *fakeSlack) authorize(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	q := r.URL.Query()
	s.authorizes = append(s.authorizes, q)
	redirect := q.Get("redirect_uri")
	if q.Get("client_id") != _slackClientID || q.Get("response_type") != "code" || q.Get("code_challenge_method") != "S256" ||
		q.Get("code_challenge") == "" || q.Get("state") == "" || q.Get("scope") == "" ||
		!strings.HasPrefix(redirect, "http://127.0.0.1:") || !strings.HasSuffix(redirect, "/oauth/callback") {
		s.t.Errorf("authorize request %v: want client %s, response_type=code, S256 challenge, state, scope and a localhost /oauth/callback redirect", q, _slackClientID)
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	code := fmt.Sprintf("fake-slack-code-%s-%d", s.tag, len(s.authorizes))
	s.codes[code] = slackCode{challenge: q.Get("code_challenge"), redirect: redirect}
	http.Redirect(w, r, redirect+"?"+url.Values{"code": {code}, "state": {q.Get("state")}}.Encode(), http.StatusFound)
}

func (s *fakeSlack) token(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := r.ParseForm(); err != nil || r.Method != http.MethodPost {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	if _, has := r.PostForm["client_secret"]; has || r.Header.Get("Authorization") != "" {
		s.t.Errorf("token request %v (Authorization %q): a public client sends no client_secret and no Authorization header", r.PostForm, r.Header.Get("Authorization"))
		writeJSON(w, 200, map[string]any{"ok": false, "error": "invalid_client"})
		return
	}
	if r.PostForm.Get("client_id") != _slackClientID {
		writeJSON(w, 200, map[string]any{"ok": false, "error": "invalid_client_id"})
		return
	}
	switch r.PostForm.Get("grant_type") {
	case "authorization_code":
		c, ok := s.codes[r.PostForm.Get("code")]
		delete(s.codes, r.PostForm.Get("code"))
		sum := sha256.Sum256([]byte(r.PostForm.Get("code_verifier")))
		if !ok || c.redirect != r.PostForm.Get("redirect_uri") {
			writeJSON(w, 200, map[string]any{"ok": false, "error": "invalid_code"})
			return
		}
		if base64.RawURLEncoding.EncodeToString(sum[:]) != c.challenge || len(r.PostForm.Get("code_verifier")) < 43 {
			s.t.Errorf("code exchange %v: want the S256 verifier of the challenge (43+ characters)", r.PostForm)
			writeJSON(w, 200, map[string]any{"ok": false, "error": "bad_code_verifier"})
			return
		}
		s.exchanges = append(s.exchanges, r.PostForm)
		s.issue(w, s.ttl)
	case "refresh_token":
		s.refreshes = append(s.refreshes, r.PostForm)
		if s.reject || r.PostForm.Get("refresh_token") != s.refresh {
			writeJSON(w, 200, map[string]any{"ok": false, "error": "invalid_refresh_token"})
			return
		}
		s.issue(w, s.refreshTTL)
	default:
		writeJSON(w, 200, map[string]any{"ok": false, "error": "invalid_grant_type"})
	}
}

// issue answers with a new access and refresh pair; the caller holds mu. The old refresh token
// is dead from here on.
func (s *fakeSlack) issue(w http.ResponseWriter, ttl int) {
	s.issued++
	s.access = fmt.Sprintf("fake-slack-access-%s-%d", s.tag, s.issued)
	s.refresh = fmt.Sprintf("fake-slack-refresh-%s-%d", s.tag, s.issued)
	writeJSON(w, 200, map[string]any{
		"ok": true, "access_token": s.access, "refresh_token": s.refresh, "token_type": "user", "expires_in": ttl,
		"scope": strings.Join(_slackScopes, ","),
	})
}

func (s *fakeSlack) set(f func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f()
}

func (s *fakeSlack) tokens() (access, refresh string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.access, s.refresh
}

func (s *fakeSlack) authorizeRequests() []url.Values {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.authorizes)
}

func (s *fakeSlack) exchangeRequests() []url.Values {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.exchanges)
}

func (s *fakeSlack) refreshRequests() []url.Values {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.refreshes)
}

// seen is how many MCP requests carried a wrong token, how many tools/call carried the right one,
// and how many requests went to a registration endpoint.
func (s *fakeSlack) seen() (bad, called, registers int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.bad, s.called, s.registers
}

// slackFixture is a machine whose Reef talks to the fake Slack instead of mcp.slack.com.
type slackFixture struct {
	*oauthFixture
	sl *fakeSlack
}

func newSlackFixture(t *testing.T, p string) *slackFixture {
	t.Helper()
	requireSecretStore(t)
	m := newMachine(t, p)
	m.signIn(t, p)
	for _, key := range []string{"DBUS_SESSION_BUS_ADDRESS", "XDG_RUNTIME_DIR", "XDG_DATA_HOME"} {
		if v := os.Getenv(key); v != "" {
			m.env = append(m.env, key+"="+v)
		}
	}
	sl := newFakeSlack(t)
	m.env = append(m.env, "ATOLL_SLACK_MCP_URL="+sl.url())
	for _, name := range []string{"mcp:slack:client_id", "mcp:slack:oauth"} {
		clearSecretEntry(name)
		t.Cleanup(func() { clearSecretEntry(name) })
	}
	return &slackFixture{oauthFixture: &oauthFixture{mcpFixture: &mcpFixture{m: m, s: m.serve(t), p: p}}, sl: sl}
}

type slackSetup struct {
	RedirectURI string   `json:"redirect_uri"`
	Scopes      []string `json:"scopes"`
}

func (f *slackFixture) setupInfo(t *testing.T) slackSetup {
	t.Helper()
	var info slackSetup
	f.s.do(t, "GET", "/api/mcp-servers/slack/setup", nil, &info, http.StatusOK)
	return info
}

// setup saves the app's Client ID, as the Set up dialog does.
func (f *slackFixture) setup(t *testing.T) {
	t.Helper()
	f.s.do(t, "POST", "/api/mcp-servers/slack/setup", map[string]string{"client_id": _slackClientID}, nil, http.StatusNoContent)
}
