package e2e

import (
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"atoll/e2e/fakes/fake"
)

// Contract: boss-quiet-contract.md.

// The helper plays testdata/*/helper-progress.jsonl: it asks to run make check, then reports
// progress. A boss woken with that report plays testdata/*/wake-quiet.jsonl: finish_run, no reply.
const (
	_progressPrompt = "Run the checks, then send a progress note."
	_progressReply  = "Tester here: 2 of 5 checks done."
	_reportedBack   = "reported back from thread "
)

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

// bossStill fails when the boss thread's latest run is not run: a run started for a report that
// the wake window should still hold.
func bossStill(t *testing.T, s *server, boss, run, why string) {
	t.Helper()
	time.Sleep(3 * _pace)
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

// TestBossQuiet_PromptTellsBossToStayQuiet: a Bot with a delegate list gets the part `Helper
// reports` in its system prompt, whose lines tell it to end a progress-only wake with finish_run
// and no reply; a Bot without one has no such part.
func TestBossQuiet_PromptTellsBossToStayQuiet(t *testing.T) {
	t.Parallel()
	for _, p := range _providers {
		t.Run(p, func(t *testing.T) {
			t.Parallel()
			_, s := start(t, p)
			poteto, tester := s.newBot(t, "Poteto", p), s.newBot(t, "Tester", p)
			if got := heySystem(t, s, p, poteto); slices.Contains(strings.Split(got, "\n"), "Helper reports") {
				t.Errorf("system prompt of a Bot with no delegates has a Helper reports part:\n%s", got)
			}
			setDelegates(t, s, poteto, tester)
			system := heySystem(t, s, p, poteto)
			lines := strings.Split(system, "\n")
			i := slices.Index(lines, "Helper reports")
			if i < 0 {
				t.Fatalf("system prompt of a Bot with delegates has no line Helper reports:\n%s", system)
			}
			part := ""
			for _, l := range lines[i+1:] {
				if l == "" {
					break
				}
				part += l + "\n"
			}
			holdsAll(t, "Helper reports part", part, "finish_run", "reply")
			assertParts(t, system, "core", "code")
		})
	}
}

// TestBossQuiet_ProgressWakeAddsNothing: a boss woken by a helper's progress report ends with
// finish_run and no reply, which adds no bot message and does not make the thread New; a wake
// whose run does reply does both.
func TestBossQuiet_ProgressWakeAddsNothing(t *testing.T) {
	t.Parallel()
	for _, p := range _providers {
		t.Run(p, func(t *testing.T) {
			t.Parallel()
			m, s := start(t, p)
			poteto, tester := s.newBot(t, "Poteto", p), s.newBot(t, "Tester", p)
			setDelegates(t, s, poteto, tester)
			boss := s.newThread(t, poteto)
			ev := s.events(t, boss)
			ids, delegating := delegateTester(t, s, boss, ev, _progressPrompt)
			s.do(t, "POST", "/api/threads/"+boss.ID+"/opened", nil, nil, http.StatusNoContent)
			botMessages := func() (n int) {
				for _, msg := range bossMessages(t, s, boss.ID) {
					if msg.Role == "bot" {
						n++
					}
				}
				return n
			}
			before := botMessages()

			helpersEnd(t, s, ids[0])
			msg, _, wake := woken(t, m, s, p, boss.ID, tester.ID, 1)
			holdsAll(t, "progress report", msg.Text, ids[0], _progressReply)
			if wake.ID == delegating || wake.Status != "done" || wake.Report == nil {
				t.Fatalf("boss run for the progress report: %+v, want a done run of its own with a finish_run report", wake)
			}
			if wake.Reply != "" {
				t.Errorf("boss run for the progress report replied %q, want no reply", wake.Reply)
			}
			if got := botMessages(); got != before {
				t.Errorf("boss thread has %d bot messages after a progress-only wake, want the %d it had", got, before)
			}
			if got := sidebarState(t, s, poteto.ID, boss.ID); got != "" {
				t.Errorf("boss thread marker %q after a progress-only wake, want none (not new)", got)
			}

			asked, _ := delegateTester(t, s, boss, ev, _reportPrompt)
			s.do(t, "POST", "/api/threads/"+boss.ID+"/opened", nil, nil, http.StatusNoContent)
			before = botMessages()
			helpersEnd(t, s, asked[0])
			_, _, wake = woken(t, m, s, p, boss.ID, tester.ID, 2)
			if wake.Reply != _wakeReply {
				t.Fatalf("boss run for the question: reply %q, want %q", wake.Reply, _wakeReply)
			}
			if got := botMessages(); got != before+1 {
				t.Errorf("boss thread has %d bot messages after a reply, want %d", got, before+1)
			}
			if got := sidebarState(t, s, poteto.ID, boss.ID); got != "new" {
				t.Errorf("boss thread marker %q after a reply, want new", got)
			}
		})
	}
}

// TestBossQuiet_ReportsInWindowStartOneRun: reports that arrive within the wake window start no run
// until it is over, then one boss run whose prompt holds every report header in arrival order, and
// each report is its own message of that run.
func TestBossQuiet_ReportsInWindowStartOneRun(t *testing.T) {
	t.Parallel()
	for _, p := range _providers {
		t.Run(p, func(t *testing.T) {
			t.Parallel()
			m, s := startWindow(t, p, "12s")
			poteto, tester := s.newBot(t, "Poteto", p), s.newBot(t, "Tester", p)
			setDelegates(t, s, poteto, tester)
			boss := s.newThread(t, poteto)
			ev := s.events(t, boss)
			ids, delegating := delegateTester(t, s, boss, ev, _reportPrompt, _reportPrompt, _reportPrompt)
			order := []string{ids[1], ids[2], ids[0]}

			for _, h := range order {
				helpersEnd(t, s, h)
				bossStill(t, s, boss.ID, delegating, "for a report inside the wake window")
			}
			_, c, wake := woken(t, m, s, p, boss.ID, tester.ID, 3)
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
				headers = append(headers, "Tester "+_reportedBack+h+".")
			}
			prompts := promptsWith(t, m, p, _reportedBack)
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
			m, s := startWindow(t, p, "6s")
			poteto, tester := s.newBot(t, "Poteto", p), s.newBot(t, "Tester", p)
			setDelegates(t, s, poteto, tester)
			boss := s.newThread(t, poteto)
			ev := s.events(t, boss)
			ids, delegating := delegateTester(t, s, boss, ev, _reportPrompt)
			helpersEnd(t, s, ids[0])
			held := time.Now()
			bossStill(t, s, boss.ID, delegating, "for a report inside the wake window")

			const ask = "Tester, how far along is it?"
			run := s.send(t, boss, ask)
			if got := s.finished(t, ev, run.ID); got.Status != "done" {
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
			if len(prompts) != 1 || !inOrder(prompts[0], ask, "Tester "+_reportedBack+ids[0]+".") {
				t.Errorf("boss prompts %q, want one with the person's text, then Tester's header", prompts)
			}

			time.Sleep(time.Until(held.Add(6*time.Second + 2*time.Second)))
			if th := getThread(t, s, boss.ID); th.RunID != run.ID {
				t.Errorf("boss thread started run %s after the window, want the report already delivered with %s", th.RunID, run.ID)
			}
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
			ids, _ := delegateTester(t, s, boss, ev, _reportPrompt)
			busy := s.send(t, boss, _writePrompt)
			until(t, ev, busy.ID, "approval")
			helpersEnd(t, s, ids[0])

			s.do(t, "POST", "/api/runs/"+busy.ID+"/stop", nil, nil, http.StatusNoContent)
			if got := s.finished(t, ev, busy.ID); got.Status != "stopped" {
				t.Fatalf("boss run after stop: status %q, want stopped", got.Status)
			}
			time.Sleep(4 * _pace)
			if th := getThread(t, s, boss.ID); th.RunID != busy.ID {
				t.Fatalf("boss thread started run %s after the user stopped %s, want none", th.RunID, busy.ID)
			}

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
			if len(prompts) != 1 || !inOrder(prompts[0], ask, "Tester "+_reportedBack+ids[0]+".") {
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
			time.Sleep(4 * _pace)
			if th := getThread(t, s, boss.ID); th.RunID != wake.ID {
				t.Errorf("boss thread started run %s after the restart wake %s, want one run", th.RunID, wake.ID)
			}
			if got := promptsWith(t, m, p, ids[0]); len(got) != 1 {
				t.Errorf("%d boss prompts name helper %s, want 1", len(got), ids[0])
			}
		})
	}
}
