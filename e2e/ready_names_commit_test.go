package e2e

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

// Contract: ready-names-commit-contract.md.

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

// stack has a new thread's Bot stack a fix on branch and returns the thread; waits is the number of
// the PR it waits for, 0 for none.
func (f *prWakeFixture) stack(t *testing.T, branch string, waits int) thread {
	t.Helper()
	th := f.thread(t, f.boss, f.repo)
	ch := f.s.events(t, th)
	prompt := "Stack a fix on " + branch
	if waits > 0 {
		prompt += fmt.Sprintf(" waiting for PR %d", waits)
	}
	r := f.s.send(t, th, prompt)
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
			th := f.stack(t, branch, 0)
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
			f.quiet(t, 2, "a merge into another branch", th, 1, 0)
			if got := f.s.thread(t, th.ID); got.RunID != before {
				t.Errorf("thread's latest run is %s, was %s: the merge line must start no run", got.RunID, before)
			}
		})
	}
}

// The scripted flow test PR then stacked fix: the fix is opened as a draft against the test's branch
// with "Waits for #1", stays one while the test is open and wakes nothing, and when the test merges
// its Bot is woken once, retargets it to main and marks it ready.
func TestReadyNamesCommit_StackedFixWaitsAsDraft(t *testing.T) {
	for _, p := range _providers {
		t.Run(p, func(t *testing.T) {
			f := newPRWakeFixture(t, p)
			test := f.openPR(t, 1)
			branch := f.gh.pullRequests()[0].Head
			fix := f.stack(t, branch, 1)

			prs := f.gh.pullRequests()
			if len(prs) != 2 || prs[1].Base != branch || !strings.HasPrefix(prs[1].Body, "Waits for #1") {
				t.Fatalf("pull requests %+v, want #2 against %q with a body starting %q", prs, branch, "Waits for #1")
			}
			if got, want := f.gh.prCalls(), []string{"create #1 base=main draft=false", "create #2 base=" + branch + " draft=true"}; !equalStrings(got, want) {
				t.Fatalf("gh calls %q, want %q", got, want)
			}
			if !f.gh.isDraft(2) {
				t.Error("the stacked fix is not a draft while its base is open")
			}
			f.quiet(t, 2, "a draft whose base is open", fix, 0, 0)

			f.gh.setPull(1, "closed", true)
			merged := f.waitWake(t, test, 1)
			w := f.waitWake(t, fix, 1)

			if want := "PR #1 merged into main at " + headSHA(1)[:7]; merged.Text != want {
				t.Errorf("test PR's wake %q, want %q", merged.Text, want)
			}
			if w.Kind != "base" || w.Text != "PR #1 merged; PR #2 was waiting for it" || w.FromBotID != "" {
				t.Errorf("stacked fix's wake %+v, want kind base, text %q, no from_bot_id", w, "PR #1 merged; PR #2 was waiting for it")
			}
			if run := f.s.run(t, f.s.thread(t, fix.ID).RunID); run.Status != "done" {
				t.Errorf("run of the base wake %+v, want done", run)
			}
			want := []string{"create #1 base=main draft=false", "create #2 base=" + branch + " draft=true", "edit #2 base=main", "ready #2"}
			if got := f.gh.prCalls(); !equalStrings(got, want) {
				t.Errorf("gh calls %q, want %q", got, want)
			}
			if f.gh.isDraft(2) {
				t.Error("the stacked fix is still a draft after the Bot marked it ready")
			}

			f.quiet(t, 2, "the base wake already sent", fix, 1, 2)
		})
	}
}

func equalStrings(a, b []string) bool { return strings.Join(a, "\n") == strings.Join(b, "\n") }

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
			Tools []struct{ Name, Description string } `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal(body, &listed); err != nil {
		t.Fatalf("tools/list answered %s: %v", body, err)
	}
	out := map[string]string{}
	for _, tool := range listed.Result.Tools {
		out[tool.Name] = tool.Description
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
