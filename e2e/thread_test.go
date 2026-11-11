package e2e

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"
)

// Prompts of the scenarios in testdata/*/run-*.jsonl.
const (
	_keepPrompt  = "Remember this fact: the codeword is kestrel. Reply with just: ok"
	_askPrompt   = "Which codeword did I give you? Reply with just the word."
	_toolsPrompt = "How many lines does notes.txt have? List the folder first, then read the file."
	_writePrompt = "Use the Write tool to create hello.txt containing the word hi."
	_hangPrompt  = "Wait for the build to finish."
	_claudeRunID = "e5249980-178d-4e8c-8829-3c64eb27639f"
	_pace        = 300 * time.Millisecond // fake.Pace: how long each fake tool call takes
)

var _providers = []string{"claude", "codex"}

// start serves atoll on a machine with provider installed and signed in.
func start(t *testing.T, provider string) (*machine, *server) {
	t.Helper()
	m := newMachine(t, provider)
	m.signIn(t, provider)
	return m, m.serve(t)
}

func TestThread_ResumesAndReports(t *testing.T) {
	t.Parallel()
	for _, p := range _providers {
		t.Run(p, func(t *testing.T) {
			m, s := start(t, p)
			th := s.newThread(t, s.newBot(t, "Ada", p))
			ev := s.events(t, th)

			first := s.finished(t, ev, s.send(t, th, _keepPrompt).ID)
			second := s.finished(t, ev, s.send(t, th, _askPrompt).ID)

			subject := map[string]string{"claude": "session", "codex": "thread"}[p]
			for _, c := range []struct {
				got   runInfo
				reply string
				done  string
			}{
				{first, "ok", `Stored codeword "kestrel" for this ` + subject + "; no files changed."},
				{second, "kestrel", "Answered the codeword from this " + subject + ": kestrel."},
			} {
				if c.got.Status != "done" || c.got.Reply != c.reply {
					t.Errorf("run %s: status %q, reply %q; want done, %q (error %q)", c.got.ID, c.got.Status, c.got.Reply, c.reply, c.got.Error)
				}
				want := report{Done: c.done, Tried: "none", Blocked: "none", Next: "none"}
				if c.got.Report == nil || *c.got.Report != want {
					t.Errorf("run %s: report %+v, want the finish_run call %+v", c.got.ID, c.got.Report, want)
				}
			}

			var msgs []message
			s.do(t, "GET", "/api/threads/"+th.ID+"/messages", nil, &msgs, http.StatusOK)
			var got []string
			for _, msg := range msgs {
				got = append(got, msg.Role+": "+msg.Text)
			}
			want := []string{"user: " + _keepPrompt, "bot: ok", "user: " + _askPrompt, "bot: kestrel"}
			if !slices.Equal(got, want) {
				t.Errorf("thread messages %q, want %q", got, want)
			}

			calls := m.sessions(t, p)
			if len(calls) != 2 {
				t.Fatalf("%s ran %d sessions, want 2", p, len(calls))
			}
			switch p {
			case "claude":
				if slices.Contains(calls[0].argv, "--resume") {
					t.Errorf("first run passed --resume: %q", calls[0].argv)
				}
				if got := flagValue(calls[1].argv, "--resume"); got != _claudeRunID {
					t.Errorf("second run passed --resume %q, want %q", got, _claudeRunID)
				}
				for _, c := range calls {
					m.assertClaudeIsolated(t, c)
				}
			case "codex":
				if r := request(calls[1], "thread/resume"); r == nil || r.ThreadID != _codexThreadID {
					t.Errorf("second run sent thread/resume %+v, want threadId %q", r, _codexThreadID)
				}
				m.assertCodexIsolated(t, calls[0], "thread/start")
				m.assertCodexIsolated(t, calls[1], "thread/resume")
			}
		})
	}
}

func TestApproval(t *testing.T) {
	t.Parallel()
	for _, p := range _providers {
		for _, approve := range []bool{true, false} {
			name := map[bool]string{true: "approve", false: "reject"}[approve]
			t.Run(p+"/"+name, func(t *testing.T) {
				_, s := start(t, p)
				th := s.newThread(t, s.newBot(t, "Ada", p))
				ev := s.events(t, th)
				r := s.send(t, th, _writePrompt)

				es := until(t, ev, r.ID, "approval")
				ask := es[len(es)-1].Data
				id, _ := ask["id"].(string)
				input, _ := json.Marshal(ask["input"])
				if id == "" || !strings.Contains(string(input), "hello.txt") {
					t.Fatalf("approval event %v: want an id and the input of the step (it writes hello.txt)", ask)
				}

				// Paused: nothing happens until the user answers.
				if got := s.run(t, r.ID).Status; got != "waiting" {
					t.Errorf("run status %q while the approval is open, want waiting", got)
				}
				quiet(t, ev, r.ID, 4*_pace)
				if got := s.run(t, r.ID).Status; got != "waiting" {
					t.Errorf("run status %q after waiting on the approval, want waiting", got)
				}

				s.do(t, "POST", "/api/approvals/"+id, map[string]bool{"approve": approve}, nil, http.StatusNoContent)
				got := s.finished(t, ev, r.ID)

				if got.Status != "done" || got.Report == nil {
					t.Fatalf("run after %s: status %q, report %+v, want done with a report (error %q)", name, got.Status, got.Report, got.Error)
				}
				switch {
				case approve && !strings.HasPrefix(got.Reply, "Created"):
					t.Errorf("reply after approve %q, want the step done: \"Created …hello.txt…\"", got.Reply)
				case !approve && !strings.Contains(got.Reply, "didn't create hello.txt"):
					t.Errorf("reply after reject %q, want the refusal reported: \"…didn't create hello.txt…\"", got.Reply)
				}
			})
		}
	}
}

func TestToolCalls_Stream(t *testing.T) {
	t.Parallel()
	for _, p := range _providers {
		t.Run(p, func(t *testing.T) {
			_, s := start(t, p)
			th := s.newThread(t, s.newBot(t, "Ada", p))
			ev := s.events(t, th)
			r := s.send(t, th, _toolsPrompt)

			es := until(t, ev, r.ID, "tool_call")
			if got := s.run(t, r.ID).Status; got != "running" {
				t.Errorf("run status %q when its first tool call arrived, want running", got)
			}
			es = append(es, until(t, ev, r.ID, "run_finished")...)

			var calls []event
			var names []string
			for _, e := range es {
				if e.Type != "tool_call" {
					continue
				}
				calls = append(calls, e)
				name, _ := e.Data["name"].(string)
				id, _ := e.Data["id"].(string)
				if name == "" || id == "" {
					t.Errorf("tool_call event %v: want an id and a name", e.Data)
				}
				names = append(names, name)
			}
			if len(calls) < 2 {
				t.Fatalf("got %d tool_call events %q, want at least 2 (list the folder, read the file)", len(calls), names)
			}
			end := es[len(es)-1]
			if lead := end.At.Sub(calls[0].At); lead < 2*_pace {
				t.Errorf("first tool call arrived %v before the run finished, want at least %v: events must stream, not arrive at the end", lead, 2*_pace)
			}
			if p == "claude" && (!slices.Contains(names, "Bash") || !slices.Contains(names, "Read")) {
				t.Errorf("tool calls %q, want Bash and Read among them", names)
			}
			if got := s.run(t, r.ID); got.Status != "done" || !strings.HasPrefix(got.Reply, "notes.txt has 3 lines") {
				t.Errorf("run: status %q, reply %q; want done, \"notes.txt has 3 lines…\"", got.Status, got.Reply)
			}
		})
	}
}

func TestRestart_FailsRunningRun(t *testing.T) {
	t.Parallel()
	for _, p := range _providers {
		t.Run(p, func(t *testing.T) {
			m, s := start(t, p)
			th := s.newThread(t, s.newBot(t, "Ada", p))
			ev := s.events(t, th)
			r := s.send(t, th, _hangPrompt)
			until(t, ev, r.ID, "tool_call")
			if got := s.run(t, r.ID).Status; got != "running" {
				t.Fatalf("run status %q mid-run, want running", got)
			}

			s.kill()
			s = m.serve(t)

			got := s.run(t, r.ID)
			if got.Status != "failed" || strings.TrimSpace(got.Error) == "" {
				t.Errorf("run after a restart: status %q, error %q; want failed with a reason", got.Status, got.Error)
			}
			var msgs []message
			s.do(t, "GET", "/api/threads/"+th.ID+"/messages", nil, &msgs, http.StatusOK)
			if len(msgs) == 0 || msgs[0].Text != _hangPrompt {
				t.Errorf("thread messages after a restart %+v, want the user message kept", msgs)
			}
		})
	}
}

// quiet fails if run gets an event in d.
func quiet(t *testing.T, ch <-chan event, runID string, d time.Duration) {
	t.Helper()
	timeout := time.After(d)
	for {
		select {
		case e, ok := <-ch:
			if ok && e.Data["run_id"] == runID {
				t.Errorf("run %s sent %s %v while waiting for approval", runID, e.Type, e.Data)
			}
			if !ok {
				return
			}
		case <-timeout:
			return
		}
	}
}
