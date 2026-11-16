package e2e

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

// prCI is the ci of a thread's PR in the API.
type prCI struct {
	Head    string   `json:"head"`
	Passed  int      `json:"passed"`
	Failed  int      `json:"failed"`
	Pending int      `json:"pending"`
	Names   []string `json:"names"`
}

// ci is the ci of the thread's only PR; the zero value when none is stored.
func (f *prWakeFixture) ci(t *testing.T, th thread) prCI {
	t.Helper()
	prs := threadPRs(t, f.s, th.ID)
	if len(prs) != 1 {
		t.Fatalf("thread %s tracks %d PRs, want 1", th.ID, len(prs))
	}
	var c prCI
	if len(prs[0].CI) > 0 && string(prs[0].CI) != "null" {
		if err := json.Unmarshal(prs[0].CI, &c); err != nil {
			t.Fatalf("ci %s: %v", prs[0].CI, err)
		}
	}
	return c
}

// waitCI waits until the thread's PR shows a ci for which ok is true, and fails with the last one.
func (f *prWakeFixture) waitCI(t *testing.T, th thread, what string, ok func(prCI) bool) {
	t.Helper()
	var c prCI
	eventually(t, 30*time.Second, fmt.Sprintf("%s (last ci %+v)", what, c), func() bool {
		c = f.ci(t, th)
		return ok(c)
	})
}

// redHead opens PR 1, fails the "unit" check of its head and waits for the one wake of that result.
func (f *prWakeFixture) redHead(t *testing.T) (th thread, sha string) {
	t.Helper()
	th = f.openPR(t, 1)
	sha = headSHA(1)
	f.gh.setChecks(sha, []fakeCheck{{"build", "completed", "success"}, {"unit", "completed", "failure"}}, nil)
	w := f.waitWake(t, th, 1)
	if !strings.Contains(w.Text, "CI finished on PR #1: 1 passed, 1 failed") {
		t.Fatalf("first wake %q, want the red result", w.Text)
	}
	f.waitCI(t, th, "the red result is stored", func(c prCI) bool { return c.Failed == 1 })
	return th, sha
}

// A head reported red keeps being read: its failed check re-runs on the same commit and the PR's CI
// follows, pending while it runs, then passed. No new commit.
func TestPRWake_RerunOnSameCommitIsFollowed(t *testing.T) {
	for _, p := range _oneProvider {
		t.Run(p, func(t *testing.T) {
			f := newPRWakeFixture(t, p)
			th, sha := f.redHead(t)

			f.gh.setChecks(sha, []fakeCheck{{"build", "completed", "success"}, {"unit", "in_progress", ""}}, nil)
			f.waitCI(t, th, "the re-run shows pending", func(c prCI) bool { return c.Pending == 1 && c.Failed == 0 })

			f.gh.setChecks(sha, []fakeCheck{{"build", "completed", "success"}, {"unit", "completed", "success"}}, nil)
			f.waitCI(t, th, "the re-run shows passed", func(c prCI) bool { return c.Passed == 2 && c.Failed == 0 && c.Pending == 0 })

			if c := f.ci(t, th); c.Head != sha {
				t.Errorf("ci head %q, want the same commit %q", c.Head, sha)
			}
		})
	}
}

// Red then green on the same commit wakes the Bot once more, with the green result, and never again
// for that result.
func TestPRWake_RerunGreenWakesOnce(t *testing.T) {
	for _, p := range _oneProvider {
		t.Run(p, func(t *testing.T) {
			f := newPRWakeFixture(t, p)
			th, sha := f.redHead(t)

			f.gh.setChecks(sha, []fakeCheck{{"build", "completed", "success"}, {"unit", "in_progress", ""}}, nil)
			f.waitCI(t, th, "the re-run shows pending", func(c prCI) bool { return c.Pending == 1 })
			if w := f.wakes(t, th); len(w) != 1 {
				t.Fatalf("a re-run still pending woke the Bot again: %+v, want only the red wake", w)
			}

			f.gh.setChecks(sha, []fakeCheck{{"build", "completed", "success"}, {"unit", "completed", "success"}}, nil)
			w := f.waitWake(t, th, 2)
			if w.Kind != "ci" || w.FromBotID != "" || !strings.Contains(w.Text, "CI finished on PR #1: 2 passed") || strings.Contains(w.Text, "failed") {
				t.Errorf("second wake %+v, want kind ci, text %q and no failed check", w, "CI finished on PR #1: 2 passed")
			}

			f.quiet(t, 1, "green result already reported", th, 2, 2)
		})
	}
}

// A red head is read at a slow cadence: over many polls of the PR its checks are read now and then,
// not on every poll.
func TestPRWake_RedHeadIsReadSlowly(t *testing.T) {
	for _, p := range _oneProvider {
		t.Run(p, func(t *testing.T) {
			f := newPRWakeFixture(t, p)
			_, sha := f.redHead(t)

			const polls = 30
			checks, pulls := f.gh.checkReads(sha), f.gh.pullReads(1)
			eventually(t, 30*time.Second, "the server polls the PR 30 more times", func() bool {
				return f.gh.pullReads(1) >= pulls+polls
			})
			got := f.gh.checkReads(sha) - checks
			if got < 1 || got > polls/2 {
				t.Errorf("%d reads of the red head's check runs over %d+ polls of the PR, want between 1 and %d (slow, not every poll)", got, polls, polls/2)
			}
		})
	}
}

// A head reported green is not read again: its check runs are read no more over many polls.
func TestPRWake_GreenHeadIsNotReadAgain(t *testing.T) {
	for _, p := range _oneProvider {
		t.Run(p, func(t *testing.T) {
			f := newPRWakeFixture(t, p)
			th := f.openPR(t, 1)
			sha := headSHA(1)
			f.gh.setChecks(sha, []fakeCheck{{"build", "completed", "success"}, {"unit", "completed", "success"}}, nil)
			f.waitWake(t, th, 1)

			pulls := f.gh.pullReads(1)
			eventually(t, 30*time.Second, "the server polls the PR twice more", func() bool { return f.gh.pullReads(1) >= pulls+2 })
			checks := f.gh.checkReads(sha)
			pulls = f.gh.pullReads(1)
			eventually(t, 30*time.Second, "the server polls the PR 15 more times", func() bool { return f.gh.pullReads(1) >= pulls+15 })
			if got := f.gh.checkReads(sha) - checks; got != 0 {
				t.Errorf("%d more reads of the green head's check runs over 15 polls of the PR, want none", got)
			}
			if w := f.wakes(t, th); len(w) != 1 {
				t.Errorf("wakes %+v, want one", w)
			}
		})
	}
}
