package e2e

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"atoll/e2e/fakes/fake"
)

var _updateGolden = flag.Bool("update", false, "rewrite prompt goldens from observed provider input; human use only")

const (
	_saveMemory   = "Save the SQLite journal decision in repo memory."
	_recallMemory = "Recall the SQLite journal decision and explain why."
	_checkMemory  = "Inspect SQLite memory for this message."
	_resumeMemory = "Recheck SQLite memory for this message."
	_retireMemory = "Retire the SQLite journal decision."
)

type memoryItem = fake.MemoryItem

type promptRecord struct {
	PID  int    `json:"pid"`
	Kind string `json:"kind"`
	Text string `json:"text"`
}

func memorySetup(t *testing.T, p string) (*repoFixture, bot) {
	t.Helper()
	f := newRepoFixture(t, p)
	f.s.do(t, "PUT", "/api/settings", map[string]string{"user_name": "Bhanu", "worktree_root": f.root}, nil, 204)
	return f, f.s.newBot(t, "Ada", p)
}

func seedMemory(t *testing.T, f *repoFixture, b bot, scope, kind, body, why string) memoryItem {
	t.Helper()
	scopeID := ""
	if scope == "repo" {
		scopeID = f.repo.ID
	}
	if scope == "bot" {
		scopeID = b.ID
	}
	var item memoryItem
	f.s.do(t, "POST", "/api/memory", map[string]string{"scope": scope, "scope_id": scopeID, "kind": kind, "body": body, "why": why}, &item, 201)
	if item.ID == "" || item.Status != "accepted" || item.Body != body || item.Scope != scope || item.ScopeID != scopeID || item.Why != why {
		t.Fatalf("seed returned %+v", item)
	}
	return item
}

func memoryList(t *testing.T, s *server) []memoryItem {
	t.Helper()
	var items []memoryItem
	s.do(t, "GET", "/api/memory", nil, &items, 200)
	return items
}

func memoryRun(t *testing.T, f *repoFixture, th thread, text string) (runInfo, []event) {
	t.Helper()
	ev := f.s.events(t, th)
	r := f.s.send(t, th, text)
	events := until(t, ev, r.ID, "run_finished")
	r = f.s.run(t, r.ID)
	if r.Status != "done" || r.Report == nil {
		t.Fatalf("memory run: %+v", r)
	}
	return r, events
}

func promptLog(t *testing.T, m *machine, p string) []promptRecord {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(m.bin, p+".log.prompts"))
	if err != nil {
		t.Fatal(err)
	}
	var records []promptRecord
	for line := range strings.Lines(string(data)) {
		var r promptRecord
		if err := json.Unmarshal([]byte(line), &r); err != nil {
			t.Fatal(err)
		}
		records = append(records, r)
	}
	return records
}

func lastPrompt(t *testing.T, m *machine, p, kind string) string {
	t.Helper()
	records := promptLog(t, m, p)
	for i := len(records) - 1; i >= 0; i-- {
		if records[i].Kind == kind {
			return records[i].Text
		}
	}
	t.Fatalf("no %s prompt in %+v", kind, records)
	return ""
}

func TestMemory_CrossThreadRecall(t *testing.T) {
	for _, p := range _providers {
		t.Run(p, func(t *testing.T) {
			f, b := memorySetup(t, p)
			th := f.thread(t, b)
			r, es := memoryRun(t, f, th, _saveMemory)
			if r.Reply != "Saved SQLite decision." {
				t.Fatalf("save reply %q", r.Reply)
			}
			saved := repoEvent(t, es, "memory_saved")
			raw, _ := json.Marshal(saved.Data["item"])
			var item memoryItem
			if err := json.Unmarshal(raw, &item); err != nil {
				t.Fatal(err)
			}
			if item.ID == "" || item.Body != fake.DecisionBody || item.Why != fake.DecisionWhy || item.Scope != "repo" || item.ScopeID != f.repo.ID || item.Status != "accepted" {
				t.Fatalf("saved item %+v", item)
			}
			if saved.Data["source_bot_id"] != b.ID || saved.Data["thread_id"] != th.ID || saved.Data["run_id"] != r.ID {
				t.Fatalf("save source: %v", saved.Data)
			}
			if item.SourceBotID != b.ID || item.SourceThreadID != th.ID || item.SourceRunID != r.ID {
				t.Fatalf("item source: %+v", item)
			}
			items := memoryList(t, f.s)
			if len(items) != 1 || items[0] != item {
				t.Fatalf("saved history: %+v", items)
			}
			fresh := f.thread(t, b)
			r, _ = memoryRun(t, f, fresh, _recallMemory)
			if r.Reply != item.Body+" "+item.Why {
				t.Fatalf("recall reply %q", r.Reply)
			}
			first := lastPrompt(t, f.m, p, "user")
			if !strings.Contains(first, item.ID) || !strings.Contains(first, item.Body) {
				t.Fatalf("decision absent from first message: %s", first)
			}
			sessions := f.m.sessions(t, p)
			if len(sessions) != 2 {
				t.Fatalf("provider sessions: %d", len(sessions))
			}
			if p == "claude" && hasArg(sessions[1].argv, "--resume") {
				t.Fatal("new thread resumed an old session")
			}
			if p == "codex" && strings.Contains(strings.Join(sessions[1].stdin, "\n"), `"method":"thread/resume"`) {
				t.Fatal("new thread resumed an old session")
			}
		})
	}
}

func hasArg(args []string, want string) bool {
	for _, s := range args {
		if s == want {
			return true
		}
	}
	return false
}

// withoutFinishRules swaps the Finish section's lines for a placeholder: tests assert those rules
// by meaning (TestPrompt_ReplyCarriesAnswerRule), not letter for letter.
func withoutFinishRules(system string) string {
	head, rest, ok := strings.Cut(system, "\nFinish\n")
	if !ok {
		return system
	}
	_, tail, _ := strings.Cut(rest, "\n\n")
	return head + "\nFinish\n<finish rules>\n\n" + tail
}

func golden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", "golden", name+".txt")
	if *_updateGolden {
		if err := os.WriteFile(path, []byte(got+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got+"\n" != string(want) {
		t.Errorf("%s golden differs\nwant:\n%s\ngot:\n%s", name, want, got)
	}
}

func TestMemory_PromptGoldenAndBudget(t *testing.T) {
	for _, p := range _providers {
		t.Run(p, func(t *testing.T) {
			f, b := memorySetup(t, p)
			repo := seedMemory(t, f, b, "repo", "decision", fake.DecisionBody, fake.DecisionWhy)
			user := seedMemory(t, f, b, "user", "convention", "SQLite journal changes require a rollback plan.", "Keep recovery predictable.")
			own := seedMemory(t, f, b, "bot", "lesson", "Check SQLite journal locks before tuning.", "Measure contention first.")
			th := f.thread(t, b)
			memoryRun(t, f, th, _recallMemory)
			system := lastPrompt(t, f.m, p, "system")
			first := lastPrompt(t, f.m, p, "user")
			normal := strings.NewReplacer(repo.ID, "<repo-item>", user.ID, "<user-item>", own.ID, "<bot-item>", f.repo.ID, "<repo>", filepath.Join(f.m.data, "threads", th.ID), "<checkout>", f.s.url, "<server>").Replace(first)
			golden(t, "system", withoutFinishRules(system))
			golden(t, "first-message", normal)
			if len(system) > 500*4 {
				t.Errorf("system additions %.2f tokens, limit 500", float64(len(system))/4)
			}
			if len(system)+len(first)-len(_recallMemory) > 4000*4 {
				t.Errorf("all Reef additions exceed 4000 tokens")
			}
		})
	}
}

func TestMemory_ResumeOnlyUnseen(t *testing.T) {
	for _, p := range _providers {
		t.Run(p, func(t *testing.T) {
			f, b := memorySetup(t, p)
			a := seedMemory(t, f, b, "repo", "fact", "SQLite checkpoint A is automatic.", "Keep reads moving.")
			th := f.thread(t, b)
			memoryRun(t, f, th, _checkMemory)
			first := lastPrompt(t, f.m, p, "user")
			if !strings.Contains(first, a.ID) || !strings.Contains(first, a.Body) {
				t.Fatalf("A absent: %s", first)
			}
			next := seedMemory(t, f, b, "repo", "fact", "SQLite checkpoint B needs monitoring.", "Watch latency.")
			memoryRun(t, f, th, _resumeMemory)
			second := lastPrompt(t, f.m, p, "user")
			want := fmt.Sprintf("Memory items:\n- [%s] fact (repo): %s\n  Why: %s\n\nMessage:\n%s", next.ID, next.Body, next.Why, _resumeMemory)
			if second != want {
				t.Fatalf("resumed prompt\nwant %q\ngot %q", want, second)
			}
			if strings.Contains(second, a.Body) || strings.Contains(second, "["+a.ID+"]") {
				t.Fatal("resumed prompt repeated A")
			}
			sessions := f.m.sessions(t, p)
			if len(sessions) != 2 {
				t.Fatalf("provider sessions %d", len(sessions))
			}
			if p == "claude" && !hasArg(sessions[1].argv, "--resume") {
				t.Fatal("second run did not resume")
			}
			if p == "codex" && !strings.Contains(strings.Join(sessions[1].stdin, "\n"), `"method":"thread/resume"`) {
				t.Fatal("second run did not resume")
			}
		})
	}
}

func TestMemory_RetireKeepsHistory(t *testing.T) {
	for _, p := range _providers {
		t.Run(p, func(t *testing.T) {
			for _, via := range []string{"mcp", "rest"} {
				t.Run(via, func(t *testing.T) {
					f, b := memorySetup(t, p)
					item := seedMemory(t, f, b, "repo", "decision", fake.DecisionBody, fake.DecisionWhy)
					th := f.thread(t, b)
					if via == "mcp" {
						r, _ := memoryRun(t, f, th, _retireMemory)
						if r.Reply != "Retired SQLite decision." {
							t.Fatal(r.Reply)
						}
					} else {
						f.s.do(t, "POST", "/api/memory/"+item.ID+"/retire", nil, nil, 204)
					}
					items := memoryList(t, f.s)
					if len(items) != 1 || items[0].ID != item.ID || items[0].Status != "retired" || items[0].Body != item.Body || items[0].Why != item.Why {
						t.Fatalf("retired history: %+v", items)
					}
					// Check both an existing thread and a fresh session after retirement.
					memoryRun(t, f, th, _checkMemory)
					for i, current := range []thread{th, f.thread(t, b)} {
						input := _resumeMemory
						if i == 1 {
							input = _checkMemory
						}
						memoryRun(t, f, current, input)
						prompt := lastPrompt(t, f.m, p, "user")
						if strings.Contains(prompt, item.Body) || strings.Contains(prompt, "["+item.ID+"]") {
							t.Fatalf("retired memory leaked into prompt: %s", prompt)
						}
					}
				})
			}
		})
	}
}

func TestMemory_SelectionAndBudget(t *testing.T) {
	for _, p := range _providers {
		t.Run(p, func(t *testing.T) {
			f, b := memorySetup(t, p)
			// The other Bot's memory must not enter this Bot's context.
			other := f.s.newBot(t, "Other", p)
			hidden := seedMemory(t, f, other, "bot", "lesson", "SQLite private lesson must stay private.", "Other Bot only.")
			for i := 0; i < 12; i++ {
				seedMemory(t, f, b, "repo", "fact", fmt.Sprintf("SQLite journal detail %02d %s", i, strings.Repeat("content ", 300)), "A relevant journal detail.")
			}
			th := f.thread(t, b)
			memoryRun(t, f, th, _checkMemory)
			prompt := lastPrompt(t, f.m, p, "user")
			if strings.Contains(prompt, hidden.Body) || strings.Contains(prompt, "["+hidden.ID+"]") {
				t.Fatal("another Bot's memory leaked")
			}
			count := strings.Count(prompt, "\n- [")
			if count == 0 || count > 8 || !strings.Contains(prompt, "SQLite journal detail") {
				t.Fatalf("prompt has %d memory entries, want 1..8", count)
			}
			if !strings.Contains(prompt, "Memory: repo 12, user 0, bot 0. Use recall.") {
				t.Fatalf("counts must include the full visible shelf: %s", prompt)
			}
			system := lastPrompt(t, f.m, p, "system")
			if len(system) > 500*4 || len(system)+len(prompt)-len(_checkMemory) > 4000*4 {
				t.Fatal("large memory shelf exceeded prompt budget")
			}
		})
	}
}
