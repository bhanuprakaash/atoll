package e2e

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"atoll/e2e/fakes/fake"
)

// Proves the scripts and exact prompt logging independently of the missing memory APIs.
func TestMemoryFixtures_ProviderScenarios(t *testing.T) {
	t.Parallel()
	for _, p := range _providers {
		for _, mode := range []string{"save", "save-user", "recall", "check", "resume", "retire"} {
			t.Run(p+"/"+mode, func(t *testing.T) {
				t.Parallel()
				m := newMachine(t, p)
				item := memoryItem{ID: "memory-fixture", Kind: "decision", Body: fake.DecisionBody, Why: "The fixture's own dynamic reason.", Scope: "repo", Status: "accepted"}
				var mu sync.Mutex
				calls := map[string]int{}
				stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					var req struct {
						ID     json.RawMessage `json:"id"`
						Method string          `json:"method"`
						Params struct {
							Name      string          `json:"name"`
							Arguments json.RawMessage `json:"arguments"`
						} `json:"params"`
					}
					if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
						t.Error(err)
						w.WriteHeader(400)
						return
					}
					if len(req.ID) == 0 {
						w.WriteHeader(202)
						return
					}
					var result any = map[string]any{}
					switch req.Method {
					case "initialize":
						result = map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{}, "serverInfo": map[string]string{"name": "fixture", "version": "0"}}
					case "tools/list":
						result = map[string]any{"tools": []any{}}
					case "tools/call":
						mu.Lock()
						defer mu.Unlock()
						calls[req.Params.Name]++
						var args map[string]any
						if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
							t.Error(err)
						}
						var value any = item
						switch req.Params.Name {
						case "remember":
							scope := "repo"
							if mode == "save-user" {
								scope = "user"
							}
							if args["scope"] != scope || args["kind"] != "decision" || args["body"] != fake.DecisionBody || args["why"] != fake.DecisionWhy {
								t.Errorf("remember args: %s", req.Params.Arguments)
							}
							saved := item
							saved.Scope = scope
							saved.Why = fake.DecisionWhy
							value = saved
						case "recall":
							if args["query"] != "SQLite journal" || args["limit"] != float64(8) {
								t.Errorf("recall args: %s", req.Params.Arguments)
							}
							value = []memoryItem{item}
						case "read_memory", "retire_memory":
							if args["id"] != item.ID {
								t.Errorf("memory id: %s", req.Params.Arguments)
							}
							if req.Params.Name == "retire_memory" {
								retired := item
								retired.Status = "retired"
								value = retired
							}
						default:
							t.Errorf("unexpected MCP call %s", req.Params.Name)
						}
						text, _ := json.Marshal(value)
						result = map[string]any{"content": []map[string]string{{"type": "text", "text": string(text)}}}
					}
					w.Header().Set("Content-Type", "application/json")
					_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result})
				}))
				defer stub.Close()
				prompt := map[string]string{"save": _saveMemory, "save-user": "Save the SQLite journal decision in user memory.", "recall": _recallMemory, "check": _checkMemory, "resume": _resumeMemory, "retire": _retireMemory}[mode]
				prompt = "Memory items:\n- [" + item.ID + "] decision (repo): " + item.Body + "\n\nMessage:\n" + prompt
				var cmd *exec.Cmd
				if p == "claude" {
					cfg, _ := json.Marshal(map[string]any{"mcpServers": map[string]any{"atoll": map[string]string{"type": "http", "url": stub.URL}}})
					cmd = exec.CommandContext(t.Context(), filepath.Join(m.bin, p), "-p", "--input-format", "stream-json", "--output-format", "stream-json", "--append-system-prompt", "system addition\nexact bytes", "--mcp-config", string(cfg), "--allowedTools", "mcp__atoll__*")
					input, _ := json.Marshal(map[string]any{"type": "user", "message": map[string]string{"role": "user", "content": prompt}})
					cmd.Stdin = strings.NewReader(string(input) + "\n")
					if mode == "resume" {
						cmd.Args = append(cmd.Args, "--resume", "e5249980-178d-4e8c-8829-3c64eb27639f")
					}
				} else {
					cmd = exec.CommandContext(t.Context(), filepath.Join(m.bin, p), "app-server")
					var input strings.Builder
					for i, req := range []map[string]any{
						{"method": "initialize", "params": map[string]any{}},
						{"method": "thread/start", "params": map[string]any{"developerInstructions": "system addition\nexact bytes", "config": map[string]any{"mcp_servers": map[string]any{"atoll": map[string]string{"url": stub.URL}}}}},
						{"method": "turn/start", "params": map[string]any{"input": []map[string]string{{"type": "text", "text": prompt}}}},
					} {
						if mode == "resume" && req["method"] == "thread/start" {
							req["method"] = "thread/resume"
							req["params"].(map[string]any)["threadId"] = _codexThreadID
						}
						req["id"] = i + 1
						b, _ := json.Marshal(req)
						input.Write(b)
						input.WriteByte('\n')
					}
					cmd.Stdin = strings.NewReader(input.String())
				}
				cmd.Dir = t.TempDir()
				cmd.Env = []string{"HOME=" + m.home, "PATH=" + m.path}
				out, err := cmd.CombinedOutput()
				if err != nil {
					t.Fatalf("scenario: %v: %s", err, out)
				}
				want := map[string]string{"save": "Saved SQLite decision.", "save-user": "Saved SQLite decision.", "recall": item.Body + " " + item.Why, "check": "Checked SQLite memory.", "resume": "Checked SQLite memory.", "retire": "Retired SQLite decision."}[mode]
				if !strings.Contains(string(out), want) {
					t.Fatalf("missing dynamic reply %q: %s", want, out)
				}
				if lastPrompt(t, m, p, "system") != "system addition\nexact bytes" || lastPrompt(t, m, p, "user") != prompt {
					t.Fatal("prompt log changed received bytes")
				}
				mu.Lock()
				defer mu.Unlock()
				for _, tool := range map[string][]string{"save": {"remember"}, "save-user": {"remember"}, "recall": {"recall", "read_memory"}, "retire": {"recall", "read_memory", "retire_memory"}}[mode] {
					if calls[tool] != 1 {
						t.Errorf("%s calls: %v", tool, calls)
					}
				}
			})
		}
	}
}
