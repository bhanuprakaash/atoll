package e2e

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"atoll/e2e/fakes/fake"
)

// Prompts of the idle-limit recordings in testdata/claude. idle-hang and idle-busy play on the
// boss's session (mcp-use), so a thread that called start_work can resume into them.
const (
	// idle-hang.jsonl: one Bash call, then no event until killed.
	_idleHangPrompt = "Wait for the deploy to finish."
	// idle-busy.jsonl: a reply streamed in 8 parts, 600 ms apart.
	_idleBusyPrompt = "Report each check as it passes, then stop."
	_idleBusyReply  = "Checked 1. Checked 2. Checked 3. Checked 4. Checked 5. Checked 6. Checked 7. Checked 8."
	// idle-approve.jsonl: 1.5 s silent after its Write call, then the approval; once allowed, 1.2 s
	// silent, then the reply.
	_idleApprovePrompt = "Use the Write tool to create slow.txt containing the word hi."
	_idleApproveReply  = "Created `hello.txt` containing `hi` at `/work/hello.txt`."
	// idle-chatter.jsonl: one Bash call, then 12 text parts 500 ms apart, then no event until killed.
	_idleChatterPrompt = "Run the slow checks and narrate each one."
	// The stuck wake's third line: the first stuck in a row, and the second.
	_stuckContinue = "Continue it to pick up where it stopped."
	_stuckTwice    = "Stuck twice in a row. Start a new helper from branch "
)

// startIdle serves atoll with ATOLL_IDLE_LIMIT set to limit.
func startIdle(t *testing.T, p, limit string) (*machine, *server) {
	t.Helper()
	m := newMachine(t, p)
	m.env = append(m.env, "ATOLL_IDLE_LIMIT="+limit)
	m.signIn(t, p)
	return m, m.serve(t)
}

// wantStuck checks that a run is stored as stopped by the idle limit: failed, error_kind stuck.
func wantStuck(t *testing.T, s *server, runID string) {
	t.Helper()
	if r := s.limitRun(t, runID); r.Status != "failed" || r.ErrorKind != "stuck" {
		t.Errorf("run %s silent past the idle limit: status %q, error_kind %q, error %q; want failed with error_kind stuck",
			runID, r.Status, r.ErrorKind, r.Error)
	}
}

// startCall is the boss's start_work on fix/seeded-bug.
func startCall() fake.MCPCall {
	return fake.MCPCall{Server: "atoll", Tool: "start_work", Args: json.RawMessage(`{"branch":"fix/seeded-bug"}`)}
}

// stuckWakeLines splits a stuck wake as stored, without `Reef: `, into its lines.
func stuckWakeLines(text string) []string {
	return strings.Split(strings.TrimPrefix(text, "Reef: "), "\n")
}

// TestIdleLimit_StopsSilentRun: a run in a person's thread with no stream event for
// ATOLL_IDLE_LIMIT is stopped as a Stop stops it (its provider process exits) and stored failed
// with error_kind stuck; not before the limit.
func TestIdleLimit_StopsSilentRun(t *testing.T) {
	t.Parallel()
	for _, p := range _providers {
		t.Run(p, func(t *testing.T) {
			t.Parallel()
			m, s := startIdle(t, p, "2s")
			th := s.newThread(t, s.newBot(t, "Ada", p))
			ev := s.events(t, th)
			r := s.send(t, th, _hangPrompt)
			es := until(t, ev, r.ID, "tool_call")
			lastEvent := es[len(es)-1].At
			sessions := m.sessions(t, p)
			pid := sessions[len(sessions)-1].pid

			es = until(t, ev, r.ID, "run_finished")
			if waited := es[len(es)-1].At.Sub(lastEvent); waited < 1500*time.Millisecond || waited > 15*time.Second {
				t.Errorf("run ended %s after its last event, want about the 2 s limit", waited)
			}
			if st := es[len(es)-1].Data["status"]; st != "failed" {
				t.Errorf("run_finished status %v at the idle limit, want failed", st)
			}
			wantStuck(t, s, r.ID)
			eventually(t, 5*time.Second, "the stuck run's provider process exits", func() bool { return gone(pid) })
		})
	}
}

// TestIdleLimit_KeepsWorkInPersonThread: a stuck run in a person's thread leaves the worktree's
// uncommitted changes, tracked and untracked, as a Stop does.
func TestIdleLimit_KeepsWorkInPersonThread(t *testing.T) {
	t.Parallel()
	f := newRepoFixtureEnv(t, "claude", "", "ATOLL_IDLE_LIMIT=3s")
	s := f.s
	th := f.thread(t, s.newBot(t, "Ada", "claude"))
	ev := s.events(t, th)
	w := bossWorktree(t, s, th, ev)

	r := s.send(t, th, _idleHangPrompt)
	until(t, ev, r.ID, "tool_call")
	writeFile(t, filepath.Join(w.Path, "answer.txt"), "42 unfinished\n")
	writeFile(t, filepath.Join(w.Path, "draft.txt"), "keep this untracked work\n")

	s.finished(t, ev, r.ID)
	wantStuck(t, s, r.ID)
	for name, want := range map[string]string{"answer.txt": "42 unfinished\n", "draft.txt": "keep this untracked work\n"} {
		if got, err := os.ReadFile(filepath.Join(w.Path, name)); err != nil || string(got) != want {
			t.Errorf("worktree %s after the idle limit: %q (error %v), want %q", name, got, err, want)
		}
	}
	if status := gitIn(t, w.Path, "status", "--porcelain"); !strings.Contains(status, "answer.txt") || !strings.Contains(status, "draft.txt") {
		t.Errorf("git status of the worktree after the idle limit:\n%s\nwant answer.txt modified and draft.txt untracked", status)
	}
}

// TestIdleLimit_ApprovalPausesClock: a run waiting on an approval for longer than the limit is not
// stopped, and the clock starts from zero when the approval closes: the run, silent 1.5 s before
// the approval and 1.2 s after it, ends done under a 2 s limit.
func TestIdleLimit_ApprovalPausesClock(t *testing.T) {
	t.Parallel()
	_, s := startIdle(t, "claude", "2s")
	th := s.newThread(t, s.newBot(t, "Ada", "claude"))
	ev := s.events(t, th)
	r := s.send(t, th, _idleApprovePrompt)

	es := until(t, ev, r.ID, "approval")
	id, _ := es[len(es)-1].Data["id"].(string)
	quiet(t, ev, r.ID, 5*time.Second)
	if got := s.run(t, r.ID).Status; got != "waiting" {
		t.Fatalf("run status %q after 5 s on an open approval under a 2 s limit, want waiting", got)
	}

	s.do(t, "POST", "/api/approvals/"+id, map[string]bool{"approve": true}, nil, http.StatusNoContent)
	done := s.finished(t, ev, r.ID)
	if done.Status != "done" || done.Reply != _idleApproveReply {
		t.Errorf("run after the approval closed: status %q, reply %q, error %q; want done, %q", done.Status, done.Reply, done.Error, _idleApproveReply)
	}
}

// TestIdleLimit_SteadyEventsNotStopped: a run that keeps emitting events, each well inside the
// limit, is not stopped though it runs longer than the limit in total.
func TestIdleLimit_SteadyEventsNotStopped(t *testing.T) {
	t.Parallel()
	_, s := startIdle(t, "claude", "2s")
	th := s.newThread(t, s.newBot(t, "Ada", "claude"))
	ev := s.events(t, th)
	began := time.Now()
	r := s.finished(t, ev, s.send(t, th, _idleBusyPrompt).ID)
	if took := time.Since(began); took < 4*time.Second {
		t.Fatalf("the busy run took %s, want over 4 s (twice the limit) for this test to mean anything", took)
	}
	if r.Status != "done" || r.Reply != _idleBusyReply {
		t.Errorf("busy run: status %q, reply %q, error %q; want done, %q", r.Status, r.Reply, r.Error, _idleBusyReply)
	}
}

// TestIdleLimit_StuckHelperWakesBoss: a helper silent past the limit is stuck: its run is stopped
// (the process exits, the worktree keeps its commits and uncommitted changes) and stored failed,
// stuck. The boss gets one wake, from the helper Bot: `Tester stopped: no progress for 5s.`, the
// branch, commits ahead and uncommitted changes with the thread id, `Continue it to pick up where
// it stopped.`, then the helpers line. The boss's run for it calls no tell_user, so the guard
// posts its final message. helpers() then shows the helper as stuck.
func TestIdleLimit_StuckHelperWakesBoss(t *testing.T) {
	t.Parallel()
	const p = "claude"
	f := newRepoFixtureEnv(t, p, "", "ATOLL_IDLE_LIMIT=5s")
	m, s := f.m, f.s
	poteto, tester := s.newBot(t, "Poteto", p), s.newBot(t, "Tester", p)
	setDelegates(t, s, poteto, tester)
	boss := f.thread(t, poteto)
	ev := s.events(t, boss)

	lines := useAtoll(t, s, boss, ev, startCall(), delegateCall("Tester", _hangPrompt, ""))
	h := handedOff(t, lines[1])
	if h.Branch == "" {
		t.Fatalf("delegate result %q has no branch", lines[1])
	}
	made := worktreeOf(t, f.repo.Path, h.Branch)
	if made == "" {
		t.Fatalf("no worktree on the helper's branch %s", h.Branch)
	}
	run := helperRunning(t, s, h.ThreadID)
	pid := helperSession(t, m, p).pid
	for _, name := range []string{"one.txt", "two.txt"} {
		writeFile(t, filepath.Join(made, name), name+"\n")
		gitIn(t, made, "add", name)
		gitIn(t, made, "commit", "-m", "wip: "+name)
	}
	writeFile(t, filepath.Join(made, "answer.txt"), "42 unfinished\n")
	writeFile(t, filepath.Join(made, "draft.txt"), "keep this untracked work\n")

	if r := settled(t, s, h.ThreadID); r.ID != run {
		t.Fatalf("helper thread's latest run %s, want %s", r.ID, run)
	}
	wantStuck(t, s, run)
	eventually(t, 5*time.Second, "the stuck helper's provider process exits", func() bool { return gone(pid) })
	if status := gitIn(t, made, "status", "--porcelain"); !strings.Contains(status, "answer.txt") || !strings.Contains(status, "draft.txt") {
		t.Errorf("git status of the helper worktree after the idle limit:\n%s\nwant answer.txt modified and draft.txt untracked", status)
	}

	msg, _, wake := woken(t, m, s, p, boss.ID, tester.ID, 1)
	want := []string{
		"Tester stopped: no progress for 5s.",
		"Branch " + h.Branch + ", 2 commits ahead, uncommitted changes. (thread " + h.ThreadID + ")",
		_stuckContinue,
		"",
		helpersLine(),
	}
	if got := stuckWakeLines(msg.Text); !slices.Equal(got, want) {
		t.Errorf("stuck wake from Tester:\n%q\nwant\n%q", got, want)
	}
	if wake.Status != "done" {
		t.Errorf("boss run for the stuck wake: status %q, error %q; want done", wake.Status, wake.Error)
	}
	if text := botText(t, s, boss.ID, wake.ID); text != _wakeReply {
		t.Errorf("Bot message of the background run for helper_stuck %q, want its final message %q posted (guard)", text, _wakeReply)
	}
	time.Sleep(_chance)
	if got := fromBot(t, s, boss.ID, tester.ID); len(got) != 1 {
		t.Errorf("boss thread has %d messages from Tester after one stuck run, want 1: %+v", len(got), got)
	}

	rows, _, _ := callHelpers(t, s, boss, ev)
	if len(rows) != 1 {
		t.Fatalf("helpers() rows %+v, want 1", rows)
	}
	wantRow(t, s, rows[0], "Tester", h.ThreadID, "stuck")
}

// TestIdleLimit_StuckHelperNoBranch: a stuck helper with no branch gets `No branch yet.` as the
// wake's second line. It is not held: the boss's next continue starts a run at once, and it ends
// done.
func TestIdleLimit_StuckHelperNoBranch(t *testing.T) {
	t.Parallel()
	const p = "claude"
	m, s := startIdle(t, p, "2s")
	poteto, tester := s.newBot(t, "Poteto", p), s.newBot(t, "Tester", p)
	setDelegates(t, s, poteto, tester)
	boss := s.newThread(t, poteto)
	ev := s.events(t, boss)

	helper := delegated(t, useAtoll(t, s, boss, ev, delegateCall("Tester", _hangPrompt, ""))[0])
	run := helperRunning(t, s, helper)
	settled(t, s, helper)
	wantStuck(t, s, run)

	msg, _, _ := woken(t, m, s, p, boss.ID, tester.ID, 1)
	want := []string{
		"Tester stopped: no progress for 2s.",
		"No branch yet. (thread " + helper + ")",
		_stuckContinue,
		"",
		helpersLine(),
	}
	if got := stuckWakeLines(msg.Text); !slices.Equal(got, want) {
		t.Errorf("stuck wake from Tester:\n%q\nwant\n%q", got, want)
	}

	got := useAtoll(t, s, boss, ev, continueCall(helper, _keepPrompt))
	if len(got) == 0 || got[0] != "atoll/continue: Sent." {
		t.Errorf("continue to a stuck helper: reply %q, want %q (sent at once, not %q)", got, "atoll/continue: Sent.", _queued)
	}
	eventually(t, 3*time.Second, "a new run of the stuck helper for the continue", func() bool {
		return getThread(t, s, helper).RunID != run
	})
	if r := settled(t, s, helper); r.Status != "done" || r.Reply != "ok" {
		t.Errorf("helper run for the continue: status %q, reply %q, error %q; want done, ok", r.Status, r.Reply, r.Error)
	}
}

// TestIdleLimit_StuckTwiceInARow: the same helper stuck a second time in a row gets `Stuck twice
// in a row. Start a new helper from branch {branch}.` as the wake's third line; a run that ends
// done in between resets the count.
func TestIdleLimit_StuckTwiceInARow(t *testing.T) {
	t.Parallel()
	const p = "claude"
	f := newRepoFixtureEnv(t, p, "", "ATOLL_IDLE_LIMIT=3s")
	m, s := f.m, f.s
	poteto, tester := s.newBot(t, "Poteto", p), s.newBot(t, "Tester", p)
	setDelegates(t, s, poteto, tester)
	boss := f.thread(t, poteto)
	ev := s.events(t, boss)

	h := handedOff(t, useAtoll(t, s, boss, ev, startCall(), delegateCall("Tester", _hangPrompt, ""))[1])
	if h.Branch == "" {
		t.Fatalf("delegate result has no branch: %+v", h)
	}
	first := helperRunning(t, s, h.ThreadID)
	// stuck waits for the helper's run to be stuck and for the boss's nth message from Tester, and
	// checks its first three lines.
	stuck := func(n int, run, third string) {
		t.Helper()
		if r := settled(t, s, h.ThreadID); r.ID != run {
			t.Fatalf("helper thread's latest run %s, want %s", r.ID, run)
		}
		wantStuck(t, s, run)
		msg, _, _ := woken(t, m, s, p, boss.ID, tester.ID, n)
		lines := stuckWakeLines(msg.Text)
		if len(lines) < 3 || lines[0] != "Tester stopped: no progress for 3s." ||
			!strings.HasPrefix(lines[1], "Branch "+h.Branch+",") || !strings.HasSuffix(lines[1], "(thread "+h.ThreadID+")") ||
			lines[2] != third {
			t.Errorf("stuck wake %d from Tester %q, want `Tester stopped: no progress for 3s.`, `Branch %s, … (thread %s)`, then %q",
				n, lines, h.Branch, h.ThreadID, third)
		}
	}
	again := func(text string) string {
		t.Helper()
		before := getThread(t, s, h.ThreadID).RunID
		useAtoll(t, s, boss, ev, continueCall(h.ThreadID, text))
		eventually(t, 3*time.Second, "a new helper run for the continue", func() bool {
			return getThread(t, s, h.ThreadID).RunID != before
		})
		return getThread(t, s, h.ThreadID).RunID
	}
	twice := _stuckTwice + h.Branch + "."

	stuck(1, first, _stuckContinue)
	stuck(2, again(_hangPrompt), twice)

	again(_keepPrompt)
	if r := settled(t, s, h.ThreadID); r.Status != "done" {
		t.Fatalf("helper run for the keep prompt: status %q, error %q; want done", r.Status, r.Error)
	}
	woken(t, m, s, p, boss.ID, tester.ID, 3)

	stuck(4, again(_hangPrompt), _stuckContinue)
}

// TestIdleLimit_HelpersIdleCountsAnyEvent: helpers() idle_s counts from the helper's last stream
// event of any kind, not its last tool call: a helper that made one tool call and has streamed
// text since shows idle_s under 1.5 s three seconds after that call.
func TestIdleLimit_HelpersIdleCountsAnyEvent(t *testing.T) {
	t.Parallel()
	const p = "claude"
	_, s := startIdle(t, p, "30s")
	poteto, tester := s.newBot(t, "Poteto", p), s.newBot(t, "Tester", p)
	setDelegates(t, s, poteto, tester)
	boss := s.newThread(t, poteto)
	ev := s.events(t, boss)

	helper := delegated(t, useAtoll(t, s, boss, ev, delegateCall("Tester", _idleChatterPrompt, ""))[0])
	helperRunning(t, s, helper)
	called := time.Now()
	time.Sleep(3 * time.Second)

	rows, _, _ := callHelpers(t, s, boss, ev)
	if len(rows) != 1 || rows[0].State != "running" || rows[0].IdleS == nil {
		t.Fatalf("helpers() rows %+v, want one running helper with idle_s", rows)
	}
	if idle := *rows[0].IdleS; idle < 0 || idle >= 1.5 {
		t.Errorf("idle_s %v %s after the helper's only tool call, while it streams text every 0.5 s; want under 1.5 (since its last event)",
			idle, time.Since(called).Round(100*time.Millisecond))
	}
}
