package e2e

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestGitLabRefresh_ExpiredTokenIsRefreshedForTheRun(t *testing.T) {
	f := newRefreshFixture(t, 1)
	first, _ := f.oauth.tokens()
	st, expires := secretTokens(t, f.id)
	if st.AccessToken != first || st.RefreshToken == "" || time.Until(expires) > time.Minute {
		t.Fatalf("keyring after sign-in: %+v, want the issued access token and a refresh token expiring in about 1s", st)
	}
	if n := len(f.oauth.refreshRequests()); n != 0 {
		t.Fatalf("%d refresh requests during sign-in", n)
	}
	waitExpired(t, expires)

	f.work(t, f.b, _fixRepoPrompt, "fix/seeded-bug")
	reqs := f.oauth.refreshRequests()
	if len(reqs) != 1 {
		t.Fatalf("refresh requests: %d, want exactly 1", len(reqs))
	}
	if reqs[0].Get("grant_type") != "refresh_token" || reqs[0].Get("refresh_token") != st.RefreshToken || reqs[0].Get("client_id") == "" {
		t.Fatalf("refresh request: %v", reqs[0])
	}
	second, secondRefresh := f.oauth.tokens()
	if second == first {
		t.Fatal("fake did not issue a new access token")
	}
	f.assertRunToken(t, second)
	if got := gitIn(t, f.repo.Path, "--git-dir", f.origin, "show", "refs/heads/fix/seeded-bug:answer.txt"); got != "42" {
		t.Fatalf("pushed answer %q", got)
	}
	assertNoSecret(t, first, f.m.data, f.root)
	assertNoSecret(t, second, f.m.data, f.root)
	assertNoSecret(t, secondRefresh, f.m.data, f.root)
	assertNoSecret(t, st.RefreshToken, f.m.data, f.root)
	f.s.kill()
	for _, secret := range []string{first, second, st.RefreshToken, secondRefresh} {
		if bytes.Contains(f.s.log.Bytes(), []byte(secret)) {
			t.Fatalf("server log leaked %s", secret)
		}
	}
}

func TestGitLabRefresh_RefreshedTokensReplaceTheOldOnes(t *testing.T) {
	f := newRefreshFixture(t, 1)
	before, expires := secretTokens(t, f.id)
	waitExpired(t, expires)

	th, _, ch, _ := f.work(t, f.b, _fixRepoPrompt, "fix/seeded-bug")
	access, refresh := f.oauth.tokens()
	after, newExpires := secretTokens(t, f.id)
	if after.AccessToken != access || after.RefreshToken != refresh || access == before.AccessToken || refresh == before.RefreshToken {
		t.Fatalf("keyring %+v, want the refreshed pair %q/%q in place of %q/%q", after, access, refresh, before.AccessToken, before.RefreshToken)
	}
	if time.Until(newExpires) < time.Hour {
		t.Fatalf("stored expiry %s, want about 2h ahead", after.ExpiresAt)
	}
	// Within the new expiry the next run only reads the keyring.
	r := f.s.send(t, th, _resumeRepoPrompt)
	if done := f.s.finished(t, ch, r.ID); done.Status != "done" {
		t.Fatalf("second run: %+v", done)
	}
	if n := len(f.oauth.refreshRequests()); n != 1 {
		t.Fatalf("refresh requests after second run: %d, want still 1", n)
	}
	f.assertRunToken(t, access)
	if again, _ := secretTokens(t, f.id); again != after {
		t.Fatalf("second run changed the keyring: %+v -> %+v", after, again)
	}
}

func TestGitLabRefresh_InvalidGrantNeedsSignIn(t *testing.T) {
	f := newRefreshFixture(t, 1)
	_, expires := secretTokens(t, f.id)
	f.oauth.set(func() { f.oauth.reject = true })
	waitExpired(t, expires)

	done := f.runFailed(t)
	if done.Status != "failed" || !strings.Contains(strings.ToLower(done.Error), "needs sign-in") {
		t.Fatalf("run: %+v, want status failed and an error saying GitLab needs sign-in", done)
	}
	if n := len(f.oauth.refreshRequests()); n != 1 {
		t.Fatalf("refresh requests: %d, want 1", n)
	}
	if got := f.status(t); got != "needs_sign_in" {
		t.Fatalf("GET /api/connections status %q, want needs_sign_in", got)
	}
	var one connectionInfo
	f.s.do(t, "GET", "/api/connections/"+f.id, nil, &one, 200)
	if one.Status != "needs_sign_in" {
		t.Fatalf("GET /api/connections/%s status %q, want needs_sign_in", f.id, one.Status)
	}
	for _, c := range f.m.sessions(t, "claude") {
		if c.env["OAUTH_TOKEN"] != "" || c.env["GLAB_CONFIG_DIR"] != "" {
			t.Fatalf("run without a usable connection still got GitLab env: %v", c.env["GLAB_CONFIG_DIR"])
		}
	}
}

func TestGitLabRefresh_SignInAgainKeepsTheConnection(t *testing.T) {
	f := newRefreshFixture(t, 1)
	_, expires := secretTokens(t, f.id)
	f.oauth.set(func() { f.oauth.reject = true })
	waitExpired(t, expires)
	if done := f.runFailed(t); done.Status != "failed" {
		t.Fatalf("run: %+v", done)
	}
	if got := f.status(t); got != "needs_sign_in" {
		t.Fatalf("status %q, want needs_sign_in", got)
	}

	f.oauth.set(func() { f.oauth.reject, f.oauth.deviceTTL = false, 7200 })
	if got := f.oauth.signIn(t, f.s, "/api/connections/gitlab", map[string]string{"connection_id": f.id}); got != f.id {
		t.Fatalf("sign in again gave connection %q, want the same id %q", got, f.id)
	}
	if got := f.status(t); got != "connected" {
		t.Fatalf("status after sign in again %q, want connected", got)
	}
	if cs := f.connections(t); len(cs) != 1 {
		t.Fatalf("connections: %+v, want only the original", cs)
	}
	var b struct {
		Connections []string `json:"connections"`
	}
	f.s.do(t, "GET", "/api/bots/"+f.b.ID, nil, &b, 200)
	if len(b.Connections) != 1 || b.Connections[0] != f.id {
		t.Fatalf("Bot connections %v, want [%s]", b.Connections, f.id)
	}
	access, _ := f.oauth.tokens()
	if st, _ := secretTokens(t, f.id); st.AccessToken != access {
		t.Fatalf("keyring %+v, want the new access token", st)
	}

	f.work(t, f.b, _fixRepoPrompt, "fix/seeded-bug")
	f.assertRunToken(t, access)
	if n := len(f.oauth.refreshRequests()); n != 1 {
		t.Fatalf("refresh requests: %d, want only the rejected one", n)
	}
}

func TestGitLabRefresh_LegacyBareTokenStillWorks(t *testing.T) {
	f := newRefreshFixture(t, 1)
	legacy := "glpat-legacy-bare-token"
	setSecretValue(t, f.id, legacy)
	time.Sleep(1200 * time.Millisecond)

	f.work(t, f.b, _fixRepoPrompt, "fix/seeded-bug")
	f.assertRunToken(t, legacy)
	if n := len(f.oauth.refreshRequests()); n != 0 {
		t.Fatalf("refresh requests for a token with no known expiry: %d, want 0", n)
	}
	if got := f.status(t); got != "connected" {
		t.Fatalf("status %q, want connected", got)
	}
}
