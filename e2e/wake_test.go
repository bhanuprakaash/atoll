package e2e

import (
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"
)

// Contract: wake-contract.md.

// The helper plays testdata/*/helper-report.jsonl: it asks to run make check, then replies. Every
// wake text names the helper Bot, Tester: the prompt of testdata/*/wake.jsonl, which the boss's
// resumed session plays.
const (
	_reportPrompt  = "Run the checks, then report back."
	_reportReply   = "Tester here: the checks pass. Should I update the changelog too?"
	_wakeReply     = "Thanks, Tester."
	_helperSession = "3c9e4b17-6a2d-4f85-9e03-b7d1c5a8f264"
)

type threadMessage struct {
	Role      string `json:"role"`
	Text      string `json:"text"`
	FromBotID string `json:"from_bot_id"`
}

func messages(t *testing.T, s *server, threadID string) []threadMessage {
	t.Helper()
	var msgs []threadMessage
	s.do(t, "GET", "/api/threads/"+threadID+"/messages", nil, &msgs, http.StatusOK)
	return msgs
}

func fromBot(t *testing.T, s *server, threadID, botID string) []threadMessage {
	t.Helper()
	var out []threadMessage
	for _, msg := range messages(t, s, threadID) {
		if msg.FromBotID == botID {
			out = append(out, msg)
		}
	}
	return out
}

// sessionWith returns the provider session whose user prompt holds text, or nil.
func sessionWith(t *testing.T, m *machine, p, text string) *call {
	t.Helper()
	for _, r := range promptLog(t, m, p) {
		if r.Kind != "user" || !strings.Contains(r.Text, text) {
			continue
		}
		for _, c := range m.calls(t, p) {
			if c.pid == r.PID {
				return c
			}
		}
	}
	return nil
}

// resumed is the session a provider run resumed: claude's --resume, codex's thread/resume.
func resumed(c *call, p string) string {
	if p == "claude" {
		return flagValue(c.argv, "--resume")
	}
	if r := request(c, "thread/resume"); r != nil {
		return r.ThreadID
	}
	return ""
}

// The sessions of the recordings: the boss's (mcp-use, run-approve) and the helper's.
func bossSessionID(p string) string {
	return map[string]string{"claude": _claudeRunID, "codex": _codexThreadID}[p]
}

func helperSessionID(p string) string {
	return map[string]string{"claude": _helperSession, "codex": _codexThreadID}[p]
}

// woken waits for the boss thread's nth message from the helper Bot, for the provider session
// whose prompt holds its text, and for the boss's latest run to end. It returns the message, that
// session and the boss's latest run.
func woken(t *testing.T, m *machine, s *server, p, boss, helperBot string, n int) (threadMessage, *call, runInfo) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for {
		msgs := fromBot(t, s, boss, helperBot)
		if len(msgs) >= n {
			c := sessionWith(t, m, p, msgs[n-1].Text)
			if th := getThread(t, s, boss); c != nil && th.RunStatus != "running" && th.RunStatus != "waiting" {
				return msgs[n-1], c, s.run(t, th.RunID)
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("boss thread %s: %d messages from the helper Bot %+v after 30s, want message %d and a finished boss run whose prompt holds it",
				boss, len(msgs), msgs, n)
		}
		time.Sleep(_pace)
	}
}

// asking waits for the thread's open approval and returns the thread.
func asking(t *testing.T, s *server, threadID string) threadInfo {
	t.Helper()
	var th threadInfo
	eventually(t, 30*time.Second, "an approval in thread "+threadID, func() bool {
		th = s.thread(t, threadID)
		return th.Approval != nil
	})
	return th
}

// allow approves the thread's next approval and returns the run that asked.
func allow(t *testing.T, s *server, threadID string) string {
	t.Helper()
	th := asking(t, s, threadID)
	s.do(t, "POST", "/api/approvals/"+th.Approval.ID, map[string]bool{"approve": true}, nil, http.StatusNoContent)
	return th.RunID
}

func helperRunning(t *testing.T, s *server, threadID string) string {
	t.Helper()
	var th threadState
	eventually(t, 30*time.Second, "helper thread "+threadID+" has a running run", func() bool {
		th = getThread(t, s, threadID)
		return th.RunStatus == "running"
	})
	return th.RunID
}

func holdsAll(t *testing.T, what, text string, want ...string) {
	t.Helper()
	for _, w := range want {
		if !strings.Contains(text, w) {
			t.Errorf("%s %q does not hold %q", what, text, w)
		}
	}
}

// TestWake_ReportWakesBoss: a helper run that ends done posts one message from the helper Bot into
// the boss thread: `Reef: Tester finished thread {id}:` then the helper's reply, with no report
// lines, and that message starts a boss run that resumes the boss's session with it. The boss's
// continue then gives the helper a run that resumes the helper's session with the message.
func TestWake_ReportWakesBoss(t *testing.T) {
	t.Parallel()
	for _, p := range _providers {
		t.Run(p, func(t *testing.T) {
			m, s := start(t, p)
			poteto, tester := s.newBot(t, "Poteto", p), s.newBot(t, "Tester", p)
			setDelegates(t, s, poteto, tester)
			boss := s.newThread(t, poteto)
			ev := s.events(t, boss)

			helper := delegated(t, useAtoll(t, s, boss, ev, delegateCall("Tester", _reportPrompt, ""))[0])
			allow(t, s, helper)
			if h := settled(t, s, helper); h.Status != "done" || h.Reply != _reportReply {
				t.Fatalf("helper run: status %q, reply %q, error %q; want done, %q", h.Status, h.Reply, h.Error, _reportReply)
			}

			msg, c, wake := woken(t, m, s, p, boss.ID, tester.ID, 1)
			_, rest, _ := strings.Cut(msg.Text, "\n")
			if got, want := noticeHead(msg.Text), "Tester finished thread "+helper+":"; got != want {
				t.Errorf("message from Tester starts %q, want %q", got, want)
			}
			if got := promptsWith(t, m, p, finishedNotice("Tester", helper)); len(got) != 1 {
				t.Errorf("%d boss prompts hold the line %q, want 1", len(got), finishedNotice("Tester", helper))
			}
			if !strings.HasPrefix(strings.TrimSpace(rest), _reportReply) {
				t.Errorf("message from Tester continues %q, want the helper's reply %q first", rest, _reportReply)
			}
			for _, field := range []string{"Done:", "Tried:", "Blocked:", "Next:"} {
				if strings.Contains(msg.Text, field) {
					t.Errorf("message from Tester %q holds the report line %q, want the reply only", msg.Text, field)
				}
			}
			if got := resumed(c, p); got != bossSessionID(p) {
				t.Errorf("boss run with Tester's message resumed %q, want the boss's session %q", got, bossSessionID(p))
			}
			if wake.Status != "done" {
				t.Errorf("boss run Tester's message started: status %q, error %q; want done", wake.Status, wake.Error)
			}
			if got := fromBot(t, s, boss.ID, tester.ID); len(got) != 1 {
				t.Errorf("boss thread has %d messages from Tester %+v, want 1", len(got), got)
			}
			for _, msg := range messages(t, s, boss.ID) {
				if msg.Role == "user" && strings.HasPrefix(msg.Text, _mcpPromptHead) && msg.FromBotID != "" {
					t.Errorf("the user's own message %q has from_bot_id %q, want none", msg.Text, msg.FromBotID)
				}
			}

			const answer = "Yes, update it. " + _reportPrompt
			if lines := useAtoll(t, s, boss, ev, continueCall(helper, answer)); len(lines) == 0 || lines[0] == "atoll/continue: unavailable" {
				t.Fatalf("continue from the boss thread: reply %q, want it accepted", lines)
			}
			allow(t, s, helper)
			c = sessionWith(t, m, p, answer)
			if c == nil {
				t.Fatalf("no %s session got the boss's continue message %q", p, answer)
			}
			if got := resumed(c, p); got != helperSessionID(p) {
				t.Errorf("helper run after continue resumed %q, want the helper's session %q", got, helperSessionID(p))
			}
			if h := settled(t, s, helper); h.Status != "done" {
				t.Errorf("helper run after continue: status %q, error %q; want done", h.Status, h.Error)
			}
		})
	}
}

// TestWake_BusyThreadsDeliverLater: a helper that finishes while the boss's run is going is
// delivered when that run ends; a continue to a helper whose run is going succeeds and is delivered
// when that run ends. Every message arrives: the boss hears from Tester once per helper run.
func TestWake_BusyThreadsDeliverLater(t *testing.T) {
	t.Parallel()
	for _, p := range _providers {
		t.Run(p, func(t *testing.T) {
			m, s := start(t, p)
			poteto, tester := s.newBot(t, "Poteto", p), s.newBot(t, "Tester", p)
			setDelegates(t, s, poteto, tester)
			boss := s.newThread(t, poteto)
			ev := s.events(t, boss)

			// A message to a thread while a helper's approval is open answers that approval, so the
			// helper that ends during the boss's run is one with no approval: a going run the user stops.
			stopped := delegated(t, useAtoll(t, s, boss, ev, delegateCall("Tester", _hangPrompt, ""))[0])
			running := helperRunning(t, s, stopped)
			busy := s.send(t, boss, _writePrompt)
			es := until(t, ev, busy.ID, "approval")
			bossAsk := asString(es[len(es)-1].Data["id"])
			s.do(t, "POST", "/api/runs/"+running+"/stop", nil, nil, http.StatusNoContent)
			if h := settled(t, s, stopped); h.ID != running || h.Status != "stopped" {
				t.Fatalf("helper run %s after stop: %s %q; want stopped", running, h.ID, h.Status)
			}
			time.Sleep(4 * _pace)
			if got := fromBot(t, s, boss.ID, tester.ID); len(got) != 0 {
				t.Errorf("messages from Tester %+v while the boss's run %s waits, want none until it ends", got, busy.ID)
			}
			if th := getThread(t, s, boss.ID); th.RunID != busy.ID || th.RunStatus != "waiting" {
				t.Errorf("boss's latest run %s is %q, want %s still waiting", th.RunID, th.RunStatus, busy.ID)
			}

			s.do(t, "POST", "/api/approvals/"+bossAsk, map[string]bool{"approve": true}, nil, http.StatusNoContent)
			if got := s.finished(t, ev, busy.ID); got.Status != "done" {
				t.Fatalf("boss's run %s: status %q, error %q; want done", busy.ID, got.Status, got.Error)
			}
			msg, _, wake := woken(t, m, s, p, boss.ID, tester.ID, 1)
			holdsAll(t, "message from Tester after the boss's run ended", msg.Text, stopped, "The user stopped Tester")
			if wake.Status != "done" {
				t.Errorf("boss run Tester's message started: status %q, error %q; want done", wake.Status, wake.Error)
			}

			helper := delegated(t, useAtoll(t, s, boss, ev, delegateCall("Tester", _reportPrompt, ""))[0])
			allow(t, s, helper)
			if h := settled(t, s, helper); h.Status != "done" {
				t.Fatalf("helper run: status %q, error %q; want done", h.Status, h.Error)
			}
			msg, _, _ = woken(t, m, s, p, boss.ID, tester.ID, 2)
			holdsAll(t, "second message from Tester", msg.Text, helper, _reportReply)

			first, second := "Yes, update it. "+_reportPrompt, "Then tag a release. "+_reportPrompt
			// Both in one boss run: a message to the boss while the helper's approval is open would answer it.
			lines := useAtoll(t, s, boss, ev, continueCall(helper, first), continueCall(helper, second))
			if len(lines) < 2 || slices.Contains(lines, "atoll/continue: unavailable") {
				t.Fatalf("two continues in a row: replies %q, want both accepted", lines)
			}
			going := asking(t, s, helper)
			time.Sleep(4 * _pace)
			if th := s.thread(t, helper); th.RunID != going.RunID || th.RunStatus != "waiting" {
				t.Errorf("helper's latest run %s is %q after a second continue, want %s still waiting", th.RunID, th.RunStatus, going.RunID)
			}
			if sessionWith(t, m, p, second) != nil {
				t.Errorf("the second continue reached the provider while the helper's run %s goes, want it held until that run ends", going.RunID)
			}

			allow(t, s, helper)
			eventually(t, 30*time.Second, "a helper run after "+going.RunID+" for the second continue", func() bool {
				return s.thread(t, helper).RunID != going.RunID
			})
			allow(t, s, helper)
			c := sessionWith(t, m, p, second)
			if c == nil {
				t.Fatalf("no %s session got the second continue %q after the helper's run ended", p, second)
			}
			if got := resumed(c, p); got != helperSessionID(p) {
				t.Errorf("helper run with the second continue resumed %q, want %q", got, helperSessionID(p))
			}
			settled(t, s, helper)

			msg, _, wake = woken(t, m, s, p, boss.ID, tester.ID, 4)
			holdsAll(t, "fourth message from Tester", msg.Text, helper, _reportReply)
			if wake.Status != "done" {
				t.Errorf("boss's latest run after four messages from Tester: status %q, error %q; want done", wake.Status, wake.Error)
			}
		})
	}
}

// TestWake_HelperFailedStoppedDeleted: a helper run that fails, is stopped by the user, or whose
// Bot is deleted mid-run wakes the boss with a message from the helper Bot that says so.
func TestWake_HelperFailedStoppedDeleted(t *testing.T) {
	t.Parallel()
	for _, p := range _providers {
		t.Run(p, func(t *testing.T) {
			m, s := start(t, p)
			poteto, tester := s.newBot(t, "Poteto", p), s.newBot(t, "Tester", p)
			setDelegates(t, s, poteto, tester)
			boss := s.newThread(t, poteto)
			ev := s.events(t, boss)
			// guard is true when the notice holds a failure Reef knows of (a helper failed or was stopped):
			// the boss called no tell_user, so its final message is posted as a reply.
			woke := func(n int, helper, want string, guard bool) {
				t.Helper()
				msg, c, wake := woken(t, m, s, p, boss.ID, tester.ID, n)
				holdsAll(t, "message from Tester", msg.Text, helper, want)
				if got := resumed(c, p); got != bossSessionID(p) {
					t.Errorf("boss run with %q resumed %q, want %q", want, got, bossSessionID(p))
				}
				if wake.Status != "done" {
					t.Errorf("boss run %q started: status %q, error %q; want done", want, wake.Status, wake.Error)
				}
				if guard {
					if text := botText(t, s, boss.ID, wake.ID); text != _wakeReply {
						t.Errorf("Bot message of the run for %q is %q, want its final message %q posted (guard)", want, text, _wakeReply)
					}
				}
			}

			failed := delegated(t, useAtoll(t, s, boss, ev, delegateCall("Tester", _noRecordingPrompt, ""))[0])
			f := settled(t, s, failed)
			if f.Status != "failed" || f.Error == "" {
				t.Fatalf("helper run with no recording: status %q, error %q; want failed with a reason", f.Status, f.Error)
			}
			woke(1, failed, "Tester's run failed: "+f.Error, true)

			stopped := delegated(t, useAtoll(t, s, boss, ev, delegateCall("Tester", _hangPrompt, ""))[0])
			s.do(t, "POST", "/api/runs/"+helperRunning(t, s, stopped)+"/stop", nil, nil, http.StatusNoContent)
			woke(2, stopped, "The user stopped Tester", true)

			deleted := delegated(t, useAtoll(t, s, boss, ev, delegateCall("Tester", _hangPrompt, ""))[0])
			helperRunning(t, s, deleted)
			s.do(t, "DELETE", "/api/bots/"+tester.ID, nil, nil, http.StatusNoContent)
			woke(3, deleted, "Tester was deleted", false)
		})
	}
}

// TestWake_SurvivesRestart: a message held for a busy boss is delivered after a restart, once the
// boss's run is marked failed; a helper run going at a restart is marked failed and wakes the boss
// on start.
func TestWake_SurvivesRestart(t *testing.T) {
	t.Parallel()
	for _, p := range _providers {
		t.Run(p, func(t *testing.T) {
			m, s := start(t, p)
			poteto, tester := s.newBot(t, "Poteto", p), s.newBot(t, "Tester", p)
			setDelegates(t, s, poteto, tester)
			boss := s.newThread(t, poteto)
			ev := s.events(t, boss)

			// A message to a thread while a helper's approval is open answers that approval, so the
			// helper that ends during the boss's run is one with no approval: a going run the user stops.
			stopped := delegated(t, useAtoll(t, s, boss, ev, delegateCall("Tester", _hangPrompt, ""))[0])
			running := helperRunning(t, s, stopped)
			busy := s.send(t, boss, _writePrompt)
			until(t, ev, busy.ID, "approval")
			s.do(t, "POST", "/api/runs/"+running+"/stop", nil, nil, http.StatusNoContent)
			if h := settled(t, s, stopped); h.ID != running || h.Status != "stopped" {
				t.Fatalf("helper run %s after stop: %s %q; want stopped", running, h.ID, h.Status)
			}
			time.Sleep(4 * _pace)
			if got := fromBot(t, s, boss.ID, tester.ID); len(got) != 0 {
				t.Fatalf("messages from Tester %+v while the boss's run %s waits, want none until it ends", got, busy.ID)
			}

			s.kill()
			s = m.serve(t)

			if got := s.run(t, busy.ID); got.Status != "failed" {
				t.Errorf("boss's waiting run after a restart: status %q, want failed", got.Status)
			}
			msg, c, wake := woken(t, m, s, p, boss.ID, tester.ID, 1)
			holdsAll(t, "message from Tester after a restart", msg.Text, stopped, "The user stopped Tester")
			if got := resumed(c, p); got != bossSessionID(p) {
				t.Errorf("boss run with Tester's message resumed %q, want %q", got, bossSessionID(p))
			}
			if wake.Status != "done" {
				t.Errorf("boss run Tester's message started after a restart: status %q, error %q; want done", wake.Status, wake.Error)
			}

			hung := delegated(t, useAtoll(t, s, boss, s.events(t, boss), delegateCall("Tester", _hangPrompt, ""))[0])
			run := helperRunning(t, s, hung)

			s.kill()
			s = m.serve(t)

			got := s.run(t, run)
			if got.Status != "failed" || got.Error == "" {
				t.Fatalf("helper run going at a restart: status %q, error %q; want failed with a reason", got.Status, got.Error)
			}
			msg, _, wake = woken(t, m, s, p, boss.ID, tester.ID, 2)
			holdsAll(t, "message from Tester after a restart", msg.Text, hung, "Tester's run failed: "+got.Error)
			if wake.Status != "done" {
				t.Errorf("boss run started on start: status %q, error %q; want done", wake.Status, wake.Error)
			}
		})
	}
}

// TestWake_CloseBossStopsHelpers: stopping the boss's run leaves its helper going; closing the
// boss's thread stops the helper's run and closes its thread.
func TestWake_CloseBossStopsHelpers(t *testing.T) {
	t.Parallel()
	for _, p := range _providers {
		t.Run(p, func(t *testing.T) {
			_, s := start(t, p)
			poteto, tester := s.newBot(t, "Poteto", p), s.newBot(t, "Tester", p)
			setDelegates(t, s, poteto, tester)
			boss := s.newThread(t, poteto)
			ev := s.events(t, boss)

			helper := delegated(t, useAtoll(t, s, boss, ev, delegateCall("Tester", _hangPrompt, ""))[0])
			run := helperRunning(t, s, helper)
			busy := s.send(t, boss, _writePrompt)
			until(t, ev, busy.ID, "approval")
			s.do(t, "POST", "/api/runs/"+busy.ID+"/stop", nil, nil, http.StatusNoContent)
			if got := s.finished(t, ev, busy.ID); got.Status != "stopped" {
				t.Fatalf("boss's run after stop: status %q, want stopped", got.Status)
			}
			time.Sleep(4 * _pace)
			if th := getThread(t, s, helper); th.RunID != run || th.RunStatus != "running" {
				t.Errorf("helper's latest run %s is %q after the boss's run was stopped, want %s still running", th.RunID, th.RunStatus, run)
			}

			s.do(t, "POST", "/api/threads/"+boss.ID+"/close", nil, nil, http.StatusNoContent)
			if got := settled(t, s, helper); got.ID != run || got.Status != "stopped" {
				t.Errorf("helper run after the boss's thread closed: %s %q, want %s stopped", got.ID, got.Status, run)
			}
			var closed struct {
				Closed bool `json:"closed"`
			}
			s.do(t, "GET", "/api/threads/"+helper, nil, &closed, http.StatusOK)
			if !closed.Closed {
				t.Errorf("helper thread %s not closed after the boss's thread closed", helper)
			}
		})
	}
}
