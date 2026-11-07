package e2e

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"atoll/e2e/fakes/fake"
)

// Contract: mcp-servers-contract.md.

const (
	_mcpEnvName      = "FAKE_MCP_TOKEN"
	_mcpEnvSecret    = "fake-mcp-env-1"
	_mcpHeaderName   = "X-Fake-Key"
	_mcpHeaderSecret = "fake-mcp-key-1"
	_mcpPromptHead   = "Use MCP tools. "
)

type mcpServer struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Kind        string   `json:"kind"`
	Command     string   `json:"command"`
	Args        []string `json:"args"`
	URL         string   `json:"url"`
	EnvNames    []string `json:"env_names"`
	HeaderNames []string `json:"header_names"`
}

type mcpBot struct {
	ID         string   `json:"id"`
	MCPServers []string `json:"mcp_servers"`
}

// httpMCP is a remote MCP server (streamable HTTP, JSON replies) that answers only requests
// carrying the right header.
type httpMCP struct {
	*httptest.Server
	mu       sync.Mutex
	called   int // tools/call requests with the header
	rejected int // requests without it
}

func newHTTPMCP(t *testing.T) *httpMCP {
	t.Helper()
	f := &httpMCP{}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if r.Header.Get(_mcpHeaderName) != _mcpHeaderSecret {
			f.mu.Lock()
			f.rejected++
			f.mu.Unlock()
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		var req struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		if len(req.ID) == 0 {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		var result any = map[string]any{}
		switch req.Method {
		case "initialize":
			w.Header().Set("Mcp-Session-Id", "fake-session")
			result = map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{"tools": map[string]any{}},
				"serverInfo": map[string]string{"name": "fake-http", "version": "0"}}
		case "tools/list":
			result = map[string]any{"tools": []any{map[string]any{"name": "answer", "description": "Answers a fixed text.",
				"inputSchema": map[string]any{"type": "object", "properties": map[string]any{}}}}}
		case "tools/call":
			f.mu.Lock()
			f.called++
			f.mu.Unlock()
			result = map[string]any{"content": []map[string]string{{"type": "text", "text": "http-answer-42"}}}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result})
	}))
	t.Cleanup(f.Close)
	return f
}

func (f *httpMCP) calls() (called, rejected int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.called, f.rejected
}

func (f *httpMCP) url() string { return f.URL + "/mcp" }

// installMCPStdio copies the local fake MCP server into its own folder, so its log counts only
// this test's starts, and returns its path.
func installMCPStdio(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(_fakes, "mcpstdio"))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "mcpstdio")
	if err := os.WriteFile(path, b, 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

// envDigest is what the local fake MCP server's inspect tool answers for a variable: the first 12
// hex digits of its SHA-256, so the answer proves the value arrived without holding it.
func envDigest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])[:12]
}

// starts is how many times the local fake MCP server at path was started.
func starts(path string) int {
	b, _ := os.ReadFile(path + ".log")
	return strings.Count(string(b), "\n")
}

// secretEntry reads an OS secret store entry with the OS tool, not through Atoll.
func secretEntry(username string) (string, bool) {
	cmd := exec.Command("secret-tool", "lookup", "service", "atoll", "username", username)
	if runtime.GOOS == "darwin" {
		cmd = exec.Command("security", "find-generic-password", "-s", "atoll", "-a", username, "-w")
	}
	out, err := cmd.Output()
	return strings.TrimSpace(string(out)), err == nil
}

func clearSecretEntry(username string) {
	cmd := exec.Command("secret-tool", "clear", "service", "atoll", "username", username)
	if runtime.GOOS == "darwin" {
		cmd = exec.Command("security", "delete-generic-password", "-s", "atoll", "-a", username)
	}
	_ = cmd.Run()
}

// mcpFixture is a machine with one provider, the two fake MCP servers and both added to the
// library: docs-local (stdio, with an env secret) and docs-remote (HTTP, with a header secret).
type mcpFixture struct {
	m             *machine
	s             *server
	p             string
	http          *httpMCP
	stdio         string
	local, remote mcpServer
}

func newMCPFixture(t *testing.T, p string) *mcpFixture {
	t.Helper()
	requireSecretStore(t)
	m := newMachine(t, p)
	m.signIn(t, p)
	for _, key := range []string{"DBUS_SESSION_BUS_ADDRESS", "XDG_RUNTIME_DIR", "XDG_DATA_HOME"} {
		if v := os.Getenv(key); v != "" {
			m.env = append(m.env, key+"="+v)
		}
	}
	f := &mcpFixture{m: m, p: p, http: newHTTPMCP(t), stdio: installMCPStdio(t)}
	f.s = m.serve(t)
	f.s.do(t, "POST", "/api/mcp-servers", map[string]any{
		"name": "docs-local", "command": f.stdio, "args": []string{"--tag", "one"}, "env": map[string]string{_mcpEnvName: _mcpEnvSecret},
	}, &f.local, http.StatusCreated)
	f.s.do(t, "POST", "/api/mcp-servers", map[string]any{
		"name": "docs-remote", "url": f.http.url(), "headers": map[string]string{_mcpHeaderName: _mcpHeaderSecret},
	}, &f.remote, http.StatusCreated)
	t.Cleanup(func() {
		clearSecretEntry("mcp:" + f.local.ID + ":env:" + _mcpEnvName)
		clearSecretEntry("mcp:" + f.remote.ID + ":header:" + _mcpHeaderName)
	})
	return f
}

// bot creates a Bot on the fixture's provider that picks the given library servers.
func (f *mcpFixture) bot(t *testing.T, name string, servers ...string) bot {
	t.Helper()
	var b bot
	f.s.do(t, "POST", "/api/bots", map[string]any{
		"name": name, "persona": "Uses documentation servers.", "provider": f.p, "mcp_servers": append([]string{}, servers...),
	}, &b, http.StatusCreated)
	return b
}

type approvalSeen struct{ id, tool, input string }

// use sends the calls as one message and answers every approval the run opens with approve,
// until it finishes.
func (f *mcpFixture) use(t *testing.T, th thread, ch <-chan event, approve bool, calls ...fake.MCPCall) (runInfo, []approvalSeen) {
	t.Helper()
	payload, _ := json.Marshal(calls)
	r := f.s.send(t, th, _mcpPromptHead+string(payload))
	return f.answerAll(t, ch, r.ID, approve, nil)
}

// answerAll answers the approvals of a run, after those in seen, until its run_finished.
func (f *mcpFixture) answerAll(t *testing.T, ch <-chan event, runID string, approve bool, seen []approvalSeen) (runInfo, []approvalSeen) {
	t.Helper()
	timeout := time.After(60 * time.Second)
	for {
		select {
		case e, ok := <-ch:
			if !ok {
				t.Fatalf("event stream closed before run %s finished", runID)
			}
			if e.Data["run_id"] != runID {
				continue
			}
			switch e.Type {
			case "approval":
				a := approvalSeen{tool: asString(e.Data["tool"])}
				a.id = asString(e.Data["id"])
				input, _ := json.Marshal(e.Data["input"])
				a.input = string(input)
				seen = append(seen, a)
				f.s.do(t, "POST", "/api/approvals/"+a.id, map[string]bool{"approve": approve}, nil, http.StatusNoContent)
			case "run_finished":
				return f.s.run(t, runID), seen
			}
		case <-timeout:
			t.Fatalf("run %s did not finish in 60s; approvals %+v", runID, seen)
		}
	}
}

func asString(v any) string {
	s, _ := v.(string)
	return s
}

func mcpCall(server, tool string, args string) fake.MCPCall {
	return fake.MCPCall{Server: server, Tool: tool, Args: json.RawMessage(args)}
}

// replyLines is the reply a run of the mcp-use scenario should have, one line per call.
func replyLines(lines ...string) string { return strings.Join(lines, "\n") }
