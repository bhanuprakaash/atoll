package e2e

import (
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"
)

// _heldText is a continue held for a helper. _keepPrompt, the next continue's text, plays
// testdata/claude/queued-resume-keep.jsonl on the session of a helper that ran _hangPrompt.
const (
	_heldText = "Then check the changelog."
	_queued   = "atoll/continue: Queued: Tester is working; it gets this when its run ends."
)

// stopWays are the two ways to stop a helper's run: the person's Stop and the boss's stop_helper.
var _stopWays = []struct {
	name string
	stop func(t *testing.T, f *stoppedHelper)
}{
	{"person", func(t *testing.T, f *stoppedHelper) {
		f.s.do(t, "POST", "/api/runs/"+f.aRun+"/stop", nil, nil, http.StatusNoContent)
	}},
	{"stop_helper", func(t *testing.T, f *stoppedHelper) {
		useAtoll(t, f.s, f.boss, f.ev, stopHelperCall(f.a))
	}},
}

// stoppedHelper is a boss with two helpers of Tester: a, hung, with a continue held for it and then
// stopped; b, which waits on an approval, so its run ends when the test allows it.
type stoppedHelper struct {
	m    *machine
	s    *server
	boss thread
	ev   <-chan event
	a, b string
	aRun string
}

func newStoppedHelper(t *testing.T, p string, stop func(t *testing.T, f *stoppedHelper)) *stoppedHelper {
	t.Helper()
	m, s := start(t, p)
	poteto, tester := s.newBot(t, "Poteto", p), s.newBot(t, "Tester", p)
	setDelegates(t, s, poteto, tester)
	boss := s.newThread(t, poteto)
	ev := s.events(t, boss)
	ids, _ := delegateTester(t, s, boss, ev, _hangPrompt, _reportPrompt)
	f := &stoppedHelper{m: m, s: s, boss: boss, ev: ev, a: ids[0], b: ids[1]}
	f.aRun = helperRunning(t, s, f.a)
	asking(t, s, f.b)

	if got := useAtoll(t, s, boss, ev, continueCall(f.a, _heldText)); !slices.Equal(got, []string{_queued}) {
		t.Fatalf("continue to a helper whose run goes: reply %q, want %q", got, []string{_queued})
	}
	stop(t, f)
	stopped(t, s, f.a, f.aRun)
	return f
}

// stillHeld checks, for _chance, that the stopped helper started no run and its message is still
// queued and has not reached the provider.
func (f *stoppedHelper) stillHeld(t *testing.T, s *server, p string) {
	t.Helper()
	stays(t, s, f.a, f.aRun, "stopped")
	if q := queuedTexts(t, s, f.a); !slices.ContainsFunc(q, func(x string) bool { return strings.Contains(x, _heldText) }) {
		t.Errorf("stopped helper's queued messages %q, want the held continue %q", q, _heldText)
	}
	if sessionWith(t, f.m, p, _heldText) != nil {
		t.Errorf("the held continue reached the provider, want it held for the boss's next continue")
	}
}

// TestStopHoldsHelper_SiblingEndsStartsNothing: a helper stopped with a continue held for it
// starts no run when a sibling's run ends done, whoever stopped it, and the message stays queued.
func TestStopHoldsHelper_SiblingEndsStartsNothing(t *testing.T) {
	t.Parallel()
	for _, p := range _oneProvider {
		for _, way := range _stopWays {
			t.Run(p+"/"+way.name, func(t *testing.T) {
				f := newStoppedHelper(t, p, way.stop)
				helpersEnd(t, f.s, f.b)
				f.stillHeld(t, f.s, p)
			})
		}
	}
}

// TestStopHoldsHelper_RestartStartsNothing: the hold is stored on the thread. After a restart,
// which fails the sibling's run it interrupted, the stopped helper starts no run and its message
// stays queued.
func TestStopHoldsHelper_RestartStartsNothing(t *testing.T) {
	t.Parallel()
	for _, p := range _oneProvider {
		for _, way := range _stopWays {
			t.Run(p+"/"+way.name, func(t *testing.T) {
				f := newStoppedHelper(t, p, way.stop)
				f.s.kill()
				s := f.m.serve(t)

				if r := settled(t, s, f.b); r.Status != "failed" {
					t.Fatalf("sibling run going at the restart: status %q, want failed", r.Status)
				}
				f.stillHeld(t, s, p)
			})
		}
	}
}

// TestStopHoldsHelper_NextContinueSendsAll: the boss's next continue to the stopped helper starts
// one run with the held message and the new one in one prompt, the held one first, and the queue
// is empty after.
func TestStopHoldsHelper_NextContinueSendsAll(t *testing.T) {
	t.Parallel()
	for _, p := range _oneProvider {
		t.Run(p, func(t *testing.T) {
			f := newStoppedHelper(t, p, _stopWays[1].stop)
			s := f.s

			got := useAtoll(t, s, f.boss, f.ev, continueCall(f.a, _keepPrompt))
			if len(got) != 1 || got[0] == "atoll/continue: unavailable" {
				t.Fatalf("continue to the stopped helper: reply %q, want it accepted", got)
			}
			eventually(t, 10*time.Second, "a new run of the stopped helper", func() bool {
				return getThread(t, s, f.a).RunID != f.aRun
			})
			if r := settled(t, s, f.a); r.Status != "done" {
				t.Fatalf("helper's run for the continue: status %q, error %q; want done", r.Status, r.Error)
			}
			prompts := promptsWith(t, f.m, p, _keepPrompt)
			if len(prompts) != 1 || !inOrder(prompts[0], _heldText, _keepPrompt) {
				t.Fatalf("provider prompts with the new continue %q, want one with the held text, then the new one", prompts)
			}
			if got := promptsWith(t, f.m, p, _heldText); len(got) != 1 {
				t.Errorf("%d provider prompts hold the held text, want 1", len(got))
			}
			if q := queuedTexts(t, s, f.a); len(q) != 0 {
				t.Errorf("helper's queued messages %q after the continue, want none", q)
			}
		})
	}
}

// TestStopHoldsHelper_FreedSlotGoesToOthers: with 3 helper runs going, a continue held for a
// running helper and one held by the cap for an idle helper, stop_helper on the first frees a slot:
// the idle helper's message takes it and the stopped helper's stays held.
func TestStopHoldsHelper_FreedSlotGoesToOthers(t *testing.T) {
	t.Parallel()
	for _, p := range _oneProvider {
		t.Run(p, func(t *testing.T) {
			m, s := start(t, p)
			poteto, tester := s.newBot(t, "Poteto", p), s.newBot(t, "Tester", p)
			setDelegates(t, s, poteto, tester)
			boss := s.newThread(t, poteto)
			ev := s.events(t, boss)

			idle := delegated(t, useAtoll(t, s, boss, ev, delegateCall("Tester", _keepPrompt, ""))[0])
			first := settled(t, s, idle)
			if first.Status != "done" {
				t.Fatalf("idle helper's run: status %q, error %q; want done", first.Status, first.Error)
			}
			quietBoss(t, s, boss.ID, tester.ID, 1)

			lines := useAtoll(t, s, boss, ev, delegateCall("Tester", _reviews[0], ""), delegateCall("Tester", _reviews[1], ""), delegateCall("Tester", _reviews[2], ""))
			ids, runs := []string{}, []string{}
			for _, l := range lines {
				id := delegated(t, l)
				ids, runs = append(ids, id), append(runs, helperRunning(t, s, id))
			}

			useAtoll(t, s, boss, ev, continueCall(ids[0], _heldText))
			if got := useAtoll(t, s, boss, ev, continueCall(idle, _askPrompt)); len(got) == 0 || got[0] == "atoll/continue: unavailable" {
				t.Fatalf("continue to the idle helper: reply %q, want it accepted", got)
			}
			stays(t, s, idle, first.ID, "done")

			useAtoll(t, s, boss, ev, stopHelperCall(ids[0]))
			stopped(t, s, ids[0], runs[0])
			eventually(t, 10*time.Second, "the idle helper's held message reaches the provider", func() bool {
				return sessionWith(t, m, p, _askPrompt) != nil
			})
			stays(t, s, ids[0], runs[0], "stopped")
			if q := queuedTexts(t, s, ids[0]); !slices.ContainsFunc(q, func(x string) bool { return strings.Contains(x, _heldText) }) {
				t.Errorf("stopped helper's queued messages %q, want the held continue %q", q, _heldText)
			}
			for i := 1; i < 3; i++ {
				if th := getThread(t, s, ids[i]); th.RunID != runs[i] || th.RunStatus != "running" {
					t.Errorf("helper %s: latest run %s is %q, want %s still running", ids[i], th.RunID, th.RunStatus, runs[i])
				}
			}
		})
	}
}

// TestStopHoldsHelper_ApprovalReplyKeepsReportsHeld: with a stopped lead holding a helper's report,
// a reply in its thread that answers another helper's approval delivers no report and starts no
// run, and the person's next ordinary message still delivers the held reports.
func TestStopHoldsHelper_ApprovalReplyKeepsReportsHeld(t *testing.T) {
	t.Parallel()
	for _, p := range _oneProvider {
		t.Run(p, func(t *testing.T) {
			m, s := startWindow(t, p, _holdWindow.String())
			poteto := s.newBot(t, "Poteto", p)
			setDelegates(t, s, poteto, s.newBot(t, "Tester", p))
			boss := s.newThread(t, poteto)
			ev := s.events(t, boss)
			ids, _ := delegateTester(t, s, boss, ev, _slowPrompt, commandPrompt("make check"))
			slow, asker := ids[0], ids[1]

			busy := s.send(t, boss, _writePrompt)
			until(t, ev, busy.ID, "approval")
			s.do(t, "POST", "/api/runs/"+busy.ID+"/stop", nil, nil, http.StatusNoContent)
			if got := s.finished(t, ev, busy.ID); got.Status != "stopped" {
				t.Fatalf("boss run after the person's Stop: status %q, want stopped", got.Status)
			}
			if r := settled(t, s, slow); r.Status != "done" {
				t.Fatalf("slow helper %s: status %q, error %q; want done", slow, r.Status, r.Error)
			}
			id := asking(t, s, asker).Approval.ID

			const text = "Run the linter instead."
			out := s.reply(t, boss, text)
			if len(out.Answered) != 1 || out.Answered[0] != id || out.Message.Queued || out.Run != nil {
				t.Fatalf("reply %+v, want helper %s's approval answered, the message not queued and no run", out, asker)
			}
			if r := settled(t, s, asker); !strings.Contains(r.Reply, text) {
				t.Errorf("helper %s's reply %q, want it told %q", asker, r.Reply, text)
			}
			bossStill(t, s, boss.ID, busy.ID, time.Now().Add(_holdWindow+2*time.Second), "after the reply and the wake window")
			if got := promptsWith(t, m, p, "Reef: Tester "); len(got) != 0 {
				t.Errorf("the provider got held reports %q after a reply that answered an approval, want them held", got)
			}
			if got := promptsWith(t, m, p, text); len(got) != 0 {
				t.Errorf("the provider got the reply %q as a boss prompt %q, want it kept as the answer", text, got)
			}

			const ask = "Tester, any news?"
			run := s.send(t, boss, ask)
			if got := s.finished(t, ev, run.ID); got.Status != "done" {
				t.Fatalf("boss run for the person's message: status %q, error %q; want done", got.Status, got.Error)
			}
			prompts := promptsWith(t, m, p, ask)
			if len(prompts) != 1 || !inOrder(prompts[0], ask, finishedNotice("Tester", slow)) {
				t.Fatalf("boss prompts %q, want one with the person's text, then Tester's header for helper %s", prompts, slow)
			}
			holdsAll(t, "the prompt with the person's message", prompts[0], asker)
		})
	}
}
