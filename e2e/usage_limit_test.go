package e2e

import (
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Contract: usage-limit-contract.md.

type limitRun struct {
	Status    string `json:"status"`
	Error     string `json:"error"`
	ErrorKind string `json:"error_kind"`
	Provider  string `json:"provider"`
	ResetsAt  string `json:"resets_at"`
}

func (s *server) limitRun(t *testing.T, id string) limitRun {
	t.Helper()
	var r limitRun
	s.do(t, "GET", "/api/runs/"+id, nil, &r, http.StatusOK)
	return r
}

// limit makes the fake provider's server refuse every turn until at, as a reached usage limit
// does, while the login stays valid.
func (m *machine) limit(t *testing.T, provider string, at time.Time) {
	t.Helper()
	file := map[string]string{"claude": ".credentials.json", "codex": "auth.json"}[provider]
	writeFile(t, filepath.Join(m.providerDir(provider), file),
		`{"fake":"limited","resets_at":`+strconv.FormatInt(at.Unix(), 10)+`}`)
}

// wantLimited checks that a run is stored as stopped by a usage limit that resets at at.
func wantLimited(t *testing.T, r limitRun, provider string, at time.Time) {
	t.Helper()
	if r.Status != "failed" || r.ErrorKind != "rate_limited" {
		t.Errorf("run on a reached %s limit: status %q, error_kind %q, error %q; want failed with error_kind rate_limited", provider, r.Status, r.ErrorKind, r.Error)
	}
	if r.Provider != provider {
		t.Errorf("run's provider %q, want %q", r.Provider, provider)
	}
	got, err := time.Parse(time.RFC3339, r.ResetsAt)
	if err != nil || !got.Equal(at) {
		t.Errorf("run's resets_at %q (%v), want the provider's reset time %s", r.ResetsAt, err, at.UTC().Format(time.RFC3339))
	}
}

// TestUsageLimit_StoredWithResetTime: a run the provider's usage limit stopped is stored failed
// with error_kind rate_limited and the reset time the provider gave (Claude's rate_limit_info,
// Codex's rate-limit snapshot), and the provider stays signed in.
func TestUsageLimit_StoredWithResetTime(t *testing.T) {
	t.Parallel()
	for _, p := range _providers {
		t.Run(p, func(t *testing.T) {
			m, s := start(t, p)
			at := time.Now().Add(2 * time.Hour).Truncate(time.Second)
			m.limit(t, p, at)

			th := s.newThread(t, s.newBot(t, "Kelp", p))
			ch := s.events(t, th)
			r := s.send(t, th, _hey)
			s.finished(t, ch, r.ID)
			wantLimited(t, s.limitRun(t, r.ID), p, at)

			if !s.providers(t)[p].SignedIn {
				t.Errorf("GET /api/providers after the limit: %s signed out, want signed in", p)
			}
		})
	}
}

// TestUsageLimit_NextMessageAfterResetRuns: Reef does not block the Bot; once the reset time has
// passed the next message in the thread starts a run that goes as any other.
func TestUsageLimit_NextMessageAfterResetRuns(t *testing.T) {
	t.Parallel()
	for _, p := range _providers {
		t.Run(p, func(t *testing.T) {
			m, s := start(t, p)
			th := s.newThread(t, s.newBot(t, "Kelp", p))
			ch := s.events(t, th)

			at := time.Now().Add(4 * time.Second).Truncate(time.Second)
			m.limit(t, p, at)
			r := s.send(t, th, _hey)
			s.finished(t, ch, r.ID)
			wantLimited(t, s.limitRun(t, r.ID), p, at)

			time.Sleep(time.Until(at) + time.Second)
			next := s.send(t, th, _hey)
			got := s.finished(t, ch, next.ID)
			if got.Status != "done" || got.Reply != "Hello." {
				t.Fatalf("run after the reset time: status %q, reply %q, error %q; want done, %q", got.Status, got.Reply, got.Error, "Hello.")
			}
			if k := s.limitRun(t, next.ID).ErrorKind; k != "" {
				t.Errorf("run after the reset time: error_kind %q, want none", k)
			}
		})
	}
}

// TestUsageLimit_WakeSaysHelperIsOut: a helper run a usage limit stopped wakes the boss with a
// message that the helper is out until the reset time, and none of the provider's text. The boss
// runs on the other provider, which is not limited.
func TestUsageLimit_WakeSaysHelperIsOut(t *testing.T) {
	t.Parallel()
	for _, limited := range _providers {
		boss := map[string]string{"claude": "codex", "codex": "claude"}[limited]
		t.Run(limited, func(t *testing.T) {
			m := newMachine(t, "claude", "codex")
			m.signIn(t, "claude")
			m.signIn(t, "codex")
			s := m.serve(t)
			at := time.Now().Add(2 * time.Hour).Truncate(time.Second)
			m.limit(t, limited, at)

			poteto, tester := s.newBot(t, "Poteto", boss), s.newBot(t, "Tester", limited)
			setDelegates(t, s, poteto, tester)
			bossThread := s.newThread(t, poteto)
			ev := s.events(t, bossThread)

			helper := delegated(t, useAtoll(t, s, bossThread, ev, delegateCall("Tester", _reportPrompt, ""))[0])
			h := settled(t, s, helper)
			wantLimited(t, s.limitRun(t, h.ID), limited, at)

			msg, c, wake := woken(t, m, s, boss, bossThread.ID, tester.ID, 1)
			holdsAll(t, "message from Tester", msg.Text, helper,
				"Tester is out until "+at.UTC().Format(time.RFC3339)+".", "Do not send it work until then.")
			for _, text := range []string{"You've hit your", "5-hour", "Upgrade to Plus", "chatgpt.com", "(UTC)", "try again later"} {
				if strings.Contains(msg.Text, text) {
					t.Errorf("message from Tester %q holds the provider's text %q", msg.Text, text)
				}
			}
			if got := resumed(c, boss); got != bossSessionID(boss) {
				t.Errorf("boss run with Tester's message resumed %q, want the boss's session %q", got, bossSessionID(boss))
			}
			if wake.Status != "done" {
				t.Errorf("boss run Tester's message started: status %q, error %q; want done", wake.Status, wake.Error)
			}
			// A helper at its limit is a failure Reef knows of: the boss called no tell_user, so its
			// final message is posted as a reply.
			if text := botText(t, s, bossThread.ID, wake.ID); text != _wakeReply {
				t.Errorf("Bot message of the background run %q, want its final message %q posted (guard)", text, _wakeReply)
			}
		})
	}
}
