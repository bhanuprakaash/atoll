// Package store keeps Atoll's data in one SQLite file: Bots, threads, messages and runs.
package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"database/sql/driver"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"modernc.org/sqlite"
)

var (
	// ErrNotFound is wrapped when the id asked for matches no row.
	ErrNotFound = errors.New("not found")
	// ErrExists is wrapped when a name is already taken.
	ErrExists = errors.New("already exists")
	// ErrUndone is wrapped when a change has already been undone.
	ErrUndone = errors.New("already undone")
	// ErrInUse is wrapped when an entity cannot be removed because a thread uses it.
	ErrInUse = errors.New("in use")
)

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

// ErrorSignedOut is the error kind of a run that failed because its provider is signed out.
const ErrorSignedOut = "signed_out"

// KindGitLab is the kind of a GitLab connection.
const KindGitLab = "gitlab"

// Connection statuses.
const (
	ConnectionPending     = "pending"
	ConnectionConnected   = "connected"
	ConnectionNeedsSignIn = "needs_sign_in"
)

// Message roles.
const (
	RoleUser = "user"
	RoleBot  = "bot"
)

type (
	// Store is Atoll's data, read and written through one SQLite file.
	Store struct{ db *sql.DB }

	// Bot is a configured agent. Provider is "claude" or "codex". Connections are the ids of the
	// connections it may use.
	Bot struct {
		ID                string     `json:"id"`
		Name              string     `json:"name"`
		Persona           string     `json:"persona"`
		Provider          string     `json:"provider"`
		Look              string     `json:"look,omitempty"`
		Model             string     `json:"model,omitempty"`
		Pinned            bool       `json:"pinned,omitempty"`
		State             string     `json:"state,omitempty"`
		Activity          string     `json:"activity,omitempty"`
		LastActivity      *time.Time `json:"last_activity,omitempty"`
		RepoAccess        RepoAccess `json:"repo_access"`
		Connections       []string   `json:"connections"`
		Skills            []string   `json:"skills"`
		CanManagePlatform bool       `json:"can_manage_platform"`
	}

	// RepoAccess is the repos a Bot may work in. Nil allows every repo and is "all" as JSON;
	// an empty list allows none.
	RepoAccess []string

	// Repo is a registered local git repo.
	Repo struct {
		ID   string `json:"id"`
		Name string `json:"name"`
		Path string `json:"path"`
	}

	// Connection is a sign-in to an external service.
	Connection struct {
		ID          string `json:"id"`
		Kind        string `json:"kind"`
		InstanceURL string `json:"instance_url"`
		Status      string `json:"status"`
	}

	// Thread is a chat with one Bot. Session is the provider session its runs resume; empty
	// until the first run starts one. The repo, branch, worktree and MR fields are empty until
	// set.
	Thread struct {
		ID              string     `json:"id"`
		BotID           string     `json:"bot_id"`
		Title           string     `json:"title"`
		Session         string     `json:"-"`
		RepoID          string     `json:"repo_id"`
		Branch          string     `json:"branch"`
		Worktree        string     `json:"worktree"`
		MRURL           string     `json:"mr_url"`
		MRState         string     `json:"mr_state"`
		MRTitle         string     `json:"mr_title"`
		WorktreeRemoved bool       `json:"worktree_removed"`
		SnapshotRef     string     `json:"snapshot_ref"`
		ClosedAt        *time.Time `json:"closed_at"`
		Closed          bool       `json:"closed"`
		Created         time.Time  `json:"created_at"`
		LastActivity    time.Time  `json:"last_activity"`
	}

	// Message is one chat line. A bot message holds a run's reply.
	Message struct {
		ID        string    `json:"id"`
		ThreadID  string    `json:"-"`
		Role      string    `json:"role"`
		Text      string    `json:"text"`
		RunID     string    `json:"run_id"`
		CreatedAt time.Time `json:"created_at"`
	}

	// Run is one message handled.
	Run struct {
		ID         string         `json:"id"`
		ThreadID   string         `json:"thread_id"`
		Status     string         `json:"status"`
		Reply      string         `json:"reply"`
		Error      string         `json:"error"`
		ErrorKind  string         `json:"error_kind"`
		Report     *Report        `json:"report"`
		StartedAt  time.Time      `json:"started_at"`
		FinishedAt time.Time      `json:"finished_at"`
		Tools      map[string]int `json:"tools"`
	}

	// Report is what the provider passed to finish_run, exactly.
	Report struct {
		Done    string `json:"done"`
		Tried   string `json:"tried"`
		Blocked string `json:"blocked"`
		Next    string `json:"next"`
	}
)

const (
	_botColumns = "id, name, persona, provider, repo_access, connections, skills, " +
		"can_manage_platform, look, model"
	_threadColumns   = "id, bot_id, session, repo_id, branch, worktree, mr_url, mr_state, closed, created, last_activity, mr_title, worktree_removed, closed_at, snapshot_ref, title"
	_messagePageSize = 50
)

// Open opens dir/atoll.db, creating dir and the file if needed, and applies any migration the
// file lacks.
func Open(dir string) (*Store, error) { return OpenWrapped(dir, nil) }

// OpenWrapped is Open with the database's connector passed through wrap when it is not nil.
// Tests use it to count statements.
func OpenWrapped(dir string, wrap func(driver.Connector) driver.Connector) (*Store, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("resolve data dir: %w", err)
	}
	path := filepath.Join(abs, "atoll.db")
	// SQLite creates a missing file as 0644 and gives its -wal and -shm files the same mode.
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("create db file: %w", err)
	}
	_ = f.Close()
	uri := url.URL{Scheme: "file", Path: path, RawQuery: _dsnParams}
	conn, err := sqlite.NewConnector(uri.String())
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	if wrap != nil {
		conn = wrap(conn)
	}
	db := sql.OpenDB(conn)
	if err := migrate(context.Background(), db); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return &Store{db: db}, nil
}

// DB is the shared handle, for packages that keep their own tables in the same file.
func (s *Store) DB() *sql.DB { return s.db }

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

// Allows reports whether a Bot with this access may work in the repo with id.
func (a RepoAccess) Allows(id string) bool {
	return a == nil || slices.Contains(a, id)
}

// MarshalJSON writes nil as "all" and a list as an array.
func (a RepoAccess) MarshalJSON() ([]byte, error) {
	if a == nil {
		return []byte(`"all"`), nil
	}
	return json.Marshal([]string(a))
}

// UnmarshalJSON reads "all" or null as nil and an array, even an empty one, as a list.
func (a *RepoAccess) UnmarshalJSON(b []byte) error {
	if string(b) == `"all"` {
		*a = nil
		return nil
	}
	var ids []string
	if err := json.Unmarshal(b, &ids); err != nil {
		return fmt.Errorf("repo_access must be \"all\" or a list of repo ids: %w", err)
	}
	*a = ids
	return nil
}

// CreateBot stores b with a new id and returns it.
func (s *Store) CreateBot(ctx context.Context, b Bot) (Bot, error) {
	b.ID = newID()
	b = normalizeBot(b)
	args, err := botArgs(b)
	if err != nil {
		return Bot{}, err
	}
	_, err = s.db.ExecContext(ctx,
		"INSERT INTO bots ("+_botColumns+") VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)", args...)
	if err != nil {
		return Bot{}, fmt.Errorf("insert bot: %w", err)
	}
	return b, nil
}

// Bot returns the Bot with id.
func (s *Store) Bot(ctx context.Context, id string) (Bot, error) {
	rows, err := query(ctx, s.db,
		"SELECT "+_botColumns+", pinned_at IS NOT NULL FROM bots WHERE id = ? AND removed = 0",
		scanBot, id)
	if err != nil {
		return Bot{}, fmt.Errorf("read bot %s: %w", id, err)
	}
	if len(rows) == 0 {
		return Bot{}, fmt.Errorf("bot %s: %w", id, ErrNotFound)
	}
	return rows[0], nil
}

// Bots lists every Bot: Chief, then the pinned Bots in the order pinned, then the rest, oldest
// first.
func (s *Store) Bots(ctx context.Context) ([]Bot, error) {
	return query(ctx, s.db,
		"SELECT "+_botColumns+", pinned_at IS NOT NULL FROM bots WHERE removed = 0 "+
			"ORDER BY id = 'chief' DESC, pinned_at IS NULL, pinned_at, rowid", scanBot)
}

// SetPinned pins the Bot with id, or unpins it. Pinning a pinned Bot keeps its place.
func (s *Store) SetPinned(ctx context.Context, id string, pinned bool) error {
	stmt, args := "UPDATE bots SET pinned_at = NULL WHERE id = ? AND removed = 0", []any{id}
	if pinned {
		stmt = "UPDATE bots SET pinned_at = coalesce(pinned_at, ?) WHERE id = ? AND removed = 0"
		args = []any{time.Now().UTC().UnixMicro(), id}
	}
	res, err := s.db.ExecContext(ctx, stmt, args...)
	if err != nil {
		return fmt.Errorf("set pinned: %w", err)
	}
	return found(res, "bot", id)
}

// DeleteBot removes the Bot with its threads, messages and runs, and returns the ids of the runs.
func (s *Store) DeleteBot(ctx context.Context, id string) ([]string, error) {
	var runIDs []string
	err := s.tx(ctx, func(tx *sql.Tx) error {
		const threads = "(SELECT id FROM threads WHERE bot_id = ?)"
		var err error
		runIDs, err = query(ctx, tx, "SELECT id FROM runs WHERE thread_id IN "+threads,
			func(r *sql.Rows) (string, error) {
				var runID string
				return runID, r.Scan(&runID)
			}, id)
		if err != nil {
			return fmt.Errorf("list runs of bot %s: %w", id, err)
		}
		for _, stmt := range []string{
			"DELETE FROM messages WHERE thread_id IN " + threads,
			"DELETE FROM runs WHERE thread_id IN " + threads,
			"DELETE FROM threads WHERE bot_id = ?",
		} {
			if _, err := tx.ExecContext(ctx, stmt, id); err != nil {
				return fmt.Errorf("delete bot %s: %w", id, err)
			}
		}
		res, err := tx.ExecContext(ctx, "DELETE FROM bots WHERE id = ? AND removed = 0", id)
		if err != nil {
			return fmt.Errorf("delete bot %s: %w", id, err)
		}
		return found(res, "bot", id)
	})
	if err != nil {
		return nil, err
	}
	return runIDs, nil
}

// CreateRepo registers a repo. It wraps ErrExists when the name is taken.
func (s *Store) CreateRepo(ctx context.Context, name, path string) (Repo, error) {
	r := Repo{ID: newID(), Name: name, Path: path}
	_, err := s.db.ExecContext(ctx, "INSERT INTO repos (id, name, path) VALUES (?, ?, ?)",
		r.ID, r.Name, r.Path)
	if err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed: repos.name") {
		return Repo{}, fmt.Errorf("repo name %q: %w", name, ErrExists)
	}
	if err != nil {
		return Repo{}, fmt.Errorf("insert repo: %w", err)
	}
	return r, nil
}

// Repo returns the repo with id.
func (s *Store) Repo(ctx context.Context, id string) (Repo, error) {
	rows, err := query(ctx, s.db, "SELECT id, name, path FROM repos WHERE id = ?", scanRepo, id)
	if err != nil {
		return Repo{}, fmt.Errorf("read repo %s: %w", id, err)
	}
	if len(rows) == 0 {
		return Repo{}, fmt.Errorf("repo %s: %w", id, ErrNotFound)
	}
	return rows[0], nil
}

// Repos lists every repo, oldest first.
func (s *Store) Repos(ctx context.Context) ([]Repo, error) {
	return query(ctx, s.db, "SELECT id, name, path FROM repos ORDER BY rowid", scanRepo)
}

// Setting returns the value stored under key, or "" when there is none.
func (s *Store) Setting(ctx context.Context, key string) (string, error) {
	var value string
	err := s.db.QueryRowContext(ctx, "SELECT value FROM settings WHERE key = ?", key).Scan(&value)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("read setting %s: %w", key, err)
	}
	return value, nil
}

// SetSetting stores value under key.
func (s *Store) SetSetting(ctx context.Context, key, value string) error {
	_, err := s.db.ExecContext(ctx,
		"INSERT INTO settings (key, value) VALUES (?, ?) "+
			"ON CONFLICT (key) DO UPDATE SET value = excluded.value", key, value)
	if err != nil {
		return fmt.Errorf("set setting %s: %w", key, err)
	}
	return nil
}

// CreateConnection stores a new pending connection of kind to instanceURL.
func (s *Store) CreateConnection(ctx context.Context, kind, instanceURL string) (Connection, error) {
	c := Connection{ID: newID(), Kind: kind, InstanceURL: instanceURL, Status: ConnectionPending}
	_, err := s.db.ExecContext(ctx,
		"INSERT INTO connections (id, kind, instance_url, status) VALUES (?, ?, ?, ?)",
		c.ID, c.Kind, c.InstanceURL, c.Status)
	if err != nil {
		return Connection{}, fmt.Errorf("insert connection: %w", err)
	}
	return c, nil
}

// Connection returns the connection with id.
func (s *Store) Connection(ctx context.Context, id string) (Connection, error) {
	rows, err := query(ctx, s.db,
		"SELECT id, kind, instance_url, status FROM connections WHERE id = ?", scanConnection, id)
	if err != nil {
		return Connection{}, fmt.Errorf("read connection %s: %w", id, err)
	}
	if len(rows) == 0 {
		return Connection{}, fmt.Errorf("connection %s: %w", id, ErrNotFound)
	}
	return rows[0], nil
}

// Connections lists every connection, oldest first.
func (s *Store) Connections(ctx context.Context) ([]Connection, error) {
	return query(ctx, s.db,
		"SELECT id, kind, instance_url, status FROM connections ORDER BY rowid", scanConnection)
}

// SetConnectionStatus sets the status of the connection with id.
func (s *Store) SetConnectionStatus(ctx context.Context, id, status string) error {
	res, err := s.db.ExecContext(ctx, "UPDATE connections SET status = ? WHERE id = ?", status, id)
	if err != nil {
		return fmt.Errorf("set connection status: %w", err)
	}
	return found(res, "connection", id)
}

// CreateThread starts a thread with the Bot botID, in the repo repoID when it is not empty.
func (s *Store) CreateThread(ctx context.Context, botID, repoID string) (Thread, error) {
	t := Thread{
		ID: newID(), BotID: botID, RepoID: repoID,
		Created: time.Now().UTC().Truncate(time.Microsecond),
	}
	res, err := s.db.ExecContext(ctx,
		"INSERT INTO threads (id, bot_id, repo_id, created, last_activity) "+
			"SELECT ?, id, ?, ?, ? FROM bots WHERE id = ? AND removed = 0",
		t.ID, t.RepoID, t.Created.UnixMicro(), t.Created.UnixMicro(), botID)
	if err != nil {
		return Thread{}, fmt.Errorf("insert thread: %w", err)
	}
	if err := found(res, "bot", botID); err != nil {
		return Thread{}, err
	}
	t.LastActivity = t.Created
	return t, nil
}

// Thread returns the thread with id.
func (s *Store) Thread(ctx context.Context, id string) (Thread, error) {
	rows, err := query(ctx, s.db,
		"SELECT "+_threadColumns+" FROM threads WHERE id = ?", scanThread, id)
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
		"SELECT "+_threadColumns+" FROM threads WHERE bot_id = ? ORDER BY last_activity DESC, rowid DESC",
		scanThread, botID)
}

// Watched lists the threads with an MR and a worktree: the ones whose MR is polled.
func (s *Store) Watched(ctx context.Context) ([]Thread, error) {
	return query(ctx, s.db,
		"SELECT "+_threadColumns+" FROM threads WHERE mr_url != '' AND worktree != '' ORDER BY rowid",
		scanThread)
}

// SetWorktree records the thread's branch and worktree; an empty worktree clears it.
func (s *Store) SetWorktree(ctx context.Context, threadID, branch, worktree string) error {
	res, err := s.db.ExecContext(ctx, "UPDATE threads SET branch = ?, worktree = ?, worktree_removed = 0, snapshot_ref = '' WHERE id = ?",
		branch, worktree, threadID)
	if err != nil {
		return fmt.Errorf("set worktree: %w", err)
	}
	return found(res, "thread", threadID)
}

// RemoveWorktree records a completed worktree cleanup.
func (s *Store) RemoveWorktree(ctx context.Context, threadID, ref string) error {
	res, err := s.db.ExecContext(ctx, "UPDATE threads SET worktree = '', worktree_removed = 1, snapshot_ref = ? WHERE id = ?", ref, threadID)
	if err != nil {
		return fmt.Errorf("remove worktree: %w", err)
	}
	return found(res, "thread", threadID)
}

// SetMR records the thread's MR and its state.
func (s *Store) SetMR(ctx context.Context, threadID, url, state, title string) error {
	res, err := s.db.ExecContext(ctx, "UPDATE threads SET mr_url = ?, mr_state = ?, mr_title = ? WHERE id = ?",
		url, state, title, threadID)
	if err != nil {
		return fmt.Errorf("set MR: %w", err)
	}
	return found(res, "thread", threadID)
}

// SetTitle sets the thread's title.
func (s *Store) SetTitle(ctx context.Context, threadID, title string) error {
	res, err := s.db.ExecContext(ctx, "UPDATE threads SET title = ? WHERE id = ?", title, threadID)
	if err != nil {
		return fmt.Errorf("set title: %w", err)
	}
	return found(res, "thread", threadID)
}

// SetClosed closes the thread: it takes no more messages.
func (s *Store) SetClosed(ctx context.Context, threadID string) error {
	res, err := s.db.ExecContext(ctx, "UPDATE threads SET closed = 1, closed_at = ? WHERE id = ?", time.Now().UTC().UnixMicro(), threadID)
	if err != nil {
		return fmt.Errorf("close thread: %w", err)
	}
	return found(res, "thread", threadID)
}

// LatestRun returns the id of the thread's newest run, or "" when it has none.
func (s *Store) LatestRun(ctx context.Context, threadID string) (string, error) {
	var id string
	err := s.db.QueryRowContext(ctx,
		"SELECT id FROM runs WHERE thread_id = ? ORDER BY rowid DESC LIMIT 1", threadID).Scan(&id)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("read latest run of thread %s: %w", threadID, err)
	}
	return id, nil
}

// ThreadRun is a thread that has a run, with the status and the reply of its newest run. Reply
// is only its first 300 characters: enough for a one-line preview.
type ThreadRun struct {
	BotID        string
	ThreadID     string
	LastActivity time.Time
	Status       string
	Reply        string
}

// ThreadRuns lists the threads that have a run, newest activity first. botID limits it to one
// Bot; empty means every Bot.
func (s *Store) ThreadRuns(ctx context.Context, botID string) ([]ThreadRun, error) {
	return query(ctx, s.db,
		`SELECT t.bot_id, t.id, t.last_activity, r.status, substr(r.reply, 1, 300)
		 FROM (SELECT max(rowid) AS rid FROM runs GROUP BY thread_id) latest
		 JOIN runs r ON r.rowid = latest.rid
		 JOIN threads t ON t.id = r.thread_id
		 WHERE ? = '' OR t.bot_id = ?
		 ORDER BY t.last_activity DESC, t.rowid DESC`,
		func(r *sql.Rows) (tr ThreadRun, err error) {
			var at int64
			err = r.Scan(&tr.BotID, &tr.ThreadID, &at, &tr.Status, &tr.Reply)
			tr.LastActivity = time.UnixMicro(at).UTC()
			return tr, err
		}, botID, botID)
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
	r := Run{ID: newID(), ThreadID: threadID, Status: StatusRunning, StartedAt: time.Now().UTC(), Tools: map[string]int{}}
	m := Message{ID: newID(), ThreadID: threadID, Role: RoleUser, Text: text, RunID: r.ID}
	err := s.tx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx,
			"INSERT INTO runs (id, thread_id, status, started_at) SELECT ?, id, ?, ? FROM threads WHERE id = ?",
			r.ID, r.Status, r.StartedAt.UnixMicro(), threadID)
		if err != nil {
			return fmt.Errorf("insert run: %w", err)
		}
		if err := found(res, "thread", threadID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx,
			"UPDATE threads SET last_activity = ?, title = CASE WHEN title = '' THEN ? ELSE title END WHERE id = ?",
			time.Now().UTC().UnixMicro(), firstWords(text), threadID); err != nil {
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
	var started, finished int64
	var tools string
	err := s.db.QueryRowContext(ctx,
		"SELECT id, thread_id, status, reply, error, error_kind, report, started_at, finished_at, tools FROM runs WHERE id = ?", id).
		Scan(&r.ID, &r.ThreadID, &r.Status, &r.Reply, &r.Error, &r.ErrorKind, &report, &started, &finished, &tools)
	if errors.Is(err, sql.ErrNoRows) {
		return Run{}, fmt.Errorf("run %s: %w", id, ErrNotFound)
	}
	if err != nil {
		return Run{}, fmt.Errorf("read run %s: %w", id, err)
	}
	if started != 0 {
		r.StartedAt = time.UnixMicro(started).UTC()
	}
	if finished != 0 {
		r.FinishedAt = time.UnixMicro(finished).UTC()
	}
	if err := json.Unmarshal([]byte(tools), &r.Tools); err != nil {
		return Run{}, err
	}
	if report.Valid {
		if err := json.Unmarshal([]byte(report.String), &r.Report); err != nil {
			return Run{}, fmt.Errorf("decode report of run %s: %w", id, err)
		}
	}
	return r, nil
}

// CountTool increments a run's count for a tool kind.
func (s *Store) CountTool(ctx context.Context, runID, kind string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE runs SET tools = json_set(tools, ?, coalesce(json_extract(tools, ?), 0) + 1) WHERE id = ?`, "$."+kind, "$."+kind, runID)
	return err
}

// SetStatus sets the status of a run that is running or waiting; a finished run keeps its status.
// It also updates the time of the thread's latest activity.
func (s *Store) SetStatus(ctx context.Context, runID, status string) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx,
			"UPDATE runs SET status = CASE WHEN status IN (?, ?) THEN ? ELSE status END WHERE id = ?",
			StatusRunning, StatusWaiting, status, runID)
		if err != nil {
			return fmt.Errorf("set status: %w", err)
		}
		if err := found(res, "run", runID); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx,
			"UPDATE threads SET last_activity = ? WHERE id = (SELECT thread_id FROM runs WHERE id = ?)",
			time.Now().UTC().UnixMicro(), runID)
		return err
	})
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

// FinishRun ends a run as done with reply, or as failed with errText and its errKind. A non-empty
// reply also becomes the thread's bot message, in the same transaction.
func (s *Store) FinishRun(ctx context.Context, runID, status, reply, errText, errKind string) error {
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
			"UPDATE runs SET status = ?, reply = ?, error = ?, error_kind = ?, finished_at = ? WHERE id = ?",
			status, reply, errText, errKind, time.Now().UTC().UnixMicro(), runID)
		if err != nil {
			return fmt.Errorf("finish run: %w", err)
		}
		_, err = tx.ExecContext(ctx, "UPDATE threads SET last_activity = ? WHERE id = ?",
			time.Now().UTC().UnixMicro(), threadID)
		if err != nil {
			return fmt.Errorf("touch thread: %w", err)
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

// Messages lists every message in the thread, oldest first.
func (s *Store) Messages(ctx context.Context, threadID string) ([]Message, error) {
	return s.messages(ctx,
		`SELECT m.id, m.thread_id, m.role, m.text, m.run_id,
            CASE WHEN m.role = 'bot' AND r.finished_at != 0 THEN r.finished_at ELSE r.started_at END
         FROM messages m JOIN runs r ON r.id = m.run_id WHERE m.thread_id = ? ORDER BY m.rowid`,
		threadID)
}

// MessagePage lists a page of messages, oldest first. An empty cursor returns the newest page.
func (s *Store) MessagePage(ctx context.Context, threadID, before, after string) ([]Message, error) {
	if after != "" {
		return s.messages(ctx,
			`SELECT m.id, m.thread_id, m.role, m.text, m.run_id,
                CASE WHEN m.role = 'bot' AND r.finished_at != 0 THEN r.finished_at ELSE r.started_at END
             FROM messages m JOIN runs r ON r.id = m.run_id
             WHERE m.thread_id = ? AND m.rowid > (
                 SELECT rowid FROM messages WHERE id = ? AND thread_id = ?
             ) ORDER BY m.rowid LIMIT `+strconv.Itoa(_messagePageSize),
			threadID, after, threadID)
	}
	return s.messages(ctx,
		`SELECT id, thread_id, role, text, run_id, at FROM (
            SELECT m.id, m.thread_id, m.role, m.text, m.run_id,
                CASE WHEN m.role = 'bot' AND r.finished_at != 0 THEN r.finished_at ELSE r.started_at END AS at,
                m.rowid
            FROM messages m JOIN runs r ON r.id = m.run_id
            WHERE m.thread_id = ?
              AND (CASE WHEN ? = '' THEN 1 ELSE m.rowid < (
                  SELECT rowid FROM messages WHERE id = ? AND thread_id = ?
              ) END)
            ORDER BY m.rowid DESC LIMIT `+strconv.Itoa(_messagePageSize)+`
         ) ORDER BY rowid`,
		threadID, before, before, threadID)
}

func (s *Store) messages(ctx context.Context, querySQL string, args ...any) ([]Message, error) {
	return query(ctx, s.db, querySQL,
		func(r *sql.Rows) (m Message, err error) {
			var at int64
			err = r.Scan(&m.ID, &m.ThreadID, &m.Role, &m.Text, &m.RunID, &at)
			if at != 0 {
				m.CreatedAt = time.UnixMicro(at).UTC()
			}
			return m, err
		}, args...)
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

// firstWords is the title a thread's first message gives it: the first six words, without the
// punctuation that ends the last one.
func firstWords(text string) string {
	words := strings.Fields(text)
	words = words[:min(6, len(words))]
	if len(words) > 0 {
		words[len(words)-1] = strings.TrimRight(words[len(words)-1], ".,;:!?")
	}
	return strings.Join(words, " ")
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
	var closed int
	var created, lastActivity int64
	var removed int
	var closedAt sql.NullInt64
	err := r.Scan(&t.ID, &t.BotID, &t.Session, &t.RepoID, &t.Branch, &t.Worktree, &t.MRURL,
		&t.MRState, &closed, &created, &lastActivity, &t.MRTitle, &removed, &closedAt, &t.SnapshotRef,
		&t.Title)
	if err != nil {
		return Thread{}, err
	}
	t.Closed = closed != 0
	t.WorktreeRemoved = removed != 0
	if closedAt.Valid {
		when := time.UnixMicro(closedAt.Int64).UTC()
		t.ClosedAt = &when
	}
	t.Created = time.UnixMicro(created).UTC()
	t.LastActivity = time.UnixMicro(lastActivity).UTC()
	return t, nil
}

func scanBot(r *sql.Rows) (Bot, error) {
	var b Bot
	var access sql.NullString
	var conns, skills string
	err := r.Scan(&b.ID, &b.Name, &b.Persona, &b.Provider, &access, &conns, &skills,
		&b.CanManagePlatform, &b.Look, &b.Model, &b.Pinned)
	if err != nil {
		return Bot{}, err
	}
	if access.Valid {
		if err := json.Unmarshal([]byte(access.String), &b.RepoAccess); err != nil {
			return Bot{}, fmt.Errorf("decode repo access of bot %s: %w", b.ID, err)
		}
	}
	if err := json.Unmarshal([]byte(conns), &b.Connections); err != nil {
		return Bot{}, fmt.Errorf("decode connections of bot %s: %w", b.ID, err)
	}
	if err := json.Unmarshal([]byte(skills), &b.Skills); err != nil {
		return Bot{}, fmt.Errorf("decode skills of bot %s: %w", b.ID, err)
	}
	return normalizeBot(b), nil
}

func scanRepo(r *sql.Rows) (rp Repo, err error) {
	return rp, r.Scan(&rp.ID, &rp.Name, &rp.Path)
}

func scanConnection(r *sql.Rows) (c Connection, err error) {
	return c, r.Scan(&c.ID, &c.Kind, &c.InstanceURL, &c.Status)
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
