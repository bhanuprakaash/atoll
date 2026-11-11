package e2e

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

// Contract: queued-messages-contract.md.

// _slowPrompt plays testdata/*/queued-slow.jsonl: a run that takes about 6 s and ends done, in
// the session that _askPrompt resumes.
const (
	_slowPrompt = "Take your time, then reply with just: ok"
	_thenText   = "Then say it twice."
)

type queuedMessage struct {
	ID    string `json:"id"`
	Role  string `json:"role"`
	Text  string `json:"text"`
	RunID string `json:"run_id"`
	// Queued is on the POST reply of a message that waits.
	Queued bool `json:"queued"`
}

type queueThread struct {
	RunID     string          `json:"run_id"`
	RunStatus string          `json:"run_status"`
	Queued    []queuedMessage `json:"queued"`
}

func queueOf(t *testing.T, s *server, threadID string) queueThread {
	t.Helper()
	var th queueThread
	s.do(t, "GET", "/api/threads/"+threadID, nil, &th, http.StatusOK)
	return th
}

// queue sends text to a thread whose run is going and returns the queued message.
func queue(t *testing.T, s *server, th thread, text string) queuedMessage {
	t.Helper()
	var out struct {
		Message queuedMessage `json:"message"`
		Run     *runInfo      `json:"run"`
	}
	s.do(t, "POST", "/api/threads/"+th.ID+"/messages", map[string]string{"text": text}, &out, http.StatusAccepted)
	if out.Message.ID == "" || out.Message.Role != "user" || out.Message.Text != text || !out.Message.Queued {
		t.Fatalf("send to a thread whose run goes: message %+v, want the user's message with an id, text %q and queued true", out.Message, text)
	}
	if out.Run != nil {
		t.Fatalf("send to a thread whose run goes: run %+v, want null: nothing started", *out.Run)
	}
	return out.Message
}

func queuedTexts(t *testing.T, s *server, threadID string) []string {
	t.Helper()
	var texts []string
	for _, q := range queueOf(t, s, threadID).Queued {
		texts = append(texts, q.Text)
	}
	return texts
}

// userPrompts returns the user prompts the provider got that hold any of texts.
func userPrompts(t *testing.T, m *machine, p string, texts ...string) []string {
	t.Helper()
	var out []string
	for _, r := range promptLog(t, m, p) {
		if r.Kind != "user" {
			continue
		}
		for _, text := range texts {
			if strings.Contains(r.Text, text) {
				out = append(out, r.Text)
				break
			}
		}
	}
	return out
}

func userMessages(t *testing.T, s *server, threadID string) []queuedMessage {
	t.Helper()
	var all, users []queuedMessage
	s.do(t, "GET", "/api/threads/"+threadID+"/messages", nil, &all, http.StatusOK)
	for _, m := range all {
		if m.Role == "user" {
			users = append(users, m)
		}
	}
	return users
}

// TestQueue_UserMessageWhileRunGoes: a message sent while a run is going is stored and listed as
// queued, never refused, not yet shown to the provider; when the run ends it is delivered as the
// next run's message, keeps its id, and leaves the queue. The send logs one INFO line.
func TestQueue_UserMessageWhileRunGoes(t *testing.T) {
	t.Parallel()
	for _, p := range _providers {
		t.Run(p, func(t *testing.T) {
			m, s := start(t, p)
			th := s.newThread(t, s.newBot(t, "Ada", p))
			ev := s.events(t, th)
			first := s.send(t, th, _slowPrompt)

			q := queue(t, s, th, _askPrompt)
			if got := queuedTexts(t, s, th.ID); len(got) != 1 || got[0] != _askPrompt {
				t.Fatalf("thread queued %q, want [%q]", got, _askPrompt)
			}
			if got := queueOf(t, s, th.ID).Queued[0].ID; got != q.ID {
				t.Errorf("thread queued id %q, want the POST's %q", got, q.ID)
			}
			for _, um := range userMessages(t, s, th.ID) {
				if um.Text == _askPrompt {
					t.Errorf("GET messages lists the queued message %+v, want it only in the queue until delivered", um)
				}
			}
			if got := userPrompts(t, m, p, _askPrompt); len(got) != 0 {
				t.Errorf("the provider got %q while its run goes, want nothing until the run ends", got)
			}
			var logged int
			for line := range strings.Lines(s.log.String()) {
				if strings.Contains(line, `"msg":"queued"`) && strings.Contains(line, th.ID) {
					logged++
				}
			}
			if logged != 1 {
				t.Errorf("server log has %d INFO lines %q with thread %s, want 1:\n%s", logged, "queued", th.ID, s.log)
			}

			eventually(t, 30*time.Second, "the queued message delivered after the run", func() bool {
				return len(queueOf(t, s, th.ID).Queued) == 0 && queueOf(t, s, th.ID).RunID != first.ID
			})
			if got := s.run(t, first.ID); got.Status != "done" {
				t.Errorf("first run %s: status %q, error %q; want done", got.ID, got.Status, got.Error)
			}
			if got := userPrompts(t, m, p, _askPrompt); len(got) != 1 {
				t.Errorf("the provider got the queued message in %d prompts %q, want 1", len(got), got)
			}
			next := queueOf(t, s, th.ID)
			if r := s.finished(t, ev, next.RunID); r.Status != "done" {
				t.Errorf("run %s for the queued message: status %q, error %q; want done", r.ID, r.Status, r.Error)
			}
			var delivered *queuedMessage
			for _, um := range userMessages(t, s, th.ID) {
				if um.Text == _askPrompt {
					delivered = &um
				}
			}
			if delivered == nil || delivered.ID != q.ID || delivered.RunID != next.RunID || delivered.Queued {
				t.Errorf("delivered message %+v, want id %q in run %q and not queued", delivered, q.ID, next.RunID)
			}
		})
	}
}

// TestQueue_SeveralArriveInOrderAsOneRun: messages queued during one run are delivered together as
// one run, in the order they were sent, joined by a blank line in one prompt.
func TestQueue_SeveralArriveInOrderAsOneRun(t *testing.T) {
	t.Parallel()
	for _, p := range _providers {
		t.Run(p, func(t *testing.T) {
			m, s := start(t, p)
			th := s.newThread(t, s.newBot(t, "Ada", p))
			ev := s.events(t, th)
			first := s.send(t, th, _slowPrompt)

			a, b := queue(t, s, th, _askPrompt), queue(t, s, th, _thenText)
			if got := queuedTexts(t, s, th.ID); len(got) != 2 || got[0] != _askPrompt || got[1] != _thenText {
				t.Fatalf("thread queued %q, want [%q %q] oldest first", got, _askPrompt, _thenText)
			}

			eventually(t, 30*time.Second, "both queued messages delivered", func() bool {
				return len(queueOf(t, s, th.ID).Queued) == 0 && queueOf(t, s, th.ID).RunID != first.ID
			})
			want := _askPrompt + "\n\n" + _thenText
			got := userPrompts(t, m, p, _askPrompt, _thenText)
			if len(got) != 1 || !strings.Contains(got[0], want) {
				t.Errorf("provider prompts holding the queued messages %q, want exactly one holding %q", got, want)
			}
			run := queueOf(t, s, th.ID).RunID
			s.finished(t, ev, run)
			var ids, runs []string
			for _, um := range userMessages(t, s, th.ID) {
				if um.Text == _askPrompt || um.Text == _thenText {
					ids, runs = append(ids, um.ID), append(runs, um.RunID)
				}
			}
			if len(ids) != 2 || ids[0] != a.ID || ids[1] != b.ID || runs[0] != run || runs[1] != run {
				t.Errorf("delivered messages ids %q runs %q, want ids [%s %s], both in run %s", ids, runs, a.ID, b.ID, run)
			}
		})
	}
}

// TestQueue_EditAndRemove: a queued message can be edited or removed until it is delivered; a
// plain Stop leaves the rest queued; sending it later makes edit and remove answer 409.
func TestQueue_EditAndRemove(t *testing.T) {
	t.Parallel()
	for _, p := range _providers {
		t.Run(p, func(t *testing.T) {
			m, s := start(t, p)
			th := s.newThread(t, s.newBot(t, "Ada", p))
			ev := s.events(t, th)
			busy := s.send(t, th, _hangPrompt)
			until(t, ev, busy.ID, "tool_call")

			a, b := queue(t, s, th, _askPrompt), queue(t, s, th, _thenText)
			const edited = _askPrompt + " Be brief."
			var got queuedMessage
			s.do(t, "PATCH", "/api/threads/"+th.ID+"/messages/"+a.ID, map[string]string{"text": edited}, &got, http.StatusOK)
			if got.ID != a.ID || got.Text != edited {
				t.Errorf("PATCH reply %+v, want id %q and text %q", got, a.ID, edited)
			}
			s.do(t, "PATCH", "/api/threads/"+th.ID+"/messages/"+a.ID, map[string]string{"text": ""}, nil, http.StatusBadRequest)
			s.do(t, "PATCH", "/api/threads/"+th.ID+"/messages/nope", map[string]string{"text": "x"}, nil, http.StatusNotFound)
			s.do(t, "DELETE", "/api/threads/"+th.ID+"/messages/nope", nil, nil, http.StatusNotFound)
			s.do(t, "DELETE", "/api/threads/"+th.ID+"/messages/"+b.ID, nil, nil, http.StatusNoContent)
			if got := queuedTexts(t, s, th.ID); len(got) != 1 || got[0] != edited {
				t.Fatalf("thread queued %q after an edit and a remove, want [%q]", got, edited)
			}

			s.do(t, "POST", "/api/runs/"+busy.ID+"/stop", nil, nil, http.StatusNoContent)
			s.finished(t, ev, busy.ID)
			time.Sleep(4 * _pace)
			if got := queuedTexts(t, s, th.ID); len(got) != 1 || got[0] != edited {
				t.Errorf("thread queued %q after the person's Stop, want [%q] still queued", got, edited)
			}
			if got := userPrompts(t, m, p, edited, _thenText); len(got) != 0 {
				t.Errorf("the provider got %q after a Stop, want neither the edited nor the removed message", got)
			}

			var sent struct {
				Run runInfo `json:"run"`
			}
			s.do(t, "POST", "/api/threads/"+th.ID+"/messages/"+a.ID+"/send", nil, &sent, http.StatusAccepted)
			if sent.Run.ID == "" || sent.Run.ID == busy.ID {
				t.Fatalf("send reply run %+v, want a new run", sent.Run)
			}
			s.finished(t, ev, sent.Run.ID)
			if got := userPrompts(t, m, p, edited, _thenText); len(got) != 1 || !strings.Contains(got[0], edited) || strings.Contains(got[0], _thenText) {
				t.Errorf("provider prompts %q, want one with the edited text and not the removed one", got)
			}
			s.do(t, "PATCH", "/api/threads/"+th.ID+"/messages/"+a.ID, map[string]string{"text": "late"}, nil, http.StatusConflict)
			s.do(t, "DELETE", "/api/threads/"+th.ID+"/messages/"+a.ID, nil, nil, http.StatusConflict)
			s.do(t, "POST", "/api/threads/"+th.ID+"/messages/"+a.ID+"/send", nil, nil, http.StatusConflict)
		})
	}
}

// TestQueue_StopAndSend: sending a queued message with a run going stops that run and starts the
// run with the queued message at once.
func TestQueue_StopAndSend(t *testing.T) {
	t.Parallel()
	for _, p := range _providers {
		t.Run(p, func(t *testing.T) {
			m, s := start(t, p)
			th := s.newThread(t, s.newBot(t, "Ada", p))
			ev := s.events(t, th)
			busy := s.send(t, th, _hangPrompt)
			until(t, ev, busy.ID, "tool_call")
			q := queue(t, s, th, _keepPrompt)

			var sent struct {
				Run runInfo `json:"run"`
			}
			s.do(t, "POST", "/api/threads/"+th.ID+"/messages/"+q.ID+"/send", nil, &sent, http.StatusAccepted)
			if sent.Run.ID == "" || sent.Run.ID == busy.ID {
				t.Fatalf("send reply run %+v, want a new run", sent.Run)
			}
			if got := s.run(t, busy.ID).Status; got != "stopped" {
				t.Errorf("the run that was going: status %q, want stopped", got)
			}
			if r := s.finished(t, ev, sent.Run.ID); r.Status != "done" {
				t.Errorf("run %s for the queued message: status %q, error %q; want done", r.ID, r.Status, r.Error)
			}
			if got := queuedTexts(t, s, th.ID); len(got) != 0 {
				t.Errorf("thread queued %q, want none", got)
			}
			if got := userPrompts(t, m, p, _keepPrompt); len(got) != 1 {
				t.Errorf("the provider got %q, want one prompt with the queued message", got)
			}
		})
	}
}

// TestQueue_BossContinueToBusyHelper: a boss's continue to a helper whose run is going is accepted
// and answers Queued, not Sent; the message waits in the helper thread's queue, which the person
// cannot edit, and runs when the helper's run ends.
func TestQueue_BossContinueToBusyHelper(t *testing.T) {
	t.Parallel()
	for _, p := range _providers {
		t.Run(p, func(t *testing.T) {
			m, s := start(t, p)
			poteto, tester := s.newBot(t, "Poteto", p), s.newBot(t, "Tester", p)
			setDelegates(t, s, poteto, tester)
			boss := s.newThread(t, poteto)
			ev := s.events(t, boss)

			helper := delegated(t, useAtoll(t, s, boss, ev, delegateCall("Tester", _reportPrompt, ""))[0])
			going := asking(t, s, helper)
			answer := "Yes, update it. " + _reportPrompt
			lines := useAtoll(t, s, boss, ev, continueCall(helper, answer))
			if len(lines) != 1 || !strings.HasPrefix(lines[0], "atoll/continue: Queued") {
				t.Fatalf("continue while the helper's run goes: reply %q, want a line starting %q", lines, "atoll/continue: Queued")
			}
			if got := queuedTexts(t, s, helper); len(got) != 1 || !strings.Contains(got[0], answer) {
				t.Fatalf("helper thread queued %q, want the boss's message %q", got, answer)
			}
			id := queueOf(t, s, helper).Queued[0].ID
			s.do(t, "PATCH", "/api/threads/"+helper+"/messages/"+id, map[string]string{"text": "x"}, nil, http.StatusConflict)
			s.do(t, "DELETE", "/api/threads/"+helper+"/messages/"+id, nil, nil, http.StatusConflict)
			if sessionWith(t, m, p, answer) != nil {
				t.Errorf("the continue reached the provider while the helper's run %s goes", going.RunID)
			}

			allow(t, s, helper)
			eventually(t, 30*time.Second, "a helper run after "+going.RunID+" for the queued continue", func() bool {
				return getThread(t, s, helper).RunID != going.RunID
			})
			if got := queuedTexts(t, s, helper); len(got) != 0 {
				t.Errorf("helper thread queued %q after delivery, want none", got)
			}
			allow(t, s, helper)
			if sessionWith(t, m, p, answer) == nil {
				t.Errorf("no %s session got the queued continue %q after the helper's run ended", p, answer)
			}
			settled(t, s, helper)
		})
	}
}
