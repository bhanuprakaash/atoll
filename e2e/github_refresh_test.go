package e2e

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"testing"
	"time"
)

// Contract: github-refresh-contract.md.

type githubRecord struct {
	AccessToken      string    `json:"access_token"`
	RefreshToken     string    `json:"refresh_token"`
	ExpiresAt        time.Time `json:"expires_at"`
	RefreshExpiresAt time.Time `json:"refresh_expires_at"`
}

// githubRecordOf is the JSON record in the connection's keyring entry.
func githubRecordOf(t *testing.T, id string) githubRecord {
	t.Helper()
	var rec githubRecord
	if err := json.Unmarshal([]byte(githubSecret(t, id)), &rec); err != nil || rec.AccessToken == "" || rec.RefreshToken == "" || rec.ExpiresAt.IsZero() || rec.RefreshExpiresAt.IsZero() {
		t.Fatalf("keyring entry github:%s is %q, want JSON with access_token, refresh_token, expires_at and refresh_expires_at in RFC 3339 (err %v)", id, githubSecret(t, id), err)
	}
	return rec
}

// waitExpired sleeps until the access token the fake GitHub issued last has expired and the fake
// refuses it.
func (g *fakeGitHub) waitExpired() {
	g.mu.Lock()
	at := g.expiresAt
	g.mu.Unlock()
	time.Sleep(time.Until(at) + _githubExpiryGrace + 200*time.Millisecond)
}

func (g *fakeGitHub) currentRefreshToken() string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.refreshToken
}

// connectWithTTL signs in to a fake GitHub that issues tokens expiring after ttl seconds.
func (f *githubFixture) connectWithTTL(t *testing.T, ttl int) string {
	t.Helper()
	f.gh.set(func() { f.gh.tokenTTL = ttl })
	return f.connect(t)
}

// newPRWakeFixtureTTL is newPRWakeFixture on a fake GitHub that issues tokens expiring after ttl
// seconds, and refreshed ones after refreshTTLs, then 8 hours.
func newPRWakeFixtureTTL(t *testing.T, ttl int, refreshTTLs ...int) (*prWakeFixture, string) {
	t.Helper()
	f := &prWakeFixture{githubRepoFixture: newGitHubRepoFixture(t, "claude"), provider: "claude"}
	f.gh.set(func() { f.gh.refreshTTLs = refreshTTLs })
	f.repo = f.register(t, "octo-hello", f.hello(t))
	id := f.connectWithTTL(t, ttl)
	f.boss = f.bot(t, "claude", []string{id})
	return f, id
}

// serverLogLines are the lines of the server log file with the msg.
func serverLogLines(t *testing.T, m *machine, msg string) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, line := range jsonLines(serverLogText(t, m)) {
		var v map[string]any
		_ = json.Unmarshal([]byte(line), &v)
		if v["msg"] == msg {
			out = append(out, v)
		}
	}
	return out
}

// wantRefreshedLines checks the server log has n `GitHub token refreshed` INFO lines naming the
// connection.
func wantRefreshedLines(t *testing.T, m *machine, id string, n int) {
	t.Helper()
	lines := serverLogLines(t, m, "GitHub token refreshed")
	if len(lines) != n {
		t.Fatalf("server log has %d `GitHub token refreshed` lines, want %d: %v", len(lines), n, lines)
	}
	for _, l := range lines {
		if l["level"] != "INFO" || l["connection_id"] != id {
			t.Errorf("refresh line %v, want level INFO and connection_id %s", l, id)
		}
	}
}

// wantNoTokensInLog checks no access or refresh token the fake GitHub issued is in the log file.
func wantNoTokensInLog(t *testing.T, m *machine, g *fakeGitHub) {
	t.Helper()
	text := serverLogText(t, m)
	g.mu.Lock()
	issued, tag := g.issued, g.tag
	g.mu.Unlock()
	for i := 1; i <= issued; i++ {
		for _, tok := range []string{fmt.Sprintf("fake-gho-%s-%d", tag, i), fmt.Sprintf("fake-ghr-%s-%d", tag, i)} {
			if strings.Contains(text, tok) {
				t.Errorf("server log holds the token %q", tok)
			}
		}
	}
}

// wantRefreshRequest checks a refresh request as GitHub documents it for an app with no secret.
func wantRefreshRequest(t *testing.T, form url.Values, refreshToken string) {
	t.Helper()
	if form.Get("grant_type") != "refresh_token" || form.Get("client_id") != _githubClientID || form.Get("refresh_token") != refreshToken || form.Has("client_secret") {
		t.Errorf("refresh request %v, want grant_type refresh_token, client_id %s, refresh_token %s and no client_secret", form, _githubClientID, refreshToken)
	}
}

func (f *githubFixture) wantConnected(t *testing.T, id string) {
	t.Helper()
	if c := connectionJSON(t, f.s, id); c["status"] != "connected" || c["signed_out_at"] != nil {
		t.Errorf("connection %v, want connected with no signed_out_at", c)
	}
}

// Sign-in keeps what the device flow gave: both tokens and when each runs out.
func TestGitHubRefresh_SignInStoresTheTokenRecord(t *testing.T) {
	t.Parallel()
	f := newGitHubFixture(t, "claude")
	id := f.connect(t)

	rec := githubRecordOf(t, id)
	if rec.AccessToken != f.gh.currentToken() || rec.RefreshToken != f.gh.currentRefreshToken() {
		t.Errorf("keyring record %+v, want the access and refresh token the fake issued", rec)
	}
	if d := time.Until(rec.ExpiresAt) - 8*time.Hour; d < -2*time.Minute || d > time.Minute {
		t.Errorf("expires_at %s, want 8 h (expires_in 28800) after sign-in", rec.ExpiresAt)
	}
	if d := time.Until(rec.RefreshExpiresAt) - 15638400*time.Second; d < -2*time.Minute || d > time.Minute {
		t.Errorf("refresh_expires_at %s, want 15638400 s after sign-in", rec.RefreshExpiresAt)
	}
	if n := len(f.gh.refreshes()); n != 0 {
		t.Errorf("%d refresh requests during sign-in, want 0", n)
	}
}

// A run starts by refreshing a token with less than 3 h left, expired or not, and gets the new
// token everywhere GitHub reaches it: GH_TOKEN, the git credential helper and the github MCP
// header. Each refresh logs one INFO line and never a token.
func TestGitHubRefresh_RunRefreshesFirst(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name    string
		ttl     int
		expired bool
	}{{"expired", 1, true}, {"under_3h_left", 3600, false}} {
		for _, p := range _providers {
			t.Run(c.name+"/"+p, func(t *testing.T) {
				t.Parallel()
				f := newGitHubFixture(t, p)
				id := f.connectWithTTL(t, c.ttl)
				old, oldRefresh := f.gh.currentToken(), f.gh.currentRefreshToken()
				if c.expired {
					f.gh.waitExpired()
				}
				b := f.bot(t, p, []string{id})

				done, probe := f.probe(t, b)
				reqs := f.gh.refreshes()
				if len(reqs) != 1 {
					t.Fatalf("fake GitHub got %d refresh requests for a run with %d s of token life at sign-in, want exactly 1 (run: %+v)", len(reqs), c.ttl, done)
				}
				wantRefreshRequest(t, reqs[0].Form, oldRefresh)
				if reqs[0].Expired != c.expired || !reqs[0].OK {
					t.Errorf("refresh request %+v, want ok, with the token expired: %v", reqs[0], c.expired)
				}
				token := f.gh.currentToken()
				if token == old {
					t.Fatal("fake GitHub issued no new token")
				}
				if done.Status != "done" {
					t.Fatalf("run: %+v", done)
				}
				if probe.GH.Exit != 0 || !strings.Contains(probe.GH.Output, "reef-fixture") {
					t.Errorf("gh api user in the run: exit %d, output %q; want the fake user", probe.GH.Exit, probe.GH.Output)
				}
				if cred := githubCredential(probe.Git.Output); cred["password_sha256"] != digest(token) {
					t.Errorf("git credential fill for github: %q, want the new token's digest %s", probe.Git.Output, digest(token))
				}
				if probe.Push.Exit != 0 {
					t.Errorf("git push to the fake github.com remote: exit %d: %s", probe.Push.Exit, probe.Push.Output)
				}
				if !probe.MCP.Connected {
					t.Errorf("get_me on the github MCP server: %+v", probe.MCP)
				}
				for _, r := range f.gh.mcpRequests() {
					if r.Auth != "Bearer "+token {
						t.Errorf("MCP request %+v, want Authorization Bearer <new token>", r)
					}
				}
				if got := githubRecordOf(t, id); got.AccessToken != token || got.RefreshToken != f.gh.currentRefreshToken() || got.RefreshToken == oldRefresh {
					t.Errorf("keyring record %+v, want the refreshed pair in place of the old one", got)
				}

				f.s.kill()
				f.m.killFakes()
				sessions := f.m.sessions(t, p)
				if len(sessions) != 1 {
					t.Fatalf("%s ran %d times, want 1", p, len(sessions))
				}
				assertGitHubEnv(t, p, sessions[0].env, token)
				if gh, ok := mcpServersOf(t, p, sessions[0])["github"]; !ok || p == "claude" && gh.Headers["Authorization"] != "Bearer "+token {
					t.Errorf("MCP config github entry %+v, want Authorization Bearer <new token>", gh)
				}
				wantRefreshedLines(t, f.m, id, 1)
				wantNoTokensInLog(t, f.m, f.gh)
				for _, secret := range []string{old, oldRefresh, token, f.gh.currentRefreshToken()} {
					assertNoSecret(t, secret, f.m.data)
				}
			})
		}
	}
}

// A token with 8 h left is used as it is: no refresh request, and the keyring is not rewritten.
func TestGitHubRefresh_PlentyOfTimeDoesNotRefresh(t *testing.T) {
	t.Parallel()
	f := newGitHubFixture(t, "claude")
	id := f.connect(t)
	rec := githubRecordOf(t, id)
	before := githubSecret(t, id)
	if time.Until(rec.ExpiresAt) < 3*time.Hour {
		t.Fatalf("expires_at %s after sign-in, want about 8 h ahead", rec.ExpiresAt)
	}
	b := f.bot(t, "claude", []string{id})

	done, probe := f.probe(t, b)
	if done.Status != "done" || probe.GH.Exit != 0 {
		t.Fatalf("run: %+v, gh api exit %d", done, probe.GH.Exit)
	}
	if n := len(f.gh.refreshes()); n != 0 {
		t.Errorf("fake GitHub got %d refresh requests for a token with 8 h left, want 0", n)
	}
	if after := githubSecret(t, id); after != before {
		t.Errorf("keyring entry changed from %q to %q, want it untouched", before, after)
	}
	sessions := f.m.sessions(t, "claude")
	if len(sessions) != 1 {
		t.Fatalf("claude ran %d times, want 1", len(sessions))
	}
	assertGitHubEnv(t, "claude", sessions[0].env, rec.AccessToken)
	f.s.kill()
	wantRefreshedLines(t, f.m, id, 0)
}

// The PR watcher leaves a token alone while it is still valid, refreshes it once it has expired,
// and keeps waking the Bot with the new one. The run that opens the pull request refreshes first
// (3600 s is under 3 h) and gets a token that lives 6 s; no run is going when that one runs out.
func TestGitHubRefresh_PRWatcherRefreshesAfterExpiry(t *testing.T) {
	f, id := newPRWakeFixtureTTL(t, 3600, 6)
	th := f.openPR(t, 1)
	if reqs := f.gh.refreshes(); len(reqs) != 1 || reqs[0].Expired || !reqs[0].OK {
		t.Fatalf("refresh requests after the run that opened the pull request: %+v, want one ok, before expiry", reqs)
	}

	eventually(t, 30*time.Second, "the watcher refreshes the expired token", func() bool { return len(f.gh.refreshes()) >= 2 })
	reqs := f.gh.refreshes()
	if len(reqs) != 2 || !reqs[1].Expired || !reqs[1].OK {
		t.Fatalf("refresh requests %+v, want a second one, ok, made only once the token had expired", reqs)
	}
	f.wantConnected(t, id)
	if got := githubSecretToken(t, id); got != f.gh.currentToken() {
		t.Errorf("keyring holds %q, want the token of the second refresh", got)
	}

	f.gh.setPull(1, "closed", true)
	w := f.waitWake(t, th, 1)
	f.wantWake(t, th, w, "merged", "PR #1 merged")
	if n := len(f.gh.refreshes()); n != 2 {
		t.Errorf("fake GitHub got %d refresh requests, want 2: the wake's run has a token with 8 h left", n)
	}
	f.wantConnected(t, id)
	f.s.kill()
	wantRefreshedLines(t, f.m, id, 2)
	wantNoTokensInLog(t, f.m, f.gh)
	if n := len(serverLogLines(t, f.m, "GitHub sign-in lost")); n != 0 {
		t.Errorf("server log has %d `GitHub sign-in lost` lines, want 0", n)
	}
}

// The issue watcher too: it reads with a valid token however little time is left on it, and
// refreshes only after expiry, with no run anywhere. A token the watcher failed to renew would 401
// and the closed issue would never retire its item.
func TestGitHubRefresh_IssueWatcher(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name      string
		ttl       int
		refreshes int
	}{{"under_3h_left_is_not_expired", 3600, 0}, {"expired", 3, 1}} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			f, id := newPRWakeFixtureTTL(t, c.ttl)
			item := f.remember(t, "design A", "#7")
			if c.refreshes == 0 {
				eventually(t, 30*time.Second, "the issue is read again and answered 304", func() bool {
					_, notModified := f.gh.issueReads(7)
					return notModified >= 3
				})
			} else {
				eventually(t, 30*time.Second, "the watcher refreshes the expired token", func() bool { return len(f.gh.refreshes()) >= 1 })
			}

			f.gh.setIssue(7, "closed", "completed")
			eventually(t, 30*time.Second, "the item of the closed issue retires", func() bool { return f.item(t, item.ID).Status == "retired" })
			reqs := f.gh.refreshes()
			if len(reqs) != c.refreshes {
				t.Fatalf("fake GitHub got %d refresh requests, want %d: %+v", len(reqs), c.refreshes, reqs)
			}
			if c.refreshes == 1 && (!reqs[0].Expired || !reqs[0].OK) {
				t.Errorf("refresh request %+v, want ok, made once the token had expired", reqs[0])
			}
			f.wantConnected(t, id)
		})
	}
}

// Two runs starting together on an expired token wait for one refresh and share its result.
// Refresh tokens are single-use, so a second request would sign the connection out.
func TestGitHubRefresh_ConcurrentRunsRefreshOnce(t *testing.T) {
	t.Parallel()
	f := newGitHubFixture(t, "claude")
	id := f.connectWithTTL(t, 1)
	f.gh.set(func() { f.gh.refreshDelay = 1500 * time.Millisecond })
	f.gh.waitExpired()
	b := f.bot(t, "claude", []string{id})

	th1, th2 := f.s.newThread(t, b), f.s.newThread(t, b)
	ch1, ch2 := f.s.events(t, th1), f.s.events(t, th2)
	r1, r2 := f.s.send(t, th1, _githubPrompt), f.s.send(t, th2, _githubPrompt)
	d1, d2 := f.s.finished(t, ch1, r1.ID), f.s.finished(t, ch2, r2.ID)

	if reqs := f.gh.refreshes(); len(reqs) != 1 || !reqs[0].OK {
		t.Fatalf("fake GitHub got refresh requests %+v from two runs, want exactly one, ok", reqs)
	}
	if d1.Status != "done" || d2.Status != "done" {
		t.Fatalf("runs: %+v and %+v, want both done", d1, d2)
	}
	token := f.gh.currentToken()
	f.s.kill()
	f.m.killFakes()
	sessions := f.m.sessions(t, "claude")
	if len(sessions) != 2 {
		t.Fatalf("claude ran %d times, want 2", len(sessions))
	}
	for _, s := range sessions {
		assertGitHubEnv(t, "claude", s.env, token)
	}
	wantRefreshedLines(t, f.m, id, 1)
}

// GitHub rejects the refresh token: the run stops before the provider, the connection is signed
// out and one WARN line says why.
func TestGitHubRefresh_RejectedRefreshTokenNeedsSignIn(t *testing.T) {
	t.Parallel()
	f := newGitHubFixture(t, "claude")
	id := f.connectWithTTL(t, 1)
	oldRefresh := f.gh.currentRefreshToken()
	f.gh.set(func() { f.gh.badRefresh = true })
	f.gh.waitExpired()
	b := f.bot(t, "claude", []string{id})

	from := time.Now()
	done, _ := f.probe(t, b)
	to := time.Now()
	reqs := f.gh.refreshes()
	if len(reqs) != 1 {
		t.Fatalf("fake GitHub got %d refresh requests, want 1 (run: %+v)", len(reqs), done)
	}
	wantRefreshRequest(t, reqs[0].Form, oldRefresh)
	if done.Status != "failed" || !strings.Contains(done.Error, "GitHub needs sign-in") {
		t.Fatalf("run: %+v, want status failed and the error \"GitHub needs sign-in\"", done)
	}
	if n := len(f.m.sessions(t, "claude")); n != 0 {
		t.Fatalf("provider sessions: %d, want 0: the run must stop before the provider starts", n)
	}
	var one connectionInfo
	f.s.do(t, "GET", "/api/connections/"+id, nil, &one, 200)
	if one.Status != "needs_sign_in" {
		t.Fatalf("GET /api/connections/%s: %+v, want needs_sign_in", id, one)
	}
	wantSignedOutAt(t, f.s, id, from, to)

	lines := serverLogLines(t, f.m, "GitHub sign-in lost")
	if len(lines) != 1 {
		t.Fatalf("server log has %d `GitHub sign-in lost` lines, want 1: %v", len(lines), lines)
	}
	l := lines[0]
	if l["level"] != "WARN" || l["connection_id"] != id || l["bot_id"] != b.ID {
		t.Errorf("sign-in lost line %v, want level WARN, connection_id %s, bot_id %s", l, id, b.ID)
	}
	named := false
	for k, v := range l {
		if s, ok := v.(string); ok && k != "msg" && strings.Contains(strings.NewReplacer("_", " ", "-", " ").Replace(strings.ToLower(s)), "refresh token") {
			named = true
		}
	}
	if !named {
		t.Errorf("sign-in lost line %v, want an attribute naming the refresh token as the rejected one", l)
	}
	wantRefreshedLines(t, f.m, id, 0)
	wantNoTokensInLog(t, f.m, f.gh)
}

// A refresh that never gets an answer leaves the connection, the keyring and the token as they
// were; the next run tries again and succeeds. The first run's own outcome is not specified.
func TestGitHubRefresh_NetworkErrorKeepsTheConnection(t *testing.T) {
	t.Parallel()
	f := newGitHubFixture(t, "claude")
	id := f.connectWithTTL(t, 3600)
	first := f.gh.currentToken()
	before := githubSecret(t, id)
	f.gh.set(func() { f.gh.dropRefresh = true })
	b := f.bot(t, "claude", []string{id})

	f.probe(t, b)
	if n := len(f.gh.refreshes()); n < 1 {
		t.Fatalf("fake GitHub got %d refresh requests from a run with under 3 h of token life, want at least 1", n)
	}
	f.wantConnected(t, id)
	if after := githubSecret(t, id); after != before {
		t.Errorf("keyring entry changed from %q to %q after a refresh with no answer, want it untouched", before, after)
	}
	if f.gh.currentToken() != first {
		t.Fatal("fake GitHub rotated the token though it answered no refresh")
	}
	if n := len(serverLogLines(t, f.m, "GitHub sign-in lost")); n != 0 {
		t.Errorf("server log has %d `GitHub sign-in lost` lines after a network error, want 0", n)
	}

	f.gh.set(func() { f.gh.dropRefresh = false })
	done, _ := f.probe(t, b)
	if done.Status != "done" {
		t.Fatalf("run after the network came back: %+v", done)
	}
	if n := f.gh.refreshed(); n != 1 {
		t.Fatalf("fake GitHub refreshed %d times, want 1 by the second run", n)
	}
	token := f.gh.currentToken()
	if token == first {
		t.Fatal("fake GitHub issued no new token")
	}
	f.wantConnected(t, id)
	if got := githubRecordOf(t, id); got.AccessToken != token {
		t.Errorf("keyring record %+v, want the new token", got)
	}
	f.s.kill()
	f.m.killFakes()
	sessions := f.m.sessions(t, "claude")
	assertGitHubEnv(t, "claude", sessions[len(sessions)-1].env, token)
	wantRefreshedLines(t, f.m, id, 1)
}

// An item an older Atoll left, a bare token, is used as it is with no refresh, however short the
// token's life was at sign-in. It holds on this branch before the change; it stays a test.
func TestGitHubRefresh_BareTokenStillWorks(t *testing.T) {
	t.Parallel()
	f := newGitHubFixture(t, "claude")
	id := f.connectWithTTL(t, 1)
	legacy := "fake-gho-legacy-" + digest(t.Name())
	f.gh.set(func() { f.gh.token, f.gh.refreshToken, f.gh.expiresAt = legacy, "", time.Time{} })
	setGitHubSecret(t, id, legacy)
	time.Sleep(1200 * time.Millisecond)
	b := f.bot(t, "claude", []string{id})

	done, probe := f.probe(t, b)
	if done.Status != "done" || probe.GH.Exit != 0 || probe.MCP.Connected != true {
		t.Fatalf("run: %+v, gh api exit %d, MCP %+v", done, probe.GH.Exit, probe.MCP)
	}
	if n := len(f.gh.refreshes()); n != 0 {
		t.Errorf("fake GitHub got %d refresh requests for a bare token, want 0", n)
	}
	f.wantConnected(t, id)
	if got := githubSecret(t, id); got != legacy {
		t.Errorf("keyring entry %q, want the bare token untouched", got)
	}
	f.s.kill()
	f.m.killFakes()
	sessions := f.m.sessions(t, "claude")
	if len(sessions) != 1 {
		t.Fatalf("claude ran %d times, want 1", len(sessions))
	}
	assertGitHubEnv(t, "claude", sessions[0].env, legacy)
}
