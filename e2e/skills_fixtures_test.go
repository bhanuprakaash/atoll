package e2e

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"atoll/e2e/fakes/fake"
)

func runSkillFake(t *testing.T, m *machine, p, cwd, extra, prompt, url string, disabled []string) string {
	t.Helper()
	var cmd *exec.Cmd
	if p == "claude" {
		cfg, _ := json.Marshal(map[string]any{"mcpServers": map[string]any{"atoll": map[string]string{"type": "http", "url": url}}})
		cmd = exec.CommandContext(t.Context(), filepath.Join(m.bin, p), "-p", "--input-format", "stream-json", "--output-format", "stream-json", "--mcp-config", string(cfg), "--allowedTools", "mcp__atoll__*", "--add-dir", extra)
		input, _ := json.Marshal(map[string]any{"type": "user", "message": map[string]string{"role": "user", "content": prompt}})
		cmd.Stdin = strings.NewReader(string(input) + "\n")
	} else {
		cmd = exec.CommandContext(t.Context(), filepath.Join(m.bin, p), "app-server")
		for _, path := range disabled {
			cmd.Args = append(cmd.Args, "-c", fmt.Sprintf("skills.config=[{path=%q,enabled=false}]", path))
		}
		var input strings.Builder
		for i, req := range []map[string]any{
			{"method": "initialize", "params": map[string]any{}},
			{"method": "skills/extraRoots/set", "params": map[string]any{"extraRoots": []string{extra}}},
			{"method": "thread/start", "params": map[string]any{"cwd": cwd, "config": map[string]any{"mcp_servers": map[string]any{"atoll": map[string]string{"url": url}}}}},
			{"method": "turn/start", "params": map[string]any{"input": []map[string]string{{"type": "text", "text": prompt}}}},
		} {
			req["id"] = i + 1
			b, _ := json.Marshal(req)
			input.Write(b)
			input.WriteByte('\n')
		}
		cmd.Stdin = strings.NewReader(input.String())
	}
	cmd.Dir = cwd
	cmd.Env = []string{"HOME=" + m.home, "PATH=" + m.path, "CLAUDE_CONFIG_DIR=" + m.providerDir("claude"), "CODEX_HOME=" + m.providerDir("codex")}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("fake %s: %v\n%s", p, err, out)
	}
	return string(out)
}

func TestSkillsFixtures_ProviderScenarios(t *testing.T) {
	t.Parallel()
	for _, p := range _providers {
		for _, mode := range []string{"create", "create-codex", "configure", "edit", "denied", "read", "bots", "use", "prompt-only", "disabled", "worktree", "hold"} {
			t.Run(p+"/"+mode, func(t *testing.T) {
				t.Parallel()
				m := newMachine(t, p)
				cwd := t.TempDir()
				extra := t.TempDir()
				skillRoot := extra
				if p == "claude" {
					skillRoot = filepath.Join(extra, ".claude", "skills")
				}
				path := filepath.Join(skillRoot, "folder-not-name", "SKILL.md")
				var off []string
				if mode == "use" || mode == "disabled" || mode == "worktree" || mode == "hold" {
					writeFile(t, path, fake.GreetingSkill)
				}
				if mode == "disabled" {
					if p == "codex" {
						off = append(off, path)
					} else {
						extra = t.TempDir()
					}
				}
				work := cwd
				if mode == "worktree" {
					gitIn(t, cwd, "init", "-b", "main")
					for _, dir := range []string{".claude", ".agents"} {
						writeFile(t, filepath.Join(cwd, dir, "skills", "repo-skill", "SKILL.md"), "---\nname: repo-skill\ndescription: Repo fixture.\n---\nrepo body\n")
					}
					gitIn(t, cwd, "add", ".")
					gitIn(t, cwd, "commit", "-m", "seed skills")
					work = filepath.Join(t.TempDir(), "work")
					gitIn(t, cwd, "worktree", "add", "-b", "fix/skill-discovery", work)
				}
				var mu sync.Mutex
				calls := []string{}
				stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					var req struct {
						ID     json.RawMessage `json:"id"`
						Method string          `json:"method"`
						Params struct {
							Name      string         `json:"name"`
							Arguments map[string]any `json:"arguments"`
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
						tools := []map[string]any{{"name": "finish_run", "inputSchema": map[string]any{"type": "object"}}}
						if mode != "denied" {
							for _, name := range append(fake.PlatformTools(), fake.ReadTools()...) {
								tools = append(tools, map[string]any{"name": name, "inputSchema": map[string]any{"type": "object"}})
							}
						}
						result = map[string]any{"tools": tools}
					case "tools/call":
						mu.Lock()
						defer mu.Unlock()
						calls = append(calls, req.Params.Name)
						args := req.Params.Arguments
						var value any = "Report saved."
						switch req.Params.Name {
						case "create_skill":
							if args["name"] != "reef-greeting" || args["content"] != fake.GreetingSkill {
								t.Errorf("create skill: %v", args)
							}
							value = map[string]any{"id": "dynamic-skill-id", "name": "reef-greeting", "content": fake.GreetingSkill}
						case "create_bot":
							if mode == "denied" {
								value = "Platform access denied."
								break
							}
							ids, _ := args["skills"].([]any)
							wantProvider := "claude"
							if mode == "create-codex" {
								wantProvider = "codex"
							}
							if len(ids) != 1 || ids[0] != "dynamic-skill-id" || args["name"] != "Lagoon" || args["provider"] != wantProvider {
								t.Errorf("create bot didn't use MCP skill id: %v", args)
							}
							value = map[string]any{"id": "dynamic-bot-id", "name": "Lagoon"}
						case "start_work":
							if args["type"] != "fix" || args["slug"] != "skill-discovery" {
								t.Errorf("start_work: %v", args)
							}
							value = map[string]any{"path": work, "branch": "fix/skill-discovery"}
						case "edit_skill":
							if args["id"] != "dynamic-skill-id" || args["content"] != strings.ReplaceAll(fake.GreetingSkill, "Hello from the lagoon.", "Hello from the reef.") {
								t.Errorf("edit_skill: %v", args)
							}
							value = map[string]any{"id": "dynamic-skill-id", "content": args["content"]}
						case "register_repo":
							if args["name"] != "chief-repo" || args["path"] != cwd {
								t.Errorf("register_repo: %v", args)
							}
							value = map[string]any{"id": "dynamic-repo-id", "path": cwd}
						case "attach_connection":
							if args["bot_id"] != "dynamic-bot-id" || args["connection_id"] != "dynamic-connection-id" {
								t.Errorf("attach_connection: %v", args)
							}
							value = map[string]any{"id": "dynamic-bot-id", "connections": []string{"dynamic-connection-id"}}
						case "edit_bot":
							if args["id"] != "dynamic-bot-id" || args["persona"] != "A lagoon guide." {
								t.Errorf("edit bot: %v", args)
							}
							value = map[string]any{"id": "dynamic-bot-id", "persona": "A lagoon guide."}
						case "list_bots", "list_skills", "list_connections", "list_repos", "list_models":
							value = _readResults[req.Params.Name]
						case "finish_run":
						default:
							t.Errorf("unexpected tool: %s", req.Params.Name)
						}
						text, _ := json.Marshal(value)
						result = map[string]any{"content": []map[string]string{{"type": "text", "text": string(text)}}, "isError": mode == "denied" && req.Params.Name == "create_bot"}
					}
					w.Header().Set("Content-Type", "application/json")
					_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result})
				}))
				defer stub.Close()
				prompt, want := "", ""
				switch mode {
				case "create":
					prompt = "Create reef-greeting and a Lagoon Bot using claude."
					want = "Created reef-greeting and Lagoon."
				case "create-codex":
					prompt = "Create reef-greeting and a Lagoon Bot using codex."
					want = "Created reef-greeting and Lagoon."
				case "configure":
					payload, _ := json.Marshal(map[string]string{"bot_id": "dynamic-bot-id", "skill_id": "dynamic-skill-id", "repo_path": cwd, "connection_id": "dynamic-connection-id"})
					prompt = "Configure platform fixtures. " + string(payload)
					want = "Edited the skill, registered the repo and attached the connection."
				case "edit":
					prompt = "Edit the Lagoon Bot persona. bot_id=dynamic-bot-id"
					want = "Updated the Bot persona."
				case "denied":
					prompt = "Create a Bot without platform access."
					want = "No platform tools available."
				case "read":
					prompt = _readAllPrompt
					reply, _ := json.Marshal(_readResults)
					encoded, _ := json.Marshal(string(reply))
					want = string(encoded[1 : len(encoded)-1])
				case "bots":
					prompt = _whichBotsPrompt
					want = "Bots: Chief, Lagoon."
				case "hold":
					prompt = "Wait, then list your skills and use reef-greeting."
					want = "Used reef-greeting: Hello from the lagoon."
				case "worktree":
					prompt = "Start work, then list your skills and use reef-greeting."
					want = "Skills: reef-greeting, repo-skill. Used reef-greeting: Hello from the lagoon."
				case "use":
					prompt = _useSkills
					want = "Used reef-greeting: Hello from the lagoon."
				default:
					prompt = _useSkills + "\n" + fake.GreetingSkill
					want = "No reef-greeting skill available."
				}
				var released chan error
				if mode == "hold" {
					released = make(chan error, 1)
					go func() {
						deadline := time.NewTimer(10 * time.Second)
						defer deadline.Stop()
						tick := time.NewTicker(20 * time.Millisecond)
						defer tick.Stop()
						for {
							select {
							case <-t.Context().Done():
								released <- t.Context().Err()
								return
							case <-deadline.C:
								released <- fmt.Errorf("fake never signaled skills ready")
								return
							case <-tick.C:
								paths, _ := filepath.Glob(filepath.Join(m.bin, p+".ready.*"))
								if len(paths) > 0 {
									released <- os.WriteFile(paths[0]+".release", nil, 0o600)
									return
								}
							}
						}
					}()
				}
				out := runSkillFake(t, m, p, cwd, extra, prompt, stub.URL, off)
				if released != nil {
					if err := <-released; err != nil {
						t.Fatal(err)
					}
				}
				if !strings.Contains(out, want) {
					t.Fatalf("fake didn't derive expected reply %q: %s", want, out)
				}
				mu.Lock()
				defer mu.Unlock()
				expected := []string{"finish_run"}
				if mode == "denied" {
					expected = []string{"create_bot", "finish_run"}
				}
				if mode == "create" || mode == "create-codex" {
					expected = []string{"create_skill", "create_bot", "finish_run"}
				}
				if mode == "worktree" {
					expected = []string{"start_work", "finish_run"}
				}
				if mode == "configure" {
					expected = []string{"edit_skill", "register_repo", "attach_connection", "finish_run"}
				}
				if mode == "edit" {
					expected = []string{"edit_bot", "finish_run"}
				}
				if mode == "read" {
					expected = append(fake.ReadTools(), "finish_run")
				}
				if mode == "bots" {
					expected = []string{"list_bots", "finish_run"}
				}
				if !slices.Equal(calls, expected) {
					t.Fatalf("calls %v, want %v", calls, expected)
				}
			})
		}
	}
}
