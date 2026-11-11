package e2e

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"
)

// prFixture is what the fake GitHub says about a pull request it accepted: open until a test
// merges or closes it.
type prFixture struct {
	state  string
	merged bool
	title  string
}

// fakeCheck is a check run; an empty conclusion is JSON null, as GitHub has it until completed.
type fakeCheck struct{ Name, Status, Conclusion string }

// fakeStatus is a commit status.
type fakeStatus struct{ Context, State string }

// prWatch is the state behind the routes the watcher reads: the pull request, and the check runs and
// statuses of a commit.
type prWatch struct {
	prs      map[int]*prFixture
	runs     map[string][]fakeCheck
	statuses map[string][]fakeStatus
	reads    map[int]int
}

// headSHA is the head commit of pull request n: the same for as long as no test pushes.
func headSHA(n int) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("head of pull request %d", n)))
	return hex.EncodeToString(sum[:])[:40]
}

// setPull merges, closes or reopens pull request n on GitHub: state is "open" or "closed".
func (g *fakeGitHub) setPull(n int, state string, merged bool) {
	g.set(func() {
		pr := g.pr(n)
		pr.state, pr.merged = state, merged
	})
}

// setTitle changes the title of pull request n, so the next read is a new reply with a new ETag.
func (g *fakeGitHub) setTitle(n int, title string) {
	g.set(func() { g.pr(n).title = title })
}

// setChecks replaces the check runs and statuses of the commit.
func (g *fakeGitHub) setChecks(sha string, runs []fakeCheck, statuses []fakeStatus) {
	g.set(func() {
		if g.watch.runs == nil {
			g.watch.runs, g.watch.statuses = map[string][]fakeCheck{}, map[string][]fakeStatus{}
		}
		g.watch.runs[sha], g.watch.statuses[sha] = runs, statuses
	})
}

// pullReads is how many times the server read pull request n.
func (g *fakeGitHub) pullReads(n int) int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.watch.reads[n]
}

// pr is pull request n; the caller holds g.mu. A pull request the fake accepted is open with its
// title; one it never accepted is nil.
func (g *fakeGitHub) pr(n int) *prFixture {
	if g.watch.prs == nil {
		g.watch.prs = map[int]*prFixture{}
	}
	if g.watch.prs[n] == nil {
		if n < 1 || n > len(g.pulls) {
			return nil
		}
		g.watch.prs[n] = &prFixture{state: "open", title: g.pulls[n-1].Title}
	}
	return g.watch.prs[n]
}

func (g *fakeGitHub) reject(w http.ResponseWriter, r *http.Request) bool {
	if g.authorised(r.Header.Get("Authorization")) {
		return false
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	_, _ = fmt.Fprint(w, `{"message":"Bad credentials","status":"401"}`)
	return true
}

// getPull is GET /repos/{owner}/{repo}/pulls/{number}: state, merged, title and head.sha, with an
// ETag, and 304 for a request whose If-None-Match is that ETag.
func (g *fakeGitHub) getPull(w http.ResponseWriter, r *http.Request) {
	if g.reject(w, r) {
		return
	}
	n, _ := strconv.Atoi(r.PathValue("number"))
	var body []byte
	g.set(func() {
		if g.watch.reads == nil {
			g.watch.reads = map[int]int{}
		}
		g.watch.reads[n]++
		if pr := g.pr(n); pr != nil {
			body, _ = json.Marshal(map[string]any{"number": n, "state": pr.state, "merged": pr.merged, "title": pr.title,
				"head": map[string]string{"sha": headSHA(n)}})
		}
	})
	w.Header().Set("Content-Type", "application/json")
	if body == nil {
		w.WriteHeader(http.StatusNotFound)
		_, _ = fmt.Fprint(w, `{"message":"Not Found"}`)
		return
	}
	sum := sha256.Sum256(body)
	etag := `"` + hex.EncodeToString(sum[:8]) + `"`
	w.Header().Set("ETag", etag)
	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	_, _ = w.Write(body)
}

// checkRuns is GET /repos/{owner}/{repo}/commits/{sha}/check-runs.
func (g *fakeGitHub) checkRuns(w http.ResponseWriter, r *http.Request) {
	if g.reject(w, r) {
		return
	}
	g.mu.Lock()
	runs := g.watch.runs[r.PathValue("sha")]
	g.mu.Unlock()
	out := []map[string]any{}
	for _, c := range runs {
		var conclusion any
		if c.Conclusion != "" {
			conclusion = c.Conclusion
		}
		out = append(out, map[string]any{"name": c.Name, "status": c.Status, "conclusion": conclusion})
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"total_count": len(out), "check_runs": out})
}

// commitStatus is GET /repos/{owner}/{repo}/commits/{sha}/status: pending with no statuses for a
// commit that has none, as GitHub answers.
func (g *fakeGitHub) commitStatus(w http.ResponseWriter, r *http.Request) {
	if g.reject(w, r) {
		return
	}
	g.mu.Lock()
	statuses := g.watch.statuses[r.PathValue("sha")]
	g.mu.Unlock()
	state := "pending"
	out := []map[string]string{}
	for _, s := range statuses {
		out = append(out, map[string]string{"context": s.Context, "state": s.State})
		if s.State == "failure" || s.State == "error" {
			state = "failure"
		} else if state == "pending" && s.State == "success" {
			state = "success"
		}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"state": state, "total_count": len(out), "statuses": out})
}

// prWakeFixture is a repo on the fake GitHub, a Bot with the GitHub connection and its threads,
// each with the pull request its Bot opened and tracked.
type prWakeFixture struct {
	*githubRepoFixture
	provider string
	boss     bot
	repo     githubRepo
}

func newPRWakeFixture(t *testing.T, provider string) *prWakeFixture {
	t.Helper()
	f := &prWakeFixture{githubRepoFixture: newGitHubRepoFixture(t, provider), provider: provider}
	f.repo = f.register(t, "octo-hello", f.hello(t))
	f.boss = f.bot(t, provider, []string{f.connect(t)})
	return f
}

// openPR starts a thread, has its Bot open and track a pull request, and returns the thread and the
// pull request's number. The fake GitHub numbers pull requests from 1.
func (f *prWakeFixture) openPR(t *testing.T, n int) thread {
	t.Helper()
	th := f.thread(t, f.boss, f.repo)
	ch := f.s.events(t, th)
	r := f.s.send(t, th, _githubFixPrompt)
	until(t, ch, r.ID, "run_finished")
	if done := f.s.run(t, r.ID); done.Status != "done" {
		t.Fatalf("run that opens the pull request: %+v", done)
	}
	if got := len(f.gh.pullRequests()); got != n {
		t.Fatalf("the fake GitHub has %d pull requests, want %d", got, n)
	}
	var got struct {
		MRURL string `json:"mr_url"`
	}
	f.s.do(t, "GET", "/api/threads/"+th.ID, nil, &got, 200)
	if want := fmt.Sprintf("%s/octo/hello/pull/%d", f.gh.URL, n); got.MRURL != want {
		t.Fatalf("thread %s tracks %q, want %q", th.ID, got.MRURL, want)
	}
	return th
}

// wakeMessage is a thread message with the kind of a Reef wake: empty for a user's and a helper's.
type wakeMessage struct {
	Role      string `json:"role"`
	Text      string `json:"text"`
	Kind      string `json:"kind"`
	FromBotID string `json:"from_bot_id"`
}

func (f *prWakeFixture) wakes(t *testing.T, th thread) []wakeMessage {
	t.Helper()
	var all, out []wakeMessage
	f.s.do(t, "GET", "/api/threads/"+th.ID+"/messages", nil, &all, 200)
	for _, m := range all {
		if m.Kind != "" {
			out = append(out, m)
		}
	}
	return out
}

// reefPrompts are the prompts the provider got that start `Reef: `, the prompt of a wake, in the
// order they came.
func (f *prWakeFixture) reefPrompts(t *testing.T) []string {
	t.Helper()
	var out []string
	for _, r := range promptLog(t, f.m, f.provider) {
		if r.Kind == "user" && strings.HasPrefix(r.Text, "Reef: ") {
			out = append(out, r.Text)
		}
	}
	return out
}

// waitWake waits for the nth wake message of the thread and for the thread's run to end, and returns
// the wake.
func (f *prWakeFixture) waitWake(t *testing.T, th thread, n int) wakeMessage {
	t.Helper()
	var w []wakeMessage
	eventually(t, 30*time.Second, fmt.Sprintf("wake %d in thread %s", n, th.ID), func() bool {
		w = f.wakes(t, th)
		return len(w) >= n
	})
	eventually(t, 30*time.Second, "the run of the wake in thread "+th.ID+" ends", func() bool {
		s := f.s.thread(t, th.ID).RunStatus
		return s != "running" && s != "waiting"
	})
	return w[n-1]
}

// quiet fails when the Bot is woken in any thread within d: a wake message in th, or a `Reef: `
// prompt that the provider got beyond the first want.
func (f *prWakeFixture) quiet(t *testing.T, d time.Duration, what string, th thread, wantPrompts int) {
	t.Helper()
	time.Sleep(d)
	if w := f.wakes(t, th); len(w) != 0 {
		t.Errorf("%s: thread %s got wake messages %+v", what, th.ID, w)
	}
	if got := f.reefPrompts(t); len(got) != wantPrompts {
		t.Errorf("%s: the provider got %d Reef prompts %q, want %d", what, len(got), got, wantPrompts)
	}
}
