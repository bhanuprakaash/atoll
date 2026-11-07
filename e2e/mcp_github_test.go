package e2e

import (
	"net/http"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
)

// Contract: mcp-servers-contract.md, "From connections".

const _userGitHubAuth = "Bearer fake-user-gh-token"

func catalogOnly() []mcpServer {
	return slices.Concat([]mcpServer{norm(_context7)}, normAll(_hostedCatalog))
}

func TestMCPGitHub_RowFollowsTheConnection(t *testing.T) {
	f := newGitHubFixture(t, "claude")
	mcpURL := f.gh.URL + "/mcp/"
	row := norm(mcpServer{ID: "github", Name: "GitHub", Kind: "connection", URL: mcpURL})
	list := func() []mcpServer {
		var l []mcpServer
		f.s.do(t, "GET", "/api/mcp-servers", nil, &l, http.StatusOK)
		return normAll(l)
	}
	if got := list(); !reflect.DeepEqual(got, catalogOnly()) {
		t.Fatalf("GET /api/mcp-servers with no GitHub connection:\n%+v\nwant only the catalog", got)
	}

	// A sign-in that has not finished is not a connection.
	var start struct {
		ID string `json:"id"`
	}
	f.s.do(t, "POST", "/api/connections/github/start", nil, &start, http.StatusCreated)
	clearGitHubSecretAfter(t, start.ID)
	if got := list(); !reflect.DeepEqual(got, catalogOnly()) {
		t.Fatalf("GET /api/mcp-servers while the GitHub sign-in is pending:\n%+v\nwant only the catalog", got)
	}
	f.gh.set(func() { f.gh.approved = true })
	var status struct {
		Status string `json:"status"`
	}
	for range 5 {
		time.Sleep(1100 * time.Millisecond)
		f.s.do(t, "POST", "/api/connections/"+start.ID+"/poll", nil, &status, http.StatusOK)
		if status.Status == "connected" {
			break
		}
	}
	if status.Status != "connected" {
		t.Fatalf("sign in did not connect: %+v", status)
	}

	var user mcpServer
	f.s.do(t, "POST", "/api/mcp-servers", map[string]any{
		"name": "docs-remote", "url": "https://mcp.example.test/mcp", "headers": map[string]string{_mcpHeaderName: _mcpHeaderSecret},
	}, &user, http.StatusCreated)
	t.Cleanup(func() { clearSecretEntry("mcp:" + user.ID + ":header:" + _mcpHeaderName) })
	want := slices.Concat(catalogOnly(), []mcpServer{row}, []mcpServer{norm(user)})
	if got := list(); !reflect.DeepEqual(got, want) {
		t.Fatalf("GET /api/mcp-servers with the GitHub connection:\n%+v\nwant the catalog, the connection's row, then the user's servers:\n%+v", got, want)
	}

	// A token GitHub rejects leaves the connection, and its row, in place until Sign in again.
	f.gh.set(func() { f.gh.rejectUser = true })
	b := f.bot(t, "claude", []string{start.ID})
	if done, _ := f.probe(t, b); done.Status != "failed" {
		t.Fatalf("run with a rejected token: %+v, want failed", done)
	}
	var conn connectionInfo
	f.s.do(t, "GET", "/api/connections/"+start.ID, nil, &conn, http.StatusOK)
	if conn.Status != "needs_sign_in" {
		t.Fatalf("connection %+v, want needs_sign_in", conn)
	}
	if got := list(); !reflect.DeepEqual(got, want) {
		t.Fatalf("GET /api/mcp-servers while the connection needs sign-in:\n%+v\nwant it unchanged:\n%+v", got, want)
	}
}

func TestMCPGitHub_RowIsReadOnly(t *testing.T) {
	f := newGitHubFixture(t, "claude")
	body := map[string]any{"name": "GitHub", "url": "https://mcp.example.test/mcp"}
	// Without the connection `github` is an id the library does not have.
	f.s.do(t, "PUT", "/api/mcp-servers/github", body, nil, http.StatusNotFound)
	f.s.do(t, "DELETE", "/api/mcp-servers/github", nil, nil, http.StatusNotFound)

	f.connect(t)
	for _, req := range []struct{ method, path string }{{"PUT", "/api/mcp-servers/github"}, {"DELETE", "/api/mcp-servers/github"}} {
		var out struct {
			Error string `json:"error"`
		}
		var in any
		if req.method == "PUT" {
			in = body
		}
		f.s.do(t, req.method, req.path, in, &out, http.StatusBadRequest)
		if out.Error == "" {
			t.Errorf("%s %s: 400 with no error text", req.method, req.path)
		}
	}
	// A Bot gets the row through its connection, never as a pick.
	var out struct {
		Error string `json:"error"`
	}
	f.s.do(t, "POST", "/api/bots", map[string]any{"name": "Bad", "persona": "x", "provider": "claude", "mcp_servers": []string{"github"}}, &out, http.StatusBadRequest)
	if !strings.Contains(out.Error, "github") {
		t.Errorf("error %q for picking the row does not name it", out.Error)
	}
	var l []mcpServer
	f.s.do(t, "GET", "/api/mcp-servers", nil, &l, http.StatusOK)
	if want := slices.Concat(catalogOnly(), []mcpServer{norm(mcpServer{ID: "github", Name: "GitHub", Kind: "connection", URL: f.gh.URL + "/mcp/"})}); !reflect.DeepEqual(normAll(l), want) {
		t.Fatalf("GET /api/mcp-servers after refused requests:\n%+v\nwant the row untouched:\n%+v", l, want)
	}

	// The API does not dedupe: a user server at GitHub's MCP URL is Add anyway.
	var dup mcpServer
	f.s.do(t, "POST", "/api/mcp-servers", map[string]any{"name": "gh-dup", "url": f.gh.URL + "/mcp/", "headers": map[string]string{"Authorization": _userGitHubAuth}}, &dup, http.StatusCreated)
	clearSecretEntryAfter(t, "mcp:"+dup.ID+":header:Authorization")
	if dup.Kind != "remote" || dup.URL != f.gh.URL+"/mcp/" {
		t.Errorf("POST reply %+v, want a remote server at GitHub's MCP URL", dup)
	}
}

func clearSecretEntryAfter(t *testing.T, username string) {
	t.Helper()
	t.Cleanup(func() { clearSecretEntry(username) })
}

func TestMCPGitHub_BotWithBothGetsOnlyTheConnectionsEntry(t *testing.T) {
	for _, p := range _providers {
		t.Run(p, func(t *testing.T) {
			f := newGitHubFixture(t, p)
			mcpURL := f.gh.URL + "/mcp/"
			id := f.connect(t)
			token := f.gh.currentToken()
			var dup mcpServer
			f.s.do(t, "POST", "/api/mcp-servers", map[string]any{"name": "gh-dup", "url": mcpURL, "headers": map[string]string{"Authorization": _userGitHubAuth}}, &dup, http.StatusCreated)
			clearSecretEntryAfter(t, "mcp:"+dup.ID+":header:Authorization")

			var both bot
			f.s.do(t, "POST", "/api/bots", map[string]any{
				"name": "Both", "persona": "Works on GitHub.", "provider": p, "connections": []string{id}, "mcp_servers": []string{dup.ID},
			}, &both, http.StatusCreated)
			done, probe := f.probe(t, both)
			if done.Status != "done" || !probe.MCP.Connected || !strings.Contains(probe.MCP.Text, `"source":"mcp"`) {
				t.Fatalf("run of the Bot with both: %+v, mcp %+v, want done and get_me answered by the connection's entry", done, probe.MCP)
			}
			sessions := f.m.sessions(t, p)
			if len(sessions) != 1 {
				t.Fatalf("%s ran %d times, want 1", p, len(sessions))
			}
			servers := mcpServersOf(t, p, sessions[0])
			var atGitHub []string
			for name, s := range servers {
				if s.URL == mcpURL {
					atGitHub = append(atGitHub, name)
				}
			}
			if !reflect.DeepEqual(atGitHub, []string{"github"}) {
				t.Errorf("MCP config entries at GitHub's MCP URL: %v (servers %v), want only the connection's github", atGitHub, servers)
			}
			if _, ok := servers["gh-dup"]; ok {
				t.Errorf("MCP config still has the user's gh-dup next to the connection's entry: %v", servers)
			}
			if _, ok := servers["atoll"]; !ok {
				t.Errorf("MCP config lost Reef's own server: %v", servers)
			}
			if p == "claude" && servers["github"].Headers["Authorization"] != "Bearer "+token {
				t.Errorf("claude github headers %v, want Authorization Bearer <the connection's token>", servers["github"].Headers)
			}
			calls := 0
			for _, r := range f.gh.mcpRequests() {
				if r.Auth != "Bearer "+token {
					t.Errorf("GitHub MCP server got a request with Authorization %q, want only the connection's token", r.Auth)
				}
				if r.Method == "tools/call" && r.Tool == "get_me" {
					calls++
				}
			}
			if calls != 1 {
				t.Errorf("get_me calls on the fake GitHub MCP server: %d, want 1", calls)
			}

			// Add anyway is honoured for a Bot that has no connection: its own entry stays, as the user made it.
			own := f.s.newBot(t, "Own", p)
			f.s.do(t, "PUT", "/api/bots/"+own.ID, map[string]any{"mcp_servers": []string{dup.ID}}, nil, http.StatusOK)
			if done, probe := f.probe(t, own); done.Status != "done" || probe.MCP.Connected {
				t.Fatalf("run of the Bot with only the user's server: %+v, mcp %+v, want done and no github entry", done, probe.MCP)
			}
			sessions = f.m.sessions(t, p)
			if len(sessions) != 2 {
				t.Fatalf("%s ran %d times, want 2", p, len(sessions))
			}
			servers = mcpServersOf(t, p, sessions[1])
			if s, ok := servers["gh-dup"]; !ok || s.URL != mcpURL {
				t.Errorf("MCP config of the Bot with only the user's server: %v, want gh-dup at %s", servers, mcpURL)
			}
			if _, ok := servers["github"]; ok {
				t.Errorf("MCP config of a Bot without the connection has a github entry: %v", servers)
			}
		})
	}
}
