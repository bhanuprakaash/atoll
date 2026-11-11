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

	"atoll/e2e/fakes/fake"
)

// Contract: ready-names-commit-contract.md.

const (
	_readyPrompt = "Fix the seeded bug, open a PR and report it ready. READY="
	_freezeLine  = "- Once a PR is called ready it is frozen: new work goes in a follow-up PR."
	_draftLine   = "- On top of an unmerged PR's branch, open yours with `gh pr create --draft`, body starting `Waits for #<n>`; mark it ready when woken that #<n> merged."
	_freezeText  = "Once a PR is called ready it is frozen: new work goes in a follow-up PR."
)

// readyEntry is one of finish_run's ready entries as the run's report gives it back.
type readyEntry struct {
	URL     string `json:"url"`
	SHA     string `json:"sha"`
	Changed bool   `json:"changed"`
}

// readyOf is the report.ready of a run.
func (f *prWakeFixture) readyOf(t *testing.T, runID string) []readyEntry {
	t.Helper()
	var got struct {
		Report *struct {
			Ready []readyEntry `json:"ready"`
		} `json:"report"`
	}
	f.s.do(t, "GET", "/api/runs/"+runID, nil, &got, 200)
	if got.Report == nil {
		t.Fatalf("run %s has no report", runID)
	}
	return got.Report.Ready
}

// prURL is the URL of pull request n of the fake GitHub's octo/hello.
func (f *prWakeFixture) prURL(n int) string {
	return fmt.Sprintf("%s/octo/hello/pull/%d", f.gh.URL, n)
}

// reportReady has a new thread's Bot open a pull request and call finish_run with ready entries
// [{url, sha}], and returns the thread and its run. The fake GitHub numbers pull requests from 1.
func (f *prWakeFixture) reportReady(t *testing.T, entries ...map[string]string) (thread, runInfo) {
	t.Helper()
	th := f.thread(t, f.boss, f.repo)
	ch := f.s.events(t, th)
	payload, _ := json.Marshal(entries)
	r := f.s.send(t, th, _readyPrompt+string(payload))
	until(t, ch, r.ID, "run_finished")
	done := f.s.run(t, r.ID)
	if done.Status != "done" {
		t.Fatalf("run that opens the pull request and reports it ready: %+v", done)
	}
	return th, done
}

func ready(url, sha string) map[string]string { return map[string]string{"url": url, "sha": sha} }

const _otherSHA = "cccccccccccccccccccccccccccccccccccccccc"

// finish_run's ready entries carry the PR head the Bot saw, and the report says when the head has
// moved since: the data the "Waiting on you" row shows (the row itself lands with #147).
func TestReadyNamesCommit_ReportCarriesHeadSHA(t *testing.T) {
	for _, p := range _providers {
		t.Run(p, func(t *testing.T) {
			f := newPRWakeFixture(t, p)
			sha := headSHA(1)
			th, run := f.reportReady(t, ready(f.prURL(1), sha))

			if got, want := f.readyOf(t, run.ID), []readyEntry{{f.prURL(1), sha, false}}; len(got) != 1 || got[0] != want[0] {
				t.Fatalf("report.ready %+v, want %+v", got, want)
			}

			f.gh.setHead(1, _otherSHA)
			eventually(t, 30*time.Second, "report.ready says the head changed since ready", func() bool {
				got := f.readyOf(t, run.ID)
				return len(got) == 1 && got[0].Changed && got[0].SHA == sha
			})
			f.quiet(t, time.Second, "a push after ready", th, 0, 0)

			f.gh.setHead(1, sha)
			eventually(t, 30*time.Second, "report.ready is unchanged again once the head is the ready commit", func() bool {
				got := f.readyOf(t, run.ID)
				return len(got) == 1 && !got[0].Changed
			})
		})
	}
}

// A ready entry without a PR url or without a 40-hex sha is a tool error, and nothing is stored.
func TestReadyNamesCommit_BadReadyEntryIsRefused(t *testing.T) {
	for _, p := range _providers {
		t.Run(p, func(t *testing.T) {
			f := newPRWakeFixture(t, p)
			call := func(entry map[string]string) fake.MCPCall {
				args, _ := json.Marshal(map[string]any{"done": "d", "tried": "t", "blocked": "", "next": "n", "ready": []map[string]string{entry}})
				return fake.MCPCall{Server: "atoll", Tool: "finish_run", Args: args}
			}
			th := f.s.newThread(t, f.boss)
			lines := useAtoll(t, f.s, th, f.s.events(t, th), call(ready(f.prURL(1), "abc123")), call(ready("", headSHA(1))))
			if len(lines) != 2 {
				t.Fatalf("reply %q, want one line per call", lines)
			}
			for i, word := range []string{"sha", "url"} {
				line := strings.ToLower(lines[i])
				if strings.Contains(line, "report saved") || !strings.Contains(line, "ready") || !strings.Contains(line, word) {
					t.Errorf("finish_run call %d answered %q, want a tool error naming the ready entry's %s", i+1, lines[i], word)
				}
			}
			if got := f.readyOf(t, f.s.thread(t, th.ID).RunID); len(got) != 0 {
				t.Errorf("report.ready %+v after refused entries, want none", got)
			}
		})
	}
}

// The merge wake says where the PR went and at which commit, and says so when that is not the commit
// the Bot called ready.
func TestReadyNamesCommit_MergeWakeNamesTheCommit(t *testing.T) {
	for _, p := range _providers {
		for _, c := range []struct {
			name, ready, moved, suffix string
		}{
			{"no ready entry", "", "", ""},
			{"ready is the merged commit", headSHA(1), "", ""},
			{"ready was another commit", _otherSHA, "", " (ready was " + _otherSHA[:7] + ")"},
			{"head moved after ready", headSHA(1), "dddddddddddddddddddddddddddddddddddddddd", " (ready was " + headSHA(1)[:7] + ")"},
		} {
			t.Run(p+"/"+c.name, func(t *testing.T) {
				f := newPRWakeFixture(t, p)
				var entries []map[string]string
				if c.ready != "" {
					entries = append(entries, ready(f.prURL(1), c.ready))
				}
				th, run := f.reportReady(t, entries...)
				head := headSHA(1)
				if c.moved != "" {
					head = c.moved
					f.gh.setHead(1, head)
					eventually(t, 30*time.Second, "the head change is read", func() bool {
						got := f.readyOf(t, run.ID)
						return len(got) > 0 && got[0].Changed
					})
				}

				f.gh.setPull(1, "closed", true)
				w := f.waitWake(t, th, 1)

				want := "PR #1 merged into main at " + head[:7] + c.suffix
				if w.Kind != "merged" || w.Text != want || w.FromBotID != "" {
					t.Errorf("wake message %+v, want kind merged, text %q, no from_bot_id", w, want)
				}
				if got := f.reefPrompts(t); len(got) != 1 || got[0] != "Reef: "+want {
					t.Errorf("Reef prompts %q, want %q", got, "Reef: "+want)
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
			f.quiet(t, 2*time.Second, "a merge into another branch", th, 1, 0)
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
			f.quiet(t, 2*time.Second, "a draft whose base is open", fix, 0, 0)

			f.gh.setPull(1, "closed", true)
			merged := f.waitWake(t, test, 1)
			w := f.waitWake(t, fix, 1)

			if want := "PR #1 merged into main at " + headSHA(1)[:7]; merged.Text != want {
				t.Errorf("test PR's wake %q, want %q", merged.Text, want)
			}
			if w.Kind != "base" || w.Text != "PR #1 merged; PR #2 was waiting for it" || w.FromBotID != "" {
				t.Errorf("stacked fix's wake %+v, want kind base, text %q, no from_bot_id", w, "PR #1 merged; PR #2 was waiting for it")
			}
			if run := f.s.run(t, f.s.thread(t, fix.ID).RunID); run.Status != "done" || run.Reply != "Ready noted." {
				t.Errorf("run of the base wake %+v, want done with reply %q", run, "Ready noted.")
			}
			want := []string{"create #1 base=main draft=false", "create #2 base=" + branch + " draft=true", "edit #2 base=main", "ready #2"}
			if got := f.gh.prCalls(); !equalStrings(got, want) {
				t.Errorf("gh calls %q, want %q", got, want)
			}
			if f.gh.isDraft(2) {
				t.Error("the stacked fix is still a draft after the Bot marked it ready")
			}

			f.quiet(t, 2*time.Second, "the base wake already sent", fix, 1, 1)
		})
	}
}

func equalStrings(a, b []string) bool { return strings.Join(a, "\n") == strings.Join(b, "\n") }

// The freeze sentence and the draft rule are in the code part of every Bot with repo access, and a
// Bot without repo access has neither.
func TestReadyNamesCommit_CodePartCarriesFreezeAndDraftRules(t *testing.T) {
	for _, p := range _providers {
		t.Run(p, func(t *testing.T) {
			_, s := start(t, p)
			var coder, plain bot
			s.do(t, "POST", "/api/bots", map[string]any{"name": "Coder", "persona": "Fixes things.", "provider": p, "repo_access": "all"}, &coder, http.StatusCreated)
			s.do(t, "POST", "/api/bots", map[string]any{"name": "Plain", "persona": "Answers.", "provider": p, "repo_access": []string{}}, &plain, http.StatusCreated)

			system := heySystem(t, s, p, coder)
			lines := strings.Split(system, "\n")
			work := -1
			for i, l := range lines {
				if l == "How you work" {
					work = i
				}
			}
			for _, want := range []string{_freezeLine, _draftLine} {
				found := false
				for _, l := range lines[work+1:] {
					if l == "" {
						break
					}
					found = found || l == want
				}
				if work < 0 || !found {
					t.Errorf("How you work has no line %q:\n%s", want, system)
				}
			}
			if system := heySystem(t, s, p, plain); strings.Contains(system, "frozen") || strings.Contains(system, "Waits for") {
				t.Errorf("a Bot without repo access has the code part's freeze or draft rule:\n%s", system)
			}
		})
	}
}

// The delegate tool's description, as the boss's run lists it, tells the boss to end every brief with
// the freeze sentence.
func TestReadyNamesCommit_DelegateBriefCarriesFreezeSentence(t *testing.T) {
	for _, p := range _providers {
		t.Run(p, func(t *testing.T) {
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
			for _, tool := range listed.Result.Tools {
				if tool.Name == "delegate" {
					if !strings.Contains(tool.Description, _freezeText) {
						t.Errorf("delegate description %q lacks %q", tool.Description, _freezeText)
					}
					return
				}
			}
			t.Errorf("tools/list has no delegate: %s", body)
		})
	}
}
