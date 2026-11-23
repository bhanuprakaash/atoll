package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"
)

// Issue #614: the header island is gone, and with it its API and what it stored.

// The island's endpoints answer like any unknown API path: 404 with the not-found sentence, also
// for a dismiss of a PR the database holds. GET /api/events was a stream, so the request carries
// its own deadline.
func TestNoIsland_EndpointsGone(t *testing.T) {
	t.Parallel()
	m := newMachine(t, "claude")
	m.signIn(t, "claude")
	dbExec(t, m, mustRead(t, "testdata/before-no-island.sql"))
	s := m.serve(t)
	for _, tc := range []struct {
		method, path string
		body         any
	}{
		{"POST", "/api/island/seen", nil},
		{"POST", "/api/island/dismiss", map[string]string{"type": "mr_updated", "id": "https://github.com/octo/hello/pull/9"}},
		{"GET", "/api/events", nil},
	} {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			var body io.Reader
			if tc.body != nil {
				b, _ := json.Marshal(tc.body)
				body = strings.NewReader(string(b))
			}
			req, err := http.NewRequestWithContext(ctx, tc.method, s.url+tc.path, body)
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Accept", "text/event-stream")
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("%s %s: %v", tc.method, tc.path, err)
			}
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode != http.StatusNotFound {
				t.Fatalf("%s %s: status %d (%s), want 404", tc.method, tc.path, resp.StatusCode, resp.Header.Get("Content-Type"))
			}
			var got failure
			b, _ := io.ReadAll(resp.Body)
			if err := json.Unmarshal(b, &got); err != nil {
				t.Fatalf("%s %s: body %q is not {error, code}: %v", tc.method, tc.path, b, err)
			}
			wantFailure(t, got, "not_found", "That no longer exists.")
		})
	}
}

// testdata/before-no-island.sql is an install from before the island went: a PR in pr_dismissals,
// a dismissed and a kept change, and the island_seen_at setting. The upgrade drops what the island
// stored and keeps every thread, PR and change.
func TestNoIsland_UpgradeDropsIslandState(t *testing.T) {
	t.Parallel()
	m := newMachine(t, "claude")
	m.signIn(t, "claude")
	dbExec(t, m, mustRead(t, "testdata/before-no-island.sql"))
	s := m.serve(t)

	wantPRs := map[string]string{
		"prthread01": "https://github.com/octo/hello/pull/7",
		"prthread02": "https://gitlab.example/fixtures/seeded/-/merge_requests/3",
		"prthread03": "https://github.com/octo/hello/pull/9",
	}
	for id, url := range wantPRs {
		if got := prURLs(threadPRs(t, s, id)); !slices.Equal(got, []string{url}) {
			t.Errorf("after the upgrade GET /api/threads/%s lists the PRs %q, want %q", id, got, url)
		}
	}
	s.do(t, "GET", "/api/threads/prthread04", nil, nil, http.StatusOK)
	var changes []struct {
		ID string `json:"id"`
	}
	s.do(t, "GET", "/api/threads/prthread04/changes", nil, &changes, http.StatusOK)
	var ids []string
	for _, c := range changes {
		ids = append(ids, c.ID)
	}
	slices.Sort(ids)
	if want := []string{"change0001", "change0002"}; !slices.Equal(ids, want) {
		t.Errorf("GET /api/threads/prthread04/changes lists %q, want %q", ids, want)
	}

	s.kill()
	m.killFakes()
	if got := dbRows(t, m, "SELECT name FROM sqlite_schema WHERE name = 'pr_dismissals'"); len(got) != 0 {
		t.Errorf("the table pr_dismissals is still there")
	}
	if got := dbRows(t, m, "SELECT name FROM pragma_table_info('changes') WHERE name = 'dismissed_at'"); len(got) != 0 {
		t.Errorf("changes still has the column dismissed_at")
	}
	if got := dbRows(t, m, "SELECT key FROM settings WHERE key = 'island_seen_at'"); len(got) != 0 {
		t.Errorf("settings still holds island_seen_at")
	}
	for query, want := range map[string]string{
		"SELECT count(*) FROM threads WHERE id LIKE 'prthread0%'":                                   "4",
		"SELECT group_concat(thread_id) FROM (SELECT thread_id FROM thread_prs ORDER BY thread_id)": "prthread01,prthread02,prthread03",
		"SELECT group_concat(id) FROM (SELECT id FROM changes ORDER BY id)":                         "change0001,change0002",
	} {
		if got := dbRows(t, m, query); fmt.Sprint(got) != fmt.Sprint([][]string{{want}}) {
			t.Errorf("%s: %v, want %s", query, got, want)
		}
	}
}
