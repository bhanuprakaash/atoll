package e2e

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"

	"atoll/e2e/fakes/fake"
)

// Contract: mcp-servers-contract.md.

var _context7 = mcpServer{ID: "context7", Name: "Context7", Kind: "catalog", URL: "https://mcp.context7.com/mcp"}

// norm makes a field a kind does not use compare equal whether the server leaves it out or sends it empty.
func norm(s mcpServer) mcpServer {
	for _, list := range []*[]string{&s.Args, &s.EnvNames, &s.HeaderNames} {
		if len(*list) == 0 {
			*list = []string{}
		}
	}
	return s
}

func normAll(list []mcpServer) []mcpServer {
	out := make([]mcpServer, len(list))
	for i, s := range list {
		out[i] = norm(s)
	}
	return out
}

// useAll is the mcp-use prompt most tests send: a call on each fake server and one on a server no
// Bot has.
func useAll() []fake.MCPCall {
	return []fake.MCPCall{
		mcpCall("docs-local", "inspect", `{"env":"`+_mcpEnvName+`"}`),
		mcpCall("docs-local", "answer", `{}`),
		mcpCall("docs-remote", "answer", `{}`),
	}
}

func TestMCPServers_LibraryKeepsSecretsInTheOSStore(t *testing.T) {
	t.Parallel()
	f := newMCPFixture(t, "claude")

	var list []mcpServer
	f.s.do(t, "GET", "/api/mcp-servers", nil, &list, http.StatusOK)
	wantLocal := norm(mcpServer{ID: f.local.ID, Name: "docs-local", Kind: "local", Command: f.stdio, Args: []string{"--tag", "one"}, EnvNames: []string{_mcpEnvName}})
	wantRemote := norm(mcpServer{ID: f.remote.ID, Name: "docs-remote", Kind: "remote", URL: f.http.url(), HeaderNames: []string{_mcpHeaderName}})
	if f.local.ID == "" || f.remote.ID == "" || f.local.ID == f.remote.ID || f.local.ID == "context7" {
		t.Fatalf("ids %q and %q, want two distinct ids of Atoll's own", f.local.ID, f.remote.ID)
	}
	if want := slices.Concat([]mcpServer{norm(_context7)}, normAll(_hostedCatalog), []mcpServer{wantLocal, wantRemote}); !reflect.DeepEqual(normAll(list), want) {
		t.Fatalf("GET /api/mcp-servers:\n%+v\nwant the catalog entries, then the user's servers in the order added:\n%+v", list, want)
	}
	if !reflect.DeepEqual(norm(f.local), wantLocal) || !reflect.DeepEqual(norm(f.remote), wantRemote) {
		t.Fatalf("POST replies %+v and %+v, want the server as listed", f.local, f.remote)
	}

	var raw bytes.Buffer
	resp, err := http.Get(f.s.url + "/api/mcp-servers")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = raw.ReadFrom(resp.Body)
	_ = resp.Body.Close()
	secrets := []string{_mcpEnvSecret, _mcpHeaderSecret}
	for _, secret := range secrets {
		if bytes.Contains(raw.Bytes(), []byte(secret)) {
			t.Errorf("GET /api/mcp-servers shows secret %q: %s", secret, raw.Bytes())
		}
	}

	for username, want := range map[string]string{
		"mcp:" + f.local.ID + ":env:" + _mcpEnvName:        _mcpEnvSecret,
		"mcp:" + f.remote.ID + ":header:" + _mcpHeaderName: _mcpHeaderSecret,
	} {
		if got, ok := secretEntry(username); !ok || got != want {
			t.Errorf("OS secret store entry %s = %q (found %v), want %q", username, got, ok, want)
		}
	}
	for _, secret := range secrets {
		assertNoSecret(t, secret, f.m.data)
	}
	if b, err := os.ReadFile(filepath.Join(f.m.data, "atoll.db")); err != nil || bytes.Contains(b, []byte(_mcpEnvSecret)) || bytes.Contains(b, []byte(_mcpHeaderSecret)) {
		t.Errorf("atoll.db holds a secret or cannot be read: %v", err)
	}
	f.s.kill()
	for _, secret := range secrets {
		if bytes.Contains(f.s.log.Bytes(), []byte(secret)) {
			t.Errorf("server log leaked %q:\n%s", secret, f.s.log)
		}
	}
}

func TestMCPServers_RunGetsExactlyThePickedServers(t *testing.T) {
	t.Parallel()
	for _, p := range _providers {
		t.Run(p, func(t *testing.T) {
			f := newMCPFixture(t, p)
			picked := f.bot(t, "Picker", f.local.ID, f.remote.ID)
			plain := f.bot(t, "Plain")

			th := f.s.newThread(t, picked)
			ch := f.s.events(t, th)
			done, approvals := f.use(t, th, ch, true, useAll()...)
			want := replyLines(
				"docs-local/inspect: args=--tag|one;env="+envDigest(_mcpEnvSecret),
				"docs-local/answer: stdio-answer-42",
				"docs-remote/answer: http-answer-42",
			)
			if done.Status != "done" || done.Reply != want {
				t.Fatalf("run of the Bot with both servers: status %q, reply %q, want done and\n%s\n(error %q)", done.Status, done.Reply, want, done.Error)
			}
			// Reef's own tools needed none: exactly the three calls to user-added servers asked.
			if len(approvals) != 3 {
				t.Fatalf("approvals %+v, want one per call to a user-added server and none for Reef's own tools", approvals)
			}
			for i, server := range []string{"docs-local", "docs-local", "docs-remote"} {
				tool := []string{"inspect", "answer", "answer"}[i]
				if got := approvals[i].tool + " " + approvals[i].input; !strings.Contains(got, server) || !strings.Contains(got, tool) {
					t.Errorf("approval %d is %q, want it to name server %s and tool %s", i+1, got, server, tool)
				}
			}
			if called, rejected := f.http.calls(); called != 1 || rejected != 0 {
				t.Errorf("remote server saw %d calls and %d requests without its header, want 1 and 0", called, rejected)
			}
			if n := starts(f.stdio); n != 1 {
				t.Errorf("local server started %d times for one run, want 1", n)
			}

			// A Bot that picked nothing gets neither server: nothing starts, nothing reaches the remote,
			// and nothing asks.
			other := f.s.newThread(t, plain)
			otherCh := f.s.events(t, other)
			done, approvals = f.use(t, other, otherCh, true, useAll()...)
			want = replyLines("docs-local/inspect: unavailable", "docs-local/answer: unavailable", "docs-remote/answer: unavailable")
			if done.Status != "done" || done.Reply != want || len(approvals) != 0 {
				t.Fatalf("run of the Bot with no servers: status %q, reply %q, approvals %+v, want done and\n%s\nwith no approvals", done.Status, done.Reply, approvals, want)
			}
			if called, _ := f.http.calls(); called != 1 {
				t.Errorf("remote server saw %d calls after the Bot with no servers ran, want still 1", called)
			}
			if n := starts(f.stdio); n != 1 {
				t.Errorf("local server started %d times after the Bot with no servers ran, want still 1", n)
			}
			assertNoSecret(t, _mcpEnvSecret, f.m.data)
			assertNoSecret(t, _mcpHeaderSecret, f.m.data)
			f.s.kill()
			for _, secret := range []string{_mcpEnvSecret, _mcpHeaderSecret} {
				if bytes.Contains(f.s.log.Bytes(), []byte(secret)) {
					t.Errorf("server log leaked %q after runs", secret)
				}
			}
		})
	}
}

func TestMCPServers_DeniedCallDoesNotReachTheServer(t *testing.T) {
	t.Parallel()
	for _, p := range _providers {
		t.Run(p, func(t *testing.T) {
			f := newMCPFixture(t, p)
			th := f.s.newThread(t, f.bot(t, "Picker", f.local.ID, f.remote.ID))
			ch := f.s.events(t, th)
			done, approvals := f.use(t, th, ch, false, useAll()[1:]...)
			want := replyLines("docs-local/answer: denied", "docs-remote/answer: denied")
			if done.Status != "done" || done.Reply != want || len(approvals) != 2 {
				t.Fatalf("run with every call denied: status %q, reply %q, approvals %d, want done, %q and 2 approvals", done.Status, done.Reply, len(approvals), want)
			}
			if called, _ := f.http.calls(); called != 0 {
				t.Errorf("remote server saw %d calls after the user denied them", called)
			}
		})
	}
}

func TestMCPServers_EditAndRemoveApplyFromTheNextRun(t *testing.T) {
	t.Parallel()
	for _, p := range _providers {
		t.Run(p, func(t *testing.T) {
			f := newMCPFixture(t, p)
			picked := f.bot(t, "Picker", f.local.ID, f.remote.ID)
			th := f.s.newThread(t, picked)
			ch := f.s.events(t, th)
			r := f.s.send(t, th, _mcpPromptHead+mustJSON(useAll()))

			// The run is live, waiting on its first approval, while the library changes.
			es := until(t, ch, r.ID, "approval")
			first := es[len(es)-1].Data
			var edited mcpServer
			f.s.do(t, "PUT", "/api/mcp-servers/"+f.local.ID, map[string]any{
				"name": "docs-local", "command": f.stdio, "args": []string{"--tag", "two"}, "env": map[string]string{_mcpEnvName: ""},
			}, &edited, http.StatusOK)
			if !reflect.DeepEqual(edited.Args, []string{"--tag", "two"}) || !reflect.DeepEqual(edited.EnvNames, []string{_mcpEnvName}) {
				t.Fatalf("PUT reply %+v, want args --tag two and env name %s kept", edited, _mcpEnvName)
			}
			f.s.do(t, "DELETE", "/api/mcp-servers/"+f.remote.ID, nil, nil, http.StatusNoContent)
			firstID := asString(first["id"])
			f.s.do(t, "POST", "/api/approvals/"+firstID, map[string]bool{"approve": true}, nil, http.StatusNoContent)
			done, _ := f.answerAll(t, ch, r.ID, true, nil)
			want := replyLines(
				"docs-local/inspect: args=--tag|one;env="+envDigest(_mcpEnvSecret),
				"docs-local/answer: stdio-answer-42",
				"docs-remote/answer: http-answer-42",
			)
			if done.Status != "done" || done.Reply != want {
				t.Fatalf("live run after Edit and Remove: status %q, reply %q, want it to keep its config:\n%s\n(error %q)", done.Status, done.Reply, want, done.Error)
			}

			var got mcpBot
			f.s.do(t, "GET", "/api/bots/"+picked.ID, nil, &got, http.StatusOK)
			if !slices.Equal(got.MCPServers, []string{f.local.ID}) {
				t.Errorf("Bot picks %v after Remove, want only %s", got.MCPServers, f.local.ID)
			}
			if _, ok := secretEntry("mcp:" + f.remote.ID + ":header:" + _mcpHeaderName); ok {
				t.Errorf("Remove left the header secret of %s in the OS secret store", f.remote.ID)
			}
			if v, ok := secretEntry("mcp:" + f.local.ID + ":env:" + _mcpEnvName); !ok || v != _mcpEnvSecret {
				t.Errorf("Edit with an empty env value left the secret as %q (found %v), want it kept", v, ok)
			}

			// The next run, in the same thread, gets the edited local server and no remote one.
			done, approvals := f.use(t, th, ch, true, useAll()...)
			want = replyLines(
				"docs-local/inspect: args=--tag|two;env="+envDigest(_mcpEnvSecret),
				"docs-local/answer: stdio-answer-42",
				"docs-remote/answer: unavailable",
			)
			if done.Status != "done" || done.Reply != want || len(approvals) != 2 {
				t.Fatalf("next run: status %q, reply %q, approvals %d, want done, 2 approvals and\n%s\n(error %q)", done.Status, done.Reply, len(approvals), want, done.Error)
			}
			if called, _ := f.http.calls(); called != 1 {
				t.Errorf("removed remote server saw %d calls in total, want only the live run's 1", called)
			}
		})
	}
}

func mustJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func TestMCPServers_Refusals(t *testing.T) {
	t.Parallel()
	f := newMCPFixture(t, "claude")
	post := func(body map[string]any, want int) {
		t.Helper()
		var out struct {
			Error string `json:"error"`
		}
		f.s.do(t, "POST", "/api/mcp-servers", body, &out, want)
		if want == http.StatusBadRequest && out.Error == "" {
			t.Errorf("POST %v: 400 with no error text", body)
		}
	}
	for _, body := range []map[string]any{
		{},
		{"name": "no-target"},
		{"command": "/bin/true"},
		{"name": "both", "command": "/bin/true", "url": "https://mcp.example.test/mcp"},
		{"name": "has space", "command": "/bin/true"},
		{"name": "atoll", "command": "/bin/true"},
		{"name": "not-http", "url": "ftp://mcp.example.test/mcp"},
		{"name": "local-headers", "command": "/bin/true", "headers": map[string]string{"X": "y"}},
		{"name": "remote-env", "url": "https://mcp.example.test/mcp", "env": map[string]string{"X": "y"}},
	} {
		post(body, http.StatusBadRequest)
	}
	post(map[string]any{"name": "docs-local", "command": "/bin/true"}, http.StatusConflict)

	var list []mcpServer
	f.s.do(t, "GET", "/api/mcp-servers", nil, &list, http.StatusOK)
	if len(list) != 7 {
		t.Fatalf("GET /api/mcp-servers lists %d servers after refused requests, want the 5 catalog entries and the 2 added", len(list))
	}
	for _, id := range []string{"context7", "mcp_nowhere"} {
		f.s.do(t, "PUT", "/api/mcp-servers/"+id, map[string]any{"name": "x", "command": "/bin/true"}, nil, http.StatusNotFound)
		f.s.do(t, "DELETE", "/api/mcp-servers/"+id, nil, nil, http.StatusNotFound)
	}
	f.s.do(t, "PUT", "/api/mcp-servers/"+f.local.ID, map[string]any{"name": "docs-remote", "command": "/bin/true"}, nil, http.StatusConflict)
}

func TestMCPServers_BotPicks(t *testing.T) {
	t.Parallel()
	f := newMCPFixture(t, "claude")
	var plain mcpBot
	f.s.do(t, "POST", "/api/bots", map[string]any{"name": "Plain", "persona": "x", "provider": "claude"}, &plain, http.StatusCreated)
	if plain.MCPServers == nil || len(plain.MCPServers) != 0 {
		t.Fatalf("a Bot created with no mcp_servers has %#v, want []", plain.MCPServers)
	}

	pick := []string{"context7", f.local.ID}
	f.s.do(t, "PUT", "/api/bots/"+plain.ID, map[string]any{"mcp_servers": pick}, &plain, http.StatusOK)
	if !slices.Equal(plain.MCPServers, pick) {
		t.Fatalf("PUT mcp_servers %v: Bot has %v", pick, plain.MCPServers)
	}
	f.s.do(t, "PUT", "/api/bots/"+plain.ID, map[string]any{"persona": "y"}, &plain, http.StatusOK)
	if !slices.Equal(plain.MCPServers, pick) {
		t.Fatalf("an edit that leaves mcp_servers out changed them to %v", plain.MCPServers)
	}

	for _, ids := range [][]string{{"mcp_nowhere"}, {f.remote.ID, "mcp_nowhere"}} {
		var out struct {
			Error string `json:"error"`
		}
		f.s.do(t, "PUT", "/api/bots/"+plain.ID, map[string]any{"mcp_servers": ids}, &out, http.StatusBadRequest)
		for _, want := range []string{"mcp_nowhere", "context7", f.local.ID, f.remote.ID} {
			if !strings.Contains(out.Error, want) {
				t.Errorf("error %q for %v does not name %q", out.Error, ids, want)
			}
		}
		f.s.do(t, "POST", "/api/bots", map[string]any{"name": "Bad", "persona": "x", "provider": "claude", "mcp_servers": ids}, nil, http.StatusBadRequest)
	}
	f.s.do(t, "GET", "/api/bots/"+plain.ID, nil, &plain, http.StatusOK)
	if !slices.Equal(plain.MCPServers, pick) {
		t.Fatalf("refused requests changed the Bot's picks to %v", plain.MCPServers)
	}
	var bots []botInfo
	f.s.do(t, "GET", "/api/bots", nil, &bots, http.StatusOK)
	if len(bots) != 2 {
		t.Fatalf("GET /api/bots: %d Bots after a refused create, want Chief and Plain", len(bots))
	}

	f.s.do(t, "DELETE", "/api/mcp-servers/"+f.local.ID, nil, nil, http.StatusNoContent)
	f.s.do(t, "GET", "/api/bots/"+plain.ID, nil, &plain, http.StatusOK)
	if !slices.Equal(plain.MCPServers, []string{"context7"}) {
		t.Fatalf("Bot picks %v after its server was removed, want [context7]", plain.MCPServers)
	}
}

func TestChiefBot_MCPServers(t *testing.T) {
	t.Parallel()
	for _, p := range _providers {
		t.Run(p, func(t *testing.T) {
			f := newMCPFixture(t, p)

			c := chiefCall(t, f.s, "list_mcp_servers", map[string]any{})
			var listed, want []mcpServer
			f.s.do(t, "GET", "/api/mcp-servers", nil, &want, http.StatusOK)
			if err := json.Unmarshal([]byte(c.reply), &listed); err != nil || !reflect.DeepEqual(normAll(listed), normAll(want)) {
				t.Fatalf("list_mcp_servers result %q (%v), want what GET /api/mcp-servers lists: %+v", c.reply, err, want)
			}
			for _, secret := range []string{_mcpEnvSecret, _mcpHeaderSecret} {
				if strings.Contains(c.reply, secret) {
					t.Errorf("list_mcp_servers shows secret %q", secret)
				}
			}

			created := chiefCall(t, f.s, "create_bot", map[string]any{
				"name": "Poteto", "persona": "Reviews diffs.", "provider": p, "mcp_servers": []string{"context7", f.local.ID},
			})
			id := createdID(t, created)
			get := func() []string {
				var b mcpBot
				f.s.do(t, "GET", "/api/bots/"+id, nil, &b, http.StatusOK)
				return b.MCPServers
			}
			if got := get(); !slices.Equal(got, []string{"context7", f.local.ID}) {
				t.Fatalf("create_bot with mcp_servers: Bot has %v", got)
			}

			edit := chiefCall(t, f.s, "edit_bot", map[string]any{"id": id, "mcp_servers": []string{f.remote.ID}})
			change := platformChange(t, edit.events, "chief", edit.thread, edit.run, "bot", id, "edit")
			if got := get(); !slices.Equal(got, []string{f.remote.ID}) {
				t.Fatalf("edit_bot with mcp_servers: Bot has %v", got)
			}
			f.s.do(t, "POST", "/api/platform/changes/"+change+"/undo", nil, nil, http.StatusNoContent)
			if got := get(); !slices.Equal(got, []string{"context7", f.local.ID}) {
				t.Fatalf("undo of edit_bot: Bot has %v, want the earlier picks", got)
			}
			chiefCall(t, f.s, "edit_bot", map[string]any{"id": id, "mcp_servers": []string{}})
			if got := get(); len(got) != 0 {
				t.Fatalf("edit_bot with an empty mcp_servers: Bot has %v, want none", got)
			}

			known := []string{"context7", f.local.ID, f.remote.ID}
			var bots []botInfo
			f.s.do(t, "GET", "/api/bots", nil, &bots, http.StatusOK)
			failsListing(t, chiefCall(t, f.s, "edit_bot", map[string]any{"id": id, "mcp_servers": []string{"mcp_nowhere"}}), append([]string{"mcp_nowhere"}, known...))
			failsListing(t, chiefCall(t, f.s, "create_bot", map[string]any{"name": "Bad", "persona": "x", "provider": p, "mcp_servers": []string{"mcp_nowhere"}}), append([]string{"mcp_nowhere"}, known...))
			var after []botInfo
			f.s.do(t, "GET", "/api/bots", nil, &after, http.StatusOK)
			if len(after) != len(bots) {
				t.Fatalf("create_bot with an unknown server created a Bot: %+v", after)
			}
			if got := get(); len(got) != 0 {
				t.Fatalf("edit_bot with an unknown server changed the picks to %v", got)
			}
		})
	}
}

// TestMCPFixtures_ProviderScenarios drives the fake providers by hand, before the product writes
// any MCP config, to prove the mcp-use scenario: a stdio server and an HTTP server with a header
// in the config, a call on each, an ask before each, and nothing for a server the config lacks.
func TestMCPFixtures_ProviderScenarios(t *testing.T) {
	t.Parallel()
	for _, p := range _providers {
		for _, decision := range []string{"allow", "deny"} {
			t.Run(p+"/"+decision, func(t *testing.T) {
				m := newMachine(t, p)
				remote := newHTTPMCP(t)
				stdio := installMCPStdio(t)
				var mu sync.Mutex
				asked := 0
				atoll := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					var req struct {
						ID     json.RawMessage `json:"id"`
						Method string          `json:"method"`
						Params struct {
							Name string `json:"name"`
						} `json:"params"`
					}
					_ = json.NewDecoder(r.Body).Decode(&req)
					if len(req.ID) == 0 {
						w.WriteHeader(http.StatusAccepted)
						return
					}
					var result any = map[string]any{}
					switch req.Method {
					case "initialize":
						result = map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{}, "serverInfo": map[string]string{"name": "stub", "version": "0"}}
					case "tools/list":
						result = map[string]any{"tools": []any{}}
					case "tools/call":
						text := "Report saved."
						if req.Params.Name == "approve" {
							mu.Lock()
							asked++
							mu.Unlock()
							text = `{"behavior":"` + decision + `","message":"no"}`
						}
						result = map[string]any{"content": []map[string]string{{"type": "text", "text": text}}}
					}
					w.Header().Set("Content-Type", "application/json")
					_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result})
				}))
				defer atoll.Close()

				calls := append(useAll(), mcpCall("docs-gone", "answer", `{}`))
				prompt := _mcpPromptHead + mustJSON(calls)
				var cmd *exec.Cmd
				if p == "claude" {
					config, _ := json.Marshal(map[string]any{"mcpServers": map[string]any{
						"atoll":       map[string]any{"type": "http", "url": atoll.URL},
						"docs-local":  map[string]any{"type": "stdio", "command": stdio, "args": []string{"--tag", "one"}, "env": map[string]string{_mcpEnvName: _mcpEnvSecret}},
						"docs-remote": map[string]any{"type": "http", "url": remote.url(), "headers": map[string]string{_mcpHeaderName: _mcpHeaderSecret}},
					}})
					cmd = fakeCmd(t, m, p, "-p", "--input-format", "stream-json", "--output-format", "stream-json", "--mcp-config", string(config),
						"--permission-prompt-tool", "mcp__atoll__approve", "--allowedTools", "mcp__atoll")
					input, _ := json.Marshal(map[string]any{"type": "user", "message": map[string]string{"role": "user", "content": prompt}})
					cmd.Stdin = strings.NewReader(string(input) + "\n")
				} else {
					cmd = fakeCmd(t, m, p, "app-server")
					action := map[string]string{"allow": "accept", "deny": "decline"}[decision]
					var input strings.Builder
					for _, req := range []map[string]any{
						{"id": 1, "method": "initialize", "params": map[string]any{}},
						{"id": 2, "method": "thread/start", "params": map[string]any{"config": map[string]any{"mcp_servers": map[string]any{
							"atoll":       map[string]any{"url": atoll.URL},
							"docs-local":  map[string]any{"command": stdio, "args": []string{"--tag", "one"}, "env": map[string]string{_mcpEnvName: _mcpEnvSecret}, "default_tools_approval_mode": "prompt"},
							"docs-remote": map[string]any{"url": remote.url(), "http_headers": map[string]string{_mcpHeaderName: _mcpHeaderSecret}, "default_tools_approval_mode": "prompt"},
						}}}},
						{"id": 3, "method": "turn/start", "params": map[string]any{"input": []map[string]string{{"type": "text", "text": prompt}}}},
						{"id": 9001, "result": map[string]string{"action": action}},
						{"id": 9002, "result": map[string]string{"action": action}},
						{"id": 9003, "result": map[string]string{"action": action}},
					} {
						b, _ := json.Marshal(req)
						input.Write(b)
						input.WriteByte('\n')
					}
					cmd.Stdin = strings.NewReader(input.String())
				}
				out, err := cmd.CombinedOutput()
				if err != nil {
					t.Fatalf("fake %s: %v\n%s", p, err, out)
				}
				want := []string{
					"docs-local/inspect: args=--tag|one;env=" + envDigest(_mcpEnvSecret),
					"docs-local/answer: stdio-answer-42",
					"docs-remote/answer: http-answer-42",
				}
				if decision == "deny" {
					want = []string{"docs-local/inspect: denied", "docs-local/answer: denied", "docs-remote/answer: denied"}
				}
				want = append(want, "docs-gone/answer: unavailable")
				for _, line := range want {
					if !strings.Contains(string(out), line) {
						t.Errorf("fake %s output lacks %q:\n%s", p, line, out)
					}
				}
				if called, rejected := remote.calls(); rejected != 0 || (called == 1) != (decision == "allow") {
					t.Errorf("remote server saw %d calls and %d requests without its header after %s", called, rejected, decision)
				}
				if p == "claude" {
					mu.Lock()
					defer mu.Unlock()
					if asked != 3 {
						t.Errorf("permission prompt tool asked %d times, want once per call to a configured server", asked)
					}
				}
			})
		}
	}
}
