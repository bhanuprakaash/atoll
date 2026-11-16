package e2e

import (
	"encoding/json"
	"net/http"
	"path/filepath"
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
	_heldPrompt  = _toolsPrompt + " Hold after the first step until I say go."
	_writePrompt = "Use the Write tool to create hello.txt containing the word hi."
	_hangPrompt  = "Wait for the build to finish."
	_claudeRunID = "e5249980-178d-4e8c-8829-3c64eb27639f"
	_pace        = 300 * time.Millisecond // fake.Pace: how long each fake tool call takes
)

var _providers = []string{"claude", "codex"}

var _oneProvider = []string{"claude"}

// start serves atoll on a machine with provider installed and signed in.
func start(t *testing.T, provider string) (*machine, *server) {
	t.Helper()
	m := newMachine(t, provider)
	m.signIn(t, provider)
	return m, m.serve(t)
}

func TestThread_Resumes(t *testing.T) {
	t.Parallel()
	for _, p := range _providers {
		t.Run(p, func(t *testing.T) {
			m, s := start(t, p)
			th := s.newThread(t, s.newBot(t, "Ada", p))
			ev := s.events(t, th)

			first := s.finished(t, ev, s.send(t, th, _keepPrompt).ID)
			second := s.finished(t, ev, s.send(t, th, _askPrompt).ID)

			for _, c := range []struct {
				got   runInfo
				reply string
			}{
				{first, "ok"},
				{second, "kestrel"},
			} {
				if c.got.Status != "done" || c.got.Reply != c.reply {
					t.Errorf("run %s: status %q, reply %q; want done, %q (error %q)", c.got.ID, c.got.Status, c.got.Reply, c.reply, c.got.Error)
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

				if got.Status != "done" {
					t.Fatalf("run after %s: status %q, want done (error %q)", name, got.Status, got.Error)
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
			m, s := start(t, p)
			th := s.newThread(t, s.newBot(t, "Ada", p))
			ev := s.events(t, th)
			r := s.send(t, th, _heldPrompt)

			// The fake holds the run after its first tool call until the test releases it, so the
			// run cannot finish while the first tool_call is checked, however slow the machine is.
			es := until(t, ev, r.ID, "tool_call")
			var held string
			for end := time.Now().Add(15 * time.Second); held == "" && time.Now().Before(end); time.Sleep(20 * time.Millisecond) {
				paths, _ := filepath.Glob(filepath.Join(m.bin, p+".ready.*"))
				if len(paths) > 0 {
					held = paths[0]
				}
			}
			if held == "" {
				t.Fatal("the fake provider never reached its hold after the first tool call")
			}
			if got := s.run(t, r.ID); got.Status != "running" || got.Reply != "" {
				t.Errorf("run while the provider is held after its first tool call: status %q, reply %q; want running, no reply: the tool call must reach the client before the run ends", got.Status, got.Reply)
			}
			writeFile(t, held+".release", "")
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
			if p == "claude" && (!slices.Contains(names, "Bash") || !slices.Contains(names, "Read")) {
				t.Errorf("tool calls %q, want Bash and Read among them", names)
			}
			if got := s.run(t, r.ID); got.Status != "done" || !strings.HasPrefix(got.Reply, "notes.txt has 3 lines") {
				t.Errorf("run: status %q, reply %q; want done, \"notes.txt has 3 lines…\"", got.Status, got.Reply)
			}
		})
	}
}

// _chance is the window an absence check waits after the event that gave the thing its chance,
// where no log line, counter or process exit says the server decided: four fake tool calls, well
// past the 200 ms wake window.
const _chance = 4 * _pace

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
