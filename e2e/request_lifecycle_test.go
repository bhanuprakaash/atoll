package e2e

import (
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"atoll/e2e/fakes/fake"
)

// Contract: request-lifecycle-contract.md.

// Prompts of testdata/claude/lifecycle-*.jsonl and testdata/codex/lifecycle-*.jsonl, and what the
// recordings answer. _lifeOther and _lifeOtherTool are in what the provider did on its own.
const (
	_lifeFirst          = "Lifecycle check, first round: say what you will do."
	_lifeSecond         = "Lifecycle check, second round: the background job is gone, answer anyway."
	_lifeResultFirst    = "Lifecycle check: the result comes before the terminal state."
	_lifeCompletedFirst = "Lifecycle check: the terminal state comes before the result."
	_lifeCancelled      = "Lifecycle check: the request is cancelled."
	_lifeNoResult       = "Lifecycle check: the request ends with no result."
	_lifeExit           = "Lifecycle check: the process exits mid-request."
	_lifeNoneFirst      = "Lifecycle check, no lifecycle events: first round."
	_lifeNoneSecond     = "Lifecycle check, no lifecycle events: second round."
	_lifeAnswerOnly     = "Lifecycle check: answer, and nothing follows it."
	_lifeCodexOther     = "Lifecycle check: another turn runs beside this one."
	_lifeCodexApproval  = "Lifecycle check: another turn asks for approval."

	_lifeAnswer      = "The background job was stopped; the answer is 42."
	_lifeOther       = "OTHER-ACTIVITY"
	_lifeOtherTool   = "bg-job.log"
	_lifeClaudeModel = "claude-sonnet-5-5"
)

func TestRequestLifecycle_NotificationBeforeRequestDoesNotEndRun(t *testing.T) {
	t.Parallel()
	m, s := start(t, "claude")
	th := s.newThread(t, s.newBot(t, "Ada", "claude"))
	ev := s.events(t, th)

	if first := s.finished(t, ev, s.send(t, th, _lifeFirst).ID); first.Status != "done" {
		t.Fatalf("first run: status %q, error %q; want done", first.Status, first.Error)
	}
	run := s.send(t, th, _lifeSecond)
	events := until(t, ev, run.ID, "run_finished")
	got := s.run(t, run.ID)
	if got.Status != "done" {
		t.Fatalf("second run: status %q, error %q; want done", got.Status, got.Error)
	}
	if got.Reply != _lifeAnswer {
		t.Errorf("second run reply %q, want the answer that follows the notification: %q", got.Reply, _lifeAnswer)
	}
	if text := botText(t, s, th.ID, run.ID); text != _lifeAnswer {
		t.Errorf("bot message %q, want %q", text, _lifeAnswer)
	}
	if text := streamed(events); !strings.Contains(text, _lifeAnswer) {
		t.Errorf("text events carry %q, want them to hold %q", text, _lifeAnswer)
	}
	assertUserUUIDs(t, m, 2)
}

func TestRequestLifecycle_UserLinesCarryUUID(t *testing.T) {
	t.Parallel()
	m, s := start(t, "claude")
	th := s.newThread(t, s.newBot(t, "Ada", "claude"))
	ev := s.events(t, th)

	if r := s.finished(t, ev, s.send(t, th, _lifeAnswerOnly).ID); r.Status != "done" {
		t.Fatalf("run: status %q, error %q; want done", r.Status, r.Error)
	}
	assertUserUUIDs(t, m, 1)
}

// assertUserUUIDs checks every user line the fake claude read in a session of the machine: at
// least n lines, each with its own uuid.
func assertUserUUIDs(t *testing.T, m *machine, n int) {
	t.Helper()
	var uuids []string
	for _, c := range m.sessions(t, "claude") {
		for _, l := range c.stdin {
			var user struct {
				Type string `json:"type"`
				UUID string `json:"uuid"`
			}
			if json.Unmarshal([]byte(l), &user) == nil && user.Type == "user" {
				uuids = append(uuids, user.UUID)
			}
		}
	}
	if len(uuids) < n {
		t.Fatalf("claude read %d user lines %q, want at least %d", len(uuids), uuids, n)
	}
	seen := map[string]bool{}
	for _, id := range uuids {
		if id == "" {
			t.Errorf("a user line Reef wrote has no uuid; uuids %q", uuids)
		}
		if seen[id] && id != "" {
			t.Errorf("uuid %q is on two user lines; uuids %q", id, uuids)
		}
		seen[id] = true
	}
}

func TestRequestLifecycle_RequestEndsOnBothSignals(t *testing.T) {
	t.Parallel()
	for _, c := range []struct{ name, prompt, reply string }{
		{"result then terminal state", _lifeResultFirst, "Result first."},
		{"terminal state then result", _lifeCompletedFirst, "Terminal state first."},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			m, s := start(t, "claude")
			th := s.newThread(t, s.newBot(t, "Ada", "claude"))
			ev := s.events(t, th)

			run := s.send(t, th, c.prompt)
			events := until(t, ev, run.ID, "run_finished")
			finished := events[len(events)-1]
			got := s.run(t, run.ID)
			if got.Status != "done" || got.Reply != c.reply {
				t.Errorf("run: status %q, reply %q, error %q; want done, %q", got.Status, got.Reply, got.Error, c.reply)
			}
			s.assertUsage(t, run.ID, wantUsage{_lifeClaudeModel, 1, 5, 30, 1000, 200, cost(0.10)})

			// The fake prints the second signal 700 ms after the first. A run that ended on the first
			// stops the process before it prints the second.
			trace := claudeTrace(t, m, m.sessions(t, "claude")[0].pid)
			var last time.Time
			printed := map[string]bool{}
			for _, e := range trace {
				if e.Kind == "emit" {
					printed[e.Detail] = true
					last = e.At
				}
			}
			if !printed["result"] || !printed["command_lifecycle:completed"] {
				t.Errorf("the fake printed only %v, want the result and the terminal state: the run ended on the first one\ntrace:\n%s",
					slices.Sorted(maps.Keys(printed)), showTrace(trace))
			} else if finished.At.Before(last) {
				t.Errorf("run_finished arrived %v before the fake printed its last line; want the run to wait for both signals\ntrace:\n%s",
					last.Sub(finished.At), showTrace(trace))
			}
		})
	}
}

func TestRequestLifecycle_FailedRequestKeepsItsReason(t *testing.T) {
	t.Parallel()
	for _, c := range []struct{ name, prompt, reason string }{
		{"cancelled with an error result", _lifeCancelled, "API Error: Overloaded. Please try again in a moment."},
		{"terminal state with no result, then exit", _lifeNoResult, "fake claude: the session ended without a result"},
		{"process exit", _lifeExit, "fake claude: the process was killed mid-request"},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			_, s := start(t, "claude")
			th := s.newThread(t, s.newBot(t, "Ada", "claude"))
			ev := s.events(t, th)

			got := s.finished(t, ev, s.send(t, th, c.prompt).ID)
			if got.Status != "failed" || !strings.Contains(got.Error, c.reason) {
				t.Errorf("run: status %q, error %q; want failed with the reason %q", got.Status, got.Error, c.reason)
			}
		})
	}
}

func TestRequestLifecycle_NoLifecycleEventsWarnOncePerProcess(t *testing.T) {
	t.Parallel()
	m, s := start(t, "claude")
	th := s.newThread(t, s.newBot(t, "Ada", "claude"))
	ev := s.events(t, th)

	// The first run's process prints an empty result with an origin, then the answer.
	first := s.finished(t, ev, s.send(t, th, _lifeNoneFirst).ID)
	const answer = "The answer without lifecycle events."
	if first.Status != "done" || first.Reply != answer {
		t.Fatalf("first run: status %q, reply %q, error %q; want done, %q", first.Status, first.Reply, first.Error, answer)
	}
	// The thread's title session is a process of its own and ends with a log line.
	for deadline := time.Now().Add(5 * time.Second); !strings.Contains(s.log.String(), "name thread") && time.Now().Before(deadline); {
		time.Sleep(100 * time.Millisecond)
	}
	before := lifecycleWarnings(s)
	if before == 0 {
		t.Errorf("no lifecycle warning after the first process; server log:\n%s", s.log)
	}

	second := s.finished(t, ev, s.send(t, th, _lifeNoneSecond).ID)
	if second.Status != "done" || second.Reply != "The second answer without lifecycle events." {
		t.Fatalf("second run: status %q, reply %q, error %q; want done, the answer", second.Status, second.Reply, second.Error)
	}
	if n := len(m.sessions(t, "claude")); n != 2 {
		t.Fatalf("claude ran %d sessions, want 2", n)
	}
	if got := lifecycleWarnings(s) - before; got != 1 {
		t.Errorf("second process logged %d lifecycle warnings, want 1 (once per process); server log:\n%s", got, s.log)
	}
}

// lifecycleWarnings counts the WARN lines of the server log whose message names the lifecycle.
func lifecycleWarnings(s *server) int {
	n := 0
	for l := range strings.Lines(s.log.String()) {
		var e struct {
			Level string `json:"level"`
			Msg   string `json:"msg"`
		}
		if json.Unmarshal([]byte(l), &e) == nil && e.Level == "WARN" && strings.Contains(strings.ToLower(e.Msg), "lifecycle") {
			n++
		}
	}
	return n
}

func TestRequestLifecycle_NothingIsSentAfterTheRequest(t *testing.T) {
	t.Parallel()
	m, s := start(t, "claude")
	th := s.newThread(t, s.newBot(t, "Ada", "claude"))
	ev := s.events(t, th)

	r := s.finished(t, ev, s.send(t, th, _lifeAnswerOnly).ID)
	if r.Status != "done" || r.Reply != "Here is the answer." {
		t.Fatalf("run: status %q, reply %q, error %q; want done, %q", r.Status, r.Reply, r.Error, "Here is the answer.")
	}
	// A nudge would arrive once the request has ended; give it the time the fake takes for a call.
	time.Sleep(4 * _pace)
	trace := claudeTrace(t, m, m.sessions(t, "claude")[0].pid)
	reads := 0
	for _, e := range trace {
		if e.Kind == "read" && e.Detail == "user" {
			reads++
		}
	}
	if reads != 1 {
		t.Errorf("the fake claude read %d user lines, want only Reef's request\ntrace:\n%s", reads, showTrace(trace))
	}
}

func TestRequestLifecycle_OtherActivityIsLoggedAndPaidFor(t *testing.T) {
	t.Parallel()
	m, s := start(t, "claude")
	th := s.newThread(t, s.newBot(t, "Ada", "claude"))
	ev := s.events(t, th)

	first := s.finished(t, ev, s.send(t, th, _lifeFirst).ID)
	run := s.send(t, th, _lifeSecond)
	events := until(t, ev, run.ID, "run_finished")

	// Tokens and calls are Reef's request's alone; the cost is the session's total (0.30) less the
	// first run's (0.10), so the other activity's spend (0.18 at its result) is in it.
	s.assertUsage(t, first.ID, wantUsage{_lifeClaudeModel, 1, 5, 30, 1000, 200, cost(0.10)})
	s.assertUsage(t, run.ID, wantUsage{_lifeClaudeModel, 1, 6, 45, 1200, 300, cost(0.20)})
	assertOtherActivityOnlyInLog(t, m, run.ID, events, _lifeOther, _lifeOtherTool)
}

// assertOtherActivityOnlyInLog checks that every marker is in the run's event log, in no event of
// the type text or tool_call (a reconnecting client would replay it as chat), and in no event the
// thread's stream sent while the run went.
func assertOtherActivityOnlyInLog(t *testing.T, m *machine, runID string, events []event, markers ...string) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(m.data, "runs", runID+".jsonl"))
	if err != nil {
		t.Fatalf("read the event log of run %s: %v", runID, err)
	}
	for _, marker := range markers {
		var in []string
		for l := range strings.Lines(string(b)) {
			var e struct {
				Type string `json:"type"`
			}
			if strings.Contains(l, marker) && json.Unmarshal([]byte(l), &e) == nil {
				in = append(in, e.Type)
			}
		}
		if len(in) == 0 {
			t.Errorf("event log of run %s has no line with %q, want the other activity there", runID, marker)
		}
		for _, typ := range in {
			if typ == "text" || typ == "tool_call" {
				t.Errorf("event log of run %s holds %q in a %s event, want other activity apart from chat", runID, marker, typ)
			}
		}
		for _, e := range events {
			if strings.Contains(fmt.Sprint(e.Data), marker) {
				t.Errorf("thread stream sent %q in a %s event of run %s: %v; want it in the event log only", marker, e.Type, runID, e.Data)
			}
		}
	}
}

func TestRequestLifecycle_CodexOtherTurnIsNotTheRun(t *testing.T) {
	t.Parallel()
	m, s := start(t, "codex")
	th := s.newThread(t, s.newBot(t, "Ada", "codex"))
	ev := s.events(t, th)

	// Another turn of the thread starts, runs a command, writes, spends 7000 input tokens and
	// completes while Reef's turn goes on. Reef's turn is 1000 input (400 cached) and 50 output.
	const reply = "The build is green; all 42 tests pass."
	run := s.send(t, th, _lifeCodexOther)
	events := until(t, ev, run.ID, "run_finished")
	got := s.run(t, run.ID)
	if got.Status != "done" || got.Reply != reply {
		t.Fatalf("run: status %q, reply %q, error %q; want done, %q", got.Status, got.Reply, got.Error, reply)
	}
	if text := botText(t, s, th.ID, run.ID); text != reply {
		t.Errorf("bot message %q, want %q", text, reply)
	}
	s.assertUsage(t, run.ID, wantUsage{"gpt-5.6-sol", 1, 600, 50, 400, 0, nil})
	assertOtherActivityOnlyInLog(t, m, run.ID, events, _lifeOther, _lifeOtherTool)
}

func TestRequestLifecycle_CodexApprovalOfOtherTurnReachesPerson(t *testing.T) {
	t.Parallel()
	_, s := start(t, "codex")
	th := s.newThread(t, s.newBot(t, "Ada", "codex"))
	ev := s.events(t, th)

	run := s.send(t, th, _lifeCodexApproval)
	id := nextOf(t, ev, run.ID, "approval")
	s.do(t, "POST", "/api/approvals/"+id, map[string]bool{"approve": true}, nil, http.StatusNoContent)
	got := s.finished(t, ev, run.ID)
	const reply = "The deploy check is done."
	if got.Status != "done" || got.Reply != reply {
		t.Errorf("run: status %q, reply %q, error %q; want done, %q", got.Status, got.Reply, got.Error, reply)
	}
}

// claudeTrace returns the lines the fake claude process pid read and printed, in order.
func claudeTrace(t *testing.T, m *machine, pid int) []fake.Trace {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(m.bin, "claude.log.trace"))
	if err != nil {
		t.Fatalf("the fake claude traced nothing: %v", err)
	}
	var trace []fake.Trace
	for l := range strings.Lines(string(b)) {
		var e fake.Trace
		if err := json.Unmarshal([]byte(l), &e); err != nil {
			t.Fatal(err)
		}
		if e.PID == pid {
			trace = append(trace, e)
		}
	}
	return trace
}

func showTrace(trace []fake.Trace) string {
	var b strings.Builder
	for i, e := range trace {
		fmt.Fprintf(&b, "%d %s %s %s uuid=%q %s\n", i, e.At.Format("15:04:05.000"), e.Kind, e.Detail, e.UUID, e.Text)
	}
	return b.String()
}
