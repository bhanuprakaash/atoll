package e2e

import (
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"
)

// Continue (docs/design.md, App and UI, the failed-run line): a run that reached the provider and
// then stopped is continued by a new run that resumes the thread's session with a Reef notice, not
// the person's text, and adds no user message.
//
// Assumed API: POST /api/runs/{failed run id}/continue, no body, answers 202 {"run": {...}} like
// POST /api/threads/{id}/messages.
//
// testdata/claude/continue-*.jsonl play the resumed sessions of lifecycle-exit.jsonl and
// run-hang.jsonl. Their prompt is the start of the notice, so any reason matches.
const (
	_continueHead = "Your last run stopped before it finished ("
	_continueTail = "). Continue from where you stopped. Check what already happened before you repeat a step."

	_exitSession  = "5b9e2d70-4a1c-4f83-a6d2-8c0e7b3a9d14"
	_exitContinue = "Picked up after the exit."
	_hangContinue = "Picked up after the restart."
)

func continueRun(t *testing.T, s *server, runID string) runInfo {
	t.Helper()
	var out struct {
		Run runInfo `json:"run"`
	}
	s.do(t, "POST", "/api/runs/"+runID+"/continue", nil, &out, http.StatusAccepted)
	if out.Run.ID == "" || out.Run.ID == runID {
		t.Fatalf("continue of run %s returned %+v, want a new run", runID, out.Run)
	}
	return out.Run
}

func TestContinue_ResumesWithTheNoticeNotTheText(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name, text, session, reply string
		// reason is the exact reason in the notice; empty when only its shape is checked.
		reason string
		fail   func(t *testing.T, m *machine, s *server, th thread, ev <-chan event) (*server, runInfo)
	}{
		{
			name: "the provider fails mid-run", text: _lifeExit, session: _exitSession, reply: _exitContinue,
			reason: "something went wrong",
			fail: func(t *testing.T, _ *machine, s *server, th thread, ev <-chan event) (*server, runInfo) {
				failed := s.finished(t, ev, s.send(t, th, _lifeExit).ID)
				return s, failed
			},
		},
		{
			name: "a restart cuts the run short", text: _hangPrompt, session: _hangSession, reply: _hangContinue,
			fail: func(t *testing.T, m *machine, s *server, th thread, ev <-chan event) (*server, runInfo) {
				run := s.send(t, th, _hangPrompt)
				// The run has made its tool call, so it reached the model before the restart.
				until(t, ev, run.ID, "tool_call")
				for deadline := time.Now().Add(15 * time.Second); len(promptsWith(t, m, "claude", _hangPrompt)) == 0; {
					if time.Now().After(deadline) {
						t.Fatal("the provider never got the hang prompt")
					}
					time.Sleep(100 * time.Millisecond)
				}
				s.kill()
				s = m.serve(t)
				for deadline := time.Now().Add(15 * time.Second); s.run(t, run.ID).Status != "failed"; {
					if time.Now().After(deadline) {
						t.Fatalf("run after the restart: %+v, want failed", s.run(t, run.ID))
					}
					time.Sleep(100 * time.Millisecond)
				}
				return s, s.run(t, run.ID)
			},
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			m, s := start(t, "claude")
			th := s.newThread(t, s.newBot(t, "Ada", "claude"))
			s, failed := c.fail(t, m, s, th, s.events(t, th))
			if failed.Status != "failed" {
				t.Fatalf("first run: %+v, want failed", failed)
			}

			ev := s.events(t, th)
			next := continueRun(t, s, failed.ID)
			got := s.finished(t, ev, next.ID)
			if got.Status != "done" || got.Reply != c.reply {
				t.Fatalf("continued run: status %q, reply %q, error %q; want done with %q", got.Status, got.Reply, got.Error, c.reply)
			}

			notices := promptsWith(t, m, "claude", "Your last run stopped before it finished")
			if len(notices) != 1 {
				t.Fatalf("provider got %d continue notices %q, want 1", len(notices), notices)
			}
			if !strings.Contains(notices[0], _continueHead) || !strings.Contains(notices[0], _continueTail) {
				t.Errorf("notice %q, want %q{reason}%q", notices[0], _continueHead, _continueTail)
			}
			if c.reason != "" && !strings.Contains(notices[0], _continueHead+c.reason+_continueTail) {
				t.Errorf("notice %q, want reason %q", notices[0], c.reason)
			}
			if n := len(promptsWith(t, m, "claude", c.text)); n != 1 {
				t.Errorf("provider got the person's text %d times, want once (the original send)", n)
			}
			if strings.Contains(notices[0], c.text) {
				t.Errorf("notice %q holds the person's text", notices[0])
			}

			// The last call may be the thread's title session, whose stdin can land late.
			var continued *call
			for _, call := range m.calls(t, "claude") {
				if slices.ContainsFunc(call.stdin, func(l string) bool { return strings.Contains(l, _continueHead) }) {
					continued = call
				}
			}
			if continued == nil {
				t.Fatalf("no provider session got the continue notice")
			}
			if resumed := flagValue(continued.argv, "--resume"); resumed != c.session {
				t.Errorf("continued run passed --resume %q, want the thread's session %q", resumed, c.session)
			}

			var stored []struct {
				threadMessage
				Kind string `json:"kind"`
			}
			s.do(t, "GET", "/api/threads/"+th.ID+"/messages", nil, &stored, http.StatusOK)
			var users, replies []threadMessage
			for _, msg := range stored {
				switch {
				case msg.Role == "user" && msg.FromBotID == "" && msg.Kind == "":
					users = append(users, msg.threadMessage)
				case msg.Role == "bot" && msg.Text == c.reply:
					replies = append(replies, msg.threadMessage)
				}
			}
			if len(users) != 1 || users[0].Text != c.text {
				t.Errorf("user messages %+v, want only the person's %q", users, c.text)
			}
			if len(replies) != 1 {
				t.Errorf("bot messages with the continued reply: %+v, want 1", replies)
			}
		})
	}
}
