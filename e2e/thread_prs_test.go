package e2e

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"atoll/e2e/fakes/fake"

	_ "modernc.org/sqlite"
)

// threadPR is one entry of the `prs` a thread summary carries.
type threadPR struct {
	URL   string          `json:"url"`
	State string          `json:"state"`
	Title string          `json:"title"`
	CI    json.RawMessage `json:"ci"`
}

func threadPRs(t *testing.T, s *server, id string) []threadPR {
	t.Helper()
	var got struct {
		PRs []threadPR `json:"prs"`
	}
	s.do(t, "GET", "/api/threads/"+id, nil, &got, http.StatusOK)
	return got.PRs
}

func prURLs(prs []threadPR) []string {
	var out []string
	for _, p := range prs {
		out = append(out, p.URL)
	}
	slices.Sort(out)
	return out
}

// noOldKeys fails when a summary still has a key of the one-PR API.
func noOldKeys(t *testing.T, what string, raw json.RawMessage) {
	t.Helper()
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(raw, &keys); err != nil {
		t.Fatalf("%s: %v: %s", what, err, raw)
	}
	for _, k := range []string{"mr_url", "mr_state", "mr_title", "mr_ci"} {
		if _, ok := keys[k]; ok {
			t.Errorf("%s still has %q, which the prs list replaces: %s", what, k, raw)
		}
	}
	if _, ok := keys["prs"]; !ok {
		t.Errorf("%s has no prs: %s", what, raw)
	}
}

// openPull has the fake GitHub accept a pull request no Bot opened, as gh would, and returns its
// number.
func (f *prWakeFixture) openPull(t *testing.T, title string) int {
	t.Helper()
	n := len(f.gh.pullRequests()) + 1
	body, _ := json.Marshal(map[string]string{"title": title, "head": fmt.Sprintf("extra-%d", n), "base": "main"})
	req, err := http.NewRequestWithContext(t.Context(), "POST", f.gh.URL+"/repos/octo/hello/pulls", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+f.gh.currentToken())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusCreated || len(f.gh.pullRequests()) != n {
		t.Fatalf("the fake GitHub answered %d to a new pull request, has %d, want %d", resp.StatusCode, len(f.gh.pullRequests()), n)
	}
	return n
}

func (f *prWakeFixture) pullURL(n int) string {
	return fmt.Sprintf("%s/octo/hello/pull/%d", f.gh.URL, n)
}

// track has the thread's Bot call track_mr in a follow-up run, which must end done, and returns the
// run's events.
func (f *prWakeFixture) track(t *testing.T, th thread, url, title string) []event {
	t.Helper()
	args, _ := json.Marshal(map[string]string{"url": url, "title": title})
	payload, _ := json.Marshal([]fake.MCPCall{{Server: "atoll", Tool: "track_mr", Args: args}})
	ch := f.s.events(t, th)
	r := f.s.send(t, th, _mcpPromptHead+string(payload))
	es := until(t, ch, r.ID, "run_finished")
	if done := f.s.run(t, r.ID); done.Status != "done" {
		t.Fatalf("run that tracks %s: %+v", url, done)
	}
	return es
}

// openTwo is a thread whose Bot opened pull request first, then tracked a second one.
func (f *prWakeFixture) openTwo(t *testing.T, first int) (th thread, second int) {
	t.Helper()
	th = f.openPR(t, first)
	second = f.openPull(t, "second pull request")
	f.track(t, th, f.pullURL(second), "second pull request")
	return th, second
}

// hiddenOf is whether GET /api/bots marks the thread hidden.
func hiddenOf(t *testing.T, s *server, botID, threadID string) bool {
	t.Helper()
	var bots []struct {
		ID      string `json:"id"`
		Threads []struct {
			ID     string          `json:"id"`
			Hidden bool            `json:"hidden"`
			PRs    json.RawMessage `json:"prs"`
		} `json:"threads"`
	}
	s.do(t, "GET", "/api/bots", nil, &bots, http.StatusOK)
	for _, b := range bots {
		for _, th := range b.Threads {
			if b.ID == botID && th.ID == threadID {
				return th.Hidden
			}
		}
	}
	t.Fatalf("GET /api/bots: no thread %s under Bot %s", threadID, botID)
	return false
}

// dbRows runs query on the data dir's database, which no server may hold open for writing, and
// returns each row's columns as text.
func dbRows(t *testing.T, m *machine, query string, args ...any) [][]string {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+filepath.Join(m.data, "atoll.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	rows, err := db.QueryContext(t.Context(), query, args...)
	if err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	defer func() { _ = rows.Close() }()
	cols, _ := rows.Columns()
	var out [][]string
	for rows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			t.Fatal(err)
		}
		row := make([]string, len(cols))
		for i, v := range vals {
			if b, ok := v.([]byte); ok {
				v = string(b)
			}
			row[i] = fmt.Sprint(v)
		}
		out = append(out, row)
	}
	return out
}

var _threadPRColumns = []string{"changed_at", "ci", "head", "state", "thread_id", "title", "tracked_at", "url", "wake", "woke"}

// wantPRSchema checks thread_prs has its columns and threads has no mr_ column left.
func wantPRSchema(t *testing.T, m *machine) {
	t.Helper()
	var got []string
	for _, r := range dbRows(t, m, "SELECT name FROM pragma_table_info('thread_prs') ORDER BY name") {
		got = append(got, r[0])
	}
	if !slices.Equal(got, _threadPRColumns) {
		t.Errorf("thread_prs columns %q, want %q", got, _threadPRColumns)
	}
	if old := dbRows(t, m, "SELECT name FROM pragma_table_info('threads') WHERE name LIKE 'mr\\_%' ESCAPE '\\'"); len(old) != 0 {
		t.Errorf("threads still has the columns %v, which moved into thread_prs", old)
	}
}

// track_mr of a second URL adds a row and replaces nothing; a repeat of a tracked URL adds no row
// and keeps the row as it is. Every summary lists the PRs and has no mr_url.
func TestThreadPRs_TrackMRKeepsEveryPR(t *testing.T) {
	for _, p := range _oneProvider {
		t.Run(p, func(t *testing.T) {
			f := newPRWakeFixture(t, p)
			th := f.openPR(t, 1)
			first := f.pullURL(1)
			secondURL := f.pullURL(f.openPull(t, "second pull request"))

			es := f.track(t, th, secondURL, "second pull request")
			if mr := repoEvent(t, es, "mr_updated"); mr.Data["url"] != secondURL || mr.Data["state"] != "opened" {
				t.Errorf("mr_updated event %v, want url %s opened: the event names the PR it is about", mr.Data, secondURL)
			}
			got := threadPRs(t, f.s, th.ID)
			if want := []string{first, secondURL}; !slices.Equal(prURLs(got), want) {
				t.Fatalf("thread tracks %q after a second track_mr, want %q (the first is not replaced)", prURLs(got), want)
			}
			for _, pr := range got {
				if pr.State != "opened" || pr.Title == "" {
					t.Errorf("tracked PR %+v, want opened with its title", pr)
				}
			}

			var raw json.RawMessage
			f.s.do(t, "GET", "/api/threads/"+th.ID, nil, &raw, http.StatusOK)
			noOldKeys(t, "GET /api/threads/{id}", raw)
			var bots []struct {
				Threads []struct {
					ID  string     `json:"id"`
					PRs []threadPR `json:"prs"`
				} `json:"threads"`
			}
			f.s.do(t, "GET", "/api/bots", nil, &bots, http.StatusOK)
			seen := false
			for _, b := range bots {
				for _, bt := range b.Threads {
					if bt.ID == th.ID {
						seen = true
						if !slices.Equal(prURLs(bt.PRs), []string{first, secondURL}) {
							t.Errorf("GET /api/bots lists %q for the thread, want both PRs", prURLs(bt.PRs))
						}
					}
				}
			}
			if !seen {
				t.Errorf("GET /api/bots has no thread %s", th.ID)
			}

			f.s.kill()
			f.m.killFakes()
			wantPRSchema(t, f.m)
			const q = "SELECT url, title, tracked_at, changed_at FROM thread_prs WHERE thread_id = ? ORDER BY url"
			before := dbRows(t, f.m, q, th.ID)
			if len(before) != 2 {
				t.Fatalf("thread_prs holds %d rows for the thread, want 2: %v", len(before), before)
			}

			f.s = f.m.serve(t)
			f.track(t, th, secondURL, "renamed by a repeat")
			f.track(t, th, first, "renamed by a repeat")
			if got := threadPRs(t, f.s, th.ID); !slices.Equal(prURLs(got), []string{first, secondURL}) {
				t.Errorf("thread tracks %q after repeats, want the same two", prURLs(got))
			}
			f.s.kill()
			f.m.killFakes()
			if after := dbRows(t, f.m, q, th.ID); fmt.Sprint(after) != fmt.Sprint(before) {
				t.Errorf("a repeat of a tracked URL changed the rows:\nbefore %v\nafter  %v", before, after)
			}
		})
	}
}

// A thread with two tracked PRs is woken once for each PR's CI result and once for each PR's
// merge: four wakes, each naming its PR, none repeated, one run each. Once merged a PR is not read
// again; another thread's PR #3 stays open, so reads of it show the watcher is alive.
func TestThreadPRs_WakePerPR(t *testing.T) {
	for _, p := range _oneProvider {
		t.Run(p, func(t *testing.T) {
			f := newPRWakeFixture(t, p)
			th, second := f.openTwo(t, 1)
			if second != 2 {
				t.Fatalf("second PR is #%d, want #2", second)
			}
			f.openPR(t, 3)
			passed := []fakeCheck{{"build", "completed", "success"}}

			f.gh.setChecks(headSHA(1), passed, nil)
			f.wantWakeAt(t, th, 1, "ci", "CI finished on PR #1: 1 passed")
			f.gh.setChecks(headSHA(2), passed, nil)
			f.wantWakeAt(t, th, 2, "ci", "CI finished on PR #2: 1 passed")
			f.quiet(t, 1, "CI already reported on both PRs", th, 2, 2)

			f.gh.setPull(1, "closed", true)
			f.wantWakeAt(t, th, 3, "merged", "PR #1 merged")
			f.gh.setPull(2, "closed", true)
			f.wantWakeAt(t, th, 4, "merged", "PR #2 merged")
			f.quiet(t, 3, "both merges reported", th, 4, 4, 1, 2)

			seen := map[string]bool{}
			for _, w := range f.wakes(t, th) {
				if seen[w.Text] {
					t.Errorf("wake %q was sent twice", w.Text)
				}
				seen[w.Text] = true
			}
			// the run that opened PR 1, the run that tracked PR 2, the run that opened PR 3, and one run per wake.
			if got := len(f.m.sessions(t, p)); got != 7 {
				t.Errorf("%s ran %d times, want 7: the two runs that opened and tracked, one for PR #3, and one per wake", p, got)
			}
		})
	}
}

// wantWakeAt waits for the nth wake of the thread and checks its kind and sentence, and that the
// provider got exactly n Reef prompts, the last of them this wake.
func (f *prWakeFixture) wantWakeAt(t *testing.T, th thread, n int, kind, sentence string) {
	t.Helper()
	w := f.waitWake(t, th, n)
	if w.Kind != kind || !strings.Contains(w.Text, sentence) || w.FromBotID != "" {
		t.Errorf("wake %d is %+v, want kind %q, text with %q, no from_bot_id", n, w, kind, sentence)
	}
	prompts := f.reefPrompts(t)
	if len(prompts) != n || !strings.HasPrefix(prompts[n-1], "Reef: ") || !strings.Contains(prompts[n-1], sentence) {
		t.Errorf("Reef prompts %q, want %d, the last with %q", prompts, n, sentence)
	}
}

// A thread with one PR merged and one open is not hidden; it hides once both are done, merged or
// closed, at least one merged. Two PRs closed unmerged never hide it.
func TestThreadPRs_HidesWhenEveryPRIsDone(t *testing.T) {
	for _, p := range _oneProvider {
		t.Run(p, func(t *testing.T) {
			f := newPRWakeFixture(t, p)
			th, _ := f.openTwo(t, 1)

			f.gh.setPull(2, "closed", true)
			f.waitWake(t, th, 1)
			if got := threadPRs(t, f.s, th.ID); len(got) != 2 || got[0].State == got[1].State {
				t.Fatalf("thread PRs %+v, want one merged and one opened", got)
			}
			if hiddenOf(t, f.s, f.boss.ID, th.ID) {
				t.Error("thread with one PR merged and one open is hidden, want shown")
			}

			f.gh.setPull(1, "closed", false)
			f.waitWake(t, th, 2)
			for _, pr := range threadPRs(t, f.s, th.ID) {
				if pr.State == "opened" {
					t.Errorf("PR %+v is still opened", pr)
				}
			}
			if !hiddenOf(t, f.s, f.boss.ID, th.ID) {
				t.Error("thread whose PRs are one merged and one closed is shown, want hidden")
			}

			other, _ := f.openTwo(t, 3)
			f.gh.setPull(3, "closed", false)
			f.gh.setPull(4, "closed", false)
			f.waitWake(t, other, 2)
			if hiddenOf(t, f.s, f.boss.ID, other.ID) {
				t.Error("thread whose PRs are both closed unmerged is hidden, want shown (at least one must be merged)")
			}
		})
	}
}

// testdata/before-prs.sql is an install from before a thread tracked several PRs: three threads
// with a PR, each mr_* column set, and one without.
func TestThreadPRs_UpgradeMovesEachPR(t *testing.T) {
	t.Parallel()
	m := newMachine(t, "claude")
	m.signIn(t, "claude")
	dbExec(t, m, mustRead(t, "testdata/before-prs.sql"))
	s := m.serve(t)

	want := map[string]threadPR{
		"prthread01": {URL: "https://github.com/octo/hello/pull/7", State: "opened", Title: "feat: add the widget"},
		"prthread02": {URL: "https://gitlab.example/fixtures/seeded/-/merge_requests/3", State: "merged", Title: "fix: the other thing"},
		"prthread03": {URL: "https://github.com/octo/hello/pull/9", State: "closed", Title: "wip: nothing"},
	}
	check := func(when string) {
		t.Helper()
		for id, w := range want {
			got := threadPRs(t, s, id)
			if len(got) != 1 || got[0].URL != w.URL || got[0].State != w.State || got[0].Title != w.Title {
				t.Errorf("%s: GET /api/threads/%s prs %+v, want one: %s %s %q", when, id, got, w.URL, w.State, w.Title)
			}
			var raw json.RawMessage
			s.do(t, "GET", "/api/threads/"+id, nil, &raw, http.StatusOK)
			noOldKeys(t, when+": "+id, raw)
		}
		if got := threadPRs(t, s, "prthread01"); len(got) == 1 && !strings.Contains(string(got[0].CI), "unit") {
			t.Errorf("%s: prthread01's ci %s lost the failed check's name", when, got[0].CI)
		}
		if got := threadPRs(t, s, "prthread04"); len(got) != 0 {
			t.Errorf("%s: the thread with no PR lists %+v, want none", when, got)
		}
	}
	check("after the upgrade")

	s.kill()
	m.killFakes()
	wantPRSchema(t, m)
	rows := dbRows(t, m, "SELECT thread_id, url, state, title, ci, woke, wake, head, changed_at FROM thread_prs ORDER BY thread_id")
	wantRows := [][]string{
		{"prthread01", "https://github.com/octo/hello/pull/7", "opened", "feat: add the widget",
			`{"head":"1111111111111111111111111111111111111111","passed":2,"failed":1,"pending":0,"failed_names":["unit"]}`,
			"2222222222222222222222222222222222222222", "1", "3333333333333333333333333333333333333333", "1794673900123456"},
		{"prthread02", "https://gitlab.example/fixtures/seeded/-/merge_requests/3", "merged", "fix: the other thing",
			"", "", "0", "", "1794673950000001"},
		{"prthread03", "https://github.com/octo/hello/pull/9", "closed", "wip: nothing",
			`{"head":"4444444444444444444444444444444444444444","passed":0,"failed":0,"pending":0,"failed_names":[]}`,
			"4444444444444444444444444444444444444444", "0", "4444444444444444444444444444444444444444", "1794674000000002"},
	}
	if fmt.Sprint(rows) != fmt.Sprint(wantRows) {
		t.Errorf("thread_prs after the upgrade:\n%v\nwant\n%v", rows, wantRows)
	}

	s = m.serve(t)
	check("after a restart")
	s.kill()
	m.killFakes()
	if got := dbRows(t, m, "SELECT count(*) FROM thread_prs"); got[0][0] != "3" {
		t.Errorf("thread_prs holds %s rows after a restart, want 3", got[0][0])
	}
}
