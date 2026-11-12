package store

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode"
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

// ThreadWaiting lists what waits on the person in one thread.
func (s *Store) ThreadWaiting(ctx context.Context, threadID string) ([]Waiting, error) {
	// Only Waiting is read, so no thread has its run polled.
	threads, err := s.threadRuns(ctx, _oneWaiting, "polled(thread_id, status, reply) AS (SELECT '', '', '' WHERE 0)",
		"t.id = ?", threadID, threadID)
	if err != nil || len(threads) == 0 {
		return nil, err
	}
	return threads[0].Waiting, nil
}

type waitRun struct {
	rowid    int64
	finished time.Time
	report   Report
}

type waitRow struct {
	Rowid    int64
	Finished int64
	Report   Report
}

type trackRow struct {
	Rowid                       int64
	URL, State, Title, CI, Head string
}

type tracker struct {
	closed    bool
	title, ci string
	heads     []string
}

// _waitingWith defines the CTEs that read the runs, the reply and the PRs that waitingFrom needs.
// waits has one row per open thread that is not a helper's, with the runs that hold an ask, a
// ready list or its newest report. For every thread, NOT INDEXED reads runs in one pass: the
// planner otherwise visits every thread's runs through runs_thread, which is twice as slow. For
// one thread, runs_thread finds its runs.
const _waitingWith = `waits AS MATERIALIZED (
		   SELECT thread_id, json_group_array(json_object(
		            'rowid', rid, 'finished', finished_at, 'report', json(report))) AS runs
		   FROM (SELECT r.thread_id, r.rowid AS rid, r.finished_at, r.report,
		                r.rowid = max(r.rowid) OVER (PARTITION BY r.thread_id) AS newest
		         FROM runs r %s JOIN threads t ON t.id = r.thread_id
		         WHERE t.closed = 0 AND t.parent_thread_id = '' AND r.report IS NOT NULL%s)
		   WHERE newest
		      OR json_extract(report, '$.ask') IS NOT NULL
		      OR json_array_length(json_extract(report, '$.ready')) > 0
		   GROUP BY thread_id),
		 replies AS MATERIALIZED (
		   SELECT m.thread_id, max(r.rowid) AS replied FROM messages m JOIN runs r ON r.id = m.run_id
		   WHERE m.role = 'user' AND m.from_bot_id = '' AND m.kind = ''
		     AND m.thread_id IN (SELECT thread_id FROM waits)
		   GROUP BY m.thread_id),
		 tracked AS MATERIALIZED (
		   SELECT u.thread_id, json_group_array(json_object(
		            'rowid', t3.rowid, 'url', t3.mr_url, 'state', t3.mr_state, 'title', t3.mr_title,
		            'ci', t3.mr_ci, 'head', t3.mr_head)) AS trackers
		   FROM (SELECT DISTINCT w.thread_id, json_extract(rd.value, '$.url') AS url
		         FROM waits w, json_each(w.runs) run, json_each(run.value, '$.report.ready') rd) u
		   JOIN threads t3 ON t3.mr_url = u.url
		   GROUP BY u.thread_id)`

var (
	_allWaiting = fmt.Sprintf(_waitingWith, "NOT INDEXED", "")
	_oneWaiting = fmt.Sprintf(_waitingWith, "", " AND t.id = ?")
)

// waitingFrom derives one thread's items from the columns of _waitingWith: its runs as a JSON
// array, the rowid of the newest run a person's message started, and the PRs of its ready lists
// as a JSON array.
func waitingFrom(runsJSON string, replied int64, trackersJSON string) ([]Waiting, error) {
	if runsJSON == "" {
		return nil, nil
	}
	var raw []waitRow
	if err := json.Unmarshal([]byte(runsJSON), &raw); err != nil {
		return nil, fmt.Errorf("decode reports: %w", err)
	}
	slices.SortFunc(raw, func(a, b waitRow) int { return cmp.Compare(a.Rowid, b.Rowid) })
	runs := make([]waitRun, len(raw))
	for i, r := range raw {
		runs[i] = waitRun{rowid: r.Rowid, report: r.Report}
		if r.Finished != 0 {
			runs[i].finished = time.UnixMicro(r.Finished).UTC()
		}
	}
	trackers, err := decodeTrackers(trackersJSON)
	if err != nil {
		return nil, err
	}
	return waitingOf(runs, replied, trackers), nil
}

// decodeTrackers reads, by PR URL, what the threads that track it last saw.
func decodeTrackers(trackersJSON string) (map[string]*tracker, error) {
	if trackersJSON == "" {
		return nil, nil
	}
	var rows []trackRow
	if err := json.Unmarshal([]byte(trackersJSON), &rows); err != nil {
		return nil, fmt.Errorf("decode tracked PRs: %w", err)
	}
	slices.SortFunc(rows, func(a, b trackRow) int { return cmp.Compare(a.Rowid, b.Rowid) })
	out := map[string]*tracker{}
	for _, r := range rows {
		t := out[r.URL]
		if t == nil {
			t = &tracker{}
			out[r.URL] = t
		}
		t.closed = t.closed || r.State == "merged" || r.State == "closed"
		if r.Title != "" {
			t.title = r.Title
		}
		if r.CI != "" {
			t.ci = r.CI
		}
		if r.Head != "" {
			t.heads = append(t.heads, r.Head)
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
		if runs[i].rowid >= replied {
			out = append(out, Waiting{Kind: WaitDecision, Question: ask.Question, Options: ask.Options})
		}
		break
	}
	if last := runs[len(runs)-1]; last.rowid >= replied && RealBlocked(last.report.Blocked) {
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
		} else if e.run.rowid < replied {
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

// RealBlocked says whether a report's blocked field names something that blocks: after its
// trailing punctuation is trimmed it is not empty or a none-like phrase.
func RealBlocked(blocked string) bool {
	blocked = strings.TrimSpace(strings.TrimRightFunc(strings.TrimSpace(blocked), unicode.IsPunct))
	switch strings.ToLower(blocked) {
	case "", "nothing blocking", "nothing blocked", "none", "nothing", "n/a":
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
