package e2e

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// Contract: mcp-oauth-contract.md.

const _oauthAnswer = "oauth-answer-42"

// _hostedCatalog is the catalog after Context7: hosted servers that need sign-in.
var _hostedCatalog = []mcpServer{
	{ID: "notion", Name: "Notion", Kind: "catalog", URL: "https://mcp.notion.com/mcp", Status: "not_connected"},
	{ID: "figma", Name: "Figma", Kind: "catalog", URL: "https://mcp.figma.com/mcp", Status: "not_connected"},
	{ID: "cloudflare", Name: "Cloudflare", Kind: "catalog", URL: "https://mcp.cloudflare.com/mcp", Status: "not_connected"},
}

type oauthRegistration struct {
	ClientName              string   `json:"client_name"`
	RedirectURIs            []string `json:"redirect_uris"`
	GrantTypes              []string `json:"grant_types"`
	ResponseTypes           []string `json:"response_types"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
	clientID                string
}

type oauthCode struct{ client, challenge, redirect string }

// oauthMCP is a hosted MCP server that needs sign-in, with its authorization server on a second
// origin: the 401 names the protected-resource metadata, which names the auth server, whose
// metadata has the endpoints. Every client, code and token it issues is unique. Its MCP tool
// `answer` answers "oauth-answer-42" to the latest access token only. The fake checks what the
// client sends (PKCE S256 with a single-use code, the registered redirect URI and client, the
// resource) and reports a wrong request with t.Errorf.
type oauthMCP struct {
	t    *testing.T
	tag  string
	mcp  *httptest.Server
	auth *httptest.Server

	mu            sync.Mutex
	registration  bool // advertises registration_endpoint
	refuse        bool // registration_endpoint answers 403, as Figma's does for an unapproved client
	requireAuth   bool // false: answers every request, as a server with no sign-in does
	ttl           int  // expires_in of the tokens a code grant issues
	refreshTTL    int  // expires_in of the tokens a refresh issues
	reject        bool // answer every refresh invalid_grant
	issued        int
	clients       map[string]string // client id -> its redirect URI
	codes         map[string]oauthCode
	registrations []oauthRegistration
	refusals      int // registrations answered 403
	authorizes    []url.Values
	exchanges     int
	refreshes     []url.Values
	access        string
	refresh       string
	probes        int // MCP requests with no Authorization header
	bad           int // MCP requests with a wrong one
	called        int // tools/call requests with the right one
}

func newOAuthMCP(t *testing.T) *oauthMCP {
	t.Helper()
	o := &oauthMCP{
		t: t, tag: strings.ReplaceAll(t.Name(), "/", "-"), registration: true, requireAuth: true, ttl: 7200, refreshTTL: 7200,
		clients: map[string]string{}, codes: map[string]oauthCode{},
	}
	o.auth = httptest.NewServer(http.HandlerFunc(o.serveAuth))
	o.mcp = httptest.NewServer(http.HandlerFunc(o.serveMCP))
	t.Cleanup(o.mcp.Close)
	t.Cleanup(o.auth.Close)
	return o
}

func (o *oauthMCP) url() string { return o.mcp.URL + "/mcp" }

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func (o *oauthMCP) serveMCP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/.well-known/oauth-protected-resource" {
		writeJSON(w, 200, map[string]any{"resource": o.url(), "authorization_servers": []string{o.auth.URL}})
		return
	}
	if r.URL.Path != "/mcp" || r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	header := r.Header.Get("Authorization")
	o.mu.Lock()
	if o.requireAuth && (o.access == "" || header != "Bearer "+o.access) {
		if header == "" {
			o.probes++
		} else {
			o.bad++
		}
		o.mu.Unlock()
		w.Header().Set("WWW-Authenticate", `Bearer resource_metadata="`+o.mcp.URL+`/.well-known/oauth-protected-resource"`)
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	o.mu.Unlock()
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
			"serverInfo": map[string]string{"name": "fake-oauth", "version": "0"}}
	case "tools/list":
		result = map[string]any{"tools": []any{map[string]any{"name": "answer", "description": "Answers a fixed text.",
			"inputSchema": map[string]any{"type": "object", "properties": map[string]any{}}}}}
	case "tools/call":
		o.mu.Lock()
		o.called++
		o.mu.Unlock()
		result = map[string]any{"content": []map[string]string{{"type": "text", "text": _oauthAnswer}}}
	}
	writeJSON(w, 200, map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result})
}

func (o *oauthMCP) serveAuth(w http.ResponseWriter, r *http.Request) {
	o.mu.Lock()
	defer o.mu.Unlock()
	switch r.URL.Path {
	case "/.well-known/oauth-authorization-server":
		meta := map[string]any{
			"issuer": o.auth.URL, "authorization_endpoint": o.auth.URL + "/authorize", "token_endpoint": o.auth.URL + "/token",
			"code_challenge_methods_supported": []string{"S256"}, "response_types_supported": []string{"code"},
			"grant_types_supported": []string{"authorization_code", "refresh_token"}, "token_endpoint_auth_methods_supported": []string{"none"},
		}
		if o.registration {
			meta["registration_endpoint"] = o.auth.URL + "/register"
		}
		writeJSON(w, 200, meta)
	case "/register":
		var reg oauthRegistration
		if r.Method != http.MethodPost || json.NewDecoder(r.Body).Decode(&reg) != nil || !o.registration {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if len(reg.RedirectURIs) != 1 {
			o.t.Errorf("registration redirect_uris %v, want exactly one", reg.RedirectURIs)
		}
		if o.refuse {
			o.refusals++
			writeJSON(w, http.StatusForbidden, map[string]any{"error": "Forbidden"})
			return
		}
		reg.clientID = fmt.Sprintf("fake-oauth-client-%s-%d", o.tag, len(o.registrations)+1)
		o.registrations = append(o.registrations, reg)
		if len(reg.RedirectURIs) > 0 {
			o.clients[reg.clientID] = reg.RedirectURIs[0]
		}
		writeJSON(w, 201, map[string]any{"client_id": reg.clientID, "client_name": reg.ClientName, "redirect_uris": reg.RedirectURIs})
	case "/authorize":
		q := r.URL.Query()
		o.authorizes = append(o.authorizes, q)
		redirect, known := o.clients[q.Get("client_id")]
		if !known || q.Get("redirect_uri") != redirect || q.Get("response_type") != "code" || q.Get("code_challenge_method") != "S256" ||
			q.Get("code_challenge") == "" || q.Get("state") == "" || q.Get("resource") != o.url() {
			o.t.Errorf("authorize request %v: want a registered client and redirect URI, response_type=code, S256 challenge, state and resource %s", q, o.url())
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		code := fmt.Sprintf("fake-oauth-code-%s-%d", o.tag, len(o.authorizes))
		o.codes[code] = oauthCode{client: q.Get("client_id"), challenge: q.Get("code_challenge"), redirect: redirect}
		http.Redirect(w, r, redirect+"?"+url.Values{"code": {code}, "state": {q.Get("state")}}.Encode(), http.StatusFound)
	case "/token":
		if err := r.ParseForm(); err != nil || r.Method != http.MethodPost {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if _, known := o.clients[r.Form.Get("client_id")]; !known {
			writeJSON(w, 401, map[string]string{"error": "invalid_client"})
			return
		}
		switch r.Form.Get("grant_type") {
		case "authorization_code":
			c, ok := o.codes[r.Form.Get("code")]
			delete(o.codes, r.Form.Get("code"))
			sum := sha256.Sum256([]byte(r.Form.Get("code_verifier")))
			if !ok || c.client != r.Form.Get("client_id") || c.redirect != r.Form.Get("redirect_uri") {
				writeJSON(w, 400, map[string]string{"error": "invalid_grant"})
				return
			}
			if base64.RawURLEncoding.EncodeToString(sum[:]) != c.challenge || len(r.Form.Get("code_verifier")) < 43 || r.Form.Get("resource") != o.url() {
				o.t.Errorf("code exchange %v: want the S256 verifier of the challenge (43+ characters) and resource %s", r.Form, o.url())
				writeJSON(w, 400, map[string]string{"error": "invalid_grant"})
				return
			}
			o.exchanges++
			o.issue(w, o.ttl)
		case "refresh_token":
			o.refreshes = append(o.refreshes, r.Form)
			if o.reject || r.Form.Get("refresh_token") != o.refresh {
				writeJSON(w, 400, map[string]string{"error": "invalid_grant", "error_description": "The refresh token is expired or revoked."})
				return
			}
			o.issue(w, o.refreshTTL)
		default:
			writeJSON(w, 400, map[string]string{"error": "unsupported_grant_type"})
		}
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

// issue answers with a new access and refresh pair; the caller holds mu.
func (o *oauthMCP) issue(w http.ResponseWriter, ttl int) {
	o.issued++
	o.access = fmt.Sprintf("fake-oauth-access-%s-%d", o.tag, o.issued)
	o.refresh = fmt.Sprintf("fake-oauth-refresh-%s-%d", o.tag, o.issued)
	writeJSON(w, 200, map[string]any{"access_token": o.access, "refresh_token": o.refresh, "token_type": "Bearer", "expires_in": ttl})
}

func (o *oauthMCP) set(f func()) {
	o.mu.Lock()
	defer o.mu.Unlock()
	f()
}

func (o *oauthMCP) tokens() (access, refresh string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.access, o.refresh
}

func (o *oauthMCP) refreshRequests() []url.Values {
	o.mu.Lock()
	defer o.mu.Unlock()
	return slices.Clone(o.refreshes)
}

func (o *oauthMCP) registered() []oauthRegistration {
	o.mu.Lock()
	defer o.mu.Unlock()
	return slices.Clone(o.registrations)
}

func (o *oauthMCP) refused() int {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.refusals
}

// seen is how many MCP requests had no Authorization header, a wrong one, and a right tools/call.
func (o *oauthMCP) seen() (probes, bad, called int) {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.probes, o.bad, o.called
}

func (o *oauthMCP) codeExchanges() int {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.exchanges
}

// oauthFixture is an MCP fixture machine with a fake OAuth MCP server to add to its library.
type oauthFixture struct {
	*mcpFixture
	o *oauthMCP
}

func newOAuthFixture(t *testing.T, p string) *oauthFixture {
	t.Helper()
	return &oauthFixture{mcpFixture: newMCPFixture(t, p), o: newOAuthMCP(t)}
}

// add puts a remote server with no headers in the library, as Add by URL does.
func (f *oauthFixture) add(t *testing.T, name string, o *oauthMCP) mcpServer {
	t.Helper()
	var s mcpServer
	f.s.do(t, "POST", "/api/mcp-servers", map[string]any{"name": name, "url": o.url()}, &s, http.StatusCreated)
	t.Cleanup(func() { clearSecretEntry("mcp:" + s.ID + ":oauth") })
	return s
}

func (f *oauthFixture) server(t *testing.T, id string) mcpServer {
	t.Helper()
	var list []mcpServer
	f.s.do(t, "GET", "/api/mcp-servers", nil, &list, http.StatusOK)
	for _, s := range list {
		if s.ID == id {
			return s
		}
	}
	t.Fatalf("GET /api/mcp-servers lacks %s: %+v", id, list)
	return mcpServer{}
}

// waitStatus waits until the server shows status.
func (f *oauthFixture) waitStatus(t *testing.T, id, status string) {
	t.Helper()
	var got string
	for range 50 {
		if got = f.server(t, id).Status; got == status {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("server %s status %q after 5s, want %q", id, got, status)
}

// connect starts the sign-in and returns the authorize URL Reef gives the browser.
func (f *oauthFixture) connect(t *testing.T, id string) string {
	t.Helper()
	var out struct {
		AuthorizeURL string `json:"authorize_url"`
	}
	f.s.do(t, "POST", "/api/mcp-servers/"+id+"/connect", nil, &out, http.StatusOK)
	if out.AuthorizeURL == "" {
		t.Fatal("POST connect gave no authorize_url")
	}
	return out.AuthorizeURL
}

type callbackReply struct {
	status      int
	contentType string
	body        string
}

// allow is the user clicking Allow: the browser opens the authorize URL, the fake redirects to
// Reef's localhost callback, and the browser follows it.
func (f *oauthFixture) allow(t *testing.T, authorizeURL string) callbackReply {
	t.Helper()
	browser := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := browser.Get(authorizeURL)
	if err != nil {
		t.Fatalf("open the authorize URL: %v", err)
	}
	_ = resp.Body.Close()
	location := resp.Header.Get("Location")
	if resp.StatusCode != http.StatusFound || !strings.HasPrefix(location, f.s.url+"/oauth/callback?") {
		t.Fatalf("authorize answered %d with Location %q, want a redirect to %s/oauth/callback", resp.StatusCode, location, f.s.url)
	}
	return f.callback(t, location)
}

func (f *oauthFixture) callback(t *testing.T, location string) callbackReply {
	t.Helper()
	resp, err := http.Get(location)
	if err != nil {
		t.Fatalf("GET %s: %v", location, err)
	}
	defer func() { _ = resp.Body.Close() }()
	b, _ := io.ReadAll(resp.Body)
	return callbackReply{resp.StatusCode, resp.Header.Get("Content-Type"), string(b)}
}

// signIn does Connect and Allow to the end and waits for Connected.
func (f *oauthFixture) signIn(t *testing.T, id string) {
	t.Helper()
	cb := f.allow(t, f.connect(t, id))
	if cb.status != http.StatusOK || !strings.HasPrefix(cb.contentType, "text/html") {
		t.Fatalf("callback answered %d %q: %s", cb.status, cb.contentType, cb.body)
	}
	f.waitStatus(t, id, "connected")
}

type oauthRecord struct {
	ClientID     string `json:"client_id"`
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresAt    string `json:"expires_at"`
}

// oauthSecret reads the server's sign-in record with the OS tool, not through Atoll, and the time
// its access token expires.
func oauthSecret(t *testing.T, id string) (oauthRecord, time.Time) {
	t.Helper()
	value, ok := secretEntry("mcp:" + id + ":oauth")
	if !ok {
		t.Fatalf("OS secret store has no entry mcp:%s:oauth", id)
	}
	var rec oauthRecord
	if err := json.Unmarshal([]byte(value), &rec); err != nil {
		t.Fatalf("mcp:%s:oauth is not the JSON record: %v", id, err)
	}
	exp, err := time.Parse(time.RFC3339, rec.ExpiresAt)
	if err != nil || rec.ClientID == "" || rec.AccessToken == "" || rec.RefreshToken == "" {
		t.Fatalf("mcp:%s:oauth record %+v: want client_id, access_token, refresh_token and RFC3339 expires_at", id, rec)
	}
	return rec, exp
}

// assertExpiresIn checks the stored expiry is about seconds ahead.
func assertExpiresIn(t *testing.T, exp time.Time, seconds int) {
	t.Helper()
	if left := time.Until(exp); left < time.Duration(seconds-120)*time.Second || left > time.Duration(seconds+120)*time.Second {
		t.Fatalf("stored expiry in %s, want about %ds", left.Round(time.Second), seconds)
	}
}

// assertNoLeak checks that none of the values is in an API reply (the connect reply excepted: its
// URL names the client), the database or any file of the data dir, or the server log.
func (f *oauthFixture) assertNoLeak(t *testing.T, values ...string) {
	t.Helper()
	resp, err := http.Get(f.s.url + "/api/mcp-servers")
	if err != nil {
		t.Fatal(err)
	}
	reply, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	for _, v := range values {
		if strings.Contains(string(reply), v) {
			t.Errorf("GET /api/mcp-servers shows %q: %s", v, reply)
		}
		assertNoSecret(t, v, f.m.data)
	}
	if b, err := os.ReadFile(filepath.Join(f.m.data, "atoll.db")); err != nil {
		t.Errorf("atoll.db: %v", err)
	} else {
		for _, v := range values {
			if strings.Contains(string(b), v) {
				t.Errorf("atoll.db holds %q", v)
			}
		}
	}
	f.s.kill()
	for _, v := range values {
		if strings.Contains(f.s.log.String(), v) {
			t.Errorf("server log leaked %q", v)
		}
	}
}

// sessionCount is how many runs the fake provider held; none when it never started.
func sessionCount(t *testing.T, m *machine, p string) int {
	t.Helper()
	if _, err := os.Stat(filepath.Join(m.bin, p+".log")); err != nil {
		return 0
	}
	return len(m.sessions(t, p))
}

// TestMCPOAuthFixtures_Fake drives the fake by hand, as a client of the MCP authorization flow
// would, so the fake is proven before the product has any sign-in.
func TestMCPOAuthFixtures_Fake(t *testing.T) {
	o := newOAuthMCP(t)
	post := func(target string, header http.Header, body string) *http.Response {
		req, _ := http.NewRequest(http.MethodPost, target, strings.NewReader(body))
		req.Header = header
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = resp.Body.Close() })
		return resp
	}
	getJSON := func(target string, out any) {
		resp, err := http.Get(target)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = resp.Body.Close() }()
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			t.Fatal(err)
		}
	}
	initialize := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`
	resp := post(o.url(), http.Header{"Content-Type": {"application/json"}}, initialize)
	challenge := resp.Header.Get("WWW-Authenticate")
	if resp.StatusCode != 401 || !strings.HasPrefix(challenge, `Bearer resource_metadata="`) {
		t.Fatalf("MCP request without a token: %d %q, want 401 and a resource_metadata challenge", resp.StatusCode, challenge)
	}
	var resource struct {
		AuthorizationServers []string `json:"authorization_servers"`
	}
	getJSON(strings.TrimSuffix(strings.TrimPrefix(challenge, `Bearer resource_metadata="`), `"`), &resource)
	var meta struct {
		AuthorizationEndpoint string   `json:"authorization_endpoint"`
		TokenEndpoint         string   `json:"token_endpoint"`
		RegistrationEndpoint  string   `json:"registration_endpoint"`
		Methods               []string `json:"code_challenge_methods_supported"`
	}
	if len(resource.AuthorizationServers) != 1 {
		t.Fatalf("protected-resource metadata %+v", resource)
	}
	getJSON(resource.AuthorizationServers[0]+"/.well-known/oauth-authorization-server", &meta)
	if meta.AuthorizationEndpoint == "" || meta.TokenEndpoint == "" || meta.RegistrationEndpoint == "" || !slices.Equal(meta.Methods, []string{"S256"}) {
		t.Fatalf("auth server metadata %+v", meta)
	}

	callback := "http://127.0.0.1:1/oauth/callback"
	var client struct {
		ClientID string `json:"client_id"`
	}
	reg := post(meta.RegistrationEndpoint, http.Header{"Content-Type": {"application/json"}},
		`{"client_name":"Reef","redirect_uris":["`+callback+`"],"grant_types":["authorization_code","refresh_token"],"response_types":["code"],"token_endpoint_auth_method":"none"}`)
	if err := json.NewDecoder(reg.Body).Decode(&client); err != nil || reg.StatusCode != 201 || client.ClientID == "" {
		t.Fatalf("registration: %d %+v %v", reg.StatusCode, client, err)
	}

	verifier := strings.Repeat("v", 43)
	sum := sha256.Sum256([]byte(verifier))
	q := url.Values{"response_type": {"code"}, "client_id": {client.ClientID}, "redirect_uri": {callback}, "code_challenge_method": {"S256"},
		"code_challenge": {base64.RawURLEncoding.EncodeToString(sum[:])}, "state": {"s1"}, "resource": {o.url()}}
	browser := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	auth, err := browser.Get(meta.AuthorizationEndpoint + "?" + q.Encode())
	if err != nil {
		t.Fatal(err)
	}
	_ = auth.Body.Close()
	loc, err := url.Parse(auth.Header.Get("Location"))
	if err != nil || auth.StatusCode != 302 || loc.Query().Get("state") != "s1" || loc.Query().Get("code") == "" {
		t.Fatalf("authorize: %d %q %v", auth.StatusCode, auth.Header.Get("Location"), err)
	}
	form := func(v url.Values) *http.Response {
		return post(meta.TokenEndpoint, http.Header{"Content-Type": {"application/x-www-form-urlencoded"}}, v.Encode())
	}
	var tok struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
	}
	exchange := form(url.Values{"grant_type": {"authorization_code"}, "code": {loc.Query().Get("code")}, "redirect_uri": {callback},
		"client_id": {client.ClientID}, "code_verifier": {verifier}, "resource": {o.url()}})
	if err := json.NewDecoder(exchange.Body).Decode(&tok); err != nil || exchange.StatusCode != 200 || tok.ExpiresIn != 7200 || tok.AccessToken == "" || tok.RefreshToken == "" {
		t.Fatalf("code exchange: %d %+v %v", exchange.StatusCode, tok, err)
	}
	if again := form(url.Values{"grant_type": {"authorization_code"}, "code": {loc.Query().Get("code")}, "redirect_uri": {callback}, "client_id": {client.ClientID}, "code_verifier": {verifier}}); again.StatusCode != 400 {
		t.Fatalf("a used code answered %d, want 400", again.StatusCode)
	}

	call := `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"answer","arguments":{}}}`
	if r := post(o.url(), http.Header{"Authorization": {"Bearer " + tok.AccessToken}}, call); r.StatusCode != 200 {
		t.Fatalf("MCP call with the token: %d", r.StatusCode)
	}
	if r := post(o.url(), http.Header{"Authorization": {"Bearer nope"}}, call); r.StatusCode != 401 {
		t.Fatalf("MCP call with another token: %d", r.StatusCode)
	}
	refreshed := form(url.Values{"grant_type": {"refresh_token"}, "refresh_token": {tok.RefreshToken}, "client_id": {client.ClientID}})
	if refreshed.StatusCode != 200 {
		t.Fatalf("refresh: %d", refreshed.StatusCode)
	}
	if r := post(o.url(), http.Header{"Authorization": {"Bearer " + tok.AccessToken}}, call); r.StatusCode != 401 {
		t.Fatalf("MCP call with the replaced token: %d, want 401", r.StatusCode)
	}
	if stale := form(url.Values{"grant_type": {"refresh_token"}, "refresh_token": {tok.RefreshToken}, "client_id": {client.ClientID}}); stale.StatusCode != 400 {
		t.Fatalf("refresh with the replaced refresh token: %d, want 400", stale.StatusCode)
	}
	if probes, bad, called := o.seen(); probes != 1 || bad != 2 || called != 1 {
		t.Fatalf("seen %d probes, %d bad, %d calls, want 1, 2 and 1", probes, bad, called)
	}
}
