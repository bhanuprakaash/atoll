package e2e

import (
	"bytes"
	"encoding/json"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
)

// Contract: github-connection-contract.md.

func TestGitHub_ConnectKeepsTheTokenInTheSecretStore(t *testing.T) {
	t.Parallel()
	f := newGitHubFixture(t, "claude")
	var start struct {
		ID              string `json:"id"`
		UserCode        string `json:"user_code"`
		VerificationURI string `json:"verification_uri"`
		IntervalMs      int    `json:"interval_ms"`
	}
	f.s.do(t, "POST", "/api/connections/github/start", nil, &start, 201)
	if start.ID == "" || start.UserCode != "ABCD-1234" || start.VerificationURI != "https://github.com/login/device" || start.IntervalMs <= 0 {
		t.Fatalf("device start: %+v", start)
	}
	f.gh.mu.Lock()
	form := f.gh.deviceForm
	f.gh.mu.Unlock()
	if form.Get("client_id") != _githubClientID || strings.Join(strings.Fields(form.Get("scope")), ",") != "repo,read:org,workflow" {
		t.Fatalf("device code request %v, want client_id %s and scope \"repo read:org workflow\"", form, _githubClientID)
	}

	var status struct {
		Status string `json:"status"`
	}
	f.s.do(t, "POST", "/api/connections/"+start.ID+"/poll", nil, &status, 200)
	if status.Status != "pending" {
		t.Fatalf("poll before the user approved: %+v, want pending", status)
	}
	f.gh.set(func() { f.gh.approved = true })
	clearGitHubSecretAfter(t, start.ID)
	for range 5 {
		time.Sleep(1100 * time.Millisecond)
		f.s.do(t, "POST", "/api/connections/"+start.ID+"/poll", nil, &status, 200)
		if status.Status == "connected" {
			break
		}
	}
	if status.Status != "connected" {
		t.Fatalf("poll after approval: %+v", status)
	}
	f.gh.mu.Lock()
	forms := f.gh.tokenForms
	f.gh.mu.Unlock()
	for _, form := range forms {
		if form.Get("client_id") != _githubClientID || form.Get("device_code") != "fixture-device-code" ||
			form.Get("grant_type") != "urn:ietf:params:oauth:grant-type:device_code" {
			t.Errorf("access token request %v", form)
		}
	}

	var conns []struct {
		ID          string `json:"id"`
		Kind        string `json:"kind"`
		InstanceURL string `json:"instance_url"`
		Status      string `json:"status"`
	}
	f.s.do(t, "GET", "/api/connections", nil, &conns, 200)
	if len(conns) != 1 || conns[0].ID != start.ID || conns[0].Kind != "github" || conns[0].InstanceURL != "https://github.com" || conns[0].Status != "connected" {
		t.Fatalf("GET /api/connections: %+v, want one connected github connection %s", conns, start.ID)
	}
	var one struct {
		Status string `json:"status"`
	}
	f.s.do(t, "GET", "/api/connections/"+start.ID, nil, &one, 200)
	if one.Status != "connected" {
		t.Fatalf("GET /api/connections/%s: %+v", start.ID, one)
	}

	token := f.gh.currentToken()
	if got := githubSecret(t, start.ID); got != token {
		t.Fatalf("OS secret store github:%s holds %q, want the access token", start.ID, got)
	}
	assertNoSecret(t, token, f.m.data)
	f.s.kill()
	if bytes.Contains(f.s.log.Bytes(), []byte(token)) {
		t.Fatalf("server log leaked the token:\n%s", f.s.log)
	}
}

// GitHub answers a poll that comes too early with slow_down and adds 5s to the interval; every
// later poll sooner than that gets slow_down again. Polled as the UI does, at the advertised
// interval, the sign-in still connects, and no poll after the slow_down is early.
func TestGitHub_SignInConnectsAfterSlowDown(t *testing.T) {
	t.Parallel()
	f := newGitHubFixture(t, "claude")
	var start struct {
		ID string `json:"id"`
	}
	f.s.do(t, "POST", "/api/connections/github/start", nil, &start, 201)
	f.gh.set(func() { f.gh.slowDownNext, f.gh.approved = true, true })
	clearGitHubSecretAfter(t, start.ID)
	var status struct {
		Status string `json:"status"`
	}
	for range 12 {
		time.Sleep(1100 * time.Millisecond)
		f.s.do(t, "POST", "/api/connections/"+start.ID+"/poll", nil, &status, 200)
		if status.Status == "connected" {
			break
		}
	}
	f.gh.mu.Lock()
	slowDowns, polls := f.gh.slowDowns, slices.Clone(f.gh.polls)
	f.gh.mu.Unlock()
	if status.Status != "connected" || slowDowns != 1 {
		t.Fatalf("after a slow_down: status %+v, %d slow_down answers to polls at %v; want connected after one", status, slowDowns, polls)
	}
	if got := githubSecret(t, start.ID); got != f.gh.currentToken() {
		t.Fatalf("OS secret store github:%s holds %q, want the access token", start.ID, got)
	}
}

func TestGitHub_RunGetsTheConnection(t *testing.T) {
	t.Parallel()
	for _, p := range _providers {
		t.Run(p, func(t *testing.T) {
			f := newGitHubFixture(t, p)
			id := f.connect(t)
			token := f.gh.currentToken()
			b := f.bot(t, p, []string{id})

			done, probe := f.probe(t, b)
			if done.Status != "done" {
				t.Fatalf("run: %+v", done)
			}
			var user struct {
				Login string `json:"login"`
			}
			if err := json.Unmarshal([]byte(probe.GH.Output), &user); probe.GH.Exit != 0 || err != nil || user.Login != "reef-fixture" {
				t.Errorf("gh api user in the run: exit %d, output %q; want the fake user reef-fixture", probe.GH.Exit, probe.GH.Output)
			}
			cred := githubCredential(probe.Git.Output)
			if cred["username"] != "x-access-token" || cred["password_sha256"] != digest(token) {
				t.Errorf("git credential fill for github: %q, want username x-access-token and the token's digest %s", probe.Git.Output, digest(token))
			}
			if strings.Contains(probe.OtherGit.Output, "password") {
				t.Errorf("git credential fill for another host returned a password: %q", probe.OtherGit.Output)
			}
			if probe.Push.Exit != 0 {
				t.Errorf("git push to the fake github.com remote: exit %d: %s", probe.Push.Exit, probe.Push.Output)
			}
			if got := gitIn(t, f.gh.root, "--git-dir", filepath.Join(f.gh.root, "octo", "hello.git"), "show", "refs/heads/reef-push:probe.txt"); got != "pushed by the probe" {
				t.Errorf("remote has probe.txt %q after the push", got)
			}
			if auth := f.gh.pushes(); len(auth) == 0 {
				t.Error("the fake GitHub accepted no git request")
			} else {
				for _, a := range auth {
					if a != "x-access-token:"+token {
						t.Errorf("git request authenticated as %q, want x-access-token and the token", a)
					}
				}
			}
			if !probe.MCP.Connected || !strings.Contains(probe.MCP.Text, `"source":"mcp"`) {
				t.Errorf("get_me on the github MCP server: %+v, want the fake's answer", probe.MCP)
			}
			calls := 0
			for _, r := range f.gh.mcpRequests() {
				if r.Auth != "Bearer "+token {
					t.Errorf("MCP request %+v, want Authorization Bearer <token>", r)
				}
				if r.Method == "tools/call" && r.Tool == "get_me" {
					calls++
				}
			}
			if calls != 1 {
				t.Errorf("get_me calls on the fake GitHub MCP server: %d, want 1", calls)
			}

			f.s.kill()
			f.m.killFakes()
			sessions := f.m.sessions(t, p)
			if len(sessions) != 1 {
				t.Fatalf("%s ran %d times, want 1", p, len(sessions))
			}
			assertGitHubEnv(t, p, sessions[0].env, token)
			ghCalls := 0
			for _, c := range f.m.calls(t, "gh") {
				if len(c.argv) == 2 && c.argv[0] == "api" {
					ghCalls++
					assertGitHubEnv(t, "gh api", c.env, token)
				}
			}
			if ghCalls != 1 {
				t.Errorf("gh api ran %d times, want 1", ghCalls)
			}
			servers := mcpServersOf(t, p, sessions[0])
			if gh, ok := servers["github"]; !ok || gh.URL != f.gh.URL+"/mcp/" {
				t.Errorf("MCP config github entry %+v (servers %v), want url %s", gh, servers, f.gh.URL+"/mcp/")
			} else if p == "claude" && gh.Headers["Authorization"] != "Bearer "+token {
				t.Errorf("claude loaded MCP config github headers %v, want Authorization Bearer <token>", gh.Headers)
			}
			if _, ok := servers["atoll"]; !ok {
				t.Errorf("MCP config lost Reef's own server: %v", servers)
			}

			assertNoSecret(t, token, f.m.data)
			if bytes.Contains(f.s.log.Bytes(), []byte(token)) {
				t.Fatalf("server log leaked the token:\n%s", f.s.log)
			}
		})
	}
}

func TestGitHub_BotWithoutTheConnectionGetsNothing(t *testing.T) {
	t.Parallel()
	for _, p := range _providers {
		t.Run(p, func(t *testing.T) {
			f := newGitHubFixture(t, p)
			f.connect(t)
			b := f.bot(t, p, []string{})

			done, probe := f.probe(t, b)
			if done.Status != "done" {
				t.Fatalf("run: %+v", done)
			}
			if probe.GH.Exit == 0 || strings.Contains(probe.GH.Output, "reef-fixture") {
				t.Errorf("gh api user in a run without the connection: exit %d, output %q; want it to fail", probe.GH.Exit, probe.GH.Output)
			}
			if strings.Contains(probe.Git.Output, "password") || strings.Contains(probe.Git.Output, "x-access-token") {
				t.Errorf("git credential fill for github in a run without the connection: %q", probe.Git.Output)
			}
			if probe.Push.Exit == 0 {
				t.Errorf("git push to the fake github.com remote succeeded without the connection: %s", probe.Push.Output)
			}
			if out := gitIn(t, f.gh.root, "--git-dir", filepath.Join(f.gh.root, "octo", "hello.git"), "for-each-ref"); out != "" {
				t.Errorf("remote has refs after a push without credentials: %s", out)
			}
			if len(f.gh.pushes()) != 0 {
				t.Errorf("the fake GitHub accepted git requests: %v", f.gh.pushes())
			}
			if probe.MCP.Connected {
				t.Errorf("a run without the connection has a github MCP server: %+v", probe.MCP)
			}
			if reqs := f.gh.mcpRequests(); len(reqs) != 0 {
				t.Errorf("the fake GitHub MCP server got requests: %+v", reqs)
			}

			sessions := f.m.sessions(t, p)
			if len(sessions) != 1 {
				t.Fatalf("%s ran %d times, want 1", p, len(sessions))
			}
			assertNoTokens(t, p, sessions[0].env)
			for _, c := range f.m.calls(t, "gh") {
				assertNoTokens(t, "gh", c.env)
			}
			if _, ok := mcpServersOf(t, p, sessions[0])["github"]; ok {
				t.Errorf("MCP config of a Bot without the connection has a github entry")
			}
		})
	}
}

func TestGitHub_RejectedTokenNeedsSignIn(t *testing.T) {
	t.Parallel()
	f := newGitHubFixture(t, "claude")
	id := f.connect(t)
	first := f.gh.currentToken()
	b := f.bot(t, "claude", []string{id})
	if done, _ := f.probe(t, b); done.Status != "done" {
		t.Fatalf("first run: %+v", done)
	}
	if n := len(f.m.sessions(t, "claude")); n != 1 {
		t.Fatalf("provider sessions after the first run: %d, want 1", n)
	}

	f.gh.set(func() { f.gh.rejectUser = true })
	done, _ := f.probe(t, b)
	if done.Status != "failed" || !strings.Contains(done.Error, "GitHub needs sign-in") {
		t.Fatalf("run with a rejected token: %+v, want status failed and the error \"GitHub needs sign-in\"", done)
	}
	if n := len(f.m.sessions(t, "claude")); n != 1 {
		t.Fatalf("provider sessions after the rejected run: %d, want still 1: the run must stop before the provider starts", n)
	}
	var conns []connectionInfo
	f.s.do(t, "GET", "/api/connections", nil, &conns, 200)
	if len(conns) != 1 || conns[0].ID != id || conns[0].Status != "needs_sign_in" {
		t.Fatalf("GET /api/connections: %+v, want %s needs_sign_in", conns, id)
	}
	var one connectionInfo
	f.s.do(t, "GET", "/api/connections/"+id, nil, &one, 200)
	if one.Status != "needs_sign_in" {
		t.Fatalf("GET /api/connections/%s: %+v, want needs_sign_in", id, one)
	}

	f.gh.set(func() { f.gh.rejectUser = false })
	if got := f.gh.signIn(t, f.s, "/api/connections/github", map[string]string{"connection_id": id}); got != id {
		t.Fatalf("sign in again gave connection %q, want the same id %q", got, id)
	}
	second := f.gh.currentToken()
	if second == first {
		t.Fatal("fake GitHub issued the same token twice")
	}
	f.s.do(t, "GET", "/api/connections", nil, &conns, 200)
	if len(conns) != 1 || conns[0].ID != id || conns[0].Status != "connected" {
		t.Fatalf("GET /api/connections after sign in again: %+v, want only %s connected", conns, id)
	}
	var got struct {
		Connections []string `json:"connections"`
	}
	f.s.do(t, "GET", "/api/bots/"+b.ID, nil, &got, 200)
	if !reflect.DeepEqual(got.Connections, []string{id}) {
		t.Fatalf("Bot connections %v, want [%s]", got.Connections, id)
	}
	if v := githubSecret(t, id); v != second {
		t.Fatalf("OS secret store holds %q, want the new token", v)
	}

	if done, _ := f.probe(t, b); done.Status != "done" {
		t.Fatalf("run after sign in again: %+v", done)
	}
	sessions := f.m.sessions(t, "claude")
	if len(sessions) != 2 {
		t.Fatalf("provider sessions: %d, want 2", len(sessions))
	}
	assertGitHubEnv(t, "claude", sessions[1].env, second)
}

func TestGitHub_ChiefHandlesTheConnection(t *testing.T) {
	t.Parallel()
	f := newGitHubFixture(t, "claude")
	id := f.connect(t)

	created := chiefCall(t, f.s, "create_bot", map[string]any{"name": "Octo", "persona": "Works on GitHub.", "provider": "claude", "connections": []string{id}})
	botID := createdID(t, created)
	type withConnections struct {
		Connections []string `json:"connections"`
	}
	connectionsOf := func(id string) []string {
		var b withConnections
		f.s.do(t, "GET", "/api/bots/"+id, nil, &b, 200)
		return b.Connections
	}
	if got := connectionsOf(botID); !reflect.DeepEqual(got, []string{id}) {
		t.Fatalf("create_bot with the GitHub connection: Bot connections %v, want [%s]", got, id)
	}

	plain := f.s.newBot(t, "Plain", "claude")
	chiefCall(t, f.s, "edit_bot", map[string]any{"id": plain.ID, "connections": []string{id}})
	if got := connectionsOf(plain.ID); !reflect.DeepEqual(got, []string{id}) {
		t.Fatalf("edit_bot adding the GitHub connection: Bot connections %v, want [%s]", got, id)
	}
	chiefCall(t, f.s, "edit_bot", map[string]any{"id": plain.ID, "connections": []string{}})
	if got := connectionsOf(plain.ID); len(got) != 0 {
		t.Fatalf("edit_bot removing it: Bot connections %v, want none", got)
	}

	listed := chiefCall(t, f.s, "list_connections", map[string]any{})
	var tool, rest []map[string]any
	if err := json.Unmarshal([]byte(listed.reply), &tool); err != nil {
		t.Fatalf("list_connections result %q: %v", listed.reply, err)
	}
	f.s.do(t, "GET", "/api/connections", nil, &rest, 200)
	fields := []string{"id", "kind", "instance_url", "status"}
	want := map[string]any{"id": id, "kind": "github", "instance_url": "https://github.com", "status": "connected"}
	if len(tool) != 1 || len(rest) != 1 || !reflect.DeepEqual(pick(tool[0], fields...), want) || !reflect.DeepEqual(pick(rest[0], fields...), want) {
		t.Fatalf("list_connections %v and GET /api/connections %v, want [%v]", tool, rest, want)
	}

	system := heySystem(t, f.s, "claude", chief(t, f.s).bot)
	guide := strings.Join(section(t, platformBlock(t, system), _guideHeading), "\n")
	if !strings.Contains(guide, "GitHub") {
		t.Errorf("Reef guide does not name GitHub among the connections:\n%s", guide)
	}
}

// TestGitHubFixtures_ProviderScenario proves the github-probe scenario, the fake gh and the fake
// GitHub work without the product: the fake provider is started by hand with the env and MCP
// config the contract describes for a run of a Bot with GitHub.
func TestGitHubFixtures_ProviderScenario(t *testing.T) {
	t.Parallel()
	for _, provider := range _providers {
		t.Run(provider, func(t *testing.T) {
			m := newMachine(t, provider, "gh")
			g := newFakeGitHub(t)
			g.set(func() { g.token = "fake-gho-direct" })
			writeFile(t, filepath.Join(m.bin, "github-url"), g.URL)
			empty := t.TempDir()
			writeFile(t, filepath.Join(empty, "hosts.yml"), "")
			mcpURL, auth := g.URL+"/mcp/", "Bearer fake-gho-direct"

			var cmd *exec.Cmd
			if provider == "claude" {
				config, _ := json.Marshal(map[string]any{"mcpServers": map[string]any{"github": map[string]any{"type": "http", "url": mcpURL, "headers": map[string]string{"Authorization": auth}}}})
				cmd = exec.CommandContext(t.Context(), filepath.Join(m.bin, provider), "-p", "--input-format", "stream-json", "--output-format", "stream-json", "--mcp-config", string(config), "--allowedTools", "mcp__atoll__*")
				input, _ := json.Marshal(map[string]any{"type": "user", "message": map[string]string{"role": "user", "content": _githubPrompt}})
				cmd.Stdin = strings.NewReader(string(input) + "\n")
			} else {
				cmd = exec.CommandContext(t.Context(), filepath.Join(m.bin, provider), "app-server")
				var input strings.Builder
				for i, req := range []map[string]any{
					{"method": "initialize", "params": map[string]any{}},
					{"method": "thread/start", "params": map[string]any{"cwd": t.TempDir(), "config": map[string]any{"mcp_servers": map[string]any{"github": map[string]any{"url": mcpURL, "http_headers": map[string]string{"Authorization": auth}}}}}},
					{"method": "turn/start", "params": map[string]any{"input": []map[string]string{{"type": "text", "text": _githubPrompt}}}},
				} {
					req["id"] = i + 1
					b, _ := json.Marshal(req)
					input.Write(b)
					input.WriteByte('\n')
				}
				cmd.Stdin = strings.NewReader(input.String())
			}
			cmd.Dir = t.TempDir()
			cmd.Env = []string{
				"HOME=" + m.home, "PATH=" + m.path, "GH_TOKEN=fake-gho-direct", "GH_CONFIG_DIR=" + empty,
				"GIT_CONFIG_COUNT=2", "GIT_CONFIG_KEY_0=credential.helper", "GIT_CONFIG_VALUE_0=",
				"GIT_CONFIG_KEY_1=credential." + g.URL + ".helper",
				"GIT_CONFIG_VALUE_1=" + `!f() { test "$1" = get && echo username=x-access-token && echo "password=$GH_TOKEN"; }; f`,
			}
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("scenario: %v: %s", err, out)
			}
			for _, want := range []string{"reef-fixture", "x-access-token", "source"} {
				if !strings.Contains(string(out), want) {
					t.Errorf("scenario reply lacks %q: %s", want, out)
				}
			}
			if got := gitIn(t, g.root, "--git-dir", filepath.Join(g.root, "octo", "hello.git"), "show", "refs/heads/reef-push:probe.txt"); got != "pushed by the probe" {
				t.Errorf("fake did not push: %q", got)
			}
			if reqs := g.mcpRequests(); len(reqs) == 0 || reqs[len(reqs)-1].Tool != "get_me" || reqs[len(reqs)-1].Auth != auth {
				t.Errorf("MCP requests %+v, want the last to be get_me with %s", reqs, auth)
			}
		})
	}
}
