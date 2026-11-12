// Package memory keeps what Bots learn: items on the user, repo and Bot shelves, found again by
// full-text search. Nothing is deleted; a retired item stays readable.
package memory

import (
	"context"
	"crypto/rand"
	"database/sql"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Item kinds.
const (
	KindDecision   = "decision"
	KindConvention = "convention"
	KindFinding    = "finding"
	KindFact       = "fact"
	KindLesson     = "lesson"
)

// Scopes: user items are seen everywhere, repo items in one repo, bot items by one Bot.
const (
	ScopeUser = "user"
	ScopeRepo = "repo"
	ScopeBot  = "bot"
)

// ReasonIssueClosed is the retired_reason of an item whose ticket's issue closed.
const ReasonIssueClosed = "issue closed"

// Item statuses. Only accepted items are found by Recall and Relevant.
const (
	StatusCandidate = "candidate"
	StatusAccepted  = "accepted"
	StatusRetired   = "retired"
)

const _cols = `m.id, m.kind, m.body, m.why, m.scope, m.scope_id, m.status, m.source_bot_id,
	m.source_thread_id, m.source_run_id, m.replaces, m.replaced_by, m.created_at, m.retired_at,
	m.ticket, m.retired_reason`

var (
	// ErrNotFound is wrapped when the id matches no item.
	ErrNotFound = errors.New("not found")
	// ErrInvalid is wrapped for bad input or an item in the wrong state.
	ErrInvalid = errors.New("invalid")
)

//go:embed migrations/*.sql
var _migrations embed.FS

var (
	_kinds  = map[string]bool{KindDecision: true, KindConvention: true, KindFinding: true, KindFact: true, KindLesson: true}
	_scopes = map[string]bool{ScopeUser: true, ScopeRepo: true, ScopeBot: true}
)

type (
	// Item is one stored memory. ScopeID is empty for the user scope, else a repo or Bot id.
	// Replaces lists the items it retired; ReplacedBy is the item that retired this one. Ticket is
	// the issue a repo item is about, "#N", else empty; RetiredReason is set only when Reef retired
	// the item.
	Item struct {
		ID             string     `json:"id"`
		Kind           string     `json:"kind"`
		Body           string     `json:"body"`
		Why            string     `json:"why"`
		Scope          string     `json:"scope"`
		ScopeID        string     `json:"scope_id"`
		Status         string     `json:"status"`
		SourceBotID    string     `json:"source_bot_id"`
		SourceThreadID string     `json:"source_thread_id"`
		SourceRunID    string     `json:"source_run_id"`
		Replaces       []string   `json:"replaces"`
		ReplacedBy     string     `json:"replaced_by"`
		CreatedAt      time.Time  `json:"created_at"`
		RetiredAt      *time.Time `json:"retired_at"`
		Ticket         string     `json:"ticket"`
		RetiredReason  string     `json:"retired_reason"`
	}

	// Note is what Remember saves. Every id in Replaces must be an accepted item of the same
	// scope.
	Note struct {
		Kind, Body, Why, Scope, ScopeID, Ticket  string
		Replaces                                 []string
		SourceBotID, SourceThreadID, SourceRunID string
	}

	// Similar is a stored item that looks like a note being saved. Score is 0 to 1.
	Similar struct {
		ID    string  `json:"id"`
		Body  string  `json:"body"`
		Score float64 `json:"score"`
	}

	// Shelf is what one run may see: the user shelf always, the repo shelf when RepoID is set,
	// the Bot shelf when BotID is set.
	Shelf struct{ RepoID, BotID string }

	// Counts is how many accepted items each scope of a shelf holds.
	Counts struct{ Repo, User, Bot int }

	// Store reads and writes memory items in a database it was given.
	Store struct {
		db    *sql.DB
		embed Embedder
	}

	querier interface {
		ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
		QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
		QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	}
)

// Open brings the memory tables in db up to date. A nil e stores no vectors.
func Open(db *sql.DB, e Embedder) (*Store, error) {
	if err := migrate(context.Background(), db); err != nil {
		return nil, fmt.Errorf("migrate memory tables: %w", err)
	}
	if e == nil {
		e = noneEmbedder{}
	}
	return &Store{db: db, embed: e}, nil
}

// Has reports whether the shelf holds items of the scope of it.
func (sh Shelf) Has(it Item) bool {
	switch it.Scope {
	case ScopeUser:
		return true
	case ScopeRepo:
		return sh.RepoID != "" && it.ScopeID == sh.RepoID
	case ScopeBot:
		return sh.BotID != "" && it.ScopeID == sh.BotID
	}
	return false
}

// Remember saves an accepted item and retires the items it replaces, all or nothing.
// Lookalikes are reported but never block the save.
func (s *Store) Remember(ctx context.Context, n Note) (Item, []Similar, error) {
	if err := validate(n); err != nil {
		return Item{}, nil, err
	}
	if n.Scope == ScopeUser {
		n.ScopeID = ""
	}
	// A failing embedder must not fail a save.
	vec, _ := s.embed.Embed(ctx, embedText(n.Body, n.Why))
	var blob []byte
	if vec != nil {
		blob = encodeVector(vec)
	}
	similar, err := s.lookalikes(ctx, n, vec)
	if err != nil {
		return Item{}, nil, err
	}
	replaces := n.Replaces
	if replaces == nil {
		replaces = []string{}
	}
	list, err := json.Marshal(replaces)
	if err != nil {
		return Item{}, nil, fmt.Errorf("encode replaces: %w", err)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Item{}, nil, fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	id := newID()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO memory (id, kind, body, why, scope, scope_id, status, source_bot_id,
		 source_thread_id, source_run_id, replaces, created_at, vector, ticket)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, n.Kind, n.Body, n.Why, n.Scope, n.ScopeID, StatusAccepted, n.SourceBotID,
		n.SourceThreadID, n.SourceRunID, string(list), now, blob, n.Ticket); err != nil {
		return Item{}, nil, fmt.Errorf("insert item: %w", err)
	}
	for _, old := range n.Replaces {
		if err := replace(ctx, tx, old, id, n, now); err != nil {
			return Item{}, nil, err
		}
	}
	item, err := read(ctx, tx, id)
	if err != nil {
		return Item{}, nil, err
	}
	if err := tx.Commit(); err != nil {
		return Item{}, nil, fmt.Errorf("commit: %w", err)
	}
	return item, similar, nil
}

// Read returns the item with id, retired or not.
func (s *Store) Read(ctx context.Context, id string) (Item, error) {
	return read(ctx, s.db, id)
}

// Retire marks an item retired and keeps it. Retiring a retired item returns it unchanged.
func (s *Store) Retire(ctx context.Context, id string) (Item, error) {
	_, err := s.db.ExecContext(ctx,
		`UPDATE memory SET status = ?, retired_at = ? WHERE id = ? AND status != ?`,
		StatusRetired, time.Now().UTC().Format(time.RFC3339Nano), id, StatusRetired)
	if err != nil {
		return Item{}, fmt.Errorf("retire %s: %w", id, err)
	}
	return read(ctx, s.db, id)
}

// Ticket is an issue of a repo that has an accepted item: Number is "#N".
type Ticket struct{ RepoID, Number string }

// Tickets lists the issues that have an accepted item, once each.
func (s *Store) Tickets(ctx context.Context) ([]Ticket, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT DISTINCT scope_id, ticket FROM memory WHERE status = ? AND ticket != '' ORDER BY scope_id, ticket`,
		StatusAccepted)
	if err != nil {
		return nil, fmt.Errorf("query tickets: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []Ticket
	for rows.Next() {
		var t Ticket
		if err := rows.Scan(&t.RepoID, &t.Number); err != nil {
			return nil, fmt.Errorf("scan ticket: %w", err)
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// RetireTicket retires the accepted items about the ticket, with the reason ReasonIssueClosed.
func (s *Store) RetireTicket(ctx context.Context, t Ticket) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE memory SET status = ?, retired_at = ?, retired_reason = ?
		 WHERE status = ? AND scope = ? AND scope_id = ? AND ticket = ?`,
		StatusRetired, time.Now().UTC().Format(time.RFC3339Nano), ReasonIssueClosed,
		StatusAccepted, ScopeRepo, t.RepoID, t.Number)
	if err != nil {
		return fmt.Errorf("retire ticket %s: %w", t.Number, err)
	}
	return nil
}

// TicketItems returns the ids of the accepted items of the repo about the ticket, oldest first.
func (s *Store) TicketItems(ctx context.Context, t Ticket) ([]string, error) {
	items, err := queryItems(ctx, s.db,
		`WHERE m.status = ? AND m.scope = ? AND m.scope_id = ? AND m.ticket = ? ORDER BY m.seq`,
		StatusAccepted, ScopeRepo, t.RepoID, t.Number)
	if err != nil {
		return nil, err
	}
	ids := make([]string, len(items))
	for i, it := range items {
		ids[i] = it.ID
	}
	return ids, nil
}

// List returns every item, retired ones included, oldest first.
func (s *Store) List(ctx context.Context) ([]Item, error) {
	return queryItems(ctx, s.db, `ORDER BY m.seq`)
}

// Count returns how many accepted items each scope of the shelf holds.
func (s *Store) Count(ctx context.Context, sh Shelf) (Counts, error) {
	where, args := shelfWhere(sh)
	rows, err := s.db.QueryContext(ctx,
		`SELECT m.scope, COUNT(*) FROM memory m WHERE `+where+` GROUP BY m.scope`, args...)
	if err != nil {
		return Counts{}, fmt.Errorf("count memory: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var c Counts
	for rows.Next() {
		var (
			scope string
			n     int
		)
		if err := rows.Scan(&scope, &n); err != nil {
			return Counts{}, fmt.Errorf("scan count: %w", err)
		}
		switch scope {
		case ScopeRepo:
			c.Repo = n
		case ScopeUser:
			c.User = n
		case ScopeBot:
			c.Bot = n
		}
	}
	if err := rows.Err(); err != nil {
		return Counts{}, fmt.Errorf("read counts: %w", err)
	}
	return c, nil
}

// Seen returns the ids of the items already shown in session.
func (s *Store) Seen(ctx context.Context, session string) (map[string]bool, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT item_id FROM memory_seen WHERE session = ?`, session)
	if err != nil {
		return nil, fmt.Errorf("query seen: %w", err)
	}
	defer func() { _ = rows.Close() }()
	seen := map[string]bool{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan seen: %w", err)
		}
		seen[id] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read seen: %w", err)
	}
	return seen, nil
}

// MarkSeen records that session has been shown the items with ids. Marking twice does nothing.
func (s *Store) MarkSeen(ctx context.Context, session string, ids []string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	for _, id := range ids {
		if _, err := tx.ExecContext(ctx,
			`INSERT OR IGNORE INTO memory_seen (session, item_id) VALUES (?, ?)`, session, id); err != nil {
			return fmt.Errorf("mark %s seen: %w", id, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

func validate(n Note) error {
	if !_kinds[n.Kind] {
		return fmt.Errorf("%w: unknown kind %q", ErrInvalid, n.Kind)
	}
	if n.Body == "" {
		return fmt.Errorf("%w: body is required", ErrInvalid)
	}
	if !_scopes[n.Scope] {
		return fmt.Errorf("%w: unknown scope %q", ErrInvalid, n.Scope)
	}
	if n.Scope != ScopeUser && n.ScopeID == "" {
		return fmt.Errorf("%w: scope_id is required for scope %s", ErrInvalid, n.Scope)
	}
	if n.Ticket != "" && (n.Scope != ScopeRepo || !validTicket(n.Ticket)) {
		return fmt.Errorf("%w: ticket %q must be # and the issue number, on a repo item", ErrInvalid, n.Ticket)
	}
	return nil
}

// validTicket reports whether s is # and digits without a leading zero.
func validTicket(s string) bool {
	digits, ok := strings.CutPrefix(s, "#")
	if !ok || digits == "" || digits[0] == '0' {
		return false
	}
	for _, r := range digits {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// replace retires old, which must be an accepted item of the scope of n.
func replace(ctx context.Context, q querier, old, by string, n Note, now string) error {
	var scope, scopeID, status string
	err := q.QueryRowContext(ctx, `SELECT scope, scope_id, status FROM memory WHERE id = ?`, old).
		Scan(&scope, &scopeID, &status)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("replace %s: %w", old, ErrNotFound)
	}
	if err != nil {
		return fmt.Errorf("replace %s: %w", old, err)
	}
	if scope != n.Scope || scopeID != n.ScopeID || status != StatusAccepted {
		return fmt.Errorf("%w: memory %s is not an accepted item of the same scope", ErrInvalid, old)
	}
	if _, err := q.ExecContext(ctx,
		`UPDATE memory SET status = ?, retired_at = ?, replaced_by = ? WHERE id = ?`,
		StatusRetired, now, by, old); err != nil {
		return fmt.Errorf("retire %s: %w", old, err)
	}
	return nil
}

func read(ctx context.Context, q querier, id string) (Item, error) {
	items, err := queryItems(ctx, q, `WHERE m.id = ?`, id)
	if err != nil {
		return Item{}, err
	}
	if len(items) == 0 {
		return Item{}, fmt.Errorf("memory %s: %w", id, ErrNotFound)
	}
	return items[0], nil
}

// queryItems selects the items of the memory table aliased m; rest is what follows FROM memory m.
func queryItems(ctx context.Context, q querier, rest string, args ...any) ([]Item, error) {
	rows, err := q.QueryContext(ctx, `SELECT `+_cols+` FROM memory m `+rest, args...)
	if err != nil {
		return nil, fmt.Errorf("query items: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var items []Item
	for rows.Next() {
		it, err := scanItem(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, it)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read items: %w", err)
	}
	return items, nil
}

func scanItem(rows *sql.Rows) (Item, error) {
	var (
		it                  Item
		replaces, createdAt string
		retiredAt           sql.NullString
	)
	if err := rows.Scan(&it.ID, &it.Kind, &it.Body, &it.Why, &it.Scope, &it.ScopeID, &it.Status,
		&it.SourceBotID, &it.SourceThreadID, &it.SourceRunID, &replaces, &it.ReplacedBy,
		&createdAt, &retiredAt, &it.Ticket, &it.RetiredReason); err != nil {
		return Item{}, fmt.Errorf("scan item: %w", err)
	}
	if err := json.Unmarshal([]byte(replaces), &it.Replaces); err != nil {
		return Item{}, fmt.Errorf("parse replaces: %w", err)
	}
	var err error
	if it.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAt); err != nil {
		return Item{}, fmt.Errorf("parse created_at: %w", err)
	}
	if retiredAt.Valid {
		t, err := time.Parse(time.RFC3339Nano, retiredAt.String)
		if err != nil {
			return Item{}, fmt.Errorf("parse retired_at: %w", err)
		}
		it.RetiredAt = &t
	}
	return it, nil
}

func newID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

type migration struct {
	version int
	name    string
	body    string
}

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
	steps := make([]migration, 0, len(names))
	for _, name := range names {
		n, err := strconv.Atoi(strings.SplitN(name, "_", 2)[0])
		if err != nil {
			return fmt.Errorf("migration %s: bad number: %w", name, err)
		}
		body, err := _migrations.ReadFile("migrations/" + name)
		if err != nil {
			return fmt.Errorf("read %s: %w", name, err)
		}
		steps = append(steps, migration{version: n, name: name, body: string(body)})
	}
	known := steps[len(steps)-1].version
	current, err := version(ctx, db)
	if err != nil {
		return err
	}
	if current > known {
		return fmt.Errorf("memory tables are newer than this atoll (version %d, this atoll knows %d)",
			current, known)
	}
	for _, m := range steps {
		if current >= m.version {
			continue
		}
		if err := apply(ctx, db, m); err != nil {
			return fmt.Errorf("apply %s: %w", m.name, err)
		}
	}
	return nil
}

// version reads the memory schema version; no tables is version 0.
func version(ctx context.Context, q querier) (int, error) {
	var has bool
	if err := q.QueryRowContext(ctx,
		`SELECT EXISTS (SELECT 1 FROM sqlite_master WHERE name = 'memory_version')`).
		Scan(&has); err != nil {
		return 0, fmt.Errorf("read version: %w", err)
	}
	if !has {
		return 0, nil
	}
	var v int
	if err := q.QueryRowContext(ctx, `SELECT version FROM memory_version`).Scan(&v); err != nil {
		return 0, fmt.Errorf("read version: %w", err)
	}
	return v, nil
}

// apply runs one migration unless another process already did.
func apply(ctx context.Context, db *sql.DB, m migration) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	current, err := version(ctx, tx)
	if err != nil {
		return err
	}
	if current >= m.version {
		return nil
	}
	if _, err := tx.ExecContext(ctx, m.body); err != nil {
		return fmt.Errorf("run: %w", err)
	}
	for _, q := range []string{
		`CREATE TABLE IF NOT EXISTS memory_version (version INTEGER NOT NULL)`,
		`DELETE FROM memory_version`,
	} {
		if _, err := tx.ExecContext(ctx, q); err != nil {
			return fmt.Errorf("set version: %w", err)
		}
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO memory_version (version) VALUES (?)`, m.version); err != nil {
		return fmt.Errorf("set version: %w", err)
	}
	return tx.Commit()
}
