package e2e

import (
	"net/http"
	"testing"
	"time"
)

// Contract: no-report-contract.md, "Thread labels". The API's `state` of a thread in GET /api/bots
// is needs_you, running, failed, limited or new (Replied), or empty; the UI turns them into words.

func opened(t *testing.T, s *server, th thread) {
	t.Helper()
	s.do(t, "POST", "/api/threads/"+th.ID+"/opened", nil, nil, http.StatusNoContent)
}

func wantState(t *testing.T, s *server, b bot, th thread, want, when string) {
	t.Helper()
	eventually(t, 15*time.Second, when+": thread state "+want, func() bool { return sidebarState(t, s, b.ID, th.ID) == want })
}

// A reply makes the thread Replied until the person opens it; the next reply does it again.
func TestThreadLabels_RepliedClearsWhenOpened(t *testing.T) {
	t.Parallel()
	for _, p := range _providers {
		t.Run(p, func(t *testing.T) {
			t.Parallel()
			_, s := start(t, p)
			b := s.newBot(t, "Ada", p)
			th := s.newThread(t, b)
			ev := s.events(t, th)

			s.finished(t, ev, s.send(t, th, _keepPrompt).ID)
			wantState(t, s, b, th, "new", "after a reply")
			opened(t, s, th)
			wantState(t, s, b, th, "", "after opening it")

			s.finished(t, ev, s.send(t, th, _askPrompt).ID)
			wantState(t, s, b, th, "new", "after a second reply")
			opened(t, s, th)
			wantState(t, s, b, th, "", "after opening it again")
		})
	}
}

// A failed run makes the thread Failed until the person opens it.
func TestThreadLabels_FailedClearsWhenOpened(t *testing.T) {
	t.Parallel()
	for _, p := range _providers {
		t.Run(p, func(t *testing.T) {
			t.Parallel()
			_, s := start(t, p)
			b := s.newBot(t, "Ada", p)
			th := s.newThread(t, b)

			if r := s.finished(t, s.events(t, th), s.send(t, th, _noRecordingPrompt).ID); r.Status != "failed" {
				t.Fatalf("run with no recording: status %q, want failed", r.Status)
			}
			wantState(t, s, b, th, "failed", "after a failed run")
			opened(t, s, th)
			wantState(t, s, b, th, "", "after opening it")
		})
	}
}

// The labels keep their priority: Needs you over Working over a reply nobody has read.
func TestThreadLabels_Priority(t *testing.T) {
	t.Parallel()
	for _, p := range _providers {
		t.Run(p, func(t *testing.T) {
			t.Parallel()
			_, s := start(t, p)
			b := s.newBot(t, "Ada", p)
			th := s.newThread(t, b)
			ev := s.events(t, th)

			s.finished(t, ev, s.send(t, th, _keepPrompt).ID)
			wantState(t, s, b, th, "new", "after a reply")

			hang := s.send(t, th, _hangPrompt)
			wantState(t, s, b, th, "running", "while a run goes with an unread reply")
			s.do(t, "POST", "/api/runs/"+hang.ID+"/stop", nil, nil, http.StatusNoContent)
			s.finished(t, ev, hang.ID)

			ask := s.send(t, th, _writePrompt)
			until(t, ev, ask.ID, "approval")
			wantState(t, s, b, th, "needs_you", "while an approval is open and a reply is unread")
		})
	}
}

// A thread of a provider at its usage limit reads Out until the reset time once the person has
// opened it; the label ends at the reset time.
func TestThreadLabels_LimitedUntilReset(t *testing.T) {
	t.Parallel()
	for _, p := range _providers {
		t.Run(p, func(t *testing.T) {
			t.Parallel()
			m, s := start(t, p)
			m.limit(t, p, time.Now().Add(2*time.Hour).Truncate(time.Second))
			b := s.newBot(t, "Ada", p)
			th := s.newThread(t, b)
			if r := s.finished(t, s.events(t, th), s.send(t, th, _keepPrompt).ID); r.Status != "failed" {
				t.Fatalf("run at the usage limit: status %q, want failed", r.Status)
			}
			opened(t, s, th)
			wantState(t, s, b, th, "limited", "after opening a thread whose provider is at its limit")
		})
	}
}

// A helper thread is never Replied: its replies are for its boss.
func TestThreadLabels_HelperThreadIsNeverReplied(t *testing.T) {
	t.Parallel()
	for _, p := range _providers {
		t.Run(p, func(t *testing.T) {
			t.Parallel()
			_, s := start(t, p)
			poteto, tester := s.newBot(t, "Poteto", p), s.newBot(t, "Tester", p)
			setDelegates(t, s, poteto, tester)
			boss := s.newThread(t, poteto)
			helper := delegated(t, useAtoll(t, s, boss, s.events(t, boss), delegateCall("Tester", _reportPrompt, ""))[0])
			allow(t, s, helper)
			if h := settled(t, s, helper); h.Status != "done" || h.Reply != _reportReply {
				t.Fatalf("helper run: status %q, reply %q, error %q; want done, %q", h.Status, h.Reply, h.Error, _reportReply)
			}
			time.Sleep(4 * _pace)
			if got := sidebarState(t, s, tester.ID, helper); got == "new" {
				t.Errorf("helper thread state %q after its reply, want it never Replied", got)
			}
		})
	}
}
