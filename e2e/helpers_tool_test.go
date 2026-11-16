package e2e

import (
	"bytes"
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"atoll/e2e/fakes/fake"
)

func helpersCall() fake.MCPCall {
	return fake.MCPCall{Server: "atoll", Tool: "helpers", Args: json.RawMessage(`{}`)}
}

// helperRow is one row of helpers(). Reading it with unknown fields refused is how a test sees that
// a row holds nothing but the design's fields, so no message text.
type helperRow struct {
	Bot      string   `json:"bot"`
	ThreadID string   `json:"thread_id"`
	Title    string   `json:"title"`
	State    string   `json:"state"`
	IdleS    *float64 `json:"idle_s"`
	Branch   string   `json:"branch"`
	Ahead    int      `json:"ahead"`
}

// listedHelpers reads the reply line of a helpers() call: its rows, its line and its JSON text.
// The "helpers" member must be a JSON array, never null.
func listedHelpers(t *testing.T, reply string) (rows []helperRow, line, text string) {
	t.Helper()
	text, ok := strings.CutPrefix(reply, "atoll/helpers: ")
	if !ok {
		t.Fatalf("helpers() reply %q, want atoll/helpers: and JSON", reply)
	}
	var out struct {
		Helpers json.RawMessage `json:"helpers"`
		Line    string          `json:"line"`
	}
	dec := json.NewDecoder(strings.NewReader(text))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&out); err != nil || !bytes.HasPrefix(out.Helpers, []byte("[")) {
		t.Fatalf("helpers() result %q (error %v), want {\"helpers\": [...], \"line\": \"...\"}", text, err)
	}
	dec = json.NewDecoder(bytes.NewReader(out.Helpers))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&rows); err != nil {
		t.Fatalf("helpers() rows %s: %v, want rows of bot, thread_id, title, state, idle_s, branch and ahead only", out.Helpers, err)
	}
	return rows, out.Line, text
}

// callHelpers has the boss call helpers() in one run.
func callHelpers(t *testing.T, s *server, boss thread, ev <-chan event) ([]helperRow, string, string) {
	t.Helper()
	got := useAtoll(t, s, boss, ev, helpersCall())
	if len(got) != 1 {
		t.Fatalf("helpers(): reply %q, want one line", got)
	}
	return listedHelpers(t, got[0])
}

// wantRow checks the fields every row has: the Bot, the thread and its title.
func wantRow(t *testing.T, s *server, r helperRow, botName, id, state string) {
	t.Helper()
	if r.Bot != botName || r.ThreadID != id || r.Title != s.thread(t, id).Title || r.State != state {
		t.Errorf("helpers() row %+v, want bot %q, thread %s, title %q, state %q", r, botName, id, s.thread(t, id).Title, state)
	}
	if state != "running" && r.IdleS != nil && *r.IdleS != 0 {
		t.Errorf("helpers() row of thread %s is %q with idle_s %v, want none: idle_s is only for a running helper", id, state, *r.IdleS)
	}
}

// TestHelpers_OnlyForBosses: helpers is listed for a Bot with delegates in a thread of its own, and
// for no other: not for a Bot with none, not in a helper run even when the helper Bot has delegates.
func TestHelpers_OnlyForBosses(t *testing.T) {
	t.Parallel()
	for _, p := range _oneProvider {
		t.Run(p, func(t *testing.T) {
			_, s := start(t, p)
			poteto, tester, reviewer, ada := s.newBot(t, "Poteto", p), s.newBot(t, "Tester", p), s.newBot(t, "Reviewer", p), s.newBot(t, "Ada", p)
			setDelegates(t, s, poteto, tester)
			setDelegates(t, s, tester, reviewer)
			absent := "atoll/helpers: unavailable"

			plain := s.newThread(t, ada)
			if got := useAtoll(t, s, plain, s.events(t, plain), helpersCall()); !slices.Equal(got, []string{absent}) {
				t.Errorf("Ada (no delegates): reply %q, want %q", got, absent)
			}

			own := s.newThread(t, tester)
			if got := useAtoll(t, s, own, s.events(t, own), helpersCall()); len(got) != 1 || got[0] == absent {
				t.Errorf("Tester (delegates Reviewer) in its own thread: reply %q, want helpers listed", got)
			}

			boss := s.newThread(t, poteto)
			ev := s.events(t, boss)
			if got := useAtoll(t, s, boss, ev, helpersCall()); len(got) != 1 || got[0] == absent {
				t.Errorf("Poteto (delegates Tester): reply %q, want helpers listed", got)
			}
			brief, _ := json.Marshal([]fake.MCPCall{helpersCall()})
			lines := useAtoll(t, s, boss, ev, delegateCall("Tester", _mcpPromptHead+string(brief), ""))
			helper := settled(t, s, delegated(t, lines[0]))
			if helper.Status != "done" || helper.Reply != absent {
				t.Errorf("Tester's helper run: status %q, reply %q, error %q; want done, %q", helper.Status, helper.Reply, helper.Error, absent)
			}
		})
	}
}

// TestHelpers_ListsEachHelper: one row per helper thread, oldest first, with the Bot, the thread,
// its title and the state of its last run: running, done, failed and stopped. Only the running
// helper has idle_s, a few seconds at most. The result holds the helpers line and no text from the
// helper threads: not the reply of the helper that finished, not the error of the one that failed,
// not the brief.
func TestHelpers_ListsEachHelper(t *testing.T) {
	t.Parallel()
	for _, p := range _oneProvider {
		t.Run(p, func(t *testing.T) {
			_, s := start(t, p)
			poteto, tester, reviewer := s.newBot(t, "Poteto", p), s.newBot(t, "Tester", p), s.newBot(t, "Reviewer", p)
			setDelegates(t, s, poteto, tester, reviewer)
			boss := s.newThread(t, poteto)
			ev := s.events(t, boss)

			lines := useAtoll(t, s, boss, ev,
				delegateCall("Reviewer", _reviews[0], ""), delegateCall("Tester", _reportPrompt, ""), delegateCall("Tester", _noRecordingPrompt, ""))
			if len(lines) != 3 {
				t.Fatalf("three delegates: reply %q, want three lines", lines)
			}
			ids := []string{delegated(t, lines[0]), delegated(t, lines[1]), delegated(t, lines[2])}
			helperRunning(t, s, ids[0])
			allow(t, s, ids[1])
			if r := settled(t, s, ids[1]); r.Status != "done" {
				t.Fatalf("reporting helper: status %q, error %q; want done", r.Status, r.Error)
			}
			failed := settled(t, s, ids[2])
			if failed.Status != "failed" {
				t.Fatalf("helper with no recording: status %q, want failed", failed.Status)
			}
			quietBoss(t, s, boss.ID, tester.ID, 2)

			ids = append(ids, delegated(t, useAtoll(t, s, boss, ev, delegateCall("Tester", _reviews[1], ""))[0]))
			s.do(t, "POST", "/api/runs/"+helperRunning(t, s, ids[3])+"/stop", nil, nil, http.StatusNoContent)
			if r := settled(t, s, ids[3]); r.Status != "stopped" {
				t.Fatalf("stopped helper: status %q, want stopped", r.Status)
			}
			quietBoss(t, s, boss.ID, tester.ID, 3)

			rows, line, text := callHelpers(t, s, boss, ev)
			if len(rows) != 4 {
				t.Fatalf("helpers() rows %+v, want 4", rows)
			}
			for i, w := range []struct{ bot, state string }{{"Reviewer", "running"}, {"Tester", "done"}, {"Tester", "failed"}, {"Tester", "stopped"}} {
				wantRow(t, s, rows[i], w.bot, ids[i], w.state)
			}
			if rows[0].IdleS == nil || *rows[0].IdleS < 0 || *rows[0].IdleS > 60 {
				t.Errorf("running helper's idle_s %v, want 0 to 60 s", rows[0].IdleS)
			}
			if want := helpersLine(onThread(t, s, "Reviewer", ids[0])); line != want {
				t.Errorf("helpers() line %q, want %q", line, want)
			}
			for _, bad := range []string{_reportReply, "Should I update", failed.Error, "delegated this to you"} {
				if strings.Contains(text, bad) {
					t.Errorf("helpers() result %q holds helper thread text %q, want none", text, bad)
				}
			}
		})
	}
}

// TestHelpers_IdleGrows: a running helper's idle_s counts seconds since its last tool call, so it
// is within 60 s and reaches 2 s while the helper waits.
func TestHelpers_IdleGrows(t *testing.T) {
	t.Parallel()
	for _, p := range _oneProvider {
		t.Run(p, func(t *testing.T) {
			_, s := start(t, p)
			poteto, tester := s.newBot(t, "Poteto", p), s.newBot(t, "Tester", p)
			setDelegates(t, s, poteto, tester)
			boss := s.newThread(t, poteto)
			ev := s.events(t, boss)

			helper := delegated(t, useAtoll(t, s, boss, ev, delegateCall("Tester", _hangPrompt, ""))[0])
			helperRunning(t, s, helper)
			eventually(t, 20*time.Second, "helpers() shows idle_s of 2 s or more for the waiting helper", func() bool {
				rows, _, _ := callHelpers(t, s, boss, ev)
				if len(rows) != 1 || rows[0].State != "running" || rows[0].IdleS == nil || *rows[0].IdleS < 0 || *rows[0].IdleS > 60 {
					t.Fatalf("helpers() rows %+v, want one running helper with idle_s of 0 to 60 s", rows)
				}
				return *rows[0].IdleS >= 2
			})
		})
	}
}

// TestHelpers_LimitedHelper: a helper whose run a provider usage limit stopped is listed as limited.
// The boss runs on the other provider, which is not limited.
func TestHelpers_LimitedHelper(t *testing.T) {
	t.Parallel()
	m := newMachine(t, "claude", "codex")
	m.signIn(t, "claude")
	m.signIn(t, "codex")
	s := m.serve(t)
	m.limit(t, "claude", time.Now().Add(2*time.Hour).Truncate(time.Second))

	poteto, tester := s.newBot(t, "Poteto", "codex"), s.newBot(t, "Tester", "claude")
	setDelegates(t, s, poteto, tester)
	boss := s.newThread(t, poteto)
	ev := s.events(t, boss)

	helper := delegated(t, useAtoll(t, s, boss, ev, delegateCall("Tester", _reportPrompt, ""))[0])
	if r := s.limitRun(t, settled(t, s, helper).ID); r.ErrorKind != "rate_limited" {
		t.Fatalf("helper run: status %q, error_kind %q, want rate_limited", r.Status, r.ErrorKind)
	}
	quietBoss(t, s, boss.ID, tester.ID, 1)

	rows, _, _ := callHelpers(t, s, boss, ev)
	if len(rows) != 1 {
		t.Fatalf("helpers() rows %+v, want 1", rows)
	}
	wantRow(t, s, rows[0], "Tester", helper, "limited")
}

// TestHelpers_BranchAndAhead: with a boss worktree, each helper's row has the helper's branch and
// the commits it is ahead of the boss's branch: 2 after two commits, 0 with none. A boss thread
// with no worktree gives its helper no branch and 0.
func TestHelpers_BranchAndAhead(t *testing.T) {
	t.Parallel()
	for _, p := range _oneProvider {
		t.Run(p, func(t *testing.T) {
			f := newRepoFixture(t, p)
			s := f.s
			poteto, tester, reviewer := s.newBot(t, "Poteto", p), s.newBot(t, "Tester", p), s.newBot(t, "Reviewer", p)
			setDelegates(t, s, poteto, tester, reviewer)
			boss := f.thread(t, poteto)
			ev := s.events(t, boss)

			own := bossWorktree(t, s, boss, ev)
			lines := useAtoll(t, s, boss, ev, delegateCall("Tester", _reviews[0], ""), delegateCall("Reviewer", _reviews[1], ""))
			ids := []string{delegated(t, lines[0]), delegated(t, lines[1])}
			made := worktreeOf(t, f.repo.Path, own.Branch+"--tester")
			if made == "" || worktreeOf(t, f.repo.Path, own.Branch+"--reviewer") == "" {
				t.Fatalf("helper worktrees on %s--tester and %s--reviewer: %q, want both", own.Branch, own.Branch, made)
			}
			gitIn(t, made, "commit", "--allow-empty", "-m", "first helper commit")
			gitIn(t, made, "commit", "--allow-empty", "-m", "second helper commit")

			rows, _, _ := callHelpers(t, s, boss, ev)
			if len(rows) != 2 {
				t.Fatalf("helpers() rows %+v, want 2", rows)
			}
			wantRow(t, s, rows[0], "Tester", ids[0], "running")
			wantRow(t, s, rows[1], "Reviewer", ids[1], "running")
			if rows[0].Branch != own.Branch+"--tester" || rows[0].Ahead != 2 {
				t.Errorf("Tester's row: branch %q, ahead %d; want %q, 2", rows[0].Branch, rows[0].Ahead, own.Branch+"--tester")
			}
			if rows[1].Branch != own.Branch+"--reviewer" || rows[1].Ahead != 0 {
				t.Errorf("Reviewer's row: branch %q, ahead %d; want %q, 0", rows[1].Branch, rows[1].Ahead, own.Branch+"--reviewer")
			}

			bare := f.thread(t, poteto)
			lone := delegated(t, useAtoll(t, s, bare, s.events(t, bare), delegateCall("Tester", _reviews[2], ""))[0])
			rows, _, _ = callHelpers(t, s, bare, s.events(t, bare))
			if len(rows) != 1 {
				t.Fatalf("helpers() of a boss with no worktree: rows %+v, want 1", rows)
			}
			wantRow(t, s, rows[0], "Tester", lone, "running")
			if rows[0].Branch != "" || rows[0].Ahead != 0 {
				t.Errorf("row of a helper of a boss with no worktree: branch %q, ahead %d; want none, 0", rows[0].Branch, rows[0].Ahead)
			}
		})
	}
}

// TestHelpers_NoHelpers: with no helper threads helpers() returns an empty list and the helpers
// line.
func TestHelpers_NoHelpers(t *testing.T) {
	t.Parallel()
	for _, p := range _oneProvider {
		t.Run(p, func(t *testing.T) {
			_, s := start(t, p)
			poteto, tester := s.newBot(t, "Poteto", p), s.newBot(t, "Tester", p)
			setDelegates(t, s, poteto, tester)
			boss := s.newThread(t, poteto)

			rows, line, text := callHelpers(t, s, boss, s.events(t, boss))
			if len(rows) != 0 || line != helpersLine() {
				t.Errorf("helpers() with no helpers: %q, want an empty list and line %q", text, helpersLine())
			}
		})
	}
}

// TestHelpers_ChangesNothing: helpers() starts no run and writes nothing to either thread: the
// running helper's run, messages and queue are as before, so are the finished helper's, and the boss
// thread gets no run or message from a helper Bot.
func TestHelpers_ChangesNothing(t *testing.T) {
	t.Parallel()
	for _, p := range _oneProvider {
		t.Run(p, func(t *testing.T) {
			_, s := start(t, p)
			poteto, tester := s.newBot(t, "Poteto", p), s.newBot(t, "Tester", p)
			setDelegates(t, s, poteto, tester)
			boss := s.newThread(t, poteto)
			ev := s.events(t, boss)

			lines := useAtoll(t, s, boss, ev, delegateCall("Tester", _hangPrompt, ""), delegateCall("Tester", _keepPrompt, ""))
			going, done := delegated(t, lines[0]), delegated(t, lines[1])
			run := helperRunning(t, s, going)
			finished := settled(t, s, done)
			if finished.Status != "done" {
				t.Fatalf("finished helper: status %q, error %q; want done", finished.Status, finished.Error)
			}
			quietBoss(t, s, boss.ID, tester.ID, 1)

			goingMsgs, doneMsgs, queued := messages(t, s, going), messages(t, s, done), queuedTexts(t, s, going)
			bossRun := getThread(t, s, boss.ID).RunID
			rows, _, _ := callHelpers(t, s, boss, ev)
			if len(rows) != 2 {
				t.Fatalf("helpers() rows %+v, want 2", rows)
			}
			after := getThread(t, s, boss.ID)
			if after.RunID == bossRun {
				t.Fatalf("boss thread's latest run is still %s after the run that called helpers()", bossRun)
			}
			bossRun = after.RunID

			stays(t, s, going, run, "running")
			stays(t, s, done, finished.ID, "done")
			if got := messages(t, s, going); !slices.Equal(got, goingMsgs) {
				t.Errorf("running helper's messages %+v after helpers(), want %+v", got, goingMsgs)
			}
			if got := messages(t, s, done); !slices.Equal(got, doneMsgs) {
				t.Errorf("finished helper's messages %+v after helpers(), want %+v", got, doneMsgs)
			}
			if got := queuedTexts(t, s, going); !slices.Equal(got, queued) {
				t.Errorf("running helper's queue %q after helpers(), want %q", got, queued)
			}
			if got := fromBot(t, s, boss.ID, tester.ID); len(got) != 1 {
				t.Errorf("messages from Tester in the boss thread %+v after helpers(), want only the one for the finished helper", got)
			}
			if th := getThread(t, s, boss.ID); th.RunID != bossRun || th.RunStatus != "done" {
				t.Errorf("boss thread's latest run %s is %q, want %s done and no new run", th.RunID, th.RunStatus, bossRun)
			}
		})
	}
}
