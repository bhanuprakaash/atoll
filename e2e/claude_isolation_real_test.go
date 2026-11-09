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
	"strings"
	"sync"
	"testing"
	"time"
)

// Contract: claude-isolation-contract.md. Run with a signed-in claude on PATH:
// go test -tags realclaude -run TestClaudeIsolation_RealClaudeReachesMCPServers ./e2e

const _realBearer = "reef-e2e-bearer-0"

func TestClaudeIsolation_RealClaudeReachesMCPServers(t *testing.T) {
	claude, err := exec.LookPath("claude")
	if err != nil {
		t.Fatal("no claude on PATH: run this test only on a machine with a signed-in Claude Code")
	}
	m := ownerSetupMachine(t)
	m.serve(t)
	prefix := filepath.Join(m.data, "bin", "claude-shell-prefix")

	var mu sync.Mutex
	var auth []string
	mcp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
		auth = append(auth, r.Header.Get("Authorization"))
		mu.Unlock()
		result := map[string]any{}
		switch req.Method {
		case "initialize":
			result = map[string]any{"protocolVersion": req.Params.ProtocolVersion, "capabilities": map[string]any{"tools": map[string]any{}}, "serverInfo": map[string]string{"name": "probe", "version": "1"}}
		case "tools/list":
			result = map[string]any{"tools": []any{}}
		}
		if len(req.ID) == 0 {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result})
	}))
	t.Cleanup(mcp.Close)

	config, _ := json.Marshal(map[string]any{"mcpServers": map[string]any{"probe": map[string]any{
		"type": "http", "url": mcp.URL + "/mcp", "headers": map[string]string{"Authorization": "Bearer ${ATOLL_MCP_0}"},
	}}})
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, claude, "-p", "--output-format", "stream-json", "--verbose", "--model", "haiku",
		"--strict-mcp-config", "--mcp-config", string(config), "--setting-sources", "project", "--allowedTools", "Bash")
	cmd.Stdin = strings.NewReader("Run this exact Bash command and reply with its output only: env | grep -c -E '^(CLAUDE|ATOLL_MCP)'")
	cmd.Dir = t.TempDir()
	cmd.Env = append(os.Environ(), "ATOLL_MCP_0="+_realBearer, "CLAUDE_CODE_SHELL_PREFIX="+prefix)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("claude: %v\n%s", err, out)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(auth) == 0 {
		t.Error("the MCP server got no request")
	}
	for _, a := range auth {
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
