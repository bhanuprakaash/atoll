package e2e

// Contract: approval-card-contract.md.

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const _push = "git push origin HEAD"

// replied is the answer to a message that answered approvals.
type replied struct {
	Message struct {
		ID       string `json:"id"`
		Role     string `json:"role"`
		Text     string `json:"text"`
		Queued   bool   `json:"queued"`
		Answered []struct {
			ID   string `json:"id"`
			Tool string `json:"tool"`
		} `json:"answered_approvals"`
	} `json:"message"`
	Run      *runInfo `json:"run"`
	Answered []string `json:"answered"`
}

func (s *server) reply(t *testing.T, th thread, text string) replied {
	t.Helper()
	var out replied
	s.do(t, "POST", "/api/threads/"+th.ID+"/messages", map[string]string{"text": text}, &out, http.StatusAccepted)
	return out
}

func userTexts(t *testing.T, s *server, threadID, text string) int {
	t.Helper()
	n := 0
	for _, m := range messages(t, s, threadID) {
		if m.Role == "user" && m.Text == text {
			n++
		}
	}
	return n
}

// TestApprovalReply_DeniesWithTheText: a message sent while an approval is open answers it: the
// approval closes, the provider is told the text as the reason, the same run goes on (no second
// run, the message is not queued) and asks about the next command. A later answer to the closed
// approval is 404.
func TestApprovalReply_DeniesWithTheText(t *testing.T) {
	t.Parallel()
	const text = "Push to a branch instead, never to HEAD."
	for _, p := range _providers {
		t.Run(p, func(t *testing.T) {
			_, s := start(t, p)
			th := s.newThread(t, s.newBot(t, "Ada", p))
			ev := s.events(t, th)
			r := s.send(t, th, commandPrompt(_push, _tmpClean))
			first := expectCard(t, ev, r.ID, _push)
			id := asString(first.Data["id"])

			out := s.reply(t, th, text)
			if out.Run == nil || out.Run.ID != r.ID {
				t.Errorf("answer's run %+v, want the going run %s", out.Run, r.ID)
			}
			if len(out.Answered) != 1 || out.Answered[0] != id {
				t.Errorf("answer's answered %q, want [%s]", out.Answered, id)
			}
			m := out.Message
			if m.Role != "user" || m.Text != text || m.Queued || len(m.Answered) != 1 || m.Answered[0].ID != id {
				t.Errorf("message %+v, want the user's text, not queued, answered_approvals [%s]", m, id)
			}
			if closed := nextOf(t, ev, r.ID, "approval_closed"); closed != id {
				t.Errorf("approval_closed id %q, want %q", closed, id)
			}

			s.answerCard(t, expectCard(t, ev, r.ID, _tmpClean), map[string]any{"approve": true}, http.StatusNoContent)
			got := s.finished(t, ev, r.ID)
			lines := strings.Split(got.Reply, "\n")
			if got.Status != "done" || len(lines) != 2 ||
				!strings.HasPrefix(lines[0], _push+": denied, told: ") || !strings.Contains(lines[0], text) || lines[1] != _tmpClean+": ran" {
				t.Errorf("run %s: status %q, reply %q, want done, %q told the text, then %q", r.ID, got.Status, got.Reply, _push+": denied", _tmpClean+": ran")
			}
			if run := getThread(t, s, th.ID).RunID; run != r.ID {
				t.Errorf("thread's latest run %s, want the one run %s (the reply starts none)", run, r.ID)
			}
			if n := userTexts(t, s, th.ID, text); n != 1 {
				t.Errorf("the thread holds the reply %d times, want once", n)
			}
			s.do(t, "POST", "/api/approvals/"+id, map[string]bool{"approve": true}, nil, http.StatusNotFound)
		})
	}
}

// TestApprovalReply_YesIsNotAllow: Reef does not read words as an answer. "yes, go ahead" denies
// like any reply; the command does not run.
func TestApprovalReply_YesIsNotAllow(t *testing.T) {
	t.Parallel()
	const text = "yes, go ahead"
	for _, p := range _providers {
		t.Run(p, func(t *testing.T) {
			_, s := start(t, p)
			th := s.newThread(t, s.newBot(t, "Ada", p))
			ev := s.events(t, th)
			r := s.send(t, th, commandPrompt(_cacheClean))
			expectCard(t, ev, r.ID, _cacheClean)
			s.reply(t, th, text)
			got := s.finished(t, ev, r.ID)
			if want := _cacheClean + ": denied, told: "; !strings.HasPrefix(got.Reply, want) || !strings.Contains(got.Reply, text) {
				t.Errorf("reply %q, want %q and the text: the command must not run", got.Reply, want)
			}
		})
	}
}

// TestApprovalReply_CodexSteerRefused: when Codex refuses the turn/steer that carries a reply's
// text, the person learns it: the server logs the refusal with the run, and the run's end, which
// the thread shows, says so.
func TestApprovalReply_CodexSteerRefused(t *testing.T) {
	t.Parallel()
	const (
		text    = "Push to a branch instead, never to HEAD."
		refusal = "no active turn to steer"
	)
	m, s := start(t, "codex")
	if err := os.WriteFile(filepath.Join(m.bin, "codex.steer.refuse"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	th := s.newThread(t, s.newBot(t, "Ada", "codex"))
	ev := s.events(t, th)
	r := s.send(t, th, commandPrompt(_push))
	expectCard(t, ev, r.ID, _push)
	s.reply(t, th, text)
	got := s.finished(t, ev, r.ID)
	steered := false
	for _, c := range m.calls(t, "codex") {
		for _, l := range c.stdin {
			steered = steered || strings.Contains(l, `"turn/steer"`) && strings.Contains(l, text)
		}
	}
	if !steered {
		t.Fatalf("codex got no turn/steer with %q", text)
	}
	if !strings.Contains(got.Error, refusal) {
		t.Errorf("run %s ended %q with error %q, want the refusal %q", r.ID, got.Status, got.Error, refusal)
	}
	eventually(t, 10*time.Second, "a server log line of the run with the refusal", func() bool {
		for _, l := range strings.Split(s.log.String(), "\n") {
			if strings.Contains(l, r.ID) && strings.Contains(l, refusal) {
				return true
			}
		}
		return false
	})
}

// TestApprovalReply_HelperApprovalFromBossThread: a helper's approval shows in the boss thread, and
// a message written there answers it: the helper is told the text, the boss thread keeps the
// message and answered_approvals, and the boss is not given the message as a run of its own.
func TestApprovalReply_HelperApprovalFromBossThread(t *testing.T) {
	t.Parallel()
	const text = "Run the linter instead."
	for _, p := range _providers {
		t.Run(p, func(t *testing.T) {
			_, s := start(t, p)
			poteto, tester := s.newBot(t, "Poteto", p), s.newBot(t, "Tester", p)
			setDelegates(t, s, poteto, tester)
			boss := s.newThread(t, poteto)
			ev := s.events(t, boss)
			helper := delegated(t, useAtoll(t, s, boss, ev, delegateCall("Tester", commandPrompt("make check"), ""))[0])

			var id string
			deadline := time.Now().Add(30 * time.Second)
			for id == "" {
				var open struct {
					Approval *struct {
						ID string `json:"id"`
					} `json:"approval"`
				}
				s.do(t, "GET", "/api/threads/"+helper, nil, &open, http.StatusOK)
				if open.Approval != nil {
					id = open.Approval.ID
				} else if time.Now().After(deadline) {
					t.Fatalf("helper thread %s has no open approval after 30s", helper)
				} else {
					time.Sleep(_pace)
				}
			}

			out := s.reply(t, boss, text)
			if len(out.Answered) != 1 || out.Answered[0] != id || out.Message.Queued || len(out.Message.Answered) != 1 {
				t.Errorf("answer %+v, want the helper's approval %s answered and the message not queued", out, id)
			}
			if out.Run != nil {
				t.Errorf("answer's run %+v, want null: the boss's own run was not going", out.Run)
			}
			got := settled(t, s, helper)
			if want := "make check: denied, told: "; !strings.Contains(got.Reply, want) || !strings.Contains(got.Reply, text) {
				t.Errorf("helper's reply %q, want %q and the text", got.Reply, want)
			}
			if n := userTexts(t, s, boss.ID, text); n != 1 {
				t.Errorf("the boss thread holds the reply %d times, want once", n)
			}
			s.do(t, "POST", "/api/approvals/"+id, map[string]bool{"approve": true}, nil, http.StatusNotFound)
		})
	}
}
