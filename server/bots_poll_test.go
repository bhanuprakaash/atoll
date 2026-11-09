package server

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"atoll/run"
	"atoll/store"
)

const (
	_pollBots     = 50
	_pollThreads  = 100
	_pollRequests = 100
)

func TestBotsPoll_Cost(t *testing.T) {
	ctx := t.Context()
	var queries atomic.Int64
	st, err := store.OpenWrapped(t.TempDir(), func(c driver.Connector) driver.Connector {
		return countingConnector{c, &queries}
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	seedPoll(ctx, t, st)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := New(st, nil, run.New(run.Config{Store: st, Log: log}), log, nil)

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
	if p95 >= 50*time.Millisecond {
		t.Errorf("p95 %v, want under 50ms", p95)
	}
	if perRequest != 2 {
		t.Errorf("%d queries per request, want 2", perRequest)
	}
}

// seedPoll gives every Bot _pollThreads threads of two runs each; nine in ten are closed, as most
// threads are in real use. The first thread of every fifth Bot is running.
func seedPoll(ctx context.Context, t *testing.T, st *store.Store) {
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
			mustExec(t, stmts["thread"], thread, bot.ID, now, now+int64(n), i%10 != 0)
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
	n *atomic.Int64
}

func (c countingConnector) Connect(ctx context.Context) (driver.Conn, error) {
	conn, err := c.Connector.Connect(ctx)
	if err != nil {
		return nil, err
	}
	return countingConn{conn.(sqliteConn), c.n}, nil
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
	n *atomic.Int64
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
	return c.sqliteConn.QueryContext(ctx, query, args)
}
