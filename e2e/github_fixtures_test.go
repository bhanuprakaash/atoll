package e2e

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/cgi"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"atoll/e2e/fakes/fake"
)

const (
	_githubClientID = "Ov23li5N8B8tlXHi1Rza"
	_githubPrompt   = "Check what this run has for GitHub."
)

type githubMCPRequest struct{ Auth, Method, Tool string }

// fakeGitHub is GitHub for the tests, on one httptest server: the device-flow routes, `GET /user`,
// the MCP server at /mcp/, and a git remote at /octo/hello.git behind basic auth. Like the real
// one it answers form-encoded unless the request has Accept: application/json, and answers an
// unapproved device code with 200 and authorization_pending. Only the token it issued last is
// valid, so signing in again revokes the old one.
type fakeGitHub struct {
	*httptest.Server
	tag  string
	root string // bare repos: <root>/octo/hello.git

	mu         sync.Mutex
	approved   bool
	rejectUser bool // GET /user and the MCP server answer 401 to every token
	issued     int
	token      string
	deviceForm url.Values
	tokenForms []url.Values
	mcp        []githubMCPRequest
	pushAuth   []string // "user:password" of every git request GitHub accepted
	pulls      []githubPull

	// slowDownNext answers the next token request slow_down, as github.com does for a poll that
	// came too early. From then on, like github.com, a poll sooner than interval after the
	// previous one answers slow_down too, and each slow_down adds 5s to interval.
	slowDownNext bool
	slowedDown   bool
	interval     time.Duration
	polls        []time.Time
	slowDowns    int
}

func newFakeGitHub(t *testing.T) *fakeGitHub {
	t.Helper()
	g := &fakeGitHub{tag: strings.ReplaceAll(t.Name(), "/", "-"), root: t.TempDir(), interval: time.Second}
	gitIn(t, g.root, "init", "--bare", "-b", "main", filepath.Join(g.root, "octo", "hello.git"))
	mux := http.NewServeMux()
	mux.HandleFunc("POST /login/device/code", g.deviceCode)
	mux.HandleFunc("POST /login/oauth/access_token", g.accessToken)
	mux.HandleFunc("GET /user", g.user)
	mux.HandleFunc("POST /repos/{owner}/{repo}/pulls", g.createPull)
	mux.HandleFunc("POST /mcp/", g.mcpServer)
	mux.HandleFunc("/octo/", g.gitRemote)
	g.Server = httptest.NewServer(mux)
	t.Cleanup(g.Close)
	return g
}

func (g *fakeGitHub) set(f func()) {
	g.mu.Lock()
	defer g.mu.Unlock()
	f()
}

func (g *fakeGitHub) currentToken() string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.token
}

func (g *fakeGitHub) mcpRequests() []githubMCPRequest {
	g.mu.Lock()
	defer g.mu.Unlock()
	return slices.Clone(g.mcp)
}

func (g *fakeGitHub) pushes() []string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return slices.Clone(g.pushAuth)
}

// githubReply answers as GitHub does: JSON for Accept: application/json, else a form body.
func githubReply(w http.ResponseWriter, r *http.Request, v map[string]any) {
	if strings.Contains(r.Header.Get("Accept"), "application/json") {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(v)
		return
	}
	form := url.Values{}
	for k, x := range v {
		form.Set(k, fmt.Sprint(x))
	}
	w.Header().Set("Content-Type", "application/x-www-form-urlencoded; charset=utf-8")
	_, _ = fmt.Fprint(w, form.Encode())
}

func (g *fakeGitHub) deviceCode(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	g.set(func() { g.deviceForm = r.Form })
	githubReply(w, r, map[string]any{"device_code": "fixture-device-code", "user_code": "ABCD-1234",
		"verification_uri": "https://github.com/login/device", "expires_in": 900, "interval": 1})
}

func (g *fakeGitHub) accessToken(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	g.mu.Lock()
	defer g.mu.Unlock()
	g.tokenForms = append(g.tokenForms, r.Form)
	if r.Form.Get("device_code") != "fixture-device-code" {
		githubReply(w, r, map[string]any{"error": "bad_verification_code"})
		return
	}
	now := time.Now()
	early := g.slowDownNext || g.slowedDown && len(g.polls) > 0 && now.Sub(g.polls[len(g.polls)-1]) < g.interval
	g.polls = append(g.polls, now)
	if early {
		g.slowDownNext, g.slowedDown = false, true
		g.interval += 5 * time.Second
		g.slowDowns++
		githubReply(w, r, map[string]any{"error": "slow_down", "interval": int(g.interval / time.Second),
			"error_description": "Too many requests have been made in the same timeframe."})
		return
	}
	if !g.approved {
		githubReply(w, r, map[string]any{"error": "authorization_pending", "error_description": "The authorization request is still pending."})
		return
	}
	g.issued++
	g.token = fmt.Sprintf("fake-gho-%s-%d", g.tag, g.issued)
	githubReply(w, r, map[string]any{"access_token": g.token, "token_type": "bearer", "scope": "repo,read:org,workflow"})
}

// authorised reports whether the Authorization header carries the valid token.
func (g *fakeGitHub) authorised(header string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	for _, scheme := range []string{"Bearer ", "token "} {
		if t, ok := strings.CutPrefix(header, scheme); ok && g.token != "" && t == g.token && !g.rejectUser {
			return true
		}
	}
	return false
}

func (g *fakeGitHub) user(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if !g.authorised(r.Header.Get("Authorization")) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = fmt.Fprint(w, `{"message":"Bad credentials","status":"401"}`)
		return
	}
	_, _ = fmt.Fprint(w, `{"login":"reef-fixture","id":1,"name":"Reef Fixture"}`)
}

// mcpServer is GitHub's MCP server with one tool, get_me.
func (g *fakeGitHub) mcpServer(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID     json.RawMessage `json:"id"`
		Method string          `json:"method"`
		Params struct {
			Name string `json:"name"`
		} `json:"params"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	auth := r.Header.Get("Authorization")
	g.set(func() { g.mcp = append(g.mcp, githubMCPRequest{Auth: auth, Method: req.Method, Tool: req.Params.Name}) })
	if !g.authorised(auth) {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	if len(req.ID) == 0 {
		w.WriteHeader(http.StatusAccepted)
		return
	}
	var result any = map[string]any{}
	switch req.Method {
	case "initialize":
		result = map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{"tools": map[string]any{}}, "serverInfo": map[string]string{"name": "github-fixture", "version": "0"}}
	case "tools/list":
		result = map[string]any{"tools": []any{map[string]any{"name": "get_me", "description": "Get the signed-in user.", "inputSchema": map[string]any{"type": "object", "properties": map[string]any{}}}}}
	case "tools/call":
		result = map[string]any{"content": []map[string]string{{"type": "text", "text": `{"login":"reef-fixture","source":"mcp"}`}}}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result})
}

// gitRemote is git over HTTP for <root>/octo/hello.git. Like github.com it takes only the user
// x-access-token with a valid token as the password.
func (g *fakeGitHub) gitRemote(w http.ResponseWriter, r *http.Request) {
	user, pass, ok := r.BasicAuth()
	if !ok || user != "x-access-token" || !g.authorised("Bearer "+pass) {
		w.Header().Set("WWW-Authenticate", `Basic realm="GitHub"`)
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	g.set(func() { g.pushAuth = append(g.pushAuth, user+":"+pass) })
	backend, err := exec.Command("git", "--exec-path").Output()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	h := &cgi.Handler{
		Path:       filepath.Join(strings.TrimSpace(string(backend)), "git-http-backend"),
		Env:        []string{"GIT_PROJECT_ROOT=" + g.root, "GIT_HTTP_EXPORT_ALL=1", "REMOTE_USER=" + user},
		InheritEnv: []string{"PATH"},
	}
	h.ServeHTTP(w, r)
}

// signIn runs the device flow to the end, as gitlabOAuth.signIn does: POST path starts it, the
// fake user approves, polls follow the advertised one-second interval. It returns the connection id.
func (g *fakeGitHub) signIn(t *testing.T, s *server, path string, body map[string]string) string {
	t.Helper()
	var start struct {
		ID              string `json:"id"`
		UserCode        string `json:"user_code"`
		VerificationURI string `json:"verification_uri"`
		IntervalMs      int    `json:"interval_ms"`
	}
	s.do(t, "POST", path, body, &start, 201)
	if start.ID == "" || start.UserCode != "ABCD-1234" || start.VerificationURI != "https://github.com/login/device" {
		t.Fatalf("device start: %+v", start)
	}
	g.set(func() { g.approved = true })
	var status struct {
		Status string `json:"status"`
	}
	for range 5 {
		time.Sleep(1100 * time.Millisecond)
		s.do(t, "POST", "/api/connections/"+start.ID+"/poll", nil, &status, 200)
		if status.Status == "connected" {
			g.set(func() { g.approved = false })
			return start.ID
		}
	}
	t.Fatalf("sign in did not connect: %+v", status)
	return ""
}

// githubSecret reads the connection's keyring entry with the OS tool, not through Atoll.
func githubSecret(t *testing.T, id string) string {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "secret-tool", "lookup", "service", "atoll", "username", "github:"+id)
	if runtime.GOOS == "darwin" {
		cmd = exec.CommandContext(t.Context(), "security", "find-generic-password", "-s", "atoll", "-a", "github:"+id, "-w")
	}
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("connection %s has no entry in the OS secret store: %v", id, err)
	}
	return strings.TrimSpace(string(out))
}

// githubFixture is a user's machine with its own glab, gh and git logins, gh tokens in the env and
// a signed-in provider, an Atoll served on it against fakeGitHub.
type githubFixture struct {
	m  *machine
	s  *server
	gh *fakeGitHub
}

func newGitHubFixture(t *testing.T, provider string) *githubFixture {
	t.Helper()
	requireSecretStore(t)
	m := newLoginsMachine(t, provider, false)
	for _, key := range []string{"DBUS_SESSION_BUS_ADDRESS", "XDG_RUNTIME_DIR", "XDG_DATA_HOME"} {
		if v := os.Getenv(key); v != "" {
			m.env = append(m.env, key+"="+v)
		}
	}
	gh := newFakeGitHub(t)
	m.env = append(m.env, "ATOLL_GITHUB_URL="+gh.URL, "ATOLL_GITHUB_MCP_URL="+gh.URL+"/mcp/")
	writeFile(t, filepath.Join(m.bin, "github-url"), gh.URL)
	return &githubFixture{m: m, s: m.serve(t), gh: gh}
}

// connect signs in to GitHub through the API and returns the connection id.
func (f *githubFixture) connect(t *testing.T) string {
	t.Helper()
	id := f.gh.signIn(t, f.s, "/api/connections/github/start", nil)
	clearGitHubSecretAfter(t, id)
	return id
}

// clearGitHubSecretAfter removes the connection's keyring entry when the test ends.
func clearGitHubSecretAfter(t *testing.T, id string) {
	t.Helper()
	t.Cleanup(func() {
		if runtime.GOOS == "darwin" {
			_ = exec.Command("security", "delete-generic-password", "-s", "atoll", "-a", "github:"+id).Run()
			return
		}
		_ = exec.Command("secret-tool", "clear", "service", "atoll", "username", "github:"+id).Run()
	})
}

func (f *githubFixture) bot(t *testing.T, provider string, connections []string) bot {
	t.Helper()
	var b bot
	f.s.do(t, "POST", "/api/bots", map[string]any{"name": "Octo", "persona": "Works on GitHub.", "provider": provider, "connections": connections}, &b, 201)
	return b
}

// githubProbe is the reply of the github-probe scenario.
type githubProbe struct {
	GH       loginProbe `json:"gh"`
	Git      loginProbe `json:"git"`
	OtherGit loginProbe `json:"other_git"`
	Push     loginProbe `json:"push"`
	MCP      struct {
		Connected bool   `json:"connected"`
		Text      string `json:"text"`
	} `json:"mcp"`
}

// probe has the Bot check what a run of it has for GitHub, in a new thread, and returns the run
// and what the fake provider found.
func (f *githubFixture) probe(t *testing.T, b bot) (runInfo, githubProbe) {
	t.Helper()
	th := f.s.newThread(t, b)
	ch := f.s.events(t, th)
	done := f.s.finished(t, ch, f.s.send(t, th, _githubPrompt).ID)
	var p githubProbe
	if done.Status == "done" {
		if err := json.Unmarshal([]byte(done.Reply), &p); err != nil {
			t.Fatalf("probe reply %q: %v", done.Reply, err)
		}
	}
	return done, p
}

// digest is the first 12 hex of the token's sha256, how the probe proves a password without echoing it.
func digest(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])[:12]
}

// githubCredential is the key=value lines `git credential fill` printed.
func githubCredential(out string) map[string]string {
	kv := map[string]string{}
	for line := range strings.Lines(out) {
		if k, v, ok := strings.Cut(strings.TrimSpace(line), "="); ok {
			kv[k] = v
		}
	}
	return kv
}

// assertGitHubEnv checks the env a run of a Bot with GitHub gives: the connection's token as
// GH_TOKEN and none of the other gh token variables.
func assertGitHubEnv(t *testing.T, who string, env map[string]string, token string) {
	t.Helper()
	if env["GH_TOKEN"] != token {
		t.Errorf("%s GH_TOKEN %q, want the connection's token", who, env["GH_TOKEN"])
	}
	for _, name := range _tokenVars[1:] {
		if _, ok := env[name]; ok {
			t.Errorf("%s env has %s, want it absent", who, name)
		}
	}
}

// mcpServersOf returns the servers of the provider session's MCP config: Claude's --mcp-config
// argv, Codex's thread/start config (or -c arguments), by name with their url and headers.
func mcpServersOf(t *testing.T, provider string, c *call) map[string]struct {
	URL     string
	Headers map[string]string
} {
	t.Helper()
	out := map[string]struct {
		URL     string
		Headers map[string]string
	}{}
	if provider == "claude" {
		var cfg struct {
			MCPServers map[string]struct {
				Type    string            `json:"type"`
				URL     string            `json:"url"`
				Headers map[string]string `json:"headers"`
			} `json:"mcpServers"`
		}
		if err := json.Unmarshal([]byte(flagValue(c.argv, "--mcp-config")), &cfg); err != nil {
			t.Fatalf("claude --mcp-config is not inline JSON: %v", err)
		}
		getenv := func(k string) string { return c.env[k] }
		for name, s := range cfg.MCPServers {
			if s.Type != "http" {
				t.Errorf("claude MCP server %s has type %q, want http", name, s.Type)
			}
			s.URL = fake.ExpandEnv(s.URL, getenv)
			for k, v := range s.Headers {
				s.Headers[k] = fake.ExpandEnv(v, getenv)
			}
			out[name] = struct {
				URL     string
				Headers map[string]string
			}{s.URL, s.Headers}
		}
		return out
	}
	flat := map[string]string{}
	for i, a := range c.argv {
		if (a == "-c" || a == "--config") && i+1 < len(c.argv) {
			k, v, _ := strings.Cut(c.argv[i+1], "=")
			flat[strings.TrimSpace(k)] = strings.Trim(strings.TrimSpace(v), `"'`)
		}
	}
	for _, line := range c.stdin {
		var m struct {
			Method string `json:"method"`
			Params struct {
				Config map[string]any `json:"config"`
			} `json:"params"`
		}
		if json.Unmarshal([]byte(line), &m) == nil && m.Method == "thread/start" {
			flatten("", m.Params.Config, flat)
		}
	}
	for k, v := range flat {
		rest, ok := strings.CutPrefix(k, "mcp_servers.")
		name, ok2 := strings.CutSuffix(rest, ".url")
		if ok && ok2 && !strings.Contains(name, ".") {
			s := out[name]
			s.URL = strings.Trim(v, `"`)
			out[name] = s
		}
	}
	return out
}
