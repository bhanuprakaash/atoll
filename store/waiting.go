package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"
)

// What waits on the person, by kind.
const (
	WaitDecision = "decision"
	WaitBlocker  = "blocker"
	WaitReady    = "ready"
)

// OnPurposeNote is the note line that says a red PR fails on its base for the right reason.
const OnPurposeNote = "fails on main for the right reason"

// Waiting is one thing that waits on the person. It is derived from runs, messages and the PRs
// threads track; nothing of it is stored.
type Waiting struct {
	Kind string `json:"kind"`
	// Decision.
	Question string   `json:"question,omitempty"`
	Options  []string `json:"options,omitempty"`
	// Blocker.
	Text string `json:"text,omitempty"`
	// Ready PR. Checks is "green", "red", "running" or "" when no thread has read them.
	URL       string    `json:"url,omitempty"`
	Title     string    `json:"title,omitempty"`
	Checks    string    `json:"checks,omitempty"`
	OnPurpose bool      `json:"on_purpose,omitempty"`
	SHA       string    `json:"sha,omitempty"`
	Changed   bool      `json:"changed,omitempty"`
	Since     time.Time `json:"since,omitzero"`
}

// Waiting lists what waits on the person in every open thread that is not a helper's, by thread.
func (s *Store) Waiting(ctx context.Context) (map[string][]Waiting, error) {
	return s.waiting(ctx, "")
}

// ThreadWaiting is Waiting for one thread.
func (s *Store) ThreadWaiting(ctx context.Context, threadID string) ([]Waiting, error) {
	all, err := s.waiting(ctx, threadID)
	return all[threadID], err
}

type waitRun struct {
	rowid    int64
	finished time.Time
	report   Report
}

type waitedRun struct {
	thread  string
	replied int64
	run     waitRun
}

type tracker struct {
	closed    bool
	title, ci string
	heads     []string
}

func (s *Store) waiting(ctx context.Context, threadID string) (map[string][]Waiting, error) {
	reports := map[string][]waitRun{}
	runs, err := query(ctx, s.db,
		`SELECT r.thread_id, r.rowid, r.finished_at, r.report, coalesce((
		        SELECT max(r2.rowid) FROM messages m JOIN runs r2 ON r2.id = m.run_id
		        WHERE m.thread_id = r.thread_id AND m.role = ?2 AND m.from_bot_id = '' AND m.kind = ''), 0)
		 FROM runs r JOIN threads t ON t.id = r.thread_id
		 WHERE t.closed = 0 AND t.parent_thread_id = '' AND r.report IS NOT NULL
		   AND (?1 = '' OR r.thread_id = ?1)
		   AND (json_extract(r.report, '$.ask') IS NOT NULL
		        OR json_array_length(json_extract(r.report, '$.ready')) > 0
		        OR r.rowid = (SELECT max(rowid) FROM runs WHERE thread_id = r.thread_id AND report IS NOT NULL))
		 ORDER BY r.rowid`,
		func(r *sql.Rows) (wr waitedRun, err error) {
			var finished int64
			var report string
			if err = r.Scan(&wr.thread, &wr.run.rowid, &finished, &report, &wr.replied); err != nil {
				return wr, err
			}
			if finished != 0 {
				wr.run.finished = time.UnixMicro(finished).UTC()
			}
			if err = json.Unmarshal([]byte(report), &wr.run.report); err != nil {
				return wr, fmt.Errorf("decode report: %w", err)
			}
			return wr, nil
		}, threadID, RoleUser)
	if err != nil {
		return nil, fmt.Errorf("list reports: %w", err)
	}
	replied := map[string]int64{}
	for _, r := range runs {
		reports[r.thread] = append(reports[r.thread], r.run)
		replied[r.thread] = r.replied
	}
	if len(reports) == 0 {
		return nil, nil
	}
	var trackers map[string]*tracker
	if slices.ContainsFunc(runs, func(r waitedRun) bool { return len(r.run.report.Ready) > 0 }) {
		if trackers, err = s.trackers(ctx); err != nil {
			return nil, err
		}
	}
	out := map[string][]Waiting{}
	for id, rs := range reports {
		if items := waitingOf(rs, replied[id], trackers); len(items) > 0 {
			out[id] = items
		}
	}
	return out, nil
}

// trackers reads, by PR URL, what the threads that track it last saw.
func (s *Store) trackers(ctx context.Context) (map[string]*tracker, error) {
	rows, err := query(ctx, s.db,
		"SELECT mr_url, mr_state, mr_title, mr_ci, mr_head FROM threads WHERE mr_url != '' ORDER BY rowid",
		func(r *sql.Rows) (row struct{ url, state, title, ci, head string }, err error) {
			return row, r.Scan(&row.url, &row.state, &row.title, &row.ci, &row.head)
		})
	if err != nil {
		return nil, fmt.Errorf("list tracked PRs: %w", err)
	}
	out := map[string]*tracker{}
	for _, r := range rows {
		t := out[r.url]
		if t == nil {
			t = &tracker{}
			out[r.url] = t
		}
		t.closed = t.closed || r.state == "merged" || r.state == "closed"
		if r.title != "" {
			t.title = r.title
		}
		if r.ci != "" {
			t.ci = r.ci
		}
		if r.head != "" {
			t.heads = append(t.heads, r.head)
		}
	}
	return out, nil
}

// waitingOf derives one thread's items from its runs that hold an ask, a ready list or the newest
// report (oldest first), the rowid of the newest run a person's message started, and the PRs.
func waitingOf(runs []waitRun, replied int64, trackers map[string]*tracker) []Waiting {
	var out []Waiting
	for i := len(runs) - 1; i >= 0; i-- {
		ask := runs[i].report.Ask
		if ask == nil {
			continue
		}
		if runs[i].rowid > replied {
			out = append(out, Waiting{Kind: WaitDecision, Question: ask.Question, Options: ask.Options})
		}
		break
	}
	if last := runs[len(runs)-1]; last.rowid > replied && RealBlocked(last.report.Blocked) {
		out = append(out, Waiting{Kind: WaitBlocker, Text: strings.TrimSpace(last.report.Blocked)})
	}
	type entry struct {
		pr    ReadyPR
		run   waitRun
		since time.Time
	}
	var order []string
	entries := map[string]*entry{}
	for _, run := range runs {
		for _, p := range run.report.Ready {
			e := entries[p.URL]
			since := run.finished
			if e == nil {
				order = append(order, p.URL)
			} else if e.pr.SHA == p.SHA {
				since = e.since
			}
			entries[p.URL] = &entry{pr: p, run: run, since: since}
		}
	}
	for _, url := range order {
		e, t := entries[url], trackers[url]
		w := Waiting{Kind: WaitReady, URL: url, SHA: e.pr.SHA, Since: e.since}
		if t != nil {
			if t.closed {
				continue
			}
			w.Title, w.Checks = t.title, checksOf(t.ci)
			for _, head := range t.heads {
				w.Changed = w.Changed || head != e.pr.SHA
			}
		} else if e.run.rowid <= replied {
			continue
		}
		w.OnPurpose = w.Checks == "red" && strings.EqualFold(strings.TrimSpace(e.pr.Note), OnPurposeNote)
		if w.Since.IsZero() {
			w.Since = time.Now().UTC()
		}
		out = append(out, w)
	}
	return out
}

// RealBlocked says whether a report's blocked field names something that blocks: it is not empty
// or a none-like word.
func RealBlocked(blocked string) bool {
	switch strings.ToLower(strings.TrimSpace(blocked)) {
	case "", "none", "nothing", "nothing.":
		return false
	}
	return true
}

func checksOf(ci string) string {
	var c struct {
		Passed  int `json:"passed"`
		Failed  int `json:"failed"`
		Pending int `json:"pending"`
	}
	if ci == "" || json.Unmarshal([]byte(ci), &c) != nil {
		return ""
	}
	switch {
	case c.Failed > 0:
		return "red"
	case c.Pending > 0:
		return "running"
	case c.Passed > 0:
		return "green"
	}
	return ""
}
