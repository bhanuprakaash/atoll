package e2e

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"atoll/e2e/fakes/fake"
)

// Contract: boss-quiet-contract.md.

// _finishedThread is in the first line of a helper's notice: `Reef: Tester finished thread {id}:`.
const _finishedThread = "finished thread "

func finishedNotice(helperName, threadID string) string {
	return "Reef: " + helperName + " " + _finishedThread + threadID + ":"
}

type bossMessage struct {
	Role      string `json:"role"`
	Text      string `json:"text"`
	FromBotID string `json:"from_bot_id"`
	RunID     string `json:"run_id"`
}

func bossMessages(t *testing.T, s *server, threadID string) []bossMessage {
	t.Helper()
	var msgs []bossMessage
	s.do(t, "GET", "/api/threads/"+threadID+"/messages", nil, &msgs, http.StatusOK)
	return msgs
}

// startWindow serves atoll with the wake window set to window.
func startWindow(t *testing.T, p, window string) (*machine, *server) {
	t.Helper()
	m := newMachine(t, p)
	m.env = append(m.env, "ATOLL_WAKE_WINDOW="+window)
	m.signIn(t, p)
	return m, m.serve(t)
}

// promptsWith are the prompts of the user kind the provider got, in order, that hold text.
func promptsWith(t *testing.T, m *machine, p, text string) []string {
	t.Helper()
	var out []string
	for _, r := range promptLog(t, m, p) {
		if r.Kind == "user" && strings.Contains(r.Text, text) {
			out = append(out, r.Text)
		}
	}
	return out
}

// helpersEnd has each helper's run end done, in order, one after the other.
func helpersEnd(t *testing.T, s *server, helpers ...string) {
	t.Helper()
	for _, h := range helpers {
		allow(t, s, h)
		if r := settled(t, s, h); r.Status != "done" {
			t.Fatalf("helper %s: status %q, error %q; want done", h, r.Status, r.Error)
		}
	}
}

// bossStill waits until at and fails when the boss thread's latest run is then not run: a run
// started for a report that should still be held.
func bossStill(t *testing.T, s *server, boss, run string, at time.Time, why string) {
	t.Helper()
	time.Sleep(time.Until(at))
	if th := getThread(t, s, boss); th.RunID != run {
		t.Fatalf("boss thread %s started run %s (%s) after run %s, want no run %s", boss, th.RunID, th.RunStatus, run, why)
	}
}

// nextBossRun waits for a boss run other than after to end and returns it.
func nextBossRun(t *testing.T, s *server, boss, after string) runInfo {
	t.Helper()
	var th threadState
	eventually(t, 30*time.Second, "a boss run after "+after+" that ended", func() bool {
		th = getThread(t, s, boss)
		return th.RunID != after && th.RunStatus != "running" && th.RunStatus != "waiting"
	})
	return s.run(t, th.RunID)
}

// delegateTester has the boss delegate one brief per call to Tester and returns the helper thread
// ids and the run that delegated.
func delegateTester(t *testing.T, s *server, boss thread, ev <-chan event, briefs ...string) ([]string, string) {
	t.Helper()
	var calls []fake.MCPCall
	for _, b := range briefs {
		calls = append(calls, delegateCall("Tester", b, ""))
	}
	lines := useAtoll(t, s, boss, ev, calls...)
	var ids []string
	for _, l := range lines {
		ids = append(ids, delegated(t, l))
	}
	return ids, getThread(t, s, boss.ID).RunID
}

// inOrder reports whether every want is in text, the first after the start and each after the last.
func inOrder(text string, want ...string) bool {
	at := 0
	for _, w := range want {
		i := strings.Index(text[at:], w)
		if i < 0 {
			return false
		}
		at += i + len(w)
	}
	return true
}

func sidebarState(t *testing.T, s *server, botID, threadID string) string {
	t.Helper()
	var bots []struct {
		ID      string `json:"id"`
		Threads []struct {
			ID    string `json:"id"`
			State string `json:"state"`
		} `json:"threads"`
	}
	s.do(t, "GET", "/api/bots", nil, &bots, http.StatusOK)
	for _, b := range bots {
		for _, th := range b.Threads {
			if b.ID == botID && th.ID == threadID {
				return th.State
			}
		}
	}
	t.Fatalf("GET /api/bots: no thread %s under Bot %s in %+v", threadID, botID, bots)
	return ""
}

// TestBossQuiet_ReportsInWindowStartOneRun: reports that arrive within the wake window start no run
// until it is over, then one boss run whose prompt holds every report header in arrival order, and
// each report is its own message of that run.
func TestBossQuiet_ReportsInWindowStartOneRun(t *testing.T) {
	t.Parallel()
	for _, p := range _providers {
		t.Run(p, func(t *testing.T) {
			t.Parallel()
			const window = 12 * time.Second
			m, s := startWindow(t, p, window.String())
			poteto, tester := s.newBot(t, "Poteto", p), s.newBot(t, "Tester", p)
			setDelegates(t, s, poteto, tester)
			boss := s.newThread(t, poteto)
			ev := s.events(t, boss)
			ids, delegating := delegateTester(t, s, boss, ev, _reportPrompt, _reportPrompt, _reportPrompt)
			order := []string{ids[1], ids[2], ids[0]}

			before := time.Now()
			helpersEnd(t, s, order[0])
			held := time.Now()
			helpersEnd(t, s, order[1:]...)
			bossStill(t, s, boss.ID, delegating, held.Add(window-2*time.Second), "2 s before the wake window ends")
			_, c, wake := woken(t, m, s, p, boss.ID, tester.ID, 3)
			var times runTimes
			s.do(t, "GET", "/api/runs/"+wake.ID, nil, &times, http.StatusOK)
			if times.StartedAt.Before(before.Add(window)) {
				t.Errorf("boss run started %v after the first report was held, want no run before the %v window ends",
					times.StartedAt.Sub(before), window)
			}
			msgs := fromBot(t, s, boss.ID, tester.ID)
			var runs []string
			for _, msg := range bossMessages(t, s, boss.ID) {
				if msg.FromBotID == tester.ID {
					runs = append(runs, msg.RunID)
				}
			}
			if len(runs) != 3 || runs[0] == "" || runs[0] != runs[1] || runs[1] != runs[2] || runs[0] != wake.ID {
				t.Errorf("the 3 reports are messages of runs %v, want all of %s", runs, wake.ID)
			}
			var headers []string
			for _, h := range order {
				headers = append(headers, finishedNotice("Tester", h))
			}
			prompts := promptsWith(t, m, p, _finishedThread)
			if len(prompts) != 1 {
				t.Fatalf("%d boss prompts hold a report, want 1: %q", len(prompts), prompts)
			}
			if !inOrder(prompts[0], headers...) {
				t.Errorf("boss prompt %q does not hold the headers %q in arrival order", prompts[0], headers)
			}
			if got := resumed(c, p); got != bossSessionID(p) {
				t.Errorf("boss run resumed %q, want the boss's session %q", got, bossSessionID(p))
			}
			if wake.Status != "done" || len(msgs) != 3 {
				t.Errorf("boss run %s: status %q, %d messages from Tester; want done, 3", wake.ID, wake.Status, len(msgs))
			}
		})
	}
}

// TestBossQuiet_PersonTakesHeldReports: a person's message sent while a report waits in the window
// starts its run at once and takes the report with it, the person's text first; the report starts no
// run of its own when the window ends.
func TestBossQuiet_PersonTakesHeldReports(t *testing.T) {
	t.Parallel()
	for _, p := range _providers {
		t.Run(p, func(t *testing.T) {
			t.Parallel()
			const window = 6 * time.Second
			m, s := startWindow(t, p, window.String())
			poteto, tester := s.newBot(t, "Poteto", p), s.newBot(t, "Tester", p)
			setDelegates(t, s, poteto, tester)
			boss := s.newThread(t, poteto)
			ev := s.events(t, boss)
			ids, delegating := delegateTester(t, s, boss, ev, _reportPrompt)
			helpersEnd(t, s, ids[0])
			held := time.Now()
			bossStill(t, s, boss.ID, delegating, held.Add(window-2*time.Second), "2 s before the wake window ends")

			const ask = "Tester, how far along is it?"
			run := s.send(t, boss, ask)
			got := s.finished(t, ev, run.ID)
			if got.Status != "done" {
				t.Fatalf("boss run for the person's message: status %q, error %q; want done", got.Status, got.Error)
			}
			var report bossMessage
			for _, msg := range bossMessages(t, s, boss.ID) {
				if msg.FromBotID == tester.ID {
					report = msg
				}
			}
			if report.RunID != run.ID {
				t.Errorf("Tester's report is a message of run %q, want the person's run %s", report.RunID, run.ID)
			}
			prompts := promptsWith(t, m, p, ask)
			if len(prompts) != 1 || !inOrder(prompts[0], ask, finishedNotice("Tester", ids[0])) {
				t.Errorf("boss prompts %q, want one with the person's text, then Tester's header", prompts)
			}
			// A message of the person makes the run ordinary: no background line, no tell_user, and the
			// final message is the reply.
			if len(prompts) == 1 && strings.Contains(prompts[0], "background run") {
				t.Errorf("boss prompt %q has the background line, want an ordinary run", prompts[0])
			}
			assertTools(t, "ordinary", toolsOf(t, m, p, contains(ask)), []string{"delegate", "continue"}, []string{"tell_user", "finish_run"})
			if text := botText(t, s, boss.ID, run.ID); got.Reply != _wakeReply || text != _wakeReply {
				t.Errorf("run reply %q and Bot message %q, want the final message %q as the reply", got.Reply, text, _wakeReply)
			}

			bossStill(t, s, boss.ID, run.ID, held.Add(window+2*time.Second), "after the window: the report went with the person's message")
		})
	}
}

// TestBossQuiet_StoppedRunStartsNone: a report held for a boss whose run the user stopped starts no
// run; the next message to the boss takes it with it.
func TestBossQuiet_StoppedRunStartsNone(t *testing.T) {
	t.Parallel()
	for _, p := range _providers {
		t.Run(p, func(t *testing.T) {
			t.Parallel()
			m, s := start(t, p)
			poteto, tester := s.newBot(t, "Poteto", p), s.newBot(t, "Tester", p)
			setDelegates(t, s, poteto, tester)
			boss := s.newThread(t, poteto)
			ev := s.events(t, boss)
			ids, _ := delegateTester(t, s, boss, ev, _slowPrompt)
			busy := s.send(t, boss, _writePrompt)
			until(t, ev, busy.ID, "approval")
			if r := settled(t, s, ids[0]); r.Status != "done" {
				t.Fatalf("helper %s: status %q, error %q; want done", ids[0], r.Status, r.Error)
			}
			bossStill(t, s, boss.ID, busy.ID, time.Now().Add(4*_pace), "while the boss's run waits for its approval")

			s.do(t, "POST", "/api/runs/"+busy.ID+"/stop", nil, nil, http.StatusNoContent)
			if got := s.finished(t, ev, busy.ID); got.Status != "stopped" {
				t.Fatalf("boss run after stop: status %q, want stopped", got.Status)
			}
			bossStill(t, s, boss.ID, busy.ID, time.Now().Add(4*_pace), "after the user stopped it")

			const ask = "Tester, any news?"
			run := s.send(t, boss, ask)
			s.finished(t, ev, run.ID)
			var report bossMessage
			for _, msg := range bossMessages(t, s, boss.ID) {
				if msg.FromBotID == tester.ID {
					report = msg
				}
			}
			if report.RunID != run.ID {
				t.Errorf("Tester's held report is a message of run %q, want the next run %s", report.RunID, run.ID)
			}
			prompts := promptsWith(t, m, p, ask)
			if len(prompts) != 1 || !inOrder(prompts[0], ask, finishedNotice("Tester", ids[0])) {
				t.Errorf("boss prompts %q, want one with the person's text, then Tester's header", prompts)
			}
		})
	}
}

// TestBossQuiet_RestartWakesOnce: a restart that interrupted 3 helper runs gives the boss one
// message that names all 3, and one run.
func TestBossQuiet_RestartWakesOnce(t *testing.T) {
	t.Parallel()
	for _, p := range _providers {
		t.Run(p, func(t *testing.T) {
			t.Parallel()
			m, s := start(t, p)
			poteto, tester := s.newBot(t, "Poteto", p), s.newBot(t, "Tester", p)
			setDelegates(t, s, poteto, tester)
			boss := s.newThread(t, poteto)
			ev := s.events(t, boss)
			ids, delegating := delegateTester(t, s, boss, ev, _hangPrompt, _hangPrompt, _hangPrompt)
			runs := map[string]string{}
			for _, h := range ids {
				runs[h] = helperRunning(t, s, h)
			}

			s.kill()
			s = m.serve(t)

			wake := nextBossRun(t, s, boss.ID, delegating)
			var texts []bossMessage
			for _, msg := range bossMessages(t, s, boss.ID) {
				if msg.Role == "user" && strings.Contains(msg.Text, ids[0]) {
					texts = append(texts, msg)
				}
			}
			if len(texts) != 1 {
				t.Fatalf("%d messages in the boss thread name helper %s after the restart, want one that names all 3: %+v", len(texts), ids[0], texts)
			}
			text := texts[0]
			if first, _, _ := strings.Cut(text.Text, "\n"); first != "A restart interrupted 3 helper runs." {
				t.Errorf("restart message starts %q, want %q", first, "A restart interrupted 3 helper runs.")
			}
			var failed []string
			for _, h := range ids {
				r := s.run(t, runs[h])
				if r.Status != "failed" || r.Error == "" {
					t.Fatalf("helper run %s going at the restart: status %q, error %q; want failed with a reason", r.ID, r.Status, r.Error)
				}
				holdsAll(t, "restart message", text.Text, h, "Tester's run failed: "+r.Error)
				failed = append(failed, h)
			}
			if !inOrder(text.Text, failed...) {
				t.Errorf("restart message %q does not name the helpers %q in the order they began", text.Text, failed)
			}
			if text.FromBotID != "" || text.RunID != wake.ID || wake.Status != "done" {
				t.Errorf("restart message from_bot_id %q, run %q; boss run %s %q; want Reef's, one done run %s", text.FromBotID, text.RunID, wake.ID, wake.Status, text.RunID)
			}
			bossStill(t, s, boss.ID, wake.ID, time.Now().Add(4*_pace), "after the restart wake")
			if got := promptsWith(t, m, p, ids[0]); len(got) != 1 {
				t.Errorf("%d boss prompts name helper %s, want 1", len(got), ids[0])
			}
		})
	}
}
