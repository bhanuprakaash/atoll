package e2e

import (
	"net/http"
	"net/url"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// Contract: mcp-oauth-contract.md.

func TestMCPOAuth_CatalogListsHostedServers(t *testing.T) {
	f := newMCPFixture(t, "claude")
	var list []mcpServer
	f.s.do(t, "GET", "/api/mcp-servers", nil, &list, http.StatusOK)
	want := slices.Concat([]mcpServer{norm(_context7)}, normAll(_hostedCatalog))
	if len(list) < len(want) || !reflect.DeepEqual(normAll(list[:len(want)]), want) {
		t.Fatalf("GET /api/mcp-servers:\n%+v\nwant it to start with Context7, Notion, Figma and Cloudflare:\n%+v", list, want)
	}
}

func TestMCPOAuth_ConnectStoresTokensInTheOSStore(t *testing.T) {
	f := newOAuthFixture(t, "claude")
	s := f.add(t, "docs-oauth", f.o)
	if s.Kind != "remote" || s.URL != f.o.url() || s.Status != "not_connected" || len(s.HeaderNames) != 0 {
		t.Fatalf("POST of a server that answers 401 with resource metadata: %+v, want a remote server, status not_connected and no header names", s)
	}
	if probes, _, _ := f.o.seen(); probes == 0 {
		t.Fatal("Add by URL sent the server no request to find out whether it needs sign-in")
	}

	// A callback whose state Reef did not issue finishes nothing.
	callback := f.s.url + "/oauth/callback"
	if cb := f.callback(t, callback+"?code=fake-oauth-code-x&state=fake-state-x"); cb.status != http.StatusBadRequest {
		t.Fatalf("callback with an unknown state answered %d, want 400", cb.status)
	}
	if got := f.server(t, s.ID).Status; got != "not_connected" || f.o.codeExchanges() != 0 {
		t.Fatalf("after a callback with an unknown state: status %q, %d code exchanges, want not_connected and 0", got, f.o.codeExchanges())
	}

	authorizeURL := f.connect(t, s.ID)
	u, err := url.Parse(authorizeURL)
	if err != nil || !strings.HasPrefix(authorizeURL, f.o.auth.URL+"/authorize?") {
		t.Fatalf("authorize_url %q (%v), want the auth server's authorization_endpoint with a query", authorizeURL, err)
	}
	q := u.Query()
	regs := f.o.registered()
	if len(regs) != 1 || len(regs[0].RedirectURIs) != 1 || regs[0].RedirectURIs[0] != callback || regs[0].TokenEndpointAuthMethod != "none" ||
		regs[0].ClientName == "" || !slices.Contains(regs[0].GrantTypes, "authorization_code") || !slices.Contains(regs[0].GrantTypes, "refresh_token") {
		t.Fatalf("client registrations %+v, want exactly one: a client_name, redirect_uris [%s], grant types authorization_code and refresh_token, token_endpoint_auth_method none", regs, callback)
	}
	for k, want := range map[string]string{
		"response_type": "code", "client_id": regs[0].clientID, "redirect_uri": callback, "code_challenge_method": "S256", "resource": f.o.url(),
	} {
		if q.Get(k) != want {
			t.Errorf("authorize_url %s=%q, want %q", k, q.Get(k), want)
		}
	}
	if q.Get("code_challenge") == "" || q.Get("state") == "" {
		t.Errorf("authorize_url %q lacks code_challenge or state", authorizeURL)
	}
	if got := f.server(t, s.ID).Status; got != "not_connected" {
		t.Fatalf("status %q before the user clicked Allow, want not_connected", got)
	}

	cb := f.allow(t, authorizeURL)
	if cb.status != http.StatusOK || !strings.HasPrefix(cb.contentType, "text/html") {
		t.Fatalf("callback answered %d %q: %s", cb.status, cb.contentType, cb.body)
	}
	f.waitStatus(t, s.ID, "connected")
	if n := f.o.codeExchanges(); n != 1 {
		t.Fatalf("%d code exchanges, want 1", n)
	}

	access, refresh := f.o.tokens()
	rec, exp := oauthSecret(t, s.ID)
	if rec.ClientID != regs[0].clientID || rec.AccessToken != access || rec.RefreshToken != refresh {
		t.Fatalf("OS secret store record %+v, want client %s, access %s, refresh %s", rec, regs[0].clientID, access, refresh)
	}
	assertExpiresIn(t, exp, 7200)
	if got := f.server(t, s.ID); got.Status != "connected" || len(got.HeaderNames) != 0 {
		t.Fatalf("server after sign-in: %+v, want connected and no header names", got)
	}
	f.assertNoLeak(t, access, refresh, rec.ClientID)
}

func TestMCPOAuth_RunGetsBearerToken(t *testing.T) {
	for _, p := range _providers {
		t.Run(p, func(t *testing.T) {
			f := newOAuthFixture(t, p)
			s := f.add(t, "docs-oauth", f.o)
			f.signIn(t, s.ID)
			picked := f.bot(t, "Picker", s.ID)

			th := f.s.newThread(t, picked)
			ch := f.s.events(t, th)
			done, approvals := f.use(t, th, ch, true, mcpCall("docs-oauth", "answer", `{}`))
			if want := "docs-oauth/answer: " + _oauthAnswer; done.Status != "done" || done.Reply != want || len(approvals) != 1 {
				t.Fatalf("run: status %q, reply %q, approvals %d, want done, %q and 1 approval (error %q)", done.Status, done.Reply, len(approvals), want, done.Error)
			}
			if _, bad, called := f.o.seen(); called != 1 || bad != 0 {
				t.Errorf("server saw %d calls with its token and %d with another, want 1 and 0", called, bad)
			}
			if n := len(f.o.refreshRequests()); n != 0 {
				t.Errorf("%d refresh requests for a token with two hours left, want 0", n)
			}
			access, refresh := f.o.tokens()
			rec, _ := oauthSecret(t, s.ID)
			f.assertNoLeak(t, access, refresh, rec.ClientID)
		})
	}
}

func TestMCPOAuth_ExpiringTokenIsRefreshedBeforeTheRun(t *testing.T) {
	f := newOAuthFixture(t, "claude")
	f.o.set(func() { f.o.ttl = 300 })
	s := f.add(t, "docs-oauth", f.o)
	f.signIn(t, s.ID)
	before, exp := oauthSecret(t, s.ID)
	assertExpiresIn(t, exp, 300)
	picked := f.bot(t, "Picker", s.ID)

	th := f.s.newThread(t, picked)
	ch := f.s.events(t, th)
	done, _ := f.use(t, th, ch, true, mcpCall("docs-oauth", "answer", `{}`))
	if want := "docs-oauth/answer: " + _oauthAnswer; done.Status != "done" || done.Reply != want {
		t.Fatalf("run with a token expiring in 5 minutes: status %q, reply %q, want done and %q (error %q)", done.Status, done.Reply, want, done.Error)
	}
	reqs := f.o.refreshRequests()
	if len(reqs) != 1 || reqs[0].Get("grant_type") != "refresh_token" || reqs[0].Get("refresh_token") != before.RefreshToken || reqs[0].Get("client_id") != before.ClientID {
		t.Fatalf("refresh requests %v, want exactly one with the stored refresh token and client_id %s", reqs, before.ClientID)
	}
	access, refresh := f.o.tokens()
	after, exp := oauthSecret(t, s.ID)
	if after.AccessToken != access || after.RefreshToken != refresh || access == before.AccessToken || after.ClientID != before.ClientID {
		t.Fatalf("OS secret store record %+v, want the refreshed pair %s/%s in place of %s/%s and the same client", after, access, refresh, before.AccessToken, before.RefreshToken)
	}
	assertExpiresIn(t, exp, 7200)
	if _, bad, called := f.o.seen(); called != 1 || bad != 0 {
		t.Errorf("server saw %d calls with the new token and %d with another, want 1 and 0", called, bad)
	}

	// The refreshed token has two hours left: the next run only reads it.
	done, _ = f.use(t, th, ch, true, mcpCall("docs-oauth", "answer", `{}`))
	if done.Status != "done" || len(f.o.refreshRequests()) != 1 {
		t.Fatalf("second run: status %q with %d refresh requests in total, want done and still 1", done.Status, len(f.o.refreshRequests()))
	}
	f.assertNoLeak(t, before.AccessToken, before.RefreshToken, access, refresh, before.ClientID)
}

func TestMCPOAuth_RejectedRefreshNeedsSignIn(t *testing.T) {
	f := newOAuthFixture(t, "claude")
	f.o.set(func() { f.o.ttl = 300 })
	s := f.add(t, "docs-oauth", f.o)
	f.signIn(t, s.ID)
	picked := f.bot(t, "Picker", s.ID)
	before, refresh := f.o.tokens()
	f.o.set(func() { f.o.reject = true })

	th := f.s.newThread(t, picked)
	ch := f.s.events(t, th)
	r := f.s.send(t, th, _mcpPromptHead+mustJSON([]any{mcpCall("docs-oauth", "answer", `{}`)}))
	f.s.finished(t, ch, r.ID)
	done := f.s.run(t, r.ID)
	msg := strings.ToLower(done.Error)
	if done.Status != "failed" || !strings.Contains(msg, "docs-oauth needs sign-in") || strings.Contains(msg, "invalid_grant") || strings.Contains(done.Error, refresh) {
		t.Fatalf("run: status %q, error %q, want failed with a plain message \"docs-oauth needs sign-in\"", done.Status, done.Error)
	}
	if n := sessionCount(t, f.m, "claude"); n != 0 {
		t.Fatalf("the provider ran %d times, want 0: the run stops before it", n)
	}
	if _, _, called := f.o.seen(); called != 0 || len(f.o.refreshRequests()) != 1 {
		t.Fatalf("server saw %d calls and %d refresh requests, want 0 and 1", called, len(f.o.refreshRequests()))
	}
	f.waitStatus(t, s.ID, "needs_sign_in")
	var listed []mcpServer
	f.s.do(t, "GET", "/api/mcp-servers", nil, &listed, http.StatusOK)

	// Sign in again keeps the server and every Bot's pick.
	f.o.set(func() { f.o.reject, f.o.ttl = false, 7200 })
	f.signIn(t, s.ID)
	var again []mcpServer
	f.s.do(t, "GET", "/api/mcp-servers", nil, &again, http.StatusOK)
	if len(again) != len(listed) || f.server(t, s.ID).Name != "docs-oauth" {
		t.Fatalf("servers after sign in again: %+v, want the same %d with the same id %s", again, len(listed), s.ID)
	}
	var got mcpBot
	f.s.do(t, "GET", "/api/bots/"+picked.ID, nil, &got, http.StatusOK)
	if !slices.Equal(got.MCPServers, []string{s.ID}) {
		t.Fatalf("Bot picks %v after sign in again, want [%s]", got.MCPServers, s.ID)
	}
	access, _ := f.o.tokens()
	if rec, _ := oauthSecret(t, s.ID); rec.AccessToken != access || access == before {
		t.Fatalf("OS secret store access token %q, want the new one %q (old %q)", rec.AccessToken, access, before)
	}
	done, _ = f.use(t, th, ch, true, mcpCall("docs-oauth", "answer", `{}`))
	if want := "docs-oauth/answer: " + _oauthAnswer; done.Status != "done" || done.Reply != want {
		t.Fatalf("run after sign in again: status %q, reply %q, want done and %q (error %q)", done.Status, done.Reply, want, done.Error)
	}
	if n := len(f.o.refreshRequests()); n != 1 {
		t.Errorf("%d refresh requests in total, want only the rejected one", n)
	}
}

func TestMCPOAuth_AddByURL(t *testing.T) {
	f := newOAuthFixture(t, "claude")

	needs := f.add(t, "docs-needs", f.o)
	if needs.Status != "not_connected" {
		t.Errorf("server that answers 401 with resource metadata: status %q, want not_connected (Connect is offered)", needs.Status)
	}

	open := newOAuthMCP(t)
	open.set(func() { open.requireAuth = false })
	plain := f.add(t, "docs-open", open)
	if plain.Status != "" || plain.Kind != "remote" {
		t.Errorf("server that answers 200: %+v, want a remote server with no status (no sign-in)", plain)
	}
	f.s.do(t, "POST", "/api/mcp-servers/"+plain.ID+"/connect", nil, nil, http.StatusBadRequest)

	app := newOAuthMCP(t)
	app.set(func() { app.registration = false })
	noApp := f.add(t, "docs-app", app)
	if noApp.Status != "needs_app" {
		t.Errorf("server without registration_endpoint: status %q, want needs_app", noApp.Status)
	}
	var refused struct {
		Error string `json:"error"`
	}
	f.s.do(t, "POST", "/api/mcp-servers/"+noApp.ID+"/connect", nil, &refused, http.StatusConflict)
	if !strings.Contains(refused.Error, "registered app") {
		t.Errorf("connect error %q, want it to say the server needs a registered app", refused.Error)
	}
	if len(app.registered()) != 0 {
		t.Errorf("Reef sent a registration to a server without registration_endpoint")
	}

	// Headers are the user's own sign-in: no request to find out.
	keyed := newOAuthMCP(t)
	var withKey mcpServer
	f.s.do(t, "POST", "/api/mcp-servers", map[string]any{"name": "docs-key", "url": keyed.url(), "headers": map[string]string{"X-Fake-Key": "fake-mcp-key-1"}}, &withKey, http.StatusCreated)
	if probes, bad, called := keyed.seen(); withKey.Status != "" || probes+bad+called != 0 {
		t.Errorf("server added with headers: status %q, %d requests to it, want no status and no request", withKey.Status, probes+bad+called)
	}

	f.s.do(t, "POST", "/api/mcp-servers/mcp_nowhere/connect", nil, nil, http.StatusNotFound)
	f.s.do(t, "POST", "/api/mcp-servers/context7/connect", nil, nil, http.StatusBadRequest)
}

func TestMCPOAuth_RemoveSignsOut(t *testing.T) {
	f := newOAuthFixture(t, "claude")
	s := f.add(t, "docs-oauth", f.o)
	f.signIn(t, s.ID)
	picked := f.bot(t, "Picker", s.ID)
	if _, ok := secretEntry("mcp:" + s.ID + ":oauth"); !ok {
		t.Fatal("no sign-in record in the OS secret store before Remove")
	}

	f.s.do(t, "DELETE", "/api/mcp-servers/"+s.ID, nil, nil, http.StatusNoContent)
	if v, ok := secretEntry("mcp:" + s.ID + ":oauth"); ok {
		t.Errorf("Remove left the sign-in record in the OS secret store: %d bytes", len(v))
	}
	var got mcpBot
	f.s.do(t, "GET", "/api/bots/"+picked.ID, nil, &got, http.StatusOK)
	if len(got.MCPServers) != 0 {
		t.Errorf("Bot picks %v after Remove, want none", got.MCPServers)
	}
}
