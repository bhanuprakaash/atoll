package e2e

import (
	"strings"
	"testing"
)

// Contract: pr-wake-contract.md.

func TestPRWake_MergedWakesTheBot(t *testing.T) {
	for _, p := range _providers {
		t.Run(p, func(t *testing.T) {
			f := newPRWakeFixture(t, p)
			th := f.openPR(t, 1)
			first := f.s.thread(t, th.ID).RunID
			f.quiet(t, 1, "an open pull request", th, 0, 0)

			f.gh.setPull(1, "closed", true)
			w := f.waitWake(t, th, 1)

			f.wantWake(t, th, w, "merged", "PR #1 merged")
			f.wantReply(t, th, "Merge noted.", first)
			if got := f.mrState(t, th); got != "merged" {
				t.Errorf("thread mr_state %q after the merge, want merged", got)
			}
		})
	}
}

func TestPRWake_ClosedUnmergedWakesTheBot(t *testing.T) {
	for _, p := range _providers {
		t.Run(p, func(t *testing.T) {
			f := newPRWakeFixture(t, p)
			th := f.openPR(t, 1)
			first := f.s.thread(t, th.ID).RunID

			f.gh.setPull(1, "closed", false)
			w := f.waitWake(t, th, 1)

			f.wantWake(t, th, w, "closed", "PR #1 closed")
			f.wantReply(t, th, "Close noted.", first)
			if got := f.mrState(t, th); got != "closed" {
				t.Errorf("thread mr_state %q after closing unmerged, want closed", got)
			}
		})
	}
}

// CI finishes once every check of the head commit is done: a pending check wakes nothing, and the
// wake gives the passed count and the failed checks' names.
func TestPRWake_CIFinishedWakesOnce(t *testing.T) {
	for _, p := range _providers {
		t.Run(p, func(t *testing.T) {
			f := newPRWakeFixture(t, p)
			th := f.openPR(t, 1)
			first := f.s.thread(t, th.ID).RunID
			sha := headSHA(1)

			f.gh.setChecks(sha,
				[]fakeCheck{{"build", "completed", "success"}, {"lint", "completed", "skipped"}, {"unit", "in_progress", ""}},
				[]fakeStatus{{"deploy/preview", "pending"}})
			f.quiet(t, 1, "CI with checks still pending", th, 0, 0)

			f.gh.setChecks(sha,
				[]fakeCheck{{"build", "completed", "success"}, {"lint", "completed", "skipped"}, {"unit", "completed", "failure"}},
				[]fakeStatus{{"deploy/preview", "error"}})
			w := f.waitWake(t, th, 1)

			f.wantWake(t, th, w, "ci", "CI finished on PR #1: 2 passed, 2 failed (")
			if one, other := `CI finished on PR #1: 2 passed, 2 failed ("unit", "deploy/preview")`,
				`CI finished on PR #1: 2 passed, 2 failed ("deploy/preview", "unit")`; w.Text != one && w.Text != other {
				t.Errorf("wake %q, want %q up to the order of the names", w.Text, one)
			}
			f.wantReply(t, th, "CI noted.", first)
			if got := f.mrState(t, th); got != "opened" {
				t.Errorf("thread mr_state %q after CI finished, want opened", got)
			}

			f.quiet(t, 1, "CI already reported", th, 1, 1)
			if got := len(f.wakes(t, th)); got != 1 {
				t.Errorf("thread has %d wakes after the same CI result was read again, want 1", got)
			}
		})
	}
}

// A check name is cleaned (no control characters, quotes or backslashes), cut at 60 characters and
// quoted; the sentence names up to 5 failed checks, then how many more.
func TestPRWake_CINamesAreCleanedAndCut(t *testing.T) {
	for _, p := range _providers {
		t.Run(p, func(t *testing.T) {
			f := newPRWakeFixture(t, p)
			th := f.openPR(t, 1)
			long := strings.Repeat("x", 70)
			f.gh.setChecks(headSHA(1), []fakeCheck{
				{"lint", "completed", "success"},
				{"bad \"name\"\\with\ttab\n", "completed", "failure"},
				{long, "completed", "failure"},
				{"c3", "completed", "failure"},
				{"c4", "completed", "failure"},
				{"c5", "completed", "failure"},
				{"c6", "completed", "failure"},
				{"c7", "completed", "timed_out"},
			}, nil)
			w := f.waitWake(t, th, 1)

			want := `CI finished on PR #1: 1 passed, 7 failed ("bad namewithtab", "` + long[:60] + `", "c3", "c4", "c5", and 2 more)`
			if w.Kind != "ci" || w.Text != want {
				t.Errorf("wake %+v, want kind ci, text %q", w, want)
			}
		})
	}
}

// A closed thread is not woken. A second, open thread with its own pull request is merged in the
// same moment, so a Bot that never watches cannot pass for one that stays quiet.
func TestPRWake_ClosedThreadIsNotWoken(t *testing.T) {
	for _, p := range _providers {
		t.Run(p, func(t *testing.T) {
			f := newPRWakeFixture(t, p)
			closed := f.openPR(t, 1)
			open := f.openPR(t, 2)
			f.s.do(t, "POST", "/api/threads/"+closed.ID+"/close", nil, nil, 204)

			f.gh.setPull(1, "closed", true)
			f.gh.setPull(2, "closed", true)
			w := f.waitWake(t, open, 1)
			if w.Kind != "merged" || !strings.Contains(w.Text, "PR #2 merged") {
				t.Fatalf("open thread's wake: %+v, want merged PR #2", w)
			}

			f.quiet(t, 2, "closed thread", closed, 0, 1)
			if got := f.reefPrompts(t); len(got) != 1 || !strings.Contains(got[0], "PR #2 merged") {
				t.Errorf("Reef prompts %q, want one, for PR #2", got)
			}
		})
	}
}

// One event is one wake: not on the next read of the same state, not when the pull request changes
// without a new event, and not after a restart. Each wake is one run.
func TestPRWake_OneWakePerEvent(t *testing.T) {
	for _, p := range _providers {
		t.Run(p, func(t *testing.T) {
			f := newPRWakeFixture(t, p)
			th := f.openPR(t, 1)

			f.gh.setPull(1, "closed", true)
			f.waitWake(t, th, 1)
			f.gh.setTitle(1, "fix: renamed after the merge")
			f.quiet(t, 1, "the same event read again", th, 1, 1)

			f.s.kill()
			f.m.killFakes()
			f.s = f.m.serve(t)
			f.quiet(t, 1, "after a restart", th, 1, 1)

			if got := len(f.m.sessions(t, p)); got != 2 {
				t.Errorf("%s ran %d times, want 2: the run that opened the pull request and one for the wake", p, got)
			}
		})
	}
}

// wantWake checks the wake message and the prompt of the run it started: from Reef, of the kind, with
// the sentence the design builds, and no message from the user beyond the one that began the thread.
func (f *prWakeFixture) wantWake(t *testing.T, th thread, w wakeMessage, kind, sentence string) {
	t.Helper()
	if w.Kind != kind || !strings.Contains(w.Text, sentence) || w.FromBotID != "" {
		t.Errorf("wake message %+v, want kind %q, text with %q, no from_bot_id", w, kind, sentence)
	}
	prompts := f.reefPrompts(t)
	if len(prompts) != 1 || !strings.HasPrefix(prompts[0], "Reef: ") || !strings.Contains(prompts[0], sentence) {
		t.Errorf("Reef prompts %q, want one: %q", prompts, "Reef: "+sentence)
	}
	var all []wakeMessage
	f.s.do(t, "GET", "/api/threads/"+th.ID+"/messages", nil, &all, 200)
	users := 0
	for _, m := range all {
		if m.Role == "user" && m.Kind == "" {
			users++
		}
	}
	if users != 1 {
		t.Errorf("thread has %d user messages, want only the one that began it: %+v", users, all)
	}
}

// wantReply checks the Bot answered the wake in a new run of the thread.
func (f *prWakeFixture) wantReply(t *testing.T, th thread, reply, firstRun string) {
	t.Helper()
	got := f.s.thread(t, th.ID)
	if got.RunID == firstRun {
		t.Fatalf("thread's latest run is still %s, the run that opened the pull request; want a new run for the wake", firstRun)
	}
	if run := f.s.run(t, got.RunID); run.Status != "done" || run.Reply != reply {
		t.Errorf("run of the wake %+v, want done with reply %q", run, reply)
	}
}

func (f *prWakeFixture) mrState(t *testing.T, th thread) string {
	t.Helper()
	var got struct {
		MRState string `json:"mr_state"`
	}
	f.s.do(t, "GET", "/api/threads/"+th.ID, nil, &got, 200)
	return got.MRState
}
