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

func stopHelperCall(thread string) fake.MCPCall {
	b, _ := json.Marshal(map[string]string{"thread": thread})
	return fake.MCPCall{Server: "atoll", Tool: "stop_helper", Args: b}
}

// stopped waits, for 5 s from the call that stopped a helper, until the helper thread's latest run
// is run and stopped.
func stopped(t *testing.T, s *server, thread, run string) {
	t.Helper()
	eventually(t, 5*time.Second, "helper run "+run+" of thread "+thread+" stopped", func() bool {
		th := getThread(t, s, thread)
		return th.RunID == run && th.RunStatus == "stopped"
	})
}

// stays checks for _chance that the thread's latest run is still run, with status want.
func stays(t *testing.T, s *server, thread, run, want string) {
	t.Helper()
	for end := time.Now().Add(_chance); time.Now().Before(end); time.Sleep(_pace / 3) {
		if th := getThread(t, s, thread); th.RunID != run || th.RunStatus != want {
			t.Fatalf("helper thread %s: latest run %s is %q, want %s still %q", thread, th.RunID, th.RunStatus, run, want)
		}
	}
}

// TestStopHelper_ToolsOnlyForBosses: stop_helper is listed for a Bot with delegates in a thread of
// its own, and for no other: not for a Bot with none, not in a helper run even when the helper Bot
// has delegates.
func TestStopHelper_ToolsOnlyForBosses(t *testing.T) {
	t.Parallel()
	for _, p := range _oneProvider {
		t.Run(p, func(t *testing.T) {
			_, s := start(t, p)
			poteto, tester, reviewer, ada := s.newBot(t, "Poteto", p), s.newBot(t, "Tester", p), s.newBot(t, "Reviewer", p), s.newBot(t, "Ada", p)
			setDelegates(t, s, poteto, tester)
			setDelegates(t, s, tester, reviewer)
			absent := "atoll/stop_helper: unavailable"
			try := stopHelperCall("no-such-thread")

			plain := s.newThread(t, ada)
			if got := useAtoll(t, s, plain, s.events(t, plain), try); !slices.Equal(got, []string{absent}) {
				t.Errorf("Ada (no delegates): reply %q, want %q", got, absent)
			}

			own := s.newThread(t, tester)
			if got := useAtoll(t, s, own, s.events(t, own), try); len(got) != 1 || got[0] == absent {
				t.Errorf("Tester (delegates Reviewer) in its own thread: reply %q, want stop_helper listed (a tool error for an unknown thread)", got)
			}

			boss := s.newThread(t, poteto)
			ev := s.events(t, boss)
			if got := useAtoll(t, s, boss, ev, try); len(got) != 1 || got[0] == absent {
				t.Errorf("Poteto (delegates Tester): reply %q, want stop_helper listed", got)
			}
			brief, _ := json.Marshal([]fake.MCPCall{try})
			lines := useAtoll(t, s, boss, ev, delegateCall("Tester", _mcpPromptHead+string(brief), ""))
			helper := settled(t, s, delegated(t, lines[0]))
			if helper.Status != "done" || helper.Reply != absent {
				t.Errorf("Tester's helper run: status %q, reply %q, error %q; want done, %q", helper.Status, helper.Reply, helper.Error, absent)
			}
		})
	}
}

// TestStopHelper_StopsRunningHelper: on a helper of this boss thread whose run is going, the call
// returns "Stopped {Helper}." and the helpers line on a new line without the stopped helper, the
// helper's run is stopped within 5 s, and the other helper goes on. A second call for the same
// thread says the helper is not running.
func TestStopHelper_StopsRunningHelper(t *testing.T) {
	t.Parallel()
	for _, p := range _oneProvider {
		t.Run(p, func(t *testing.T) {
			_, s := start(t, p)
			poteto, tester, reviewer := s.newBot(t, "Poteto", p), s.newBot(t, "Tester", p), s.newBot(t, "Reviewer", p)
			setDelegates(t, s, poteto, tester, reviewer)
			boss := s.newThread(t, poteto)
			ev := s.events(t, boss)

			lines := useAtoll(t, s, boss, ev, delegateCall("Tester", _reviews[0], ""), delegateCall("Reviewer", _reviews[1], ""))
			if len(lines) != 2 {
				t.Fatalf("two delegates: reply %q, want two lines", lines)
			}
			target, other := delegated(t, lines[0]), delegated(t, lines[1])
			run := helperRunning(t, s, target)
			otherRun := helperRunning(t, s, other)
			keep := onThread(t, s, "Reviewer", other)

			got := useAtoll(t, s, boss, ev, stopHelperCall(target))
			want := []string{"atoll/stop_helper: Stopped Tester.", helpersLine(keep)}
			if !slices.Equal(got, want) {
				t.Errorf("stop_helper on a running helper: reply %q, want %q", got, want)
			}
			stopped(t, s, target, run)
			if th := getThread(t, s, other); th.RunID != otherRun || th.RunStatus != "running" {
				t.Errorf("other helper's latest run %s is %q, want %s still running", th.RunID, th.RunStatus, otherRun)
			}

			got = useAtoll(t, s, boss, ev, stopHelperCall(target))
			want = []string{"atoll/stop_helper: Tester is not running.", helpersLine(keep)}
			if !slices.Equal(got, want) {
				t.Errorf("stop_helper on the helper again: reply %q, want %q", got, want)
			}
		})
	}
}

// TestStopHelper_SendsNoWake: a helper run stopped by stop_helper posts nothing from the helper
// Bot into the boss thread and starts no boss run, where the person's Stop does wake the boss.
func TestStopHelper_SendsNoWake(t *testing.T) {
	t.Parallel()
	for _, p := range _oneProvider {
		t.Run(p, func(t *testing.T) {
			m, s := start(t, p)
			poteto, tester := s.newBot(t, "Poteto", p), s.newBot(t, "Tester", p)
			setDelegates(t, s, poteto, tester)
			boss := s.newThread(t, poteto)
			ev := s.events(t, boss)

			lines := useAtoll(t, s, boss, ev, delegateCall("Tester", _hangPrompt, ""), delegateCall("Tester", _hangPrompt, ""))
			mine, theirs := delegated(t, lines[0]), delegated(t, lines[1])
			run := helperRunning(t, s, mine)
			theirRun := helperRunning(t, s, theirs)

			useAtoll(t, s, boss, ev, stopHelperCall(mine))
			bossRun := getThread(t, s, boss.ID).RunID
			stopped(t, s, mine, run)
			time.Sleep(_chance)
			if got := fromBot(t, s, boss.ID, tester.ID); len(got) != 0 {
				t.Errorf("messages from Tester %+v after stop_helper, want none", got)
			}
			if th := getThread(t, s, boss.ID); th.RunID != bossRun || th.RunStatus == "running" || th.RunStatus == "waiting" {
				t.Errorf("boss thread's latest run %s is %q after stop_helper, want %s, ended and no new run", th.RunID, th.RunStatus, bossRun)
			}

			s.do(t, "POST", "/api/runs/"+theirRun+"/stop", nil, nil, http.StatusNoContent)
			msg, _, _ := woken(t, m, s, p, boss.ID, tester.ID, 1)
			holdsAll(t, "wake for the person's Stop", msg.Text, theirs, "The user stopped Tester")
			if strings.Contains(msg.Text, mine) {
				t.Errorf("the only wake %q names the helper %s that stop_helper stopped", msg.Text, mine)
			}
		})
	}
}

// TestStopHelper_KeepsUncommittedWork: the stopped helper's worktree keeps its uncommitted changes,
// tracked and untracked.
func TestStopHelper_KeepsUncommittedWork(t *testing.T) {
	t.Parallel()
	for _, p := range _oneProvider {
		t.Run(p, func(t *testing.T) {
			f := newRepoFixture(t, p)
			s := f.s
			poteto, tester := s.newBot(t, "Poteto", p), s.newBot(t, "Tester", p)
			setDelegates(t, s, poteto, tester)
			boss := f.thread(t, poteto)
			ev := s.events(t, boss)

			start := fake.MCPCall{Server: "atoll", Tool: "start_work", Args: json.RawMessage(`{"type":"fix","slug":"Seeded BUG"}`)}
			lines := useAtoll(t, s, boss, ev, start, delegateCall("Tester", _hangPrompt, ""))
			h := handedOff(t, lines[1])
			made := worktreeOf(t, f.repo.Path, startedWork(t, lines[0]).Branch+"--tester")
			if made == "" {
				t.Fatalf("no worktree for the helper %s", h.ThreadID)
			}
			run := helperRunning(t, s, h.ThreadID)
			writeFile(t, filepath.Join(made, "answer.txt"), "42 unfinished\n")
			writeFile(t, filepath.Join(made, "draft.txt"), "keep this untracked work\n")

			useAtoll(t, s, boss, ev, stopHelperCall(h.ThreadID))
			stopped(t, s, h.ThreadID, run)
			for name, want := range map[string]string{"answer.txt": "42 unfinished\n", "draft.txt": "keep this untracked work\n"} {
				if got, err := os.ReadFile(filepath.Join(made, name)); err != nil || string(got) != want {
					t.Errorf("helper worktree %s after stop_helper: %q (error %v), want %q", name, got, err, want)
				}
			}
			if status := gitIn(t, made, "status", "--porcelain"); !strings.Contains(status, "answer.txt") || !strings.Contains(status, "draft.txt") {
				t.Errorf("git status of the helper worktree after stop_helper:\n%s\nwant answer.txt modified and draft.txt untracked", status)
			}
		})
	}
}

// TestStopHelper_NotRunning: a helper of this boss thread whose run ended gets "{Helper} is not
// running." and the helpers line as a normal result, and nothing starts.
func TestStopHelper_NotRunning(t *testing.T) {
	t.Parallel()
	for _, p := range _oneProvider {
		t.Run(p, func(t *testing.T) {
			_, s := start(t, p)
			poteto, tester := s.newBot(t, "Poteto", p), s.newBot(t, "Tester", p)
			setDelegates(t, s, poteto, tester)
			boss := s.newThread(t, poteto)
			ev := s.events(t, boss)

			helper := delegated(t, useAtoll(t, s, boss, ev, delegateCall("Tester", _keepPrompt, ""))[0])
			done := settled(t, s, helper)
			if done.Status != "done" {
				t.Fatalf("helper run: status %q, error %q; want done", done.Status, done.Error)
			}
			quietBoss(t, s, boss.ID, tester.ID, 1)

			got := useAtoll(t, s, boss, ev, stopHelperCall(helper))
			want := []string{"atoll/stop_helper: Tester is not running.", helpersLine()}
			if !slices.Equal(got, want) {
				t.Errorf("stop_helper on a helper that is done: reply %q, want %q", got, want)
			}
			if th := getThread(t, s, helper); th.RunID != done.ID || th.RunStatus != "done" {
				t.Errorf("helper's latest run %s is %q, want %s still done", th.RunID, th.RunStatus, done.ID)
			}
		})
	}
}

// TestStopHelper_RefusesOtherThreads: an unknown id, another boss's helper and the boss's own
// thread each get "{thread} is not one of your helper threads." as a tool error, and nothing
// stops: the other boss's helper still runs, and the boss's own run ends done.
func TestStopHelper_RefusesOtherThreads(t *testing.T) {
	t.Parallel()
	for _, p := range _oneProvider {
		t.Run(p, func(t *testing.T) {
			_, s := start(t, p)
			poteto, mako, tester := s.newBot(t, "Poteto", p), s.newBot(t, "Mako", p), s.newBot(t, "Tester", p)
			setDelegates(t, s, poteto, tester)
			setDelegates(t, s, mako, tester)
			boss, theirs := s.newThread(t, poteto), s.newThread(t, mako)
			ev := s.events(t, boss)

			foreign := delegated(t, useAtoll(t, s, theirs, s.events(t, theirs), delegateCall("Tester", _hangPrompt, ""))[0])
			run := helperRunning(t, s, foreign)
			mine := delegated(t, useAtoll(t, s, boss, ev, delegateCall("Tester", _hangPrompt, ""))[0])
			mineRun := helperRunning(t, s, mine)

			ids := []string{"no-such-thread", foreign, boss.ID}
			calls := []fake.MCPCall{}
			for _, id := range ids {
				calls = append(calls, stopHelperCall(id))
			}
			got := useAtoll(t, s, boss, ev, calls...)
			var want []string
			for _, id := range ids {
				want = append(want, "atoll/stop_helper: "+id+" is not one of your helper threads.")
			}
			if !slices.Equal(got, want) {
				t.Errorf("stop_helper on other threads: reply %q, want %q", got, want)
			}
			stays(t, s, foreign, run, "running")
			stays(t, s, mine, mineRun, "running")
		})
	}
}

// TestStopHelper_HeldContinueStaysHeld: a continue held for a helper whose run goes is still held
// after stop_helper stops that run, and it starts no run, as after the person's Stop.
func TestStopHelper_HeldContinueStaysHeld(t *testing.T) {
	t.Parallel()
	for _, p := range _oneProvider {
		t.Run(p, func(t *testing.T) {
			m, s := start(t, p)
			poteto, tester := s.newBot(t, "Poteto", p), s.newBot(t, "Tester", p)
			setDelegates(t, s, poteto, tester)
			boss := s.newThread(t, poteto)
			ev := s.events(t, boss)

			helper := delegated(t, useAtoll(t, s, boss, ev, delegateCall("Tester", _hangPrompt, ""))[0])
			run := helperRunning(t, s, helper)
			held := "Then check the changelog. " + _askPrompt
			got := useAtoll(t, s, boss, ev, continueCall(helper, held), stopHelperCall(helper))
			if len(got) < 3 || got[0] != "atoll/continue: Queued: Tester is working; it gets this when its run ends." || got[2] != "atoll/stop_helper: Stopped Tester." {
				t.Fatalf("continue then stop_helper: reply %q, want Queued, the helpers line, then Stopped Tester.", got)
			}
			stopped(t, s, helper, run)

			stays(t, s, helper, run, "stopped")
			if q := queuedTexts(t, s, helper); !slices.ContainsFunc(q, func(x string) bool { return strings.Contains(x, held) }) {
				t.Errorf("helper's queued messages %q after stop_helper, want the held continue %q", q, held)
			}
			if sessionWith(t, m, p, held) != nil {
				t.Errorf("the held continue reached the provider after stop_helper, want it held")
			}
		})
	}
}
