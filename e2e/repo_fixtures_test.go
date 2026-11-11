package e2e

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// These checks prove the new fake scenarios can execute past the still-missing product APIs.
func TestRepoFixtures_ProviderScenarios(t *testing.T) {
	t.Parallel()
	for _, provider := range []string{"claude", "codex"} {
		for _, scenario := range []struct{ mode, prompt, want string }{
			{"fix", _fixRepoPrompt, "Fixed the seeded bug."},
			{"question", _questionRepoPrompt, ": 41"},
			{"uncommitted", _unfinishedRepoPrompt, "Left tracked and untracked edits unfinished."},
			{"resume", _resumeRepoPrompt, ": 42"},
		} {
			t.Run(provider+"/"+scenario.mode, func(t *testing.T) {
				t.Parallel()
				m := newMachine(t, provider, "glab")
				repoPath := t.TempDir()
				origin := filepath.Join(t.TempDir(), "origin.git")
				gitIn(t, repoPath, "init", "--bare", origin)
				gitIn(t, repoPath, "init", "-b", "main")
				writeFile(t, filepath.Join(repoPath, "answer.txt"), "41\n")
				gitIn(t, repoPath, "add", "answer.txt")
				gitIn(t, repoPath, "commit", "-m", "seed bug")
				gitIn(t, repoPath, "remote", "add", "origin", origin)
				gitIn(t, repoPath, "push", "-u", "origin", "main")
				work := filepath.Join(t.TempDir(), "work")
				gitIn(t, repoPath, "worktree", "add", "-b", "fix/seeded-bug", work)
				cwd := repoPath
				if scenario.mode == "resume" {
					writeFile(t, filepath.Join(work, "answer.txt"), "42\n")
					gitIn(t, work, "add", "answer.txt")
					gitIn(t, work, "commit", "-m", "prior fix")
					cwd = work
				}
				var mu sync.Mutex
				calls := map[string]int{}
				stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					var req struct {
						ID     json.RawMessage `json:"id"`
						Method string          `json:"method"`
						Params struct {
							Name string `json:"name"`
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
						calls[req.Params.Name]++
						mu.Unlock()
						text := "saved"
						if req.Params.Name == "start_work" {
							text = fmt.Sprintf(`{"path":%q,"branch":"fix/seeded-bug"}`, work)
						}
						result = map[string]any{"content": []map[string]string{{"type": "text", "text": text}}}
					}
					w.Header().Set("Content-Type", "application/json")
					_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result})
				}))
				defer stub.Close()
				var cmd *exec.Cmd
				if provider == "claude" {
					config, _ := json.Marshal(map[string]any{"mcpServers": map[string]any{"atoll": map[string]string{"type": "http", "url": stub.URL}}})
					cmd = exec.CommandContext(t.Context(), filepath.Join(m.bin, provider), "-p", "--input-format", "stream-json", "--output-format", "stream-json", "--mcp-config", string(config), "--allowedTools", "mcp__atoll__*")
					input, _ := json.Marshal(map[string]any{"type": "user", "message": map[string]string{"role": "user", "content": scenario.prompt}})
					cmd.Stdin = strings.NewReader(string(input) + "\n")
				} else {
					cmd = exec.CommandContext(t.Context(), filepath.Join(m.bin, provider), "app-server")
					var input strings.Builder
					for i, req := range []map[string]any{
						{"method": "initialize", "params": map[string]any{}},
						{"method": "thread/start", "params": map[string]any{"cwd": cwd, "config": map[string]any{"mcp_servers": map[string]any{"atoll": map[string]string{"url": stub.URL}}}}},
						{"method": "turn/start", "params": map[string]any{"input": []map[string]string{{"type": "text", "text": scenario.prompt}}}},
					} {
						req["id"] = i + 1
						b, _ := json.Marshal(req)
						input.Write(b)
						input.WriteByte('\n')
					}
					cmd.Stdin = strings.NewReader(input.String())
				}
				cmd.Dir = cwd
				cmd.Env = []string{"HOME=" + m.home, "PATH=" + m.path, "GITLAB_TOKEN=fixture-token"}
				out, err := cmd.CombinedOutput()
				if err != nil {
					t.Fatalf("scenario: %v: %s", err, out)
				}
				if !strings.Contains(string(out), scenario.want) {
					t.Fatalf("missing scenario reply %q: %s", scenario.want, out)
				}
				mu.Lock()
				defer mu.Unlock()
				if calls["finish_run"] != 1 {
					t.Fatalf("finish_run calls: %v", calls)
				}
				if scenario.mode == "fix" {
					if calls["start_work"] != 1 || calls["track_mr"] != 1 {
						t.Fatalf("MCP calls: %v", calls)
					}
					if gitIn(t, repoPath, "--git-dir", origin, "show", "fix/seeded-bug:answer.txt") != "42" {
						t.Fatal("fake did not push fix")
					}
				}
				if scenario.mode == "uncommitted" {
					if b, err := os.ReadFile(filepath.Join(work, "unfinished.txt")); err != nil || string(b) != "keep this untracked work\n" {
						t.Fatal("fake did not leave untracked work")
					}
					if gitIn(t, work, "status", "--porcelain") == "" {
						t.Fatal("fake did not leave changes")
					}
				}
			})
		}
	}
}
