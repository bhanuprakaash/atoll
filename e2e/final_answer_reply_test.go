package e2e

import (
	"net/http"
	"slices"
	"strings"
	"testing"
)

// Issue #189: a run's reply is what the Bot says after its last tool call.
//
// testdata/*/final-answer.jsonl, prompt _finalPrompt: text, a command, text, finish_run, then the
// closing paragraph. testdata/*/no-tool-call.jsonl, prompt _noToolPrompt: two paragraphs, no tool.
const (
	_finalPrompt  = "Check the build, then say what you found."
	_finalOpening = "Status: reading the build output, then running the checks."
	_finalMiddle  = "The build is green; saving the report now."
	_finalClosing = "The build is green: all checks pass. Nothing else needs doing."
	_noToolPrompt = "Explain the build in two paragraphs."
	_noToolText   = "The build compiles the Go server and the web app.\n\nIt then runs the tests and prints the result."
)

// botText is the text of the bot message the run wrote.
func botText(t *testing.T, s *server, threadID, runID string) string {
	t.Helper()
	var msgs []message
	s.do(t, "GET", "/api/threads/"+threadID+"/messages", nil, &msgs, http.StatusOK)
	var got []string
	for _, m := range msgs {
		if m.Role == "bot" && m.RunID == runID {
			got = append(got, m.Text)
		}
	}
	if len(got) != 1 {
		t.Fatalf("thread %s has %d bot messages for run %s %q, want 1", threadID, len(got), runID, got)
	}
	return got[0]
}

func TestFinalAnswer_ReplyIsClosingParagraph(t *testing.T) {
	for _, p := range _providers {
		t.Run(p, func(t *testing.T) {
			_, s := start(t, p)
			th := s.newThread(t, s.newBot(t, "Ada", p))
			ev := s.events(t, th)

			r := s.finished(t, ev, s.send(t, th, _finalPrompt).ID)
			if r.Status != "done" {
				t.Fatalf("run: status %q, error %q; want done", r.Status, r.Error)
			}
			if r.Reply != _finalClosing {
				t.Errorf("run reply %q, want only the closing paragraph %q", r.Reply, _finalClosing)
			}
			if got := botText(t, s, th.ID, r.ID); got != _finalClosing {
				t.Errorf("bot message %q, want only the closing paragraph %q", got, _finalClosing)
			}
		})
	}
}

func TestFinalAnswer_EarlierTextStillStreams(t *testing.T) {
	for _, p := range _providers {
		t.Run(p, func(t *testing.T) {
			_, s := start(t, p)
			th := s.newThread(t, s.newBot(t, "Ada", p))
			ev := s.events(t, th)

			run := s.send(t, th, _finalPrompt)
			events := until(t, ev, run.ID, "run_finished")

			var streamed strings.Builder
			for _, e := range events {
				if e.Type == "text" {
					streamed.WriteString(e.Data["text"].(string))
				}
			}
			for _, want := range []string{_finalOpening, _finalMiddle, _finalClosing} {
				if !strings.Contains(streamed.String(), want) {
					t.Errorf("text events carry %q, want them to hold %q", streamed.String(), want)
				}
			}
			text := slices.IndexFunc(events, func(e event) bool {
				s, _ := e.Data["text"].(string)
				return e.Type == "text" && strings.Contains(s, _finalOpening)
			})
			tool := slices.IndexFunc(events, func(e event) bool { return e.Type == "tool_call" })
			if text < 0 || tool < 0 || text > tool {
				t.Errorf("opening text event at %d, first tool_call at %d in %s; want the text streamed before the tool call", text, tool, types(events))
			}
		})
	}
}

func TestFinalAnswer_HelperReportRelaysClosingParagraph(t *testing.T) {
	for _, p := range _providers {
		t.Run(p, func(t *testing.T) {
			m, s := start(t, p)
			poteto, tester := s.newBot(t, "Poteto", p), s.newBot(t, "Tester", p)
			setDelegates(t, s, poteto, tester)
			boss := s.newThread(t, poteto)
			ev := s.events(t, boss)

			helper := delegated(t, useAtoll(t, s, boss, ev, delegateCall("Tester", _finalPrompt, ""))[0])
			if h := settled(t, s, helper); h.Status != "done" {
				t.Fatalf("helper run: status %q, error %q; want done", h.Status, h.Error)
			} else if h.Reply != _finalClosing {
				t.Errorf("helper run reply %q, want only the closing paragraph %q", h.Reply, _finalClosing)
			}

			msg, _, _ := woken(t, m, s, p, boss.ID, tester.ID, 1)
			_, rest, _ := strings.Cut(msg.Text, "\n")
			body, _, _ := strings.Cut(rest, "\n\nDone: ")
			if body != _finalClosing {
				t.Errorf("message to the boss holds the reply %q, want only the closing paragraph %q; full text %q", body, _finalClosing, msg.Text)
			}
			for _, note := range []string{_finalOpening, _finalMiddle} {
				if strings.Contains(msg.Text, note) {
					t.Errorf("message to the boss %q holds the working note %q", msg.Text, note)
				}
			}
		})
	}
}

func TestFinalAnswer_NoToolCallKeepsWholeText(t *testing.T) {
	for _, p := range _providers {
		t.Run(p, func(t *testing.T) {
			_, s := start(t, p)
			th := s.newThread(t, s.newBot(t, "Ada", p))
			ev := s.events(t, th)

			r := s.finished(t, ev, s.send(t, th, _noToolPrompt).ID)
			if r.Status != "done" {
				t.Fatalf("run: status %q, error %q; want done", r.Status, r.Error)
			}
			if r.Reply != _noToolText {
				t.Errorf("run reply %q, want the whole text %q", r.Reply, _noToolText)
			}
			if got := botText(t, s, th.ID, r.ID); got != _noToolText {
				t.Errorf("bot message %q, want the whole text %q", got, _noToolText)
			}
		})
	}
}
