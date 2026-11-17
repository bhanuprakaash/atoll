package e2e

import (
	"net/http"
	"testing"
	"time"
)

const _holdWindow = 3 * time.Second

// _bossHangPrompt plays testdata/claude/chat-polish-hang.jsonl: a run on the boss's session that
// never ends. A message that names Tester plays the wake recording on the same session.
const _bossHangPrompt = "Continue waiting for the build to finish."

// stoppedLead is a boss thread whose run the person stopped, with two helpers of Tester: slow, whose
// run ends done by itself and whose report is held, and hung, whose run goes on.
type stoppedLead struct {
	m         *machine
	s         *server
	boss      thread
	ev        <-chan event
	tester    bot
	slow      string
	hung      string
	hungRun   string
	stoppedAt string // the boss's stopped run
}

func newStoppedLead(t *testing.T, p string) *stoppedLead {
	t.Helper()
	m, s := startWindow(t, p, _holdWindow.String())
	poteto, tester := s.newBot(t, "Poteto", p), s.newBot(t, "Tester", p)
	setDelegates(t, s, poteto, tester)
	boss := s.newThread(t, poteto)
	ev := s.events(t, boss)
	ids, _ := delegateTester(t, s, boss, ev, _slowPrompt, _hangPrompt)
	l := &stoppedLead{m: m, s: s, boss: boss, ev: ev, tester: tester, slow: ids[0], hung: ids[1]}
	l.hungRun = helperRunning(t, s, l.hung)

	busy := s.send(t, boss, _writePrompt)
	until(t, ev, busy.ID, "approval")
	s.do(t, "POST", "/api/runs/"+busy.ID+"/stop", nil, nil, http.StatusNoContent)
	if got := s.finished(t, ev, busy.ID); got.Status != "stopped" {
		t.Fatalf("boss run after the person's Stop: status %q, want stopped", got.Status)
	}
	l.stoppedAt = busy.ID
	return l
}

// slowEnds waits for the slow helper's run to end done: its report is then held.
func (l *stoppedLead) slowEnds(t *testing.T) {
	t.Helper()
	if r := settled(t, l.s, l.slow); r.Status != "done" {
		t.Fatalf("slow helper %s: status %q, error %q; want done", l.slow, r.Status, r.Error)
	}
}

// wakesAgain checks the hold is cleared: a helper of the boss that ends after the person's message
// wakes it as usual.
func wakesAgain(t *testing.T, m *machine, s *server, p string, boss thread, ev <-chan event) {
	t.Helper()
	ids, delegating := delegateTester(t, s, boss, ev, _reportPrompt)
	helpersEnd(t, s, ids...)
	wake := nextBossRun(t, s, boss.ID, delegating)
	if wake.Status != "done" {
		t.Errorf("boss run for a report after the held ones were delivered: status %q, error %q; want done", wake.Status, wake.Error)
	}
	if got := promptsWith(t, m, p, finishedNotice("Tester", ids[0])); len(got) != 1 {
		t.Errorf("%d boss prompts hold the report of helper %s, want 1: the hold is cleared", len(got), ids[0])
	}
}

// TestStopHolds_NoWakeAfterStop: after the person stops a lead, a helper that ends done and a helper
// the person stops start no boss run, at once or after the wake window, and no report reaches the
// provider.
func TestStopHolds_NoWakeAfterStop(t *testing.T) {
	t.Parallel()
	for _, p := range _oneProvider {
		t.Run(p, func(t *testing.T) {
			t.Parallel()
			l := newStoppedLead(t, p)
			s := l.s
			l.slowEnds(t)
			s.do(t, "POST", "/api/runs/"+l.hungRun+"/stop", nil, nil, http.StatusNoContent)
			if r := settled(t, s, l.hung); r.Status != "stopped" {
				t.Fatalf("hung helper %s: status %q, want stopped", l.hung, r.Status)
			}

			bossStill(t, s, l.boss.ID, l.stoppedAt, time.Now().Add(4*_pace), "at once after the helpers ended")
			bossStill(t, s, l.boss.ID, l.stoppedAt, time.Now().Add(_holdWindow+2*time.Second), "after the wake window")
			if th := getThread(t, s, l.boss.ID); th.RunStatus != "stopped" {
				t.Errorf("boss's latest run %s is %q after the helpers ended, want it still stopped", th.RunID, th.RunStatus)
			}
			for _, h := range []string{l.slow, l.hung} {
				if got := promptsWith(t, l.m, p, h); len(got) != 0 {
					t.Errorf("the provider got %d boss prompts that name helper %s, want none while the reports are held: %q", len(got), h, got)
				}
			}
		})
	}
}

// TestStopHolds_NextMessageDeliversReports: the held reports, a done one and one of a helper the
// person stopped, go with the person's next message in the same run, its text first; that run
// clears the hold, so a later report wakes the boss again.
func TestStopHolds_NextMessageDeliversReports(t *testing.T) {
	t.Parallel()
	for _, p := range _oneProvider {
		t.Run(p, func(t *testing.T) {
			t.Parallel()
			l := newStoppedLead(t, p)
			s := l.s
			l.slowEnds(t)
			s.do(t, "POST", "/api/runs/"+l.hungRun+"/stop", nil, nil, http.StatusNoContent)
			settled(t, s, l.hung)
			bossStill(t, s, l.boss.ID, l.stoppedAt, time.Now().Add(_holdWindow+2*time.Second), "after the wake window")

			const ask = "Tester, any news?"
			run := s.send(t, l.boss, ask)
			if got := s.finished(t, l.ev, run.ID); got.Status != "done" {
				t.Fatalf("boss run for the person's message: status %q, error %q; want done", got.Status, got.Error)
			}
			prompts := promptsWith(t, l.m, p, ask)
			if len(prompts) != 1 || !inOrder(prompts[0], ask, finishedNotice("Tester", l.slow)) {
				t.Fatalf("boss prompts %q, want one with the person's text, then Tester's header for helper %s", prompts, l.slow)
			}
			holdsAll(t, "the prompt with the person's message", prompts[0], l.hung, "The user stopped Tester")
			reports := 0
			for _, msg := range bossMessages(t, s, l.boss.ID) {
				if msg.FromBotID == l.tester.ID {
					reports++
					if msg.RunID != run.ID {
						t.Errorf("held report %q is a message of run %q, want the person's run %s", msg.Text, msg.RunID, run.ID)
					}
				}
			}
			if reports != 2 {
				t.Errorf("%d reports from Tester in the boss thread, want 2 (done, stopped)", reports)
			}
			bossStill(t, s, l.boss.ID, run.ID, time.Now().Add(_holdWindow+2*time.Second), "after the window: the reports went with the person's message")

			wakesAgain(t, l.m, s, p, l.boss, l.ev)
		})
	}
}

// TestStopHolds_HelpersKeepRunning: a helper of a stopped lead goes on after the Stop, and after the
// person's next message, until the person stops it.
func TestStopHolds_HelpersKeepRunning(t *testing.T) {
	t.Parallel()
	for _, p := range _oneProvider {
		t.Run(p, func(t *testing.T) {
			t.Parallel()
			l := newStoppedLead(t, p)
			s := l.s
			stays(t, s, l.hung, l.hungRun, "running")

			if got := s.finished(t, l.ev, s.send(t, l.boss, "Tester, any news?").ID); got.Status != "done" {
				t.Fatalf("boss run for the person's message: status %q, error %q; want done", got.Status, got.Error)
			}
			stays(t, s, l.hung, l.hungRun, "running")

			s.do(t, "POST", "/api/runs/"+l.hungRun+"/stop", nil, nil, http.StatusNoContent)
			stopped(t, s, l.hung, l.hungRun)
		})
	}
}

// TestStopHolds_SurvivesRestart: the hold is stored on the thread. After a restart a held done
// report and the restart message for a helper run it interrupted start no boss run, at once or
// after the wake window; the person's next message takes both.
func TestStopHolds_SurvivesRestart(t *testing.T) {
	t.Parallel()
	for _, p := range _oneProvider {
		t.Run(p, func(t *testing.T) {
			t.Parallel()
			l := newStoppedLead(t, p)
			l.slowEnds(t)

			l.s.kill()
			s := l.m.serve(t)

			got := s.run(t, l.hungRun)
			if got.Status != "failed" || got.Error != _restarted {
				t.Fatalf("hung helper run going at the restart: status %q, error %q; want failed, %q", got.Status, got.Error, _restarted)
			}
			bossStill(t, s, l.boss.ID, l.stoppedAt, time.Now().Add(4*_pace), "at once after the restart")
			bossStill(t, s, l.boss.ID, l.stoppedAt, time.Now().Add(_holdWindow+2*time.Second), "after the wake window since the restart")

			const ask = "Tester, any news after the restart?"
			ev := s.events(t, l.boss)
			run := s.send(t, l.boss, ask)
			if r := s.finished(t, ev, run.ID); r.Status != "done" {
				t.Fatalf("boss run for the person's message: status %q, error %q; want done", r.Status, r.Error)
			}
			prompts := promptsWith(t, l.m, p, ask)
			if len(prompts) != 1 || !inOrder(prompts[0], ask, l.slow) {
				t.Fatalf("boss prompts %q, want one with the person's text, then the report of helper %s", prompts, l.slow)
			}
			holdsAll(t, "the prompt with the person's message", prompts[0], finishedNotice("Tester", l.slow), "Tester stopped: "+_restarted+".\nNo branch yet. (thread "+l.hung+")")
			bossStill(t, s, l.boss.ID, run.ID, time.Now().Add(_holdWindow+2*time.Second), "after the window: the reports went with the person's message")

			wakesAgain(t, l.m, s, p, l.boss, ev)
		})
	}
}

// TestStopHolds_StopAndSendDelivers: Stop and send on a queued message stops the boss's run and
// starts the run with the message at once; the report of a helper that ended meanwhile goes in that
// run, and the hold is cleared.
func TestStopHolds_StopAndSendDelivers(t *testing.T) {
	t.Parallel()
	for _, p := range _oneProvider {
		t.Run(p, func(t *testing.T) {
			t.Parallel()
			m, s := startWindow(t, p, _holdWindow.String())
			poteto, tester := s.newBot(t, "Poteto", p), s.newBot(t, "Tester", p)
			setDelegates(t, s, poteto, tester)
			boss := s.newThread(t, poteto)
			ev := s.events(t, boss)
			ids, _ := delegateTester(t, s, boss, ev, _slowPrompt)
			busy := s.send(t, boss, _bossHangPrompt)
			eventually(t, 30*time.Second, "the boss's run "+busy.ID+" going", func() bool {
				th := getThread(t, s, boss.ID)
				return th.RunID == busy.ID && th.RunStatus == "running"
			})
			const ask = "Tester, report now."
			q := queue(t, s, boss, ask)
			if r := settled(t, s, ids[0]); r.Status != "done" {
				t.Fatalf("helper %s: status %q, error %q; want done", ids[0], r.Status, r.Error)
			}
			bossStill(t, s, boss.ID, busy.ID, time.Now().Add(_holdWindow+2*time.Second), "while the boss's run goes")

			var sent struct {
				Run runInfo `json:"run"`
			}
			s.do(t, "POST", "/api/threads/"+boss.ID+"/messages/"+q.ID+"/send", nil, &sent, http.StatusAccepted)
			if sent.Run.ID == "" || sent.Run.ID == busy.ID {
				t.Fatalf("Stop and send reply run %+v, want a new run", sent.Run)
			}
			if got := s.finished(t, ev, sent.Run.ID); got.Status != "done" {
				t.Fatalf("run for the queued message: status %q, error %q; want done", got.Status, got.Error)
			}
			if got := s.run(t, busy.ID).Status; got != "stopped" {
				t.Errorf("the run that was going: status %q, want stopped", got)
			}
			prompts := promptsWith(t, m, p, ask)
			if len(prompts) != 1 || !inOrder(prompts[0], ask, finishedNotice("Tester", ids[0])) {
				t.Fatalf("boss prompts %q, want one with the queued text, then Tester's header", prompts)
			}
			for _, msg := range bossMessages(t, s, boss.ID) {
				if msg.FromBotID == tester.ID && msg.RunID != sent.Run.ID {
					t.Errorf("report %q is a message of run %q, want run %s", msg.Text, msg.RunID, sent.Run.ID)
				}
			}
			bossStill(t, s, boss.ID, sent.Run.ID, time.Now().Add(_holdWindow+2*time.Second), "after the window: the report went with the message")

			wakesAgain(t, m, s, p, boss, ev)
		})
	}
}

// TestStopHolds_UnstoppedLeadStillWakes: of two boss threads, the one the person stopped holds its
// helper's report, and the other, not stopped, wakes on its helper's report after the window.
func TestStopHolds_UnstoppedLeadStillWakes(t *testing.T) {
	t.Parallel()
	for _, p := range _oneProvider {
		t.Run(p, func(t *testing.T) {
			t.Parallel()
			m, s := startWindow(t, p, _holdWindow.String())
			poteto, tester := s.newBot(t, "Poteto", p), s.newBot(t, "Tester", p)
			setDelegates(t, s, poteto, tester)
			stoppedBoss, freeBoss := s.newThread(t, poteto), s.newThread(t, poteto)
			stoppedEv, freeEv := s.events(t, stoppedBoss), s.events(t, freeBoss)
			stoppedIDs, _ := delegateTester(t, s, stoppedBoss, stoppedEv, _slowPrompt)
			freeIDs, freeDelegating := delegateTester(t, s, freeBoss, freeEv, _slowPrompt)

			busy := s.send(t, stoppedBoss, _writePrompt)
			until(t, stoppedEv, busy.ID, "approval")
			s.do(t, "POST", "/api/runs/"+busy.ID+"/stop", nil, nil, http.StatusNoContent)
			s.finished(t, stoppedEv, busy.ID)
			for _, h := range []string{stoppedIDs[0], freeIDs[0]} {
				if r := settled(t, s, h); r.Status != "done" {
					t.Fatalf("helper %s: status %q, error %q; want done", h, r.Status, r.Error)
				}
			}

			wake := nextBossRun(t, s, freeBoss.ID, freeDelegating)
			if wake.Status != "done" {
				t.Errorf("boss run for the report of the lead that was not stopped: status %q, error %q; want done", wake.Status, wake.Error)
			}
			if got := promptsWith(t, m, p, finishedNotice("Tester", freeIDs[0])); len(got) != 1 {
				t.Errorf("%d boss prompts hold the report of helper %s, want 1", len(got), freeIDs[0])
			}
			bossStill(t, s, stoppedBoss.ID, busy.ID, time.Now().Add(_holdWindow+2*time.Second), "after the wake window")
			if got := promptsWith(t, m, p, finishedNotice("Tester", stoppedIDs[0])); len(got) != 0 {
				t.Errorf("the provider got the held report of helper %s: %q, want it held", stoppedIDs[0], got)
			}
		})
	}
}
