package e2e

import (
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"
)

// Contract: slack-mcp-contract.md.
//
// These tests stay serial: they share the fixed secret store entries mcp:slack:*.

func TestSlackMCP_SetupSavesTheClientIDInTheOSStore(t *testing.T) {
	f := newSlackFixture(t, "claude")
	if got := f.server(t, "slack"); got.Kind != "catalog" || got.Name != "Slack" || got.URL != f.sl.url() || got.Status != "needs_setup" {
		t.Fatalf("Slack before Set up: %+v, want a catalog entry at %s with status needs_setup", got, f.sl.url())
	}

	info := f.setupInfo(t)
	if want := f.s.url + "/oauth/callback"; info.RedirectURI != want || f.setupInfo(t).RedirectURI != want {
		t.Fatalf("setup redirect_uri %q, want the one fixed %s", info.RedirectURI, want)
	}
	if !slices.Equal(info.Scopes, _slackScopes) {
		t.Fatalf("setup scopes %v, want the scopes_supported of Slack's resource metadata %v", info.Scopes, _slackScopes)
	}

	var refused struct {
		Error string `json:"error"`
	}
	f.s.do(t, "POST", "/api/mcp-servers/slack/connect", nil, &refused, http.StatusConflict)
	if !strings.Contains(strings.ToLower(refused.Error), "client id") {
		t.Errorf("connect before Set up: error %q, want it to ask for the client ID", refused.Error)
	}
	f.s.do(t, "POST", "/api/mcp-servers/slack/setup", map[string]string{"client_id": ""}, nil, http.StatusBadRequest)
	f.s.do(t, "POST", "/api/mcp-servers/slack/setup", map[string]string{}, nil, http.StatusBadRequest)
	if got := f.server(t, "slack").Status; got != "needs_setup" {
		t.Fatalf("status %q after refused Set up requests, want needs_setup", got)
	}
	for _, id := range []string{"notion", "context7", "mcp_nowhere"} {
		f.s.do(t, "GET", "/api/mcp-servers/"+id+"/setup", nil, nil, http.StatusNotFound)
		f.s.do(t, "POST", "/api/mcp-servers/"+id+"/setup", map[string]string{"client_id": "x"}, nil, http.StatusNotFound)
	}

	f.setup(t)
	if got := f.server(t, "slack"); got.Status != "not_connected" || len(got.HeaderNames) != 0 {
		t.Fatalf("Slack after Set up: %+v, want not_connected", got)
	}
	if v, ok := secretEntry("mcp:slack:client_id"); !ok || v != _slackClientID {
		t.Fatalf("OS secret store mcp:slack:client_id = %q (found %v), want the bare client ID", v, ok)
	}
	if bad, called, registers := f.sl.seen(); bad+called+registers != 0 {
		t.Errorf("Slack saw %d wrong, %d right and %d registration requests during Set up, want none", bad, called, registers)
	}
	f.assertNoLeak(t, _slackClientID)
}

func TestSlackMCP_ConnectStoresTokensInTheOSStore(t *testing.T) {
	f := newSlackFixture(t, "claude")
	f.setup(t)
	redirect := f.setupInfo(t).RedirectURI

	authorizeURL := f.connect(t, "slack")
	u, err := url.Parse(authorizeURL)
	if err != nil || !strings.HasPrefix(authorizeURL, f.sl.srv.URL+"/oauth/v2_user/authorize?") {
		t.Fatalf("authorize_url %q (%v), want the authorization_endpoint of Slack's metadata with a query", authorizeURL, err)
	}
	q := u.Query()
	for k, want := range map[string]string{"response_type": "code", "client_id": _slackClientID, "redirect_uri": redirect, "code_challenge_method": "S256"} {
		if q.Get(k) != want {
			t.Errorf("authorize_url %s=%q, want %q", k, q.Get(k), want)
		}
	}
	if scopes := strings.Fields(q.Get("scope")); !slices.Equal(scopes, _slackScopes) {
		t.Errorf("authorize_url scope %q, want the user scopes %q separated by spaces", q.Get("scope"), strings.Join(_slackScopes, " "))
	}
	if q.Get("code_challenge") == "" || q.Get("state") == "" {
		t.Errorf("authorize_url %q lacks code_challenge or state", authorizeURL)
	}
	if got := f.server(t, "slack").Status; got != "not_connected" || len(f.sl.exchangeRequests()) != 0 {
		t.Fatalf("status %q and %d code exchanges before the user clicked Allow, want not_connected and 0", got, len(f.sl.exchangeRequests()))
	}

	cb := f.allow(t, authorizeURL)
	if cb.status != http.StatusOK || !strings.HasPrefix(cb.contentType, "text/html") {
		t.Fatalf("callback answered %d %q: %s", cb.status, cb.contentType, cb.body)
	}
	f.waitStatus(t, "slack", "connected")

	exchanges := f.sl.exchangeRequests()
	if len(exchanges) != 1 {
		t.Fatalf("%d code exchanges, want 1", len(exchanges))
	}
	for k, want := range map[string]string{"grant_type": "authorization_code", "client_id": _slackClientID, "redirect_uri": redirect} {
		if exchanges[0].Get(k) != want {
			t.Errorf("code exchange %s=%q, want %q", k, exchanges[0].Get(k), want)
		}
	}
	if exchanges[0].Get("code") == "" || exchanges[0].Get("code_verifier") == "" {
		t.Errorf("code exchange %v lacks code or code_verifier", exchanges[0])
	}
	if authorizes := f.sl.authorizeRequests(); len(authorizes) != 1 || authorizes[0].Get("redirect_uri") != redirect {
		t.Errorf("authorize requests %v, want one with redirect_uri %s", authorizes, redirect)
	}
	if _, _, registers := f.sl.seen(); registers != 0 {
		t.Errorf("Reef sent %d registration requests, want none: Slack has no dynamic registration", registers)
	}

	access, refresh := f.sl.tokens()
	rec, exp := oauthSecret(t, "slack")
	if rec.ClientID != _slackClientID || rec.AccessToken != access || rec.RefreshToken != refresh {
		t.Fatalf("OS secret store record %+v, want client %s, access %s, refresh %s", rec, _slackClientID, access, refresh)
	}
	assertExpiresIn(t, exp, 43200)
	f.assertNoLeak(t, access, refresh, _slackClientID)
}

func TestSlackMCP_RunGetsBearerToken(t *testing.T) {
	for _, p := range _providers {
		t.Run(p, func(t *testing.T) {
			f := newSlackFixture(t, p)
			f.setup(t)
			f.signIn(t, "slack")
			picked := f.bot(t, "Picker", "slack")

			th := f.s.newThread(t, picked)
			ch := f.s.events(t, th)
			done, approvals := f.use(t, th, ch, true, mcpCall("Slack", "answer", `{}`))
			if want := "Slack/answer: " + _slackAnswer; done.Status != "done" || done.Reply != want || len(approvals) != 1 {
				t.Fatalf("run: status %q, reply %q, approvals %d, want done, %q and 1 approval (error %q)", done.Status, done.Reply, len(approvals), want, done.Error)
			}
			if bad, called, _ := f.sl.seen(); called != 1 || bad != 0 {
				t.Errorf("Slack saw %d calls with its token and %d with another, want 1 and 0", called, bad)
			}
			if n := len(f.sl.refreshRequests()); n != 0 {
				t.Errorf("%d refresh requests for a token with 12 hours left, want 0", n)
			}
			access, refresh := f.sl.tokens()
			f.assertNoLeak(t, access, refresh, _slackClientID)
		})
	}
}

func TestSlackMCP_RefreshUsesTheSingleUseTokenAndSavesTheNewOne(t *testing.T) {
	f := newSlackFixture(t, "claude")
	f.sl.set(func() { f.sl.ttl, f.sl.refreshTTL = 300, 300 })
	f.setup(t)
	f.signIn(t, "slack")
	first, exp := oauthSecret(t, "slack")
	assertExpiresIn(t, exp, 300)
	picked := f.bot(t, "Picker", "slack")
	th := f.s.newThread(t, picked)
	ch := f.s.events(t, th)

	// The token expires within ten minutes: the run refreshes it first.
	done, _ := f.use(t, th, ch, true, mcpCall("Slack", "answer", `{}`))
	if want := "Slack/answer: " + _slackAnswer; done.Status != "done" || done.Reply != want {
		t.Fatalf("run with a token expiring in 5 minutes: status %q, reply %q, want done and %q (error %q)", done.Status, done.Reply, want, done.Error)
	}
	reqs := f.sl.refreshRequests()
	if len(reqs) != 1 || reqs[0].Get("grant_type") != "refresh_token" || reqs[0].Get("refresh_token") != first.RefreshToken || reqs[0].Get("client_id") != _slackClientID {
		t.Fatalf("refresh requests %v, want exactly one with the stored refresh token and client_id %s", reqs, _slackClientID)
	}
	second, _ := oauthSecret(t, "slack")
	access, refresh := f.sl.tokens()
	if second.AccessToken != access || second.RefreshToken != refresh || refresh == first.RefreshToken || access == first.AccessToken || second.ClientID != _slackClientID {
		t.Fatalf("OS secret store record %+v, want the refreshed pair %s/%s in place of %s/%s", second, access, refresh, first.AccessToken, first.RefreshToken)
	}

	// The refreshed token expires within ten minutes again. Slack accepts only the newest refresh
	// token, so this run succeeds only if Reef saved the new one.
	f.sl.set(func() { f.sl.refreshTTL = 43200 })
	done, _ = f.use(t, th, ch, true, mcpCall("Slack", "answer", `{}`))
	if done.Status != "done" {
		t.Fatalf("second run: status %q, want done (error %q)", done.Status, done.Error)
	}
	reqs = f.sl.refreshRequests()
	if len(reqs) != 2 || reqs[1].Get("refresh_token") != second.RefreshToken {
		t.Fatalf("refresh requests %v, want a second one carrying the refresh token saved by the first", reqs)
	}
	third, exp := oauthSecret(t, "slack")
	access, refresh = f.sl.tokens()
	if third.AccessToken != access || third.RefreshToken != refresh || refresh == second.RefreshToken {
		t.Fatalf("OS secret store record %+v, want the second refreshed pair %s/%s", third, access, refresh)
	}
	assertExpiresIn(t, exp, 43200)
	if bad, called, _ := f.sl.seen(); called != 2 || bad != 0 {
		t.Errorf("Slack saw %d calls with the new token and %d with another, want 2 and 0", called, bad)
	}

	done, _ = f.use(t, th, ch, true, mcpCall("Slack", "answer", `{}`))
	if done.Status != "done" || len(f.sl.refreshRequests()) != 2 {
		t.Fatalf("third run: status %q with %d refresh requests in total, want done and still 2", done.Status, len(f.sl.refreshRequests()))
	}
	f.assertNoLeak(t, first.AccessToken, first.RefreshToken, second.AccessToken, second.RefreshToken, access, refresh, _slackClientID)
}

func TestSlackMCP_RejectedRefreshNeedsSignIn(t *testing.T) {
	f := newSlackFixture(t, "claude")
	f.sl.set(func() { f.sl.ttl = 300 })
	f.setup(t)
	f.signIn(t, "slack")
	picked := f.bot(t, "Picker", "slack")
	before, refresh := f.sl.tokens()
	f.sl.set(func() { f.sl.reject = true })

	th := f.s.newThread(t, picked)
	ch := f.s.events(t, th)
	r := f.s.send(t, th, _mcpPromptHead+mustJSON([]any{mcpCall("Slack", "answer", `{}`)}))
	f.s.finished(t, ch, r.ID)
	done := f.s.run(t, r.ID)
	msg := strings.ToLower(done.Error)
	if done.Status != "failed" || !strings.Contains(msg, "slack needs sign-in") || strings.Contains(msg, "invalid_refresh_token") || strings.Contains(done.Error, refresh) {
		t.Fatalf("run: status %q, error %q, want failed with a plain message \"Slack needs sign-in\"", done.Status, done.Error)
	}
	if n := sessionCount(t, f.m, "claude"); n != 0 {
		t.Fatalf("the provider ran %d times, want 0: the run stops before it", n)
	}
	if _, called, _ := f.sl.seen(); called != 0 || len(f.sl.refreshRequests()) != 1 {
		t.Fatalf("Slack saw %d calls and %d refresh requests, want 0 and 1", called, len(f.sl.refreshRequests()))
	}
	f.waitStatus(t, "slack", "needs_sign_in")

	// Sign in again needs no new Client ID and keeps every Bot's pick.
	f.sl.set(func() { f.sl.reject, f.sl.ttl = false, 43200 })
	f.signIn(t, "slack")
	access, _ := f.sl.tokens()
	if rec, _ := oauthSecret(t, "slack"); rec.AccessToken != access || access == before || rec.ClientID != _slackClientID {
		t.Fatalf("OS secret store record %+v, want the new access token %q (old %q) and the same client", rec, access, before)
	}
	var got mcpBot
	f.s.do(t, "GET", "/api/bots/"+picked.ID, nil, &got, http.StatusOK)
	if !slices.Equal(got.MCPServers, []string{"slack"}) {
		t.Fatalf("Bot picks %v after sign in again, want [slack]", got.MCPServers)
	}
	done, _ = f.use(t, th, ch, true, mcpCall("Slack", "answer", `{}`))
	if want := "Slack/answer: " + _slackAnswer; done.Status != "done" || done.Reply != want {
		t.Fatalf("run after sign in again: status %q, reply %q, want done and %q (error %q)", done.Status, done.Reply, want, done.Error)
	}
	if n := len(f.sl.refreshRequests()); n != 1 {
		t.Errorf("%d refresh requests in total, want only the rejected one", n)
	}
}
