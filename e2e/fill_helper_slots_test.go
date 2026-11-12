package e2e

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"atoll/e2e/fakes/fake"
)

// Contract: fill-helper-slots-contract.md.

const _bossRule = "Before finish_run, if a slot is free and a ticket is ready, delegate it first."

// Hung helpers whose thread titles differ: a title is the brief's first words.
var _reviews = []string{
	"Review #184: " + _hangPrompt,
	"Review #200: " + _hangPrompt,
	"Review #201: " + _hangPrompt,
}

func helpersLine(running ...string) string {
	if len(running) == 0 {
		return "Helpers: 0 of 3 running. 3 free."
	}
	return fmt.Sprintf("Helpers: %d of 3 running (%s). %d free.", len(running), strings.Join(running, "; "), 3-len(running))
}

// onThread is how the line names a running helper: its Bot and its thread's title.
func onThread(t *testing.T, s *server, botName, threadID string) string {
	t.Helper()
	title := s.thread(t, threadID).Title
	if !strings.HasPrefix(title, "Review #") {
		t.Fatalf("helper thread %s title %q, want the brief's first words (Review #…)", threadID, title)
	}
	return botName + " on " + title
}

func finishRunCall() fake.MCPCall {
	return fake.MCPCall{Server: "atoll", Tool: "finish_run",
		Args: json.RawMessage(`{"done":"Delegated.","tried":"Nothing.","blocked":"","next":"Wait."}`)}
}

func endsWithLine(t *testing.T, what, text, line string) {
	t.Helper()
	if !strings.HasSuffix(text, "\n\n"+line) {
		t.Errorf("%s %q does not end with a blank line and %q", what, text, line)
	}
}

// TestSlots_WakeEndsWithHelpersLine: a boss wake, whatever ended the helper's run (done, the user's
// stop, a failure, the Bot's deletion), ends with the Helpers line built from the helper runs going
// when it was posted: the helper that just ended is not counted, the others are named oldest first.
func TestSlots_WakeEndsWithHelpersLine(t *testing.T) {
	t.Parallel()
	for _, p := range _providers {
		t.Run(p, func(t *testing.T) {
			m, s := start(t, p)
			poteto, tester, reviewer := s.newBot(t, "Poteto", p), s.newBot(t, "Tester", p), s.newBot(t, "Reviewer", p)
			setDelegates(t, s, poteto, tester, reviewer)
			boss := s.newThread(t, poteto)
			ev := s.events(t, boss)

			lines := useAtoll(t, s, boss, ev,
				delegateCall("Tester", _reviews[0], ""), delegateCall("Tester", _reportPrompt, ""))
			if len(lines) != 2 {
				t.Fatalf("two delegates: reply %q, want two lines", lines)
			}
			review, reporter := delegated(t, lines[0]), delegated(t, lines[1])
			helperRunning(t, s, review)

			allow(t, s, reporter)
			msg, _, _ := woken(t, m, s, p, boss.ID, tester.ID, 1)
			holdsAll(t, "done wake", msg.Text, "Tester reported back from thread "+reporter+".", _reportReply)
			endsWithLine(t, "done wake", msg.Text, helpersLine(onThread(t, s, "Tester", review)))

			s.do(t, "POST", "/api/runs/"+helperRunning(t, s, review)+"/stop", nil, nil, http.StatusNoContent)
			msg, _, _ = woken(t, m, s, p, boss.ID, tester.ID, 2)
			holdsAll(t, "stopped wake", msg.Text, "The user stopped Tester", review)
			endsWithLine(t, "stopped wake", msg.Text, helpersLine())

			lines = useAtoll(t, s, boss, ev,
				delegateCall("Tester", _reviews[1], ""), delegateCall("Reviewer", _reviews[2], ""),
				delegateCall("Tester", _noRecordingPrompt, ""))
			if len(lines) != 3 {
				t.Fatalf("three delegates: reply %q, want three lines", lines)
			}
			first, second, failed := delegated(t, lines[0]), delegated(t, lines[1]), delegated(t, lines[2])
			running := []string{onThread(t, s, "Tester", first), onThread(t, s, "Reviewer", second)}
			f := settled(t, s, failed)
			if f.Status != "failed" {
				t.Fatalf("helper run with no recording: status %q, want failed", f.Status)
			}
			msg, _, _ = woken(t, m, s, p, boss.ID, tester.ID, 3)
			holdsAll(t, "failed wake", msg.Text, failed, "Tester's run failed: "+f.Error)
			endsWithLine(t, "failed wake", msg.Text, helpersLine(running...))

			s.do(t, "DELETE", "/api/bots/"+reviewer.ID, nil, nil, http.StatusNoContent)
			msg, _, _ = woken(t, m, s, p, boss.ID, reviewer.ID, 1)
			holdsAll(t, "deleted wake", msg.Text, second, "Reviewer was deleted")
			endsWithLine(t, "deleted wake", msg.Text, helpersLine(running[0]))
		})
	}
}

// TestSlots_ToolResultsCarryHelpersLine: delegate returns the line as the "helpers" field of its
// JSON result (the new helper counts, a refusal keeps its text); continue and finish_run return
// their usual text and the line on a new line. All three name every helper going.
func TestSlots_ToolResultsCarryHelpersLine(t *testing.T) {
	t.Parallel()
	for _, p := range _providers {
		t.Run(p, func(t *testing.T) {
			_, s := start(t, p)
			poteto, tester := s.newBot(t, "Poteto", p), s.newBot(t, "Tester", p)
			setDelegates(t, s, poteto, tester)
			boss := s.newThread(t, poteto)
			ev := s.events(t, boss)

			lines := useAtoll(t, s, boss, ev,
				delegateCall("Tester", _reviews[0], ""), delegateCall("Tester", _reviews[1], ""),
				delegateCall("Tester", _reviews[2], ""), delegateCall("Tester", _hangPrompt, ""))
			if len(lines) != 4 {
				t.Fatalf("four delegates: reply %q, want four lines", lines)
			}
			var running, ids []string
			for i, l := range lines[:3] {
				var out struct {
					ThreadID string `json:"thread_id"`
					Helpers  string `json:"helpers"`
				}
				text, _ := strings.CutPrefix(l, "atoll/delegate: ")
				if err := json.Unmarshal([]byte(text), &out); err != nil || out.ThreadID == "" {
					t.Fatalf("delegate %d result %q, want JSON with thread_id", i+1, l)
				}
				ids = append(ids, out.ThreadID)
				running = append(running, onThread(t, s, "Tester", out.ThreadID))
				if want := helpersLine(running...); out.Helpers != want {
					t.Errorf("delegate %d: helpers %q, want %q", i+1, out.Helpers, want)
				}
			}
			if want := "atoll/delegate: " + _helpersFull; lines[3] != want {
				t.Errorf("fourth delegate with three helpers running: %q, want %q (a refusal has no line)", lines[3], want)
			}

			got := useAtoll(t, s, boss, ev, continueCall(ids[0], _askPrompt), finishRunCall())
			want := []string{"atoll/continue: Sent.", helpersLine(running...), "atoll/finish_run: Report saved.", helpersLine(running...)}
			if strings.Join(got, "\n") != strings.Join(want, "\n") {
				t.Errorf("continue and finish_run in the boss thread: reply %q, want %q", got, want)
			}
		})
	}
}

// TestSlots_FinishRunLineOnlyForBosses: finish_run answers only "Report saved." to a Bot with no
// delegates and to a helper's run, even when the helper Bot has delegates of its own.
func TestSlots_FinishRunLineOnlyForBosses(t *testing.T) {
	t.Parallel()
	for _, p := range _providers {
		t.Run(p, func(t *testing.T) {
			_, s := start(t, p)
			poteto, tester, reviewer, ada := s.newBot(t, "Poteto", p), s.newBot(t, "Tester", p), s.newBot(t, "Reviewer", p), s.newBot(t, "Ada", p)
			setDelegates(t, s, poteto, tester)
			setDelegates(t, s, tester, reviewer)
			const want = "atoll/finish_run: Report saved."

			plain := s.newThread(t, ada)
			if got := useAtoll(t, s, plain, s.events(t, plain), finishRunCall()); len(got) != 1 || got[0] != want {
				t.Errorf("Ada (no delegates): reply %q, want %q", got, want)
			}

			boss := s.newThread(t, poteto)
			brief, _ := json.Marshal([]fake.MCPCall{finishRunCall()})
			lines := useAtoll(t, s, boss, s.events(t, boss), delegateCall("Tester", _mcpPromptHead+string(brief), ""))
			if h := settled(t, s, delegated(t, lines[0])); h.Status != "done" || h.Reply != want {
				t.Errorf("Tester's helper run: status %q, reply %q, error %q; want done, %q", h.Status, h.Reply, h.Error, want)
			}
		})
	}
}

// TestPrompt_BossGetsHelpersRule: a Bot with delegates, in a thread that lists delegate, has the
// rule in its system prompt; a Bot without delegates and a helper run of a Bot that has some do
// not.
func TestPrompt_BossGetsHelpersRule(t *testing.T) {
	t.Parallel()
	for _, p := range _providers {
		t.Run(p, func(t *testing.T) {
			m, s := start(t, p)
			poteto, tester, reviewer, ada := s.newBot(t, "Poteto", p), s.newBot(t, "Tester", p), s.newBot(t, "Reviewer", p), s.newBot(t, "Ada", p)
			setDelegates(t, s, poteto, tester)
			setDelegates(t, s, tester, reviewer)

			for _, b := range []bot{ada, poteto} {
				th := s.newThread(t, b)
				if r := s.finished(t, s.events(t, th), s.send(t, th, _hey).ID); r.Status != "done" {
					t.Fatalf("hey run of %s: status %q, error %q; want done", b.Name, r.Status, r.Error)
				}
			}
			systems := func(name string) []string {
				var out []string
				for _, r := range promptLog(t, m, p) {
					if r.Kind == "system" && strings.HasPrefix(r.Text, "You are "+name+",") {
						out = append(out, r.Text)
					}
				}
				return out
			}
			if got := systems("Ada"); len(got) != 1 || strings.Contains(got[0], _bossRule) {
				t.Errorf("Ada (no delegates): system prompts %q, want one without %q", got, _bossRule)
			}
			if got := systems("Poteto"); len(got) != 1 || !strings.Contains(got[0], _bossRule) {
				t.Errorf("Poteto (delegates Tester): system prompts %q, want one with %q", got, _bossRule)
			}

			boss := s.newThread(t, poteto)
			helper := delegated(t, useAtoll(t, s, boss, s.events(t, boss), delegateCall("Tester", _hangPrompt, ""))[0])
			helperRunning(t, s, helper)
			got := systems("Tester")
			if len(got) != 1 || strings.Contains(got[0], _bossRule) {
				t.Errorf("Tester's helper run (Tester delegates Reviewer): system prompts %q, want one without %q", got, _bossRule)
			}
		})
	}
}
