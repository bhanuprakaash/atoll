// Package store keeps Atoll's data in one SQLite file: Bots, threads, messages and runs.
package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite" // registers the "sqlite" driver
)

// ErrNotFound is wrapped when the id asked for matches no row.
var ErrNotFound = errors.New("not found")

//go:embed migrations/*.sql
var _migrations embed.FS

// _dsnParams: WAL lets readers run beside a writer; immediate transactions take the write lock
// up front, so busy_timeout can wait instead of failing a lock upgrade.
const _dsnParams = "_pragma=journal_mode(WAL)&_pragma=foreign_keys(ON)" +
	"&_pragma=busy_timeout(5000)&_txlock=immediate"

// Run statuses.
const (
	StatusRunning = "running"
	StatusWaiting = "waiting" // an approval is open
	StatusDone    = "done"
	StatusFailed  = "failed"
)

// Message roles.
const (
	RoleUser = "user"
	RoleBot  = "bot"
)

type (
	// Store is Atoll's data, read and written through one SQLite file.
	Store struct{ db *sql.DB }

	// Bot is a configured agent. Provider is "claude" or "codex".
	Bot struct {
		ID       string `json:"id"`
		Name     string `json:"name"`
		Persona  string `json:"persona"`
		Provider string `json:"provider"`
	}

	// Thread is a chat with one Bot. Session is the provider session its runs resume; empty
	// until the first run starts one.
	Thread struct {
		ID      string    `json:"id"`
		BotID   string    `json:"bot_id"`
		Session string    `json:"-"`
		Created time.Time `json:"created_at"`
	}

	// Message is one chat line. A bot message holds a run's reply.
	Message struct {
		ID       string `json:"id"`
		ThreadID string `json:"-"`
		Role     string `json:"role"`
		Text     string `json:"text"`
		RunID    string `json:"run_id"`
	}

	// Run is one message handled.
	Run struct {
		ID       string  `json:"id"`
		ThreadID string  `json:"thread_id"`
		Status   string  `json:"status"`
		Reply    string  `json:"reply"`
		Error    string  `json:"error"`
		Report   *Report `json:"report"`
	}

	// Report is what the provider passed to finish_run, exactly.
	Report struct {
		Done    string `json:"done"`
		Tried   string `json:"tried"`
		Blocked string `json:"blocked"`
		Next    string `json:"next"`
	}
)

// Open opens dir/atoll.db, creating dir and the file if needed, and applies any migration the
// file lacks.
func Open(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("resolve data dir: %w", err)
	}
	uri := url.URL{Scheme: "file", Path: filepath.Join(abs, "atoll.db"), RawQuery: _dsnParams}
	db, err := sql.Open("sqlite", uri.String())
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	if err := migrate(context.Background(), db); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return &Store{db: db}, nil
}

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

// CreateBot stores b with a new id and returns it.
func (s *Store) CreateBot(ctx context.Context, b Bot) (Bot, error) {
	b.ID = newID()
	_, err := s.db.ExecContext(ctx,
		"INSERT INTO bots (id, name, persona, provider) VALUES (?, ?, ?, ?)",
		b.ID, b.Name, b.Persona, b.Provider)
	if err != nil {
		return Bot{}, fmt.Errorf("insert bot: %w", err)
	}
	return b, nil
}

// Bot returns the Bot with id.
func (s *Store) Bot(ctx context.Context, id string) (Bot, error) {
	var b Bot
	err := s.db.QueryRowContext(ctx,
		"SELECT id, name, persona, provider FROM bots WHERE id = ?", id).
		Scan(&b.ID, &b.Name, &b.Persona, &b.Provider)
	if errors.Is(err, sql.ErrNoRows) {
		return Bot{}, fmt.Errorf("bot %s: %w", id, ErrNotFound)
	}
	if err != nil {
		return Bot{}, fmt.Errorf("read bot %s: %w", id, err)
	}
	return b, nil
}

// Bots lists every Bot, oldest first.
func (s *Store) Bots(ctx context.Context) ([]Bot, error) {
	return query(ctx, s.db, "SELECT id, name, persona, provider FROM bots ORDER BY rowid",
		func(r *sql.Rows) (b Bot, err error) {
			return b, r.Scan(&b.ID, &b.Name, &b.Persona, &b.Provider)
		})
}

// CreateThread starts a thread with the Bot botID.
func (s *Store) CreateThread(ctx context.Context, botID string) (Thread, error) {
	t := Thread{ID: newID(), BotID: botID, Created: time.Now().UTC().Truncate(time.Microsecond)}
	res, err := s.db.ExecContext(ctx,
		"INSERT INTO threads (id, bot_id, created) SELECT ?, id, ? FROM bots WHERE id = ?",
		t.ID, t.Created.UnixMicro(), botID)
	if err != nil {
		return Thread{}, fmt.Errorf("insert thread: %w", err)
	}
	if err := found(res, "bot", botID); err != nil {
		return Thread{}, err
	}
	return t, nil
}

// Thread returns the thread with id.
func (s *Store) Thread(ctx context.Context, id string) (Thread, error) {
	rows, err := query(ctx, s.db,
		"SELECT id, bot_id, session, created FROM threads WHERE id = ?", scanThread, id)
	if err != nil {
		return Thread{}, fmt.Errorf("read thread %s: %w", id, err)
	}
	if len(rows) == 0 {
		return Thread{}, fmt.Errorf("thread %s: %w", id, ErrNotFound)
	}
	return rows[0], nil
}

// Threads lists the threads of the Bot botID, newest first.
func (s *Store) Threads(ctx context.Context, botID string) ([]Thread, error) {
	return query(ctx, s.db,
		"SELECT id, bot_id, session, created FROM threads WHERE bot_id = ? ORDER BY rowid DESC",
		scanThread, botID)
}

// SetSession records the provider session the thread's next runs resume.
func (s *Store) SetSession(ctx context.Context, threadID, session string) error {
	res, err := s.db.ExecContext(ctx, "UPDATE threads SET session = ? WHERE id = ?",
		session, threadID)
	if err != nil {
		return fmt.Errorf("set session: %w", err)
	}
	return found(res, "thread", threadID)
}

// StartRun stores the user's message and a running run for it, in one transaction.
func (s *Store) StartRun(ctx context.Context, threadID, text string) (Message, Run, error) {
	r := Run{ID: newID(), ThreadID: threadID, Status: StatusRunning}
	m := Message{ID: newID(), ThreadID: threadID, Role: RoleUser, Text: text, RunID: r.ID}
	err := s.tx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx,
			"INSERT INTO runs (id, thread_id, status) SELECT ?, id, ? FROM threads WHERE id = ?",
			r.ID, r.Status, threadID)
		if err != nil {
			return fmt.Errorf("insert run: %w", err)
		}
		if err := found(res, "thread", threadID); err != nil {
			return err
		}
		return insertMessage(ctx, tx, m)
	})
	if err != nil {
		return Message{}, Run{}, err
	}
	return m, r, nil
}

// Run returns the run with id.
func (s *Store) Run(ctx context.Context, id string) (Run, error) {
	var r Run
	var report sql.NullString
	err := s.db.QueryRowContext(ctx,
		"SELECT id, thread_id, status, reply, error, report FROM runs WHERE id = ?", id).
		Scan(&r.ID, &r.ThreadID, &r.Status, &r.Reply, &r.Error, &report)
	if errors.Is(err, sql.ErrNoRows) {
		return Run{}, fmt.Errorf("run %s: %w", id, ErrNotFound)
	}
	if err != nil {
		return Run{}, fmt.Errorf("read run %s: %w", id, err)
	}
	if report.Valid {
		if err := json.Unmarshal([]byte(report.String), &r.Report); err != nil {
			return Run{}, fmt.Errorf("decode report of run %s: %w", id, err)
		}
	}
	return r, nil
}

// SetStatus sets the status of an unfinished run: running or waiting.
func (s *Store) SetStatus(ctx context.Context, runID, status string) error {
	res, err := s.db.ExecContext(ctx, "UPDATE runs SET status = ? WHERE id = ?", status, runID)
	if err != nil {
		return fmt.Errorf("set status: %w", err)
	}
	return found(res, "run", runID)
}

// SetReport stores the run's finish_run report. A second call replaces the first.
func (s *Store) SetReport(ctx context.Context, runID string, r Report) error {
	body, err := json.Marshal(r)
	if err != nil {
		return fmt.Errorf("encode report: %w", err)
	}
	res, err := s.db.ExecContext(ctx, "UPDATE runs SET report = ? WHERE id = ?", string(body), runID)
	if err != nil {
		return fmt.Errorf("set report: %w", err)
	}
	return found(res, "run", runID)
}

// FinishRun ends a run as done with reply, or as failed with errText. A non-empty reply also
// becomes the thread's bot message, in the same transaction.
func (s *Store) FinishRun(ctx context.Context, runID, status, reply, errText string) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		var threadID string
		err := tx.QueryRowContext(ctx, "SELECT thread_id FROM runs WHERE id = ?", runID).
			Scan(&threadID)
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("run %s: %w", runID, ErrNotFound)
		}
		if err != nil {
			return fmt.Errorf("read run %s: %w", runID, err)
		}
		_, err = tx.ExecContext(ctx,
			"UPDATE runs SET status = ?, reply = ?, error = ? WHERE id = ?",
			status, reply, errText, runID)
		if err != nil {
			return fmt.Errorf("finish run: %w", err)
		}
		if reply == "" {
			return nil
		}
		return insertMessage(ctx, tx,
			Message{ID: newID(), ThreadID: threadID, Role: RoleBot, Text: reply, RunID: runID})
	})
}

// FailUnfinished marks every running or waiting run failed with reason and returns their ids.
// Called once at start: no run outlives the process that ran it.
func (s *Store) FailUnfinished(ctx context.Context, reason string) ([]string, error) {
	var ids []string
	err := s.tx(ctx, func(tx *sql.Tx) error {
		var err error
		ids, err = query(ctx, tx,
			"UPDATE runs SET status = ?, error = ? WHERE status IN (?, ?) RETURNING id",
			func(r *sql.Rows) (id string, err error) { return id, r.Scan(&id) },
			StatusFailed, reason, StatusRunning, StatusWaiting)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("fail unfinished runs: %w", err)
	}
	return ids, nil
}

// Messages lists the thread's messages, oldest first.
func (s *Store) Messages(ctx context.Context, threadID string) ([]Message, error) {
	return query(ctx, s.db,
		"SELECT id, thread_id, role, text, run_id FROM messages WHERE thread_id = ? ORDER BY rowid",
		func(r *sql.Rows) (m Message, err error) {
			return m, r.Scan(&m.ID, &m.ThreadID, &m.Role, &m.Text, &m.RunID)
		}, threadID)
}

func (s *Store) tx(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := fn(tx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

func insertMessage(ctx context.Context, tx *sql.Tx, m Message) error {
	_, err := tx.ExecContext(ctx,
		"INSERT INTO messages (id, thread_id, role, text, run_id) VALUES (?, ?, ?, ?, ?)",
		m.ID, m.ThreadID, m.Role, m.Text, m.RunID)
	if err != nil {
		return fmt.Errorf("insert message: %w", err)
	}
	return nil
}

// found turns an INSERT ... SELECT or UPDATE that touched no row into ErrNotFound for the kind
// and id the caller named.
func found(res sql.Result, kind, id string) error {
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("count rows: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("%s %s: %w", kind, id, ErrNotFound)
	}
	return nil
}

func scanThread(r *sql.Rows) (Thread, error) {
	var t Thread
	var created int64
	if err := r.Scan(&t.ID, &t.BotID, &t.Session, &created); err != nil {
		return Thread{}, err
	}
	t.Created = time.UnixMicro(created).UTC()
	return t, nil
}

type queryer interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

func query[T any](
	ctx context.Context, q queryer, stmt string, scan func(*sql.Rows) (T, error), args ...any,
) ([]T, error) {
	rows, err := q.QueryContext(ctx, stmt, args...)
	if err != nil {
		return nil, fmt.Errorf("query: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []T
	for rows.Next() {
		v, err := scan(rows)
		if err != nil {
			return nil, fmt.Errorf("scan: %w", err)
		}
		out = append(out, v)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read rows: %w", err)
	}
	return out, nil
}

func newID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// migrate applies each embedded NNN_name.sql above the file's user_version, in order.
func migrate(ctx context.Context, db *sql.DB) error {
	files, err := _migrations.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("list migrations: %w", err)
	}
	names := make([]string, 0, len(files))
	for _, f := range files {
		names = append(names, f.Name())
	}
	sort.Strings(names)
	for _, name := range names {
		n, err := strconv.Atoi(strings.SplitN(name, "_", 2)[0])
		if err != nil {
			return fmt.Errorf("migration %s: bad number: %w", name, err)
		}
		body, err := _migrations.ReadFile("migrations/" + name)
		if err != nil {
			return fmt.Errorf("read %s: %w", name, err)
		}
		if err := apply(ctx, db, n, string(body)); err != nil {
			return fmt.Errorf("apply %s: %w", name, err)
		}
	}
	return nil
}

// apply runs one migration unless the file's user_version already covers it. The version is
// read inside the transaction, so two processes opening a fresh file apply it once.
func apply(ctx context.Context, db *sql.DB, version int, body string) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var current int
	if err := tx.QueryRowContext(ctx, "PRAGMA user_version").Scan(&current); err != nil {
		return fmt.Errorf("read user_version: %w", err)
	}
	if current >= version {
		return nil
	}
	if _, err := tx.ExecContext(ctx, body); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "PRAGMA user_version = "+strconv.Itoa(version)); err != nil {
		return err
	}
	return tx.Commit()
}
