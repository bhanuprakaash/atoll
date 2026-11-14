package e2e

import (
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"
	"time"
)

// Contract: no-report-contract.md.

// processFor waits for the provider process whose first user prompt satisfies match (the fake logs
// the prompt a moment after the run starts) and returns its pid and the whole prompt log.
func processFor(t *testing.T, m *machine, p string, match func(prompt string) bool) (int, []promptRecord) {
	t.Helper()
	var pid int
	var records []promptRecord
	eventually(t, 15*time.Second, "a provider process that got a matching user prompt", func() bool {
		records = promptLog(t, m, p)
		for _, r := range records {
			if r.Kind == "user" && match(r.Text) {
				pid = r.PID
				return true
			}
		}
		return false
	})
	return pid, records
}

// toolsOf is the tool names the atoll MCP server listed to the provider process whose first user
// prompt satisfies match, from the fake's "tools" records.
func toolsOf(t *testing.T, m *machine, p string, match func(prompt string) bool) []string {
	t.Helper()
	pid, records := processFor(t, m, p, match)
	for _, r := range records {
		if r.Kind != "tools" || r.PID != pid {
			continue
		}
		var listed map[string][]string
		if err := json.Unmarshal([]byte(r.Text), &listed); err != nil {
			t.Fatalf("tools record %q: %v", r.Text, err)
		}
		if _, ok := listed["atoll"]; !ok {
			t.Fatalf("tools record %q lists no atoll server", r.Text)
		}
		return listed["atoll"]
	}
	t.Fatalf("process %d recorded no tools: %+v", pid, records)
	return nil
}

func assertTools(t *testing.T, kind string, got []string, has, lacks []string) {
	t.Helper()
	for _, name := range has {
		if !slices.Contains(got, name) {
			t.Errorf("%s run lists %v, want %q among them", kind, got, name)
		}
	}
	for _, name := range lacks {
		if slices.Contains(got, name) {
			t.Errorf("%s run lists %v, want no %q", kind, got, name)
		}
	}
}

func mustRead(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func personaOf(t *testing.T, s *server, id string) string {
	t.Helper()
	var b struct {
		Persona string `json:"persona"`
	}
	s.do(t, "GET", "/api/bots/"+id, nil, &b, 200)
	return b.Persona
}

func contains(text string) func(string) bool {
	return func(prompt string) bool { return strings.Contains(prompt, text) }
}

// finish_run is in no run's tool list; the Bot's other tools stay as they were, and tell_user and
// delegate are where the design lists them (the background run is in background_runs_test.go).
func TestNoReport_FinishRunIsInNoToolList(t *testing.T) {
	t.Parallel()
	for _, p := range _providers {
		t.Run(p+"/ordinary", func(t *testing.T) {
			t.Parallel()
			m, s := start(t, p)
			heySystem(t, s, p, s.newBot(t, "Ada", p))
			assertTools(t, "ordinary", toolsOf(t, m, p, contains(_hey)),
				[]string{"remember", "recall", "start_work", "track_mr", "approve"},
				[]string{"finish_run", "tell_user", "delegate", "continue"})
		})
		t.Run(p+"/boss", func(t *testing.T) {
			t.Parallel()
			m, s := start(t, p)
			poteto := s.newBot(t, "Poteto", p)
			setDelegates(t, s, poteto, s.newBot(t, "Tester", p))
			heySystem(t, s, p, poteto)
			assertTools(t, "boss", toolsOf(t, m, p, contains(_hey)),
				[]string{"remember", "delegate", "continue", "approve"},
				[]string{"finish_run", "tell_user"})
		})
		t.Run(p+"/helper", func(t *testing.T) {
			t.Parallel()
			m, s := start(t, p)
			poteto, tester := s.newBot(t, "Poteto", p), s.newBot(t, "Tester", p)
			setDelegates(t, s, poteto, tester)
			boss := s.newThread(t, poteto)
			helper := delegated(t, useAtoll(t, s, boss, s.events(t, boss), delegateCall("Tester", _hangPrompt, ""))[0])
			helperRunning(t, s, helper)
			assertTools(t, "helper", toolsOf(t, m, p, func(prompt string) bool {
				return strings.Contains(prompt, "delegated this to you") && strings.Contains(prompt, _hangPrompt)
			}), []string{"remember", "approve"}, []string{"finish_run", "tell_user", "delegate", "continue"})
		})
		t.Run(p+"/platform", func(t *testing.T) {
			t.Parallel()
			m, s := start(t, p)
			heySystem(t, s, p, chief(t, s).bot)
			assertTools(t, "platform", toolsOf(t, m, p, contains(_hey)),
				[]string{"create_bot", "list_bots", "remember", "approve"},
				[]string{"finish_run", "tell_user"})
		})
	}
}

// No nudge follows a run's request: the provider reads one user line for the run, even when the
// run ends without a tool call, and none arrives after it ends.
func TestNoReport_NoNudgeIsSent(t *testing.T) {
	t.Parallel()
	for _, p := range _providers {
		t.Run(p, func(t *testing.T) {
			t.Parallel()
			m, s := start(t, p)
			b := s.newBot(t, "Ada", p)
			for _, prompt := range []string{_keepPrompt, _noToolPrompt} {
				th := s.newThread(t, b)
				if r := s.finished(t, s.events(t, th), s.send(t, th, prompt).ID); r.Status != "done" {
					t.Fatalf("run %q: status %q, error %q; want done", prompt, r.Status, r.Error)
				}
			}
			time.Sleep(4 * _pace)
			perProcess := map[int][]string{}
			for _, r := range promptLog(t, m, p) {
				if r.Kind == "user" {
					perProcess[r.PID] = append(perProcess[r.PID], r.Text)
				}
			}
			if len(perProcess) != 2 {
				t.Fatalf("the provider ran %d processes %v, want one per run", len(perProcess), perProcess)
			}
			for pid, lines := range perProcess {
				if len(lines) != 1 {
					t.Errorf("process %d read %d user lines %q, want only the run's prompt", pid, len(lines), lines)
				}
			}
		})
	}
}

// forbidden are the fields a run's report and the card built from it used to put in the API.
var _reportFields = []string{"report", "waiting", "done", "tried", "blocked", "next", "ask", "ready"}

// reportKeys lists the paths of every report field in a decoded JSON value.
func reportKeys(v any, path string) []string {
	var out []string
	switch v := v.(type) {
	case map[string]any:
		for k, x := range v {
			if slices.Contains(_reportFields, k) {
				out = append(out, path+"."+k)
			}
			out = append(out, reportKeys(x, path+"."+k)...)
		}
	case []any:
		for i, x := range v {
			out = append(out, reportKeys(x, fmt.Sprintf("%s[%d]", path, i))...)
		}
	}
	return out
}

func assertNoReportFields(t *testing.T, s *server, paths ...string) {
	t.Helper()
	for _, path := range paths {
		var got any
		s.do(t, "GET", path, nil, &got, 200)
		if keys := reportKeys(got, ""); len(keys) > 0 {
			t.Errorf("GET %s has report fields %v, want none", path, keys)
		}
	}
}

// GET /api/runs/{id}, the thread summaries and the messages carry no report or waiting field.
func TestNoReport_APIHasNoReportOrWaiting(t *testing.T) {
	t.Parallel()
	for _, p := range _providers {
		t.Run(p, func(t *testing.T) {
			t.Parallel()
			_, s := start(t, p)
			b := s.newBot(t, "Ada", p)
			th := s.newThread(t, b)
			r := s.finished(t, s.events(t, th), s.send(t, th, _keepPrompt).ID)
			if r.Status != "done" || r.Reply != "ok" {
				t.Fatalf("run: status %q, reply %q, error %q; want done, ok", r.Status, r.Reply, r.Error)
			}
			assertNoReportFields(t, s,
				"/api/runs/"+r.ID,
				"/api/threads/"+th.ID,
				"/api/threads/"+th.ID+"/messages",
				"/api/bots/"+b.ID+"/threads",
				"/api/bots")
		})
	}
}

// A run stored before keeps its report column: the row is not touched by an upgrade or a restart,
// and the API gives the run, its reply and its messages without the report.
func TestNoReport_StoredReportKeepsItsRow(t *testing.T) {
	t.Parallel()
	m := newMachine(t, "claude")
	m.signIn(t, "claude")
	old := mustRead(t, "testdata/before-modes.sql")
	const report = `{"done":"Checked it.","tried":"make check","blocked":"","next":"none"}`
	dbExec(t, m, old,
		`INSERT INTO threads (id, bot_id, created) VALUES ('t-old', 'c31d276614b94b87', 1700000000)`,
		`INSERT INTO runs (id, thread_id, status, reply, report, started_at, finished_at) VALUES ('r-old', 't-old', 'done', 'Old reply.', '`+report+`', 1700000000, 1700000010)`,
		`INSERT INTO messages VALUES ('m-old-1', 't-old', 'user', 'Check it.', 'r-old'), ('m-old-2', 't-old', 'bot', 'Old reply.', 'r-old')`)

	s := m.serve(t)
	for restart := 0; ; restart++ {
		if n := dbCount(t, m, `SELECT count(*) FROM runs WHERE id = 'r-old' AND report = '`+report+`'`); n != 1 {
			t.Fatalf("after %d restarts the report column of the old run is not as stored", restart)
		}
		got := s.run(t, "r-old")
		if got.Status != "done" || got.Reply != "Old reply." {
			t.Errorf("old run: status %q, reply %q; want done, Old reply.", got.Status, got.Reply)
		}
		assertNoReportFields(t, s, "/api/runs/r-old", "/api/threads/t-old", "/api/threads/t-old/messages")
		if got := botText(t, s, "t-old", "r-old"); got != "Old reply." {
			t.Errorf("old bot message %q, want Old reply.", got)
		}
		if restart == 1 {
			return
		}
		s.kill()
		s = m.serve(t)
	}
}

// Existing Bots with repo access get "Never merge: {user} merges." appended to their persona once:
// at the launch that upgrades them, not again at the next, and not to a Bot with no repo access or to
// a Bot made later.
func TestNeverMerge_AppendedOnceToBotsWithRepoAccess(t *testing.T) {
	t.Parallel()
	m := newMachine(t, "claude")
	m.signIn(t, "claude")
	dbExec(t, m, mustRead(t, "testdata/before-modes.sql"),
		`INSERT INTO settings (key, value) VALUES ('user_name', 'Bhanu')`,
		`INSERT INTO bots VALUES ('aaaa00000000aaaa', 'Old lister', 'Has one repo.', 'claude', '["repo-x"]', '[]', '[]', 0, 0, 'shell', '', NULL, '[]')`,
		`INSERT INTO bots VALUES ('bbbb00000000bbbb', 'Old talker', 'Only talks.', 'claude', '[]', '[]', '[]', 0, 0, 'shell', '', NULL, '[]')`)
	const line = "Never merge: Bhanu merges."

	check := func(s *server, when string) {
		t.Helper()
		for id, base := range map[string]string{"c31d276614b94b87": "Was here before modes.", "34bf601fa249914c": "Was here before modes.", "aaaa00000000aaaa": "Has one repo."} {
			persona := personaOf(t, s, id)
			if !strings.HasPrefix(persona, base) || !strings.HasSuffix(persona, line) || strings.Count(persona, line) != 1 {
				t.Errorf("%s: persona of %s is %q, want %q then %q once", when, id, persona, base, line)
			}
		}
		if persona := personaOf(t, s, "bbbb00000000bbbb"); persona != "Only talks." {
			t.Errorf("%s: persona of the Bot with no repo access is %q, want it untouched", when, persona)
		}
		if persona := personaOf(t, s, "chief"); strings.Contains(persona, "Never merge") {
			t.Errorf("%s: Chief's persona is %q, want no merge line: Chief has no repo access", when, persona)
		}
	}

	s := m.serve(t)
	check(s, "after the upgrade")
	var fresh bot
	s.do(t, "POST", "/api/bots", map[string]any{"name": "Fresh", "persona": "Made after.", "provider": "claude", "repo_access": "all"}, &fresh, 201)
	for i := 1; i <= 2; i++ {
		s.kill()
		s = m.serve(t)
		check(s, fmt.Sprintf("after restart %d", i))
		if persona := personaOf(t, s, fresh.ID); persona != "Made after." {
			t.Errorf("after restart %d: persona of the Bot made after the upgrade is %q, want it untouched", i, persona)
		}
	}
}
