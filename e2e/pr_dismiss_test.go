package e2e

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"
)

// islandPRs are the PR URLs of the `mr_updated` rows in the island's Earlier list, the first event
// of GET /api/events after the approvals open now.
func islandPRs(t *testing.T, s *server) []string {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", s.url+"/api/events", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Accept", "text/event-stream")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /api/events: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(nil, 16*1024*1024)
	var typ string
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "event:"):
			typ = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		case strings.HasPrefix(line, "data:") && typ == "earlier":
			var rows []struct {
				Type string `json:"type"`
				Data struct {
					URL string `json:"url"`
				} `json:"data"`
			}
			if err := json.Unmarshal([]byte(strings.TrimSpace(strings.TrimPrefix(line, "data:"))), &rows); err != nil {
				t.Fatalf("earlier event: %v: %s", err, line)
			}
			out := []string{}
			for _, r := range rows {
				if r.Type == "mr_updated" {
					out = append(out, r.Data.URL)
				}
			}
			slices.Sort(out)
			return out
		}
	}
	t.Fatalf("GET /api/events ended before an earlier event: %v", sc.Err())
	return nil
}

func dismissPR(t *testing.T, s *server, url string) {
	t.Helper()
	s.do(t, "POST", "/api/island/dismiss", map[string]string{"type": "mr_updated", "id": url}, nil, http.StatusNoContent)
}

func wantIsland(t *testing.T, s *server, when string, want ...string) {
	t.Helper()
	slices.Sort(want)
	if got := islandPRs(t, s); !slices.Equal(got, want) {
		t.Errorf("%s: the island lists the PRs %q, want %q", when, got, want)
	}
}

// A thread with two newly opened PRs shows two `opened` rows in the island; dismissing one leaves
// the other, and dismissing the other clears the list. A dismissed PR comes back when its
// changed_at is newer than its dismiss (set in the database: a closed PR is final and is not read
// again, so no GitHub change reopens it); the other PR stays dismissed, and dismissing the
// returned PR again hides it again. The dismiss time lives in pr_dismissals (thread_id, url,
// dismissed_at), one row per dismissed PR, and thread_prs keeps its columns.
func TestPRDismiss_PerPR(t *testing.T) {
	for _, p := range _oneProvider {
		t.Run(p, func(t *testing.T) {
			f := newPRWakeFixture(t, p)
			th, second := f.openTwo(t, 1)
			first, secondURL := f.pullURL(1), f.pullURL(second)

			wantIsland(t, f.s, "two new PRs", first, secondURL)
			dismissPR(t, f.s, first)
			wantIsland(t, f.s, "after dismissing the first", secondURL)
			dismissPR(t, f.s, secondURL)
			wantIsland(t, f.s, "after dismissing both")

			f.s.kill()
			f.m.killFakes()
			wantPRSchema(t, f.m)
			var cols []string
			pk := map[string]string{}
			for _, r := range dbRows(t, f.m, "SELECT name, pk FROM pragma_table_info('pr_dismissals') ORDER BY name") {
				cols = append(cols, r[0])
				pk[r[0]] = r[1]
			}
			if want := []string{"dismissed_at", "thread_id", "url"}; !slices.Equal(cols, want) {
				t.Errorf("pr_dismissals columns %q, want %q", cols, want)
			}
			if pk["thread_id"] == "0" || pk["url"] == "0" || pk["dismissed_at"] != "0" {
				t.Errorf("pr_dismissals primary key marks %v, want thread_id and url", pk)
			}
			rows := dbRows(t, f.m, "SELECT thread_id, url, dismissed_at > 0 FROM pr_dismissals ORDER BY url")
			want := [][]string{{th.ID, first, "1"}, {th.ID, secondURL, "1"}}
			if fmt.Sprint(rows) != fmt.Sprint(want) {
				t.Errorf("pr_dismissals holds %v, want one row for each of %s and %s with a time: %v", rows, first, secondURL, want)
			}

			if n := dbExec(t, f.m, "UPDATE thread_prs SET changed_at = CAST(unixepoch('subsec') * 1000000 AS INTEGER) WHERE url = '"+first+"'"); n != 1 {
				t.Fatalf("changed %d PR rows, want 1", n)
			}
			f.s = f.m.serve(t)
			wantIsland(t, f.s, "after the first PR changed again", first)

			dismissPR(t, f.s, first)
			wantIsland(t, f.s, "after dismissing the first again")
			f.s.kill()
			f.m.killFakes()
			if got := dbRows(t, f.m, "SELECT count(*) FROM pr_dismissals"); got[0][0] != "2" {
				t.Errorf("pr_dismissals holds %s rows after a second dismiss of the same PR, want 2 (the row is updated)", got[0][0])
			}
		})
	}
}
