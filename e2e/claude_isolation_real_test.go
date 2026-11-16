//go:build realclaude

package e2e

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// Run with claude on PATH and a signed-in test home (see realClaude):
// go test -tags realclaude -run 'TestClaudeIsolation_Real|TestNoRepoShell_Real' ./e2e

const _realBearer = "reef-e2e-bearer-0"

// _standInTools are the tools of the MCP server that stands in for Reef's.
var _standInTools = []string{"start_work", "track_mr", "remember", "approve"}

// probeMCP is an MCP server on 127.0.0.1 with the given tools, which records the Authorization
// header of every request.
func probeMCP(t *testing.T, tools ...string) (url string, auth func() []string) {
	t.Helper()
	var mu sync.Mutex
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params struct {
				ProtocolVersion string `json:"protocolVersion"`
			} `json:"params"`
		}
		if r.Method != http.MethodPost || json.NewDecoder(r.Body).Decode(&req) != nil {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		mu.Lock()
		seen = append(seen, r.Header.Get("Authorization"))
		mu.Unlock()
		result := map[string]any{}
		switch req.Method {
		case "initialize":
			result = map[string]any{"protocolVersion": req.Params.ProtocolVersion, "capabilities": map[string]any{"tools": map[string]any{}}, "serverInfo": map[string]string{"name": "probe", "version": "1"}}
		case "tools/list":
			list := []any{}
			for _, name := range tools {
				list = append(list, map[string]any{"name": name, "description": name, "inputSchema": map[string]any{"type": "object", "properties": map[string]any{}}})
			}
			result = map[string]any{"tools": list}
		}
		if len(req.ID) == 0 {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result})
	}))
	t.Cleanup(srv.Close)
	return srv.URL + "/mcp", func() []string {
		mu.Lock()
		defer mu.Unlock()
		return slices.Clone(seen)
	}
}

// realClaude returns claude on PATH and the environment to run it in: the caller's, without its
// ANTHROPIC_* and CLAUDE* entries, with CLAUDE_CONFIG_DIR at Reef's own Claude folder
// (providers/claude in Reef's data folder, found as cmd/atoll dataDir finds it) and HOME at the
// user's real home, on purpose: on macOS Reef's sign-in is in the user's login keychain, which the
// run's temporary HOME does not have. The sign-in is used in place and copied nowhere, and no
// setting, hook or CLAUDE.md of the developer's own Claude Code reaches the test.
func realClaude(t *testing.T) (string, []string) {
	t.Helper()
	claude, err := exec.LookPath("claude")
	if err != nil {
		t.Fatal("no claude on PATH: run this test only on a machine with Claude Code installed")
	}
	data := os.Getenv("ATOLL_DATA_DIR")
	switch {
	case data != "":
		data, err = filepath.Abs(data)
		if err != nil {
			t.Fatal(err)
		}
	case runtime.GOOS == "darwin":
		data = filepath.Join(_userHome, "Library", "Application Support", "Atoll")
	default:
		cfg := os.Getenv("XDG_CONFIG_HOME")
		if cfg == "" {
			cfg = filepath.Join(_userHome, ".config")
		}
		data = filepath.Join(cfg, "Atoll")
	}
	dir := filepath.Join(data, "providers", "claude")
	var env []string
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "ANTHROPIC_") && !strings.HasPrefix(kv, "CLAUDE") && !strings.HasPrefix(kv, "HOME=") {
			env = append(env, kv)
		}
	}
	env = append(env, "CLAUDE_CONFIG_DIR="+dir, "HOME="+_userHome)
	notSignedIn := "Reef's Claude is not signed in (looked in %s: %v).\nSign in to Claude in Reef (Settings → Connections), then run the test again."
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf(notSignedIn, dir, err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	status := exec.CommandContext(ctx, claude, "auth", "status")
	status.Env = env
	if err := status.Run(); err != nil {
		t.Fatalf(notSignedIn, dir, err)
	}
	return claude, env
}

func TestClaudeIsolation_RealClaudeReachesMCPServers(t *testing.T) {
	claude, env := realClaude(t)
	m := ownerSetupMachine(t)
	m.serve(t)
	prefix := filepath.Join(m.data, "bin", "claude-shell-prefix")
	url, auth := probeMCP(t)

	config, _ := json.Marshal(map[string]any{"mcpServers": map[string]any{"probe": map[string]any{
		"type": "http", "url": url, "headers": map[string]string{"Authorization": "Bearer ${ATOLL_MCP_0}"},
	}}})
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, claude, "-p", "--output-format", "stream-json", "--verbose", "--model", "haiku",
		"--strict-mcp-config", "--mcp-config", string(config), "--setting-sources", "project", "--allowedTools", "Bash")
	cmd.Stdin = strings.NewReader("Run this exact Bash command and reply with its output only: env | grep -c -E '^(CLAUDE|ATOLL_MCP)'")
	cmd.Dir = t.TempDir()
	cmd.Env = append(env, "ATOLL_MCP_0="+_realBearer, "CLAUDE_CODE_SHELL_PREFIX="+prefix)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("claude: %v\n%s", err, out)
	}

	seen := auth()
	if len(seen) == 0 {
		t.Error("the MCP server got no request")
	}
	for _, a := range seen {
		if a != "Bearer "+_realBearer {
			t.Errorf("the MCP server got Authorization %q, want %q on every request", a, "Bearer "+_realBearer)
		}
	}
	var result string
	for _, line := range strings.Split(string(out), "\n") {
		var l struct{ Type, Result string }
		if json.Unmarshal([]byte(line), &l) == nil && l.Type == "result" {
			result = strings.TrimSpace(l.Result)
		}
	}
	if result != "0" {
		t.Errorf("the Bash tool counted %q CLAUDE or ATOLL_MCP variables in its env, want 0\n%s", result, out)
	}
}

// TestClaudeIsolation_RealClaudeToolList starts the real claude with the argv and the Reef env
// atoll builds for a Bot in each Mode (captured from the fake), with the atoll MCP server swapped
// for a stand-in, and compares the tools of its init event with the allowlist plus the stand-in's
// tools. A Claude Code that adds a built-in tool does not change the list, because the allowlist
// closes it; one that stops listing a tool, or a flag that stops working, fails here.
func TestClaudeIsolation_RealClaudeToolList(t *testing.T) {
	claude, env := realClaude(t)
	m := ownerSetupMachine(t)
	s := m.serve(t)
	for _, mode := range _modes {
		var b bot
		s.do(t, "POST", "/api/bots", map[string]string{"name": "Mode " + mode, "persona": "x", "provider": "claude", "mode": mode}, &b, http.StatusCreated)
		th := s.newThread(t, b)
		s.finished(t, s.events(t, th), s.send(t, th, _keepPrompt).ID)
	}
	captured := m.sessions(t, "claude")
	if len(captured) != len(_modes) {
		t.Fatalf("claude ran %d times, want %d", len(captured), len(_modes))
	}

	want := []string{"Bash", "Edit", "Glob", "Grep", "NotebookEdit", "Read", "Skill", "WebFetch", "WebSearch", "Write"}
	for _, name := range _standInTools {
		if name != "approve" { // claude keeps the --permission-prompt-tool out of its tool list
			want = append(want, "mcp__atoll__"+name)
		}
	}
	slices.Sort(want)

	for i, mode := range _modes {
		t.Run(mode, func(t *testing.T) {
			c := captured[i]
			url, _ := probeMCP(t, _standInTools...)
			argv := slices.Clone(c.argv)
			j := slices.Index(argv, "--mcp-config")
			if j < 0 {
				t.Fatalf("argv has no --mcp-config: %q", argv)
			}
			var config struct {
				McpServers map[string]map[string]any `json:"mcpServers"`
			}
			if err := json.Unmarshal(c.mcp, &config); err != nil || config.McpServers["atoll"] == nil {
				t.Fatalf("MCP config %s has no atoll server (%v)", c.mcp, err)
			}
			config.McpServers["atoll"]["url"] = url
			delete(config.McpServers["atoll"], "headers")
			b, _ := json.Marshal(config)
			argv[j+1] = string(b)

			ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
			defer cancel()
			cmd := exec.CommandContext(ctx, claude, argv...)
			cmd.Stdin = strings.NewReader(`{"type":"user","message":{"role":"user","content":"Reply with just: ok"}}` + "\n")
			cmd.Dir = t.TempDir()
			cmd.Env = slices.Clone(env)
			for k, v := range c.env {
				if k == "CLAUDE_CODE_SHELL_PREFIX" || k == "ENABLE_CLAUDEAI_MCP_SERVERS" || strings.HasPrefix(k, "ATOLL_MCP_") {
					cmd.Env = append(cmd.Env, k+"="+v)
				}
			}
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("claude %q: %v\n%s", argv, err, out)
			}
			var got []string
			for _, line := range strings.Split(string(out), "\n") {
				var l struct {
					Type, Subtype string
					Tools         []string
				}
				if json.Unmarshal([]byte(line), &l) == nil && l.Type == "system" && l.Subtype == "init" {
					got = l.Tools
				}
			}
			slices.Sort(got)
			if !slices.Equal(got, want) {
				t.Errorf("init tools in %s\n%q\nwant\n%q", mode, got, want)
			}
		})
	}
}

// TestNoRepoShell_RealClaudeCannotReadRegisteredRepo starts the real claude with the argv and env
// atoll builds for a Bot with no repo access (captured from the fake) and asks it to read a
// registered repo's README by its absolute path through Bash (`head`, and python, which no
// permission rule covers) and Read. Ask is left out: the
// stand-in MCP server answers no approval, so every call would be refused for that reason.
func TestNoRepoShell_RealClaudeCannotReadRegisteredRepo(t *testing.T) {
	claude, env := realClaude(t)
	m := ownerSetupMachine(t)
	s := m.serve(t)
	w := newRepoWorld(t, m, s)
	// Another Bot's thread on beta gets its read-only checkout at its first run.
	s.finished(t, s.events(t, w.betaThread), s.send(t, w.betaThread, _hey).ID)
	checkout := filepath.Join(m.data, "threads", w.betaThread.ID, "README.md")

	for _, mode := range []string{"auto", "full"} {
		t.Run(mode, func(t *testing.T) {
			none := w.bot(t, "None-"+mode, []string{}, mode)
			_, _, c := w.session(t, none, "", _hey)
			if err := os.WriteFile(filepath.Join(c.cwd, "control.txt"), []byte("control-line\n"), 0o600); err != nil {
				t.Fatal(err)
			}

			url, _ := probeMCP(t, _standInTools...)
			argv := slices.Clone(c.argv)
			j := slices.Index(argv, "--mcp-config")
			if j < 0 {
				t.Fatalf("argv has no --mcp-config: %q", argv)
			}
			var config struct {
				McpServers map[string]map[string]any `json:"mcpServers"`
			}
			if err := json.Unmarshal(c.mcp, &config); err != nil || config.McpServers["atoll"] == nil {
				t.Fatalf("MCP config %s has no atoll server (%v)", c.mcp, err)
			}
			config.McpServers["atoll"]["url"] = url
			delete(config.McpServers["atoll"], "headers")
			b, _ := json.Marshal(config)
			argv[j+1] = string(b)

			readme := filepath.Join(w.beta.Path, "README.md")
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
			defer cancel()
			cmd := exec.CommandContext(ctx, claude, argv...)
			ask := "Do these five things with tools, one tool call each, and say what each returned or that it failed. " +
				"1. Bash: head -1 " + readme + " " +
				"2. Bash: python3 -c \"print(open('" + readme + "').readline())\" " +
				"3. Read the file " + readme + " " +
				"4. Bash: python3 -c \"print(open('" + checkout + "').readline())\" " +
				"5. Bash: cat control.txt"
			cmd.Stdin = strings.NewReader(`{"type":"user","message":{"role":"user","content":` + string(mustJSON(ask)) + `}}` + "\n")
			cmd.Dir = c.cwd
			cmd.Env = slices.Clone(env)
			for k, v := range c.env {
				if k == "CLAUDE_CODE_SHELL_PREFIX" || k == "ENABLE_CLAUDEAI_MCP_SERVERS" || strings.HasPrefix(k, "ATOLL_MCP_") {
					cmd.Env = append(cmd.Env, k+"="+v)
				}
			}
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("claude %q: %v\n%s", argv, err, out)
			}
			var results []string
			for _, line := range strings.Split(string(out), "\n") {
				var l struct {
					Type    string
					Message struct {
						Content json.RawMessage
					}
				}
				if json.Unmarshal([]byte(line), &l) == nil && l.Type == "user" {
					results = append(results, string(l.Message.Content))
				}
			}
			all := strings.Join(results, "\n")
			if strings.Contains(all, _readmeMarker) {
				t.Errorf("a tool result held the README of a repo the Bot lacks:\n%s", all)
			}
			if !strings.Contains(all, "control-line") {
				t.Errorf("Bash did not read the control file in the Bot's own folder, so a refusal proves nothing:\n%s", all)
			}
		})
	}
}
