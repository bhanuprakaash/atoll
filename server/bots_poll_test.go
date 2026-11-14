package server

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"atoll/provider"
	"atoll/run"
	"atoll/store"
)

const (
	_pollBots     = 50
	_pollThreads  = 100
	_pollRequests = 100
)

func TestBotsPoll_Cost(t *testing.T) {
	for _, seed := range []struct {
		name    string
		allOpen bool
	}{{"closed90", false}, {"allopen", true}} {
		t.Run(seed.name, func(t *testing.T) { testBotsPollCost(t, seed.allOpen) })
	}
}

func testBotsPollCost(t *testing.T, allOpen bool) {
	ctx := t.Context()
	var queries atomic.Int64
	var seen queryLog
	st, err := store.OpenWrapped(t.TempDir(), func(c driver.Connector) driver.Connector {
		return countingConnector{c, &queries, &seen}
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	seedPoll(ctx, t, st, allOpen)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := New(st, nil, run.New(run.Config{Store: st, Log: log}), log, nil, "")

	queries.Store(0)
	took := make([]time.Duration, _pollRequests)
	for i := range took {
		req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/api/bots", nil)
		req.Host = "127.0.0.1:4317"
		rec := httptest.NewRecorder()
		start := time.Now()
		h.ServeHTTP(rec, req)
		took[i] = time.Since(start)
		if rec.Code != http.StatusOK {
			t.Fatalf("status %d: %s", rec.Code, rec.Body)
		}
	}
	slices.Sort(took)
	perRequest := queries.Load() / _pollRequests
	p50, p95 := took[_pollRequests/2], took[_pollRequests*95/100]
	t.Logf("GET /api/bots, %d Bots x %d threads: p50 %v, p95 %v, %d queries per request",
		_pollBots, _pollThreads, p50, p95, perRequest)
	if perRequest != 2 {
		t.Errorf("%d queries per request, want 2", perRequest)
	}
	for _, q := range seen.distinct() {
		for _, step := range perThreadRunsReads(ctx, t, st.DB(), q) {
			t.Errorf("the poll reads runs once per thread: %s\nin: %s", step, q)
		}
	}
}

// perThreadRunsReads returns the steps of query's plan that read runs inside the loop over the
// result rows: at the top of the plan, or in a correlated subquery there. A set of runs computed
// once (a MATERIALIZE, a LIST SUBQUERY) is not such a step. A clock cannot tell this apart: the
// cost of the reads is hidden until there are thousands of threads.
func perThreadRunsReads(ctx context.Context, t *testing.T, db *sql.DB, query string) []string {
	t.Helper()
	rows, err := db.QueryContext(ctx, "EXPLAIN QUERY PLAN "+query, make([]any, strings.Count(query, "?"))...)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	type step struct {
		parent int
		detail string
	}
	steps := map[int]step{}
	for rows.Next() {
		var id, parent, unused int
		var detail string
		if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
			t.Fatal(err)
		}
		steps[id] = step{parent, detail}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	var found []string
	for _, s := range steps {
		if !strings.Contains(s.detail, "runs") {
			continue
		}
		parent := steps[s.parent]
		if s.parent == 0 || (parent.parent == 0 && strings.HasPrefix(parent.detail, "CORRELATED SCALAR SUBQUERY")) {
			found = append(found, s.detail)
		}
	}
	slices.Sort(found)
	return found
}

// TestBotsPoll_New proves the poll marks a thread New exactly when a run finished after the user
// last opened it, in the cases a thread row alone could get wrong: a thread with no run, a
// message after the thread was opened, a run failed at start-up while the thread was open, a
// helper thread and a closed thread.
func TestBotsPoll_New(t *testing.T) {
	ctx := t.Context()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := New(st, nil, run.New(run.Config{Store: st, Log: log}), log, nil, "")
	bot, err := st.CreateBot(ctx, store.Bot{Name: "a", Persona: "p", Provider: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	helperBot, err := st.CreateBot(ctx, store.Bot{Name: "b", Persona: "p", Provider: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	ok := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	thread := func(botID, parent string) string {
		t.Helper()
		th, err := st.CreateHelperThread(ctx, parent, botID, "")
		ok(err)
		return th.ID
	}
	start := func(id string) string {
		t.Helper()
		_, ru, err := st.StartRunFrom(ctx, store.InboxItem{ThreadID: id, Text: "hi"})
		ok(err)
		return ru.ID
	}
	finish := func(runID string) {
		t.Helper()
		ok(st.FinishRun(ctx, runID, store.RunResult{Status: store.StatusDone, Reply: "ok"}, nil))
	}
	states := func() map[string]string {
		t.Helper()
		req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/api/bots", nil)
		req.Host = "127.0.0.1:4317"
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		var bots []struct {
			Threads []struct {
				ID, State string
				Helper    bool
			}
		}
		ok(json.Unmarshal(rec.Body.Bytes(), &bots))
		got := map[string]string{}
		for _, b := range bots {
			for _, th := range b.Threads {
				got[th.ID] = th.State
				if th.Helper {
					got[th.ID] += " helper"
				}
			}
		}
		return got
	}

	empty := thread(bot.ID, "")
	unread := thread(bot.ID, "")
	finish(start(unread))
	read := thread(bot.ID, "")
	finish(start(read))
	ok(st.MarkOpened(ctx, read))
	closed := thread(bot.ID, "")
	finish(start(closed))
	ok(st.SetClosed(ctx, closed))
	boss := thread(bot.ID, "")
	helper := thread(helperBot.ID, boss)
	finish(start(helper))

	replied := thread(bot.ID, "")
	finish(start(replied))
	ok(st.MarkOpened(ctx, replied))
	repliedRun := start(replied)
	if got := states()[replied]; got != "running" {
		t.Errorf("replied, run going: state %q, want running", got)
	}
	finish(repliedRun)

	restarted := thread(bot.ID, "")
	start(restarted)
	ok(st.MarkOpened(ctx, restarted))
	if _, err := st.FailUnfinished(ctx, "restart", nil); err != nil {
		t.Fatal(err)
	}

	got := states()
	for name, tt := range map[string]struct{ id, want string }{
		"no run":                       {empty, ""},
		"finished, never opened":       {unread, "new"},
		"finished, then opened":        {read, ""},
		"replied after open, finished": {replied, "new"},
		"run failed at restart":        {restarted, "failed"},
		"helper thread, never replied": {helper, " helper"},
		"boss of a helper":             {boss, ""},
		"closed":                       {closed, "<not listed>"},
	} {
		state, listed := got[tt.id]
		if !listed {
			state = "<not listed>"
		}
		if state != tt.want {
			t.Errorf("%s: state %q, want %q", name, state, tt.want)
		}
	}
}

// TestBotsPoll_ClosedThreadApproval proves the poll still shows an approval that waits on a closed
// thread: closed alone, as a closed helper of an open boss, and as a closed helper of a closed
// boss. A person must never miss an approval.
func TestBotsPoll_ClosedThreadApproval(t *testing.T) {
	ctx := t.Context()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	m := run.New(run.Config{
		Store: st, DataDir: t.TempDir(), Log: log,
		Launch: func(context.Context, string) (provider.Session, provider.Config, error) {
			return &approvalSession{events: make(chan provider.Event, 4)}, provider.Config{}, nil
		},
	})
	t.Cleanup(func() { _ = m.Close() })
	h := New(st, nil, m, log, nil, "")

	bots := map[string]store.Bot{}
	for _, name := range []string{"alone", "boss1", "helper1", "boss2", "helper2", "quiet"} {
		bots[name], err = st.CreateBot(ctx, store.Bot{Name: name, Persona: "p", Provider: "claude"})
		if err != nil {
			t.Fatal(err)
		}
	}
	thread := func(bot, parent string) store.Thread {
		th, err := st.CreateThread(ctx, bots[bot].ID, "")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := st.DB().ExecContext(ctx, "UPDATE threads SET parent_thread_id = ? WHERE id = ?", parent, th.ID); err != nil {
			t.Fatal(err)
		}
		return th
	}
	waitOnApproval := func(th store.Thread) {
		if _, _, err := m.Send(ctx, th.ID, "approve", nil); err != nil {
			t.Fatal(err)
		}
		for range 500 {
			if _, approvals, _ := m.Active(th.ID); len(approvals) > 0 {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		var status, errText string
		_ = st.DB().QueryRow("SELECT status, error FROM runs WHERE thread_id = ?", th.ID).Scan(&status, &errText)
		t.Fatalf("thread %s: no approval opened; run %q %q", th.ID, status, errText)
	}
	closeThread := func(th store.Thread) {
		if err := st.SetClosed(ctx, th.ID); err != nil {
			t.Fatal(err)
		}
	}
	// A newer finished thread of each Bot keeps the waiting thread from being the one the poll
	// keeps as its Bot's newest.
	decoy := func(bot string) {
		id := "decoy-" + bot
		if _, err := st.DB().ExecContext(ctx, "INSERT INTO threads (id, bot_id, created, last_activity, closed) VALUES (?, ?, 1, ?, 1)",
			id, bots[bot].ID, time.Now().Add(time.Hour).UnixMicro()); err != nil {
			t.Fatal(err)
		}
		if _, err := st.DB().ExecContext(ctx, "INSERT INTO runs (id, thread_id, status, started_at, finished_at) VALUES (?, ?, 'done', 1, 2)",
			"run-"+id, id); err != nil {
			t.Fatal(err)
		}
	}
	thread("quiet", "")
	alone := thread("alone", "")
	waitOnApproval(alone)
	closeThread(alone)
	boss1 := thread("boss1", "")
	helper1 := thread("helper1", boss1.ID)
	waitOnApproval(helper1)
	closeThread(helper1)
	boss2 := thread("boss2", "")
	helper2 := thread("helper2", boss2.ID)
	waitOnApproval(helper2)
	closeThread(helper2)
	closeThread(boss2)
	for _, bot := range []string{"alone", "helper1", "helper2"} {
		decoy(bot)
	}

	get := func(path string) []byte {
		req := httptest.NewRequestWithContext(ctx, http.MethodGet, path, nil)
		req.Host = "127.0.0.1:4317"
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s: status %d: %s", path, rec.Code, rec.Body)
		}
		return rec.Body.Bytes()
	}
	var list []struct{ ID, State string }
	if err := json.Unmarshal(get("/api/bots"), &list); err != nil {
		t.Fatal(err)
	}
	listed := map[string]string{}
	for _, b := range list {
		listed[b.ID] = b.State
	}
	for name, want := range map[string]string{
		"alone": "needs_you", "boss1": "needs_you", "helper1": "needs_you",
		"boss2": "needs_you", "helper2": "needs_you", "quiet": "idle",
	} {
		id := bots[name].ID
		var one struct{ State string }
		if err := json.Unmarshal(get("/api/bots/"+id), &one); err != nil {
			t.Fatal(err)
		}
		if listed[id] != want || one.State != want {
			t.Errorf("%s: state %q in the list, %q alone, want %q", name, listed[id], one.State, want)
		}
	}
}

// approvalSession asks for an approval on a prompt that has "approve" in it and never gets an
// answer; any other prompt, a title session's, just ends its turn.
type approvalSession struct {
	events chan provider.Event
}

func (s *approvalSession) Start(context.Context, provider.Config) error { return nil }

func (s *approvalSession) Resume(context.Context, provider.Config, string) error { return nil }

func (s *approvalSession) Events() <-chan provider.Event { return s.events }

func (s *approvalSession) Answer(string, bool, string) error { return nil }

func (s *approvalSession) Interrupt() error {
	close(s.events)
	return nil
}

func (s *approvalSession) Send(_ context.Context, prompt string, _ []provider.Image) error {
	if strings.Contains(prompt, "approve") {
		s.events <- provider.Event{Kind: provider.KindApproval, ID: "p1", Name: "Write", Input: json.RawMessage(`{}`)}
		return nil
	}
	s.events <- provider.Event{Kind: provider.KindTurnEnd}
	return nil
}

// seedPoll gives every Bot _pollThreads threads of two runs each; nine in ten are closed unless
// allOpen. The first thread of every fifth Bot is running.
func seedPoll(ctx context.Context, t *testing.T, st *store.Store, allOpen bool) {
	t.Helper()
	var bots []store.Bot
	for b := range _pollBots {
		bot, err := st.CreateBot(ctx, store.Bot{Name: fmt.Sprintf("Bot %d", b), Persona: "p", Provider: "claude"})
		if err != nil {
			t.Fatal(err)
		}
		bots = append(bots, bot)
	}
	tx, err := st.DB().BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	stmts := map[string]*sql.Stmt{}
	for name, q := range map[string]string{
		"thread":  "INSERT INTO threads (id, bot_id, created, last_activity, closed) VALUES (?, ?, ?, ?, ?)",
		"run":     "INSERT INTO runs (id, thread_id, status, started_at, finished_at) VALUES (?, ?, ?, ?, ?)",
		"message": "INSERT INTO messages (id, thread_id, role, text, run_id) VALUES (?, ?, ?, ?, ?)",
	} {
		if stmts[name], err = tx.PrepareContext(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now().UnixMicro()
	for b, bot := range bots {
		for i := range _pollThreads {
			n := b*_pollThreads + i
			thread := fmt.Sprintf("t%d", n)
			mustExec(t, stmts["thread"], thread, bot.ID, now, now+int64(n), !allOpen && i%10 != 0)
			for r := range 2 {
				id := fmt.Sprintf("r%d-%d", n, r)
				status := []string{store.StatusDone, store.StatusFailed, store.StatusStopped}[n%3]
				if r == 1 && i == 0 && b%5 == 0 {
					status = "running"
				}
				mustExec(t, stmts["run"], id, thread, status, now, now+1)
				mustExec(t, stmts["message"], "mu"+id, thread, "user", "Please look into case "+id, id)
				mustExec(t, stmts["message"], "mb"+id, thread, "bot", "Looked into it.", id)
			}
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}

func mustExec(t *testing.T, stmt *sql.Stmt, args ...any) {
	t.Helper()
	if _, err := stmt.Exec(args...); err != nil {
		t.Fatal(err)
	}
}

type countingConnector struct {
	driver.Connector
	n    *atomic.Int64
	seen *queryLog
}

// queryLog keeps the text of every query it is given.
type queryLog struct {
	mu sync.Mutex
	q  map[string]bool
}

func (l *queryLog) add(query string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.q == nil {
		l.q = map[string]bool{}
	}
	l.q[query] = true
}

func (l *queryLog) distinct() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return slices.Sorted(maps.Keys(l.q))
}

func (c countingConnector) Connect(ctx context.Context) (driver.Conn, error) {
	conn, err := c.Connector.Connect(ctx)
	if err != nil {
		return nil, err
	}
	return countingConn{conn.(sqliteConn), c.n, c.seen}, nil
}

type sqliteConn interface {
	driver.Conn
	driver.ConnBeginTx
	driver.ConnPrepareContext
	driver.ExecerContext
	driver.QueryerContext
	driver.Pinger
	driver.SessionResetter
	driver.Validator
}

type countingConn struct {
	sqliteConn
	n    *atomic.Int64
	seen *queryLog
}

func (c countingConn) ExecContext(
	ctx context.Context, query string, args []driver.NamedValue,
) (driver.Result, error) {
	c.n.Add(1)
	return c.sqliteConn.ExecContext(ctx, query, args)
}

func (c countingConn) QueryContext(
	ctx context.Context, query string, args []driver.NamedValue,
) (driver.Rows, error) {
	c.n.Add(1)
	c.seen.add(query)
	return c.sqliteConn.QueryContext(ctx, query, args)
}
