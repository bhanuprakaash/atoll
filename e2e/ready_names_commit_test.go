package e2e

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

const _freezeText = "Once a PR is called ready it is frozen: new work goes in a follow-up PR."

// The merge wake says where the PR went and at which commit: the head the watcher read at the
// merge, whether or not it moved since the PR was opened.
func TestReadyNamesCommit_MergeWakeNamesTheCommit(t *testing.T) {
	for _, p := range _providers {
		for _, c := range []struct{ name, moved string }{
			{"head as opened", ""},
			{"head moved before the merge", "dddddddddddddddddddddddddddddddddddddddd"},
		} {
			t.Run(p+"/"+c.name, func(t *testing.T) {
				f := newPRWakeFixture(t, p)
				th := f.openPR(t, 1)
				head := headSHA(1)
				if c.moved != "" {
					head = c.moved
					f.gh.setHead(1, head)
				}

				f.gh.setPull(1, "closed", true)
				w := f.waitWake(t, th, 1)

				want := "PR #1 merged into main at " + head[:7]
				if w.Kind != "merged" || w.Text != want || w.FromBotID != "" {
					t.Errorf("wake message %+v, want kind merged, text %q, no from_bot_id", w, want)
				}
				if got := f.reefPrompts(t); len(got) != 1 || !strings.Contains(got[0], "\nReef: "+want) && !strings.HasPrefix(got[0], "Reef: "+want) {
					t.Errorf("Reef prompts %q, want one with the line %q", got, "Reef: "+want)
				}
			})
		}
	}
}

// stack has a new thread's Bot stack a fix on branch and returns the thread.
func (f *prWakeFixture) stack(t *testing.T, branch string) thread {
	t.Helper()
	th := f.thread(t, f.boss, f.repo)
	ch := f.s.events(t, th)
	r := f.s.send(t, th, "Stack a fix on "+branch)
	until(t, ch, r.ID, "run_finished")
	if done := f.s.run(t, r.ID); done.Status != "done" {
		t.Fatalf("run that stacks the fix: %+v", done)
	}
	return th
}

// A fix merged into another PR's branch is a quiet line in its thread: the message is there, the Bot
// is not woken.
func TestReadyNamesCommit_MergeIntoAnotherBranchIsQuiet(t *testing.T) {
	for _, p := range _providers {
		t.Run(p, func(t *testing.T) {
			f := newPRWakeFixture(t, p)
			f.openPR(t, 1)
			branch := f.gh.pullRequests()[0].Head
			th := f.stack(t, branch)
			before := f.s.thread(t, th.ID).RunID

			f.gh.setPull(2, "closed", true)
			var w []wakeMessage
			eventually(t, 30*time.Second, "the merge line in the fix's thread", func() bool {
				w = f.wakes(t, th)
				return len(w) > 0
			})
			want := "PR #2 merged into " + branch + " at " + headSHA(2)[:7]
			if len(w) != 1 || w[0].Kind != "merged" || w[0].Text != want || w[0].FromBotID != "" {
				t.Errorf("messages with a kind %+v, want one merged line from Reef: %q", w, want)
			}
			f.quiet(t, 1, "a merge into another branch", th, 1, 0, 2)
			if got := f.s.thread(t, th.ID); got.RunID != before {
				t.Errorf("thread's latest run is %s, was %s: the merge line must start no run", got.RunID, before)
			}
		})
	}
}

// The freeze sentence and the draft rule moved out of Reef's prompt into the repo's own
// instructions: no Bot's system prompt carries either.
func TestReadyNamesCommit_PromptHasNoFreezeOrDraftRules(t *testing.T) {
	for _, p := range _providers {
		t.Run(p, func(t *testing.T) {
			_, s := start(t, p)
			var coder, plain bot
			s.do(t, "POST", "/api/bots", map[string]any{"name": "Coder", "persona": "Fixes things.", "provider": p, "repo_access": "all"}, &coder, http.StatusCreated)
			s.do(t, "POST", "/api/bots", map[string]any{"name": "Plain", "persona": "Answers.", "provider": p, "repo_access": []string{}}, &plain, http.StatusCreated)

			for _, b := range []bot{coder, plain} {
				system := heySystem(t, s, p, b)
				for _, gone := range []string{"frozen", "Waits for", "--draft"} {
					if strings.Contains(system, gone) {
						t.Errorf("%s's system prompt has %q, want it only in the repo's instructions:\n%s", b.Name, gone, system)
					}
				}
			}
		})
	}
}

// listedTools is what tools/list answers to a boss's run, tool name to description.
func listedTools(t *testing.T, p string) map[string]string {
	t.Helper()
	out := map[string]string{}
	for name, tool := range listedToolSpecs(t, p) {
		out[name] = tool.Description
	}
	return out
}

type toolSpec struct {
	Name, Description string
	InputSchema       struct {
		Properties map[string]json.RawMessage `json:"properties"`
		Required   []string                   `json:"required"`
	} `json:"inputSchema"`
}

// listedToolSpecs is what tools/list answers to a boss's run, tool name to its description and schema.
func listedToolSpecs(t *testing.T, p string) map[string]toolSpec {
	t.Helper()
	m, s := start(t, p)
	boss, helper := s.newBot(t, "Poteto", p), s.newBot(t, "Tester", p)
	setDelegates(t, s, boss, helper)
	th := s.newThread(t, boss)
	events := s.events(t, th)
	run := s.send(t, th, _hangPrompt)
	until(t, events, run.ID, "tool_call")
	sessions := m.sessions(t, p)
	atoll, ok := mcpServersOf(t, p, sessions[len(sessions)-1])["atoll"]
	if !ok {
		t.Fatal("the boss's run has no atoll MCP server")
	}
	req, _ := http.NewRequest("POST", atoll.URL, bytes.NewReader([]byte(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)))
	req.Header.Set("Content-Type", "application/json")
	for k, v := range atoll.Headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	var listed struct {
		Result struct {
			Tools []toolSpec `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal(body, &listed); err != nil {
		t.Fatalf("tools/list answered %s: %v", body, err)
	}
	out := map[string]toolSpec{}
	for _, tool := range listed.Result.Tools {
		out[tool.Name] = tool
	}
	return out
}

// The delegate tool's description no longer ends the brief with the freeze sentence, and neither
// delegate's nor track_mr's description names finish_run or a frozen PR, or says never to merge:
// that rule lives in each Bot's persona, where a release Bot can drop it.
func TestReadyNamesCommit_ToolDescriptionsHaveNoFreezeSentence(t *testing.T) {
	for _, p := range _providers {
		t.Run(p, func(t *testing.T) {
			tools := listedTools(t, p)
			for _, name := range []string{"delegate", "track_mr"} {
				desc, ok := tools[name]
				if !ok {
					t.Fatalf("tools/list has no %s: %v", name, tools)
				}
				for _, gone := range []string{_freezeText, "frozen", "finish_run", "Never merge"} {
					if strings.Contains(desc, gone) {
						t.Errorf("%s description %q holds %q", name, desc, gone)
					}
				}
			}
		})
	}
}
