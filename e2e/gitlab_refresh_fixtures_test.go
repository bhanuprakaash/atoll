package e2e

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

const _refreshedTTL = 7200

// gitlabOAuth is a GitLab OAuth server that issues refreshable tokens: device-flow tokens live
// deviceTTL seconds, refreshed ones two hours. Every token it issues is unique.
type gitlabOAuth struct {
	*httptest.Server
	tag string

	mu        sync.Mutex
	allowed   bool
	deviceTTL int
	reject    bool
	issued    int
	access    string
	refresh   string
	refreshes []url.Values
}

func newGitLabOAuth(t *testing.T, deviceTTL int) *gitlabOAuth {
	t.Helper()
	o := &gitlabOAuth{tag: strings.ReplaceAll(t.Name(), "/", "-"), deviceTTL: deviceTTL}
	o.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := r.ParseForm(); err != nil || r.Method != "POST" || r.Form.Get("client_id") == "" {
			t.Errorf("OAuth needs a POST form with client_id: %v", err)
			w.WriteHeader(400)
			return
		}
		o.mu.Lock()
		defer o.mu.Unlock()
		switch r.URL.Path {
		case "/oauth/authorize_device":
			_ = json.NewEncoder(w).Encode(map[string]any{"device_code": "fixture-device", "user_code": "REEF-1234", "verification_uri": "https://gitlab.example/oauth/device", "expires_in": 600, "interval": 1})
		case "/oauth/token":
			switch r.Form.Get("grant_type") {
			case "urn:ietf:params:oauth:grant-type:device_code":
				if r.Form.Get("device_code") != "fixture-device" {
					t.Error("wrong device code")
					w.WriteHeader(400)
					return
				}
				if !o.allowed {
					w.WriteHeader(400)
					_, _ = fmt.Fprint(w, `{"error":"authorization_pending"}`)
					return
				}
				o.issue(w, o.deviceTTL)
			case "refresh_token":
				o.refreshes = append(o.refreshes, r.Form)
				if o.reject || r.Form.Get("refresh_token") != o.refresh {
					w.WriteHeader(400)
					_, _ = fmt.Fprint(w, `{"error":"invalid_grant","error_description":"The provided authorization grant is invalid, expired, revoked."}`)
					return
				}
				o.issue(w, _refreshedTTL)
			default:
				t.Errorf("unexpected grant_type %q", r.Form.Get("grant_type"))
				w.WriteHeader(400)
				_, _ = fmt.Fprint(w, `{"error":"unsupported_grant_type"}`)
			}
		default:
			t.Errorf("unexpected GitLab HTTP call: %s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(o.Close)
	return o
}

// issue answers with a new access/refresh pair; the caller holds mu.
func (o *gitlabOAuth) issue(w http.ResponseWriter, ttl int) {
	o.issued++
	o.access = fmt.Sprintf("glpat-%s-%d", o.tag, o.issued)
	o.refresh = fmt.Sprintf("glrt-%s-%d", o.tag, o.issued)
	_ = json.NewEncoder(w).Encode(map[string]any{"access_token": o.access, "refresh_token": o.refresh, "token_type": "Bearer", "scope": "api", "expires_in": ttl, "created_at": time.Now().Unix()})
}

func (o *gitlabOAuth) tokens() (access, refresh string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.access, o.refresh
}

func (o *gitlabOAuth) refreshRequests() []url.Values {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]url.Values(nil), o.refreshes...)
}

func (o *gitlabOAuth) set(f func()) {
	o.mu.Lock()
	defer o.mu.Unlock()
	f()
}

// signIn runs the device flow to the end: POST path starts it, the fake user approves, and polls
// follow the advertised one-second interval. It returns the connection id.
func (o *gitlabOAuth) signIn(t *testing.T, s *server, path string, body map[string]string) string {
	t.Helper()
	o.set(func() { o.allowed = true })
	var start struct {
		ID              string `json:"id"`
		UserCode        string `json:"user_code"`
		VerificationURI string `json:"verification_uri"`
	}
	s.do(t, "POST", path, body, &start, 201)
	if start.ID == "" || start.UserCode != "REEF-1234" {
		t.Fatalf("device start: %+v", start)
	}
	var status struct {
		Status string `json:"status"`
	}
	for range 5 {
		time.Sleep(1100 * time.Millisecond)
		s.do(t, "POST", "/api/connections/"+start.ID+"/poll", nil, &status, 200)
		if status.Status == "connected" {
			return start.ID
		}
	}
	t.Fatalf("sign in did not connect: %+v", status)
	return ""
}

type storedTokens struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresAt    string `json:"expires_at"`
}

func requireSecretStore(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "linux" && os.Getenv("DBUS_SESSION_BUS_ADDRESS") == "" {
		t.Fatal("run via make e2e: Linux needs the private Secret Service session")
	}
}

// secretValue reads the connection's keyring entry with the OS tool, not through Atoll.
func secretValue(t *testing.T, id string) string {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "secret-tool", "lookup", "service", "atoll", "username", "gitlab:"+id)
	if runtime.GOOS == "darwin" {
		cmd = exec.CommandContext(t.Context(), "security", "find-generic-password", "-s", "atoll", "-a", "gitlab:"+id, "-w")
	}
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("connection %s has no entry in the OS secret store: %v", id, err)
	}
	return strings.TrimSpace(string(out))
}

// setSecretValue replaces the connection's keyring entry, as an older Atoll would have left it.
func setSecretValue(t *testing.T, id, value string) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "secret-tool", "store", "--label=atoll-e2e", "service", "atoll", "username", "gitlab:"+id)
	cmd.Stdin = strings.NewReader(value)
	if runtime.GOOS == "darwin" {
		cmd = exec.CommandContext(t.Context(), "security", "add-generic-password", "-U", "-s", "atoll", "-a", "gitlab:"+id, "-w", value)
	}
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("write keyring: %v: %s", err, out)
	}
}

func secretTokens(t *testing.T, id string) (storedTokens, time.Time) {
	t.Helper()
	var st storedTokens
	if err := json.Unmarshal([]byte(secretValue(t, id)), &st); err != nil {
		t.Fatalf("keyring value is not the JSON token record: %v", err)
	}
	exp, err := time.Parse(time.RFC3339, st.ExpiresAt)
	if err != nil || st.AccessToken == "" || st.RefreshToken == "" {
		t.Fatalf("keyring record %+v: want access_token, refresh_token and RFC3339 expires_at", st)
	}
	return st, exp
}

// refreshFixture is a repo machine with a signed-in GitLab connection and a Bot that has it.
type refreshFixture struct {
	*repoFixture
	oauth *gitlabOAuth
	id    string
	b     bot
}

func newRefreshFixture(t *testing.T, deviceTTL int) *refreshFixture {
	t.Helper()
	requireSecretStore(t)
	f := newRepoFixture(t, "claude")
	o := newGitLabOAuth(t, deviceTTL)
	id := o.signIn(t, f.s, "/api/connections/gitlab/start", map[string]string{"instance_url": o.URL})
	t.Cleanup(func() {
		if runtime.GOOS == "darwin" {
			_ = exec.Command("security", "delete-generic-password", "-s", "atoll", "-a", "gitlab:"+id).Run()
			return
		}
		_ = exec.Command("secret-tool", "clear", "service", "atoll", "username", "gitlab:"+id).Run()
	})
	return &refreshFixture{repoFixture: f, oauth: o, id: id, b: f.bot(t, "claude", "all", []string{id})}
}

// waitExpired sleeps until the keyring's access token is past its expiry.
func waitExpired(t *testing.T, expires time.Time) {
	t.Helper()
	time.Sleep(time.Until(expires) + 200*time.Millisecond)
}

type connectionInfo struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

func (f *refreshFixture) connections(t *testing.T) []connectionInfo {
	t.Helper()
	var cs []connectionInfo
	f.s.do(t, "GET", "/api/connections", nil, &cs, 200)
	return cs
}

func (f *refreshFixture) status(t *testing.T) string {
	t.Helper()
	for _, c := range f.connections(t) {
		if c.ID == f.id {
			return c.Status
		}
	}
	t.Fatalf("connection %s not listed: %+v", f.id, f.connections(t))
	return ""
}

// runFailed sends the read-only question, which never needs a worktree, and returns its failed run.
func (f *refreshFixture) runFailed(t *testing.T) runInfo {
	t.Helper()
	th := f.thread(t, f.b)
	ch := f.s.events(t, th)
	r := f.s.send(t, th, _questionRepoPrompt)
	f.s.finished(t, ch, r.ID)
	return f.s.run(t, r.ID)
}

// assertGlabExpiry checks the glab config names an expiry, which glab refuses to start without
// for an OAuth host.
func assertGlabExpiry(t *testing.T, who string, env map[string]string) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(env["GLAB_CONFIG_DIR"], "config.yml"))
	if err != nil {
		t.Fatalf("%s glab config: %v", who, err)
	}
	if !regexp.MustCompile(`(?m)^\s*oauth2_expiry_date:\s*["']?[^"'\s]`).Match(b) {
		t.Fatalf("%s glab config has no oauth2_expiry_date value:\n%s", who, b)
	}
}

// assertRunToken checks the last provider run and every glab mr create got token, with a config glab accepts.
func (f *refreshFixture) assertRunToken(t *testing.T, token string) {
	t.Helper()
	sessions := f.m.sessions(t, "claude")
	last := sessions[len(sessions)-1]
	assertGitLabEnv(t, "provider", last.env, token)
	assertGlabExpiry(t, "provider", last.env)
	create := false
	for _, c := range f.m.calls(t, "glab") {
		if len(c.argv) >= 2 && c.argv[0] == "mr" && c.argv[1] == "create" {
			create = true
			assertGitLabEnv(t, "glab create", c.env, token)
			assertGlabExpiry(t, "glab create", c.env)
		}
	}
	if !create {
		t.Fatal("glab mr create never ran")
	}
}
