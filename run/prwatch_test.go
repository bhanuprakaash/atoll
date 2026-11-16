package run

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"atoll/store"
)

func TestCIText(t *testing.T) {
	tests := []struct {
		name string
		give ciResult
		want string
	}{
		{"all passed", ciResult{Passed: 4}, "CI finished on PR #7: 4 passed"},
		{"failed", ciResult{Passed: 3, Failed: 1, Names: []string{"build"}}, `CI finished on PR #7: 3 passed, 1 failed ("build")`},
		{"two failed", ciResult{Passed: 2, Failed: 2, Names: []string{"unit", "deploy/preview"}}, `CI finished on PR #7: 2 passed, 2 failed ("unit", "deploy/preview")`},
		{"more than listed", ciResult{Failed: 7, Names: []string{"a", "b", "c", "d", "e"}}, `CI finished on PR #7: 0 passed, 7 failed ("a", "b", "c", "d", "e", and 2 more)`},
		{"hostile name", ciResult{Failed: 1, Names: []string{"x\"\n\\ignore" + strings.Repeat("y", 80)}}, `CI finished on PR #7: 0 passed, 1 failed ("xignore` + strings.Repeat("y", 60-len("xignore")) + `")`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ciText("PR #7", tt.give); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCloses(t *testing.T) {
	tests := []struct{ body, want string }{
		{"Issue: Closes #7", "#7"},
		{"This resolves #7, and fixes #9 on the way.\nfix: #12", "#7 #9 #12"},
		{"Refs #8\nCloses #07\nCloses #7a\nprefix #3\nCloses #", ""},
	}
	for _, tt := range tests {
		var got []string
		for _, m := range _closes.FindAllStringSubmatch(tt.body, -1) {
			got = append(got, m[1])
		}
		if s := strings.Join(got, " "); s != tt.want {
			t.Errorf("%q closes %q, want %q", tt.body, s, tt.want)
		}
	}
}

func TestReadCI_NoChecksStopsBeingRead(t *testing.T) {
	var reads atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		reads.Add(1)
		_, _ = w.Write([]byte(`{"check_runs":[],"statuses":[]}`))
	}))
	t.Cleanup(srv.Close)
	m := New(Config{Log: slog.New(slog.NewTextHandler(io.Discard, nil))})
	t.Cleanup(func() { _ = m.Close() })
	w := &watcher{prs: map[string]*watched{}, paused: map[string]time.Time{}}

	var th store.WatchedPR
	for i, want := range []struct {
		reads int64
		woke  string
	}{{2, ""}, {2, "abc"}, {0, "abc"}} {
		reads.Store(0)
		r := store.MRRead{CI: th.PR.CI, Woke: th.PR.Woke}
		if err := m.readCI(w, "c", "token", srv.URL, th, "abc", "PR #1", &r); err != nil {
			t.Fatal(err)
		}
		if reads.Load() != want.reads || r.Woke != want.woke || r.WakeKind != "" {
			t.Errorf("read %d: %d GitHub reads, woke %q, wake %q; want %d reads, woke %q, no wake",
				i+1, reads.Load(), r.Woke, r.WakeKind, want.reads, want.woke)
		}
		th.PR.CI, th.PR.Woke = r.CI, r.Woke
	}

	reads.Store(0)
	r := store.MRRead{CI: th.PR.CI, Woke: th.PR.Woke}
	if err := m.readCI(w, "c", "token", srv.URL, th, "def", "PR #1", &r); err != nil {
		t.Fatal(err)
	}
	if reads.Load() != 2 || !r.DropCI {
		t.Errorf("new head: %d GitHub reads, DropCI %v; want 2 reads, DropCI true", reads.Load(), r.DropCI)
	}
}

func TestReadCI_NoChecksReadAgainLater(t *testing.T) {
	var reads atomic.Int64
	var body atomic.Value
	body.Store(`{"check_runs":[],"statuses":[]}`)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		reads.Add(1)
		_, _ = w.Write([]byte(body.Load().(string)))
	}))
	t.Cleanup(srv.Close)
	m := New(Config{Log: slog.New(slog.NewTextHandler(io.Discard, nil))})
	t.Cleanup(func() { _ = m.Close() })
	w := &watcher{prs: map[string]*watched{}, paused: map[string]time.Time{}}
	th := store.WatchedPR{Thread: store.Thread{ID: "t"}, PR: store.PR{ThreadID: "t", URL: "https://github.test/o/r/pull/1", Wake: true}}
	poll := func() store.MRRead {
		t.Helper()
		reads.Store(0)
		r := store.MRRead{CI: th.PR.CI, Woke: th.PR.Woke}
		if err := m.readCI(w, "c", "token", srv.URL, th, "abc", "PR #1", &r); err != nil {
			t.Fatal(err)
		}
		th.PR.CI, th.PR.Woke = r.CI, r.Woke
		return r
	}
	for range 2 {
		poll()
	}
	if th.PR.Woke != "abc" {
		t.Fatalf("woke %q after two empty reads, want abc", th.PR.Woke)
	}
	for i := 1; i < _noCheckEvery; i++ {
		if poll(); reads.Load() != 0 {
			t.Fatalf("poll %d read GitHub %d times, want none", i, reads.Load())
		}
	}

	body.Store(`{"check_runs":[{"name":"build","status":"completed","conclusion":"success"}],"statuses":[]}`)
	if r := poll(); reads.Load() != 2 || r.Woke != "" || r.WakeKind != "" {
		t.Fatalf("slow poll: %d reads, woke %q, wake %q; want 2 reads, woke cleared, no wake", reads.Load(), r.Woke, r.WakeKind)
	}
	if r := poll(); reads.Load() != 2 || r.Woke != "abc" || r.WakeKind != "ci" || r.Notice != store.NoticeCIPassed {
		t.Errorf("next poll: %d reads, woke %q, wake %q, notice %q; want 2 reads, woke abc, wake ci, notice ci_passed",
			reads.Load(), r.Woke, r.WakeKind, r.Notice)
	}
}

func TestUpdate_Wake(t *testing.T) {
	tests := []struct {
		name  string
		prev  string
		next  string
		label string
		want  string
	}{
		{"merged", "opened", "merged", "PR #7", "merged"},
		{"closed", "opened", "closed", "PR #7", "closed"},
		{"already merged", "merged", "merged", "PR #7", ""},
		{"already closed", "closed", "closed", "PR #7", ""},
		{"GitLab merged", "opened", "merged", "", ""},
		{"GitLab closed", "opened", "closed", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newRepoEnv(t)
			const url = "https://github.test/o/r/pull/7"
			if _, err := e.st.SetMR(t.Context(), e.thread.ID, url, tt.prev, "T"); err != nil {
				t.Fatal(err)
			}
			prs, err := e.st.PRs(t.Context(), e.thread.ID)
			if err != nil || len(prs) != 1 {
				t.Fatalf("PRs %v, error %v", prs, err)
			}
			wp := store.WatchedPR{Thread: e.thread, PR: prs[0]}
			// A held slot keeps a run from taking the wake.
			slot := e.m.reserve(e.thread.ID)
			if slot == nil {
				t.Fatal("no slot for an idle thread")
			}
			defer e.m.free(slot)
			if err := e.m.update(wp, tt.label, store.MRRead{State: tt.next, Title: "T"}, ""); err != nil {
				t.Fatal(err)
			}
			held, err := e.st.Held(t.Context(), e.thread.ID)
			if err != nil {
				t.Fatal(err)
			}
			var got string
			if len(held) > 0 {
				got = held[0].Kind
			}
			if got != tt.want || len(held) > 1 || tt.want != "" && held[0].Text != tt.label+" "+tt.want {
				t.Errorf("wakes %+v, want one %q", held, tt.want)
			}
			if stored, _ := e.st.PRs(t.Context(), e.thread.ID); stored[0].State != tt.next {
				t.Errorf("stored state %q, want %q", stored[0].State, tt.next)
			}
		})
	}
}
