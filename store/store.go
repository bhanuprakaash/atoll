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
	// ErrPinLimit is returned when a Bot already has the most pinned threads.
	ErrPinLimit = errors.New("3 threads of this Bot are pinned")
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
	StatusStopped = "stopped"
)

// ErrorSignedOut is the error kind of a run that failed because its provider is signed out.
const ErrorSignedOut = "signed_out"

// KindGitLab is the kind of a GitLab connection.
const KindGitLab = "gitlab"

// KindGitHub is a GitHub connection.
const KindGitHub = "github"

// Connection statuses.
const (
	ConnectionPending     = "pending"
	ConnectionConnected   = "connected"
	ConnectionNeedsSignIn = "needs_sign_in"
)

// _maxPinnedThreads is how many open threads of one Bot may be pinned.
const _maxPinnedThreads = 3

// Message roles.
const (
	RoleUser = "user"
	RoleBot  = "bot"
)

type (
	// Store is Atoll's data, read and written through one SQLite file.
	Store struct {
		db   dbConn
		root *sql.DB
	}

	// Tx is a Store whose writes commit together on Commit and vanish on Rollback.
	Tx struct {
		Store *Store
		tx    *sql.Tx
	}

	// Bot is a configured agent. Provider is "claude" or "codex". Connections are the ids of the
	// connections it may use.
	Bot struct {
		ID                string     `json:"id"`
		Name              string     `json:"name"`
		Persona           string     `json:"persona"`
		Provider          string     `json:"provider"`
		Look              string     `json:"look,omitempty"`
		Model             string     `json:"model,omitempty"`
		Mode              string     `json:"mode"`
		Pinned            bool       `json:"pinned,omitempty"`
		State             string     `json:"state,omitempty"`
		Activity          string     `json:"activity,omitempty"`
		LastActivity      *time.Time `json:"last_activity,omitempty"`
		RepoAccess        RepoAccess `json:"repo_access"`
		Connections       []string   `json:"connections"`
		Skills            []string   `json:"skills"`
		MCPServers        []string   `json:"mcp_servers"`
		Delegates         []string   `json:"delegates"`
		CanManagePlatform bool       `json:"can_manage_platform"`
		// GroupID is the sidebar group, empty when Ungrouped. Only SetGroup writes it.
		GroupID string `json:"group_id,omitempty"`
	}

	// Group is a sidebar section of Bots.
	Group struct {
		ID     string `json:"id"`
		Name   string `json:"name"`
		Folded bool   `json:"folded"`
	}

	// RepoAccess is the repos a Bot may work in. Nil allows every repo and is "all" as JSON;
	// an empty list allows none.
	RepoAccess []string

	// Repo is a registered local git repo.
	Repo struct {
		ID   string `json:"id"`
		Name string `json:"name"`
		Path string `json:"path"`
		// Host is github or gitlab. It is not stored: the API fills it from the repo's origin.
		Host string `json:"host,omitempty"`
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
		ParentThreadID  string     `json:"parent_thread_id"`
		Title           string     `json:"title"`
		Session         string     `json:"-"`
		SessionProvider string     `json:"-"`
		RepoID          string     `json:"repo_id"`
		Branch          string     `json:"branch"`
		Worktree        string     `json:"worktree"`
		CheckoutGitDir  string     `json:"-"`
		WorktreeGitDir  string     `json:"-"`
		MRURL           string     `json:"mr_url"`
		MRState         string     `json:"mr_state"`
		MRTitle         string     `json:"mr_title"`
		MRCI            string     `json:"-"`
		MRWoke          string     `json:"-"`
		MRWake          bool       `json:"-"`
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
		FromBotID string    `json:"from_bot_id,omitempty"`
		Kind      string    `json:"kind"`
		CreatedAt time.Time `json:"created_at"`
		// Attachments are the message's images. Data is set only on a message just stored.
		Attachments []Attachment `json:"attachments"`
	}

	// Attachment is an image sent with a message.
	Attachment struct {
		ID   string `json:"id"`
		Mime string `json:"mime"`
		Data []byte `json:"-"`
	}

	// InboxItem is a held message. Images ride along only when the item is sent straight away.
	InboxItem struct {
		ID        string
		ThreadID  string
		FromBotID string
		Text      string
		Kind      string
		Images    []Attachment
	}

	// MRRead is what one poll of a thread's MR changes: the values to store, and the wake they
	// report. Prev is the state the poll started from.
	MRRead struct {
		URL, Prev, State, Title, CI, Woke string
		// DropCI deletes the thread's held ci wake: its commit is no longer the head or the MR is done.
		DropCI   bool
		WakeKind string
		WakeText string
	}

	// Run is one message handled.
	Run struct {
		ID         string         `json:"id"`
		ThreadID   string         `json:"thread_id"`
		Status     string         `json:"status"`
		Reply      string         `json:"reply"`
		Error      string         `json:"error"`
		ErrorKind  string         `json:"error_kind"`
		Provider   string         `json:"provider"`
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
		"can_manage_platform, look, model, mcp_servers, mode, delegates"
	_threadColumns   = "id, bot_id, session, repo_id, branch, worktree, mr_url, mr_state, closed, created, last_activity, mr_title, worktree_removed, closed_at, snapshot_ref, title, parent_thread_id, session_provider, checkout_git_dir, worktree_git_dir, mr_ci, mr_woke, mr_wake"
	_messagePageSize = 50
)

// _retireLooks moves Bots off the creatures the design dropped onto the nearest new one by colour.
const _retireLooks = `UPDATE bots SET look = CASE look
	WHEN 'starfish' THEN 'scallop' WHEN 'sand_dollar' THEN 'seal' WHEN 'sponge' THEN 'shark'
	WHEN 'kelp' THEN 'narwhal' WHEN 'shell' THEN 'octopus' WHEN 'wave' THEN 'manta'
	WHEN 'island' THEN 'seal' WHEN 'sun' THEN 'pufferfish' WHEN 'sailboat' THEN 'seal'
	WHEN 'turtle' THEN 'narwhal' WHEN 'seahorse' THEN 'pufferfish' WHEN 'otter' THEN 'seal'
	WHEN 'clownfish' THEN 'scallop' END
	WHERE look IN ('starfish', 'sand_dollar', 'sponge', 'kelp', 'shell', 'wave', 'island', 'sun',
		'sailboat', 'turtle', 'seahorse', 'otter', 'clownfish')`

// Open opens dir/atoll.db, creating dir and the file if needed, applies any migration the file
// lacks, and moves Bots off retired looks.
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
	if _, err := db.Exec(_retireLooks); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("retire looks: %w", err)
	}
	return &Store{db: db, root: db}, nil
}

// DB is the shared handle, for packages that keep their own tables in the same file.
func (s *Store) DB() *sql.DB { return s.root }

// Close closes the database.
func (s *Store) Close() error { return s.root.Close() }

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
		"INSERT INTO bots ("+_botColumns+") VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)", args...)
	if err != nil {
		return Bot{}, fmt.Errorf("insert bot: %w", err)
	}
	return b, nil
}

// Bot returns the Bot with id.
func (s *Store) Bot(ctx context.Context, id string) (Bot, error) {
	rows, err := query(ctx, s.db,
		"SELECT "+_botColumns+", pinned_at IS NOT NULL, coalesce(group_id, '') FROM bots "+
			"WHERE id = ? AND removed = 0",
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
		"SELECT "+_botColumns+", pinned_at IS NOT NULL, coalesce(group_id, '') FROM bots "+
			"WHERE removed = 0 "+
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

func dropDelegate(ctx context.Context, tx *sql.Tx, id string) error {
	_, err := tx.ExecContext(ctx,
		"UPDATE bots SET delegates = (SELECT json_group_array(value) FROM json_each(delegates) "+
			"WHERE value != ?1) WHERE EXISTS (SELECT 1 FROM json_each(delegates) WHERE value = ?1)", id)
	if err != nil {
		return fmt.Errorf("drop bot %s from delegates: %w", id, err)
	}
	return nil
}

// DeleteBot removes the Bot with its threads, messages and runs, and returns the ids of the runs.
// In the same transaction it holds each of wakes for its thread; one for a thread the Bot owns goes
// with it.
func (s *Store) DeleteBot(ctx context.Context, id string, wakes []InboxItem) ([]string, error) {
	var runIDs []string
	err := s.tx(ctx, func(tx *sql.Tx) error {
		const threads = "(SELECT id FROM threads WHERE bot_id = ?)"
		for _, w := range wakes {
			if err := insertInbox(ctx, tx, w.ThreadID, w.FromBotID, w.Text, ""); err != nil {
				return err
			}
		}
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
			"DELETE FROM inbox WHERE thread_id IN " + threads,
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
		if err := found(res, "bot", id); err != nil {
			return err
		}
		return dropDelegate(ctx, tx, id)
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
	return s.createThread(ctx, botID, repoID, "")
}

// CreateHelperThread starts a thread that helps parentID.
func (s *Store) CreateHelperThread(
	ctx context.Context, parentID, botID, repoID string,
) (Thread, error) {
	return s.createThread(ctx, botID, repoID, parentID)
}

func (s *Store) createThread(ctx context.Context, botID, repoID, parentID string) (Thread, error) {
	t := Thread{
		ID: newID(), BotID: botID, RepoID: repoID, ParentThreadID: parentID,
		Created: time.Now().UTC().Truncate(time.Microsecond),
	}
	res, err := s.db.ExecContext(ctx,
		"INSERT INTO threads (id, bot_id, repo_id, parent_thread_id, created, last_activity) "+
			"SELECT ?, id, ?, ?, ?, ? FROM bots WHERE id = ? AND removed = 0",
		t.ID, t.RepoID, t.ParentThreadID, t.Created.UnixMicro(), t.Created.UnixMicro(), botID)
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

// HelperThreads lists the threads that help parentID, oldest first.
func (s *Store) HelperThreads(ctx context.Context, parentID string) ([]Thread, error) {
	return query(ctx, s.db,
		"SELECT "+_threadColumns+" FROM threads WHERE parent_thread_id = ? ORDER BY rowid",
		scanThread, parentID)
}

// Watched lists the threads with an MR and a worktree: the ones whose MR is polled.
func (s *Store) Watched(ctx context.Context) ([]Thread, error) {
	return query(ctx, s.db,
		"SELECT "+_threadColumns+" FROM threads WHERE mr_url != '' AND worktree != '' ORDER BY rowid",
		scanThread)
}

// SetWorktree records the thread's branch and worktree; an empty worktree clears it.
func (s *Store) SetWorktree(ctx context.Context, threadID, branch, worktree string) error {
	res, err := s.db.ExecContext(ctx, "UPDATE threads SET branch = ?, worktree = ?, worktree_git_dir = '', worktree_removed = 0, snapshot_ref = '' WHERE id = ?",
		branch, worktree, threadID)
	if err != nil {
		return fmt.Errorf("set worktree: %w", err)
	}
	return found(res, "thread", threadID)
}

// SetGitDirs records the checkout and worktree git directory IDs.
func (s *Store) SetGitDirs(ctx context.Context, threadID, checkout, worktree string) error {
	res, err := s.db.ExecContext(ctx, "UPDATE threads SET checkout_git_dir = ?, worktree_git_dir = ? WHERE id = ?", checkout, worktree, threadID)
	if err != nil {
		return fmt.Errorf("set git directories: %w", err)
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

// SetMR records the thread's MR and, for a new URL, its state. A repeat call with the URL the
// thread has keeps the state and what the poll stored, so a re-track after a merge never wakes
// twice.
func (s *Store) SetMR(ctx context.Context, threadID, url, state, title string) error {
	res, err := s.db.ExecContext(ctx, `UPDATE threads SET
		mr_state = CASE WHEN mr_url = ?1 THEN mr_state ELSE ?2 END,
		mr_ci = CASE WHEN mr_url = ?1 THEN mr_ci ELSE '' END,
		mr_woke = CASE WHEN mr_url = ?1 THEN mr_woke ELSE '' END,
		mr_url = ?1, mr_title = ?3, mr_wake = 1 WHERE id = ?4`,
		url, state, title, threadID)
	if err != nil {
		return fmt.Errorf("set MR: %w", err)
	}
	return found(res, "thread", threadID)
}

// StoreMR stores what a poll read and, with it, the wake it reports, in one transaction: a restart
// cannot repeat the wake or lose it. It reports false and stores nothing when the thread is
// closed, tracks another MR or no longer has the state the poll started from.
func (s *Store) StoreMR(ctx context.Context, threadID string, r MRRead) (bool, error) {
	stored := false
	err := s.tx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `UPDATE threads SET mr_state = ?, mr_title = ?, mr_ci = ?, mr_woke = ?, mr_wake = 1
			WHERE id = ? AND mr_url = ? AND mr_state = ? AND closed = 0`,
			r.State, r.Title, r.CI, r.Woke, threadID, r.URL, r.Prev)
		if err != nil {
			return fmt.Errorf("store MR: %w", err)
		}
		if n, err := res.RowsAffected(); err != nil || n == 0 {
			return err
		}
		stored = true
		if r.DropCI || r.WakeKind == "ci" {
			if _, err := tx.ExecContext(ctx, "DELETE FROM inbox WHERE thread_id = ? AND kind = 'ci'", threadID); err != nil {
				return fmt.Errorf("drop held ci wake: %w", err)
			}
		}
		if r.WakeKind == "" {
			return nil
		}
		return insertInbox(ctx, tx, threadID, "", r.WakeText, r.WakeKind)
	})
	return stored, err
}

// SetTitle sets the thread's title.
func (s *Store) SetTitle(ctx context.Context, threadID, title string) error {
	res, err := s.db.ExecContext(ctx, "UPDATE threads SET title = ? WHERE id = ?", title, threadID)
	if err != nil {
		return fmt.Errorf("set title: %w", err)
	}
	return found(res, "thread", threadID)
}

// ReplaceTitle sets the thread's title to title if it is still old.
func (s *Store) ReplaceTitle(ctx context.Context, threadID, old, title string) error {
	if _, err := s.db.ExecContext(ctx, "UPDATE threads SET title = ? WHERE id = ? AND title = ?",
		title, threadID, old); err != nil {
		return fmt.Errorf("replace title: %w", err)
	}
	return nil
}

// SetClosed closes the thread: it takes no more messages. A closed thread is not pinned.
func (s *Store) SetClosed(ctx context.Context, threadID string) error {
	res, err := s.db.ExecContext(ctx, "UPDATE threads SET closed = 1, closed_at = ?, pinned_at = NULL WHERE id = ?", time.Now().UTC().UnixMicro(), threadID)
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

// ThreadRun is a thread with the status and the reply of its newest run. Status is empty for a
// thread with no run, and, in a poll, for a thread that has no live run and is not its Bot's
// newest with a run; Reply is only its first 300 characters: enough for a one-line preview.
// Unseen is set when the thread had activity after the user last opened it, has had a run and has
// no live run: every run that finishes, and a start of one, sets the thread's last activity.
type ThreadRun struct {
	BotID          string
	ThreadID       string
	ParentThreadID string
	Title          string
	Closed         bool
	Pinned         bool
	LastActivity   time.Time
	Unseen         bool
	Status         string
	Reply          string
}

// ThreadRuns lists every thread with its newest run, newest activity first. botID limits it to
// one Bot; empty means every Bot.
func (s *Store) ThreadRuns(ctx context.Context, botID string) ([]ThreadRun, error) {
	return s.threadRuns(ctx,
		`polled AS MATERIALIZED (
		   SELECT t.id AS thread_id, r.status, substr(r.reply, 1, 300) AS reply FROM threads t
		   JOIN runs r ON r.rowid = (SELECT max(rowid) FROM runs WHERE thread_id = t.id))`,
		"? = '' OR t.bot_id = ?", botID, botID)
}

// PollThreadRuns is ThreadRuns for every Bot without the closed threads nobody reads: it keeps
// the open ones, the closed ones with a live run, the parents of those (a helper finds its
// boss through its parent), and the newest thread with a run of each Bot. Only the threads with
// a live run and each Bot's newest thread with a run are read with their run; the rest have no
// Status.
func (s *Store) PollThreadRuns(ctx context.Context) ([]ThreadRun, error) {
	return s.threadRuns(ctx,
		`live AS MATERIALIZED (SELECT thread_id FROM runs WHERE status IN ('running', 'waiting')),
		 polled AS MATERIALIZED (
		   SELECT n.thread_id, r.status, substr(r.reply, 1, 300) AS reply
		   FROM (SELECT thread_id FROM live
		         UNION SELECT (SELECT t2.id FROM threads t2
		                       WHERE t2.bot_id = b.id AND EXISTS (SELECT 1 FROM runs WHERE thread_id = t2.id)
		                       ORDER BY t2.last_activity DESC, t2.rowid DESC LIMIT 1) FROM bots b) n
		   JOIN runs r ON r.rowid = (SELECT max(rowid) FROM runs WHERE thread_id = n.thread_id))`,
		`t.closed = 0
		 OR p.thread_id IS NOT NULL
		 OR t.id IN (SELECT parent_thread_id FROM threads
		             WHERE parent_thread_id != '' AND (closed = 0 OR id IN live))`)
}

// threadRuns reads the threads matching where, newest activity first. with defines the CTEs it
// needs, among them polled(thread_id, status, reply): the threads whose newest run is read.
func (s *Store) threadRuns(ctx context.Context, with, where string, args ...any) ([]ThreadRun, error) {
	return query(ctx, s.db,
		`WITH `+with+`
		 SELECT t.bot_id, t.id, t.parent_thread_id, t.title, t.closed, t.pinned_at IS NOT NULL,
		        t.last_activity,
		        t.last_activity > t.last_opened_at AND t.last_activity > t.created
		          AND coalesce(p.status, '') NOT IN ('running', 'waiting'),
		        coalesce(p.status, ''), coalesce(p.reply, '')
		 FROM threads t
		 LEFT JOIN polled p ON p.thread_id = t.id
		 WHERE `+where+`
		 ORDER BY t.last_activity DESC, t.rowid DESC`,
		func(r *sql.Rows) (tr ThreadRun, err error) {
			var active int64
			err = r.Scan(&tr.BotID, &tr.ThreadID, &tr.ParentThreadID, &tr.Title, &tr.Closed,
				&tr.Pinned, &active, &tr.Unseen, &tr.Status, &tr.Reply)
			tr.LastActivity = time.UnixMicro(active).UTC()
			return tr, err
		}, args...)
}

// MarkOpened records that the user opened the thread now.
func (s *Store) MarkOpened(ctx context.Context, threadID string) error {
	res, err := s.db.ExecContext(ctx, "UPDATE threads SET last_opened_at = ? WHERE id = ?",
		time.Now().UTC().UnixMicro(), threadID)
	if err != nil {
		return fmt.Errorf("mark opened: %w", err)
	}
	return found(res, "thread", threadID)
}

// PinThread pins an open thread that is not a helper's; pinning a pinned one keeps its place.
// replace, when set, is unpinned in the same transaction. It returns ErrPinLimit when the Bot
// already has the most other threads pinned.
func (s *Store) PinThread(ctx context.Context, threadID, replace string) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		var botID string
		var pinned bool
		err := tx.QueryRowContext(ctx,
			"SELECT bot_id, pinned_at IS NOT NULL FROM threads "+
				"WHERE id = ? AND closed = 0 AND parent_thread_id = ''", threadID).Scan(&botID, &pinned)
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("thread %s: %w", threadID, ErrNotFound)
		}
		if err != nil {
			return fmt.Errorf("read thread %s: %w", threadID, err)
		}
		if pinned {
			return nil
		}
		if replace != "" {
			if _, err := tx.ExecContext(ctx,
				"UPDATE threads SET pinned_at = NULL WHERE id = ? AND bot_id = ?", replace, botID); err != nil {
				return fmt.Errorf("unpin thread %s: %w", replace, err)
			}
		}
		var n int
		if err := tx.QueryRowContext(ctx,
			"SELECT count(*) FROM threads WHERE bot_id = ? AND pinned_at IS NOT NULL", botID).
			Scan(&n); err != nil {
			return fmt.Errorf("count pinned threads: %w", err)
		}
		if n >= _maxPinnedThreads {
			return ErrPinLimit
		}
		if _, err := tx.ExecContext(ctx, "UPDATE threads SET pinned_at = ? WHERE id = ?",
			time.Now().UTC().UnixMicro(), threadID); err != nil {
			return fmt.Errorf("pin thread: %w", err)
		}
		return nil
	})
}

// UnpinThread unpins the thread.
func (s *Store) UnpinThread(ctx context.Context, threadID string) error {
	res, err := s.db.ExecContext(ctx, "UPDATE threads SET pinned_at = NULL WHERE id = ?", threadID)
	if err != nil {
		return fmt.Errorf("unpin thread: %w", err)
	}
	return found(res, "thread", threadID)
}

// Groups lists the groups in the user's order.
func (s *Store) Groups(ctx context.Context) ([]Group, error) {
	return query(ctx, s.db, "SELECT id, name, folded FROM bot_groups ORDER BY position, rowid",
		func(r *sql.Rows) (g Group, err error) { return g, r.Scan(&g.ID, &g.Name, &g.Folded) })
}

// CreateGroup adds a group after the others.
func (s *Store) CreateGroup(ctx context.Context, name string) (Group, error) {
	g := Group{ID: newID(), Name: name}
	_, err := s.db.ExecContext(ctx,
		"INSERT INTO bot_groups (id, name, position) "+
			"SELECT ?, ?, coalesce(max(position), 0) + 1 FROM bot_groups", g.ID, g.Name)
	if err != nil {
		return Group{}, fmt.Errorf("insert group: %w", err)
	}
	return g, nil
}

// SetGroupName renames the group.
func (s *Store) SetGroupName(ctx context.Context, id, name string) error {
	res, err := s.db.ExecContext(ctx, "UPDATE bot_groups SET name = ? WHERE id = ?", name, id)
	if err != nil {
		return fmt.Errorf("set group name: %w", err)
	}
	return found(res, "group", id)
}

// SetGroupFolded folds or unfolds the group.
func (s *Store) SetGroupFolded(ctx context.Context, id string, folded bool) error {
	res, err := s.db.ExecContext(ctx, "UPDATE bot_groups SET folded = ? WHERE id = ?", folded, id)
	if err != nil {
		return fmt.Errorf("set group folded: %w", err)
	}
	return found(res, "group", id)
}

// MoveGroup swaps the group with the one above it (up) or below it; at the end it does nothing.
func (s *Store) MoveGroup(ctx context.Context, id string, up bool) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		ids, err := query(ctx, tx, "SELECT id FROM bot_groups ORDER BY position, rowid",
			func(r *sql.Rows) (id string, err error) { return id, r.Scan(&id) })
		if err != nil {
			return fmt.Errorf("list groups: %w", err)
		}
		i := slices.Index(ids, id)
		if i < 0 {
			return fmt.Errorf("group %s: %w", id, ErrNotFound)
		}
		j := i + 1
		if up {
			j = i - 1
		}
		if j < 0 || j >= len(ids) {
			return nil
		}
		ids[i], ids[j] = ids[j], ids[i]
		for pos, gid := range ids {
			if _, err := tx.ExecContext(ctx, "UPDATE bot_groups SET position = ? WHERE id = ?",
				pos+1, gid); err != nil {
				return fmt.Errorf("move group: %w", err)
			}
		}
		return nil
	})
}

// DeleteGroup removes the group; its Bots become Ungrouped.
func (s *Store) DeleteGroup(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, "DELETE FROM bot_groups WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("delete group: %w", err)
	}
	return found(res, "group", id)
}

// SetGroup puts the Bot in the group groupID, or Ungrouped when groupID is empty. It wraps
// ErrNotFound for an unknown group.
func (s *Store) SetGroup(ctx context.Context, botID, groupID string) error {
	res, err := s.db.ExecContext(ctx,
		"UPDATE bots SET group_id = nullif(?1, '') WHERE id = ?2 AND removed = 0 "+
			"AND (?1 = '' OR EXISTS (SELECT 1 FROM bot_groups WHERE id = ?1))", groupID, botID)
	if err != nil {
		return fmt.Errorf("set group: %w", err)
	}
	return found(res, "bot or group", botID)
}

// SetSession records the provider session the thread's next runs resume.
func (s *Store) SetSession(ctx context.Context, threadID, session, provider string) error {
	res, err := s.db.ExecContext(ctx, "UPDATE threads SET session = ?, session_provider = ? WHERE id = ?",
		session, provider, threadID)
	if err != nil {
		return fmt.Errorf("set session: %w", err)
	}
	return found(res, "thread", threadID)
}

// StartRunFrom stores the message and a running run, removing the inbox item when it has an ID,
// in one transaction.
func (s *Store) StartRunFrom(ctx context.Context, it InboxItem) (Message, Run, error) {
	return s.startRun(ctx, it.ThreadID, it.FromBotID, it.Text, it.Kind, it.ID, it.Images)
}

func (s *Store) startRun(
	ctx context.Context, threadID, fromBotID, text, kind, inboxID string, images []Attachment,
) (Message, Run, error) {
	r := Run{ID: newID(), ThreadID: threadID, Status: StatusRunning, StartedAt: time.Now().UTC(), Tools: map[string]int{}}
	m := Message{ID: newID(), ThreadID: threadID, Role: RoleUser, Text: text, RunID: r.ID, FromBotID: fromBotID, Kind: kind}
	m.Attachments = []Attachment{}
	for _, img := range images {
		m.Attachments = append(m.Attachments, Attachment{ID: newID(), Mime: img.Mime, Data: img.Data})
	}
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
			time.Now().UTC().UnixMicro(), FirstWords(text), threadID); err != nil {
			return err
		}
		if inboxID != "" {
			if _, err := tx.ExecContext(ctx, "DELETE FROM inbox WHERE id = ?", inboxID); err != nil {
				return fmt.Errorf("delete inbox item: %w", err)
			}
		}
		return insertMessage(ctx, tx, m)
	})
	if err != nil {
		return Message{}, Run{}, err
	}
	return m, r, nil
}

// Enqueue holds text for the thread until a run can start.
func (s *Store) Enqueue(ctx context.Context, threadID, fromBotID, text string) error {
	return insertInbox(ctx, s.db, threadID, fromBotID, text, "")
}

// OldestInbox returns the oldest held message; ok is false when none.
func (s *Store) OldestInbox(ctx context.Context, threadID string) (it InboxItem, ok bool, err error) {
	items, err := query(ctx, s.db,
		"SELECT id, thread_id, from_bot_id, text, kind FROM inbox WHERE thread_id = ? ORDER BY rowid LIMIT 1",
		func(r *sql.Rows) (it InboxItem, err error) {
			return it, r.Scan(&it.ID, &it.ThreadID, &it.FromBotID, &it.Text, &it.Kind)
		}, threadID)
	if err != nil || len(items) == 0 {
		return InboxItem{}, false, err
	}
	return items[0], true, nil
}

// DeleteInbox drops the held message with id.
func (s *Store) DeleteInbox(ctx context.Context, id string) error {
	if _, err := s.db.ExecContext(ctx, "DELETE FROM inbox WHERE id = ?", id); err != nil {
		return fmt.Errorf("delete inbox item: %w", err)
	}
	return nil
}

// InboxThreads lists the threads that have a held message.
func (s *Store) InboxThreads(ctx context.Context) ([]string, error) {
	return query(ctx, s.db, "SELECT DISTINCT thread_id FROM inbox ORDER BY thread_id",
		func(r *sql.Rows) (id string, err error) { return id, r.Scan(&id) })
}

// Run returns the run with id.
func (s *Store) Run(ctx context.Context, id string) (Run, error) {
	var r Run
	var report sql.NullString
	var started, finished int64
	var tools string
	err := s.db.QueryRowContext(ctx,
		"SELECT id, thread_id, status, reply, error, error_kind, provider, report, started_at, finished_at, tools FROM runs WHERE id = ?", id).
		Scan(&r.ID, &r.ThreadID, &r.Status, &r.Reply, &r.Error, &r.ErrorKind, &r.Provider, &report, &started, &finished, &tools)
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

// RunResult is how a run ended.
type RunResult struct {
	Status    string
	Reply     string
	Error     string
	ErrorKind string
	Provider  string // the provider the run used
}

// Wake is a finished helper run with what the boss's message is made of.
type Wake struct {
	Helper     string // the helper Bot's name
	ThreadID   string // the helper's thread
	Branch     string // the helper thread's branch
	BossBranch string // the boss thread's branch
	Run        Run
}

// FinishRun ends a run with res. A non-empty reply also becomes the thread's bot message, in the
// same transaction. When wake is not nil and the run is a helper's, the message wake returns is
// held for the boss thread in the same transaction.
func (s *Store) FinishRun(
	ctx context.Context, runID string, res RunResult, wake func(Wake) string,
) error {
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
			"UPDATE runs SET status = ?, reply = ?, error = ?, error_kind = ?, provider = ?, finished_at = ? WHERE id = ?",
			res.Status, res.Reply, res.Error, res.ErrorKind, res.Provider, time.Now().UTC().UnixMicro(), runID)
		if err != nil {
			return fmt.Errorf("finish run: %w", err)
		}
		_, err = tx.ExecContext(ctx, "UPDATE threads SET last_activity = ? WHERE id = ?",
			time.Now().UTC().UnixMicro(), threadID)
		if err != nil {
			return fmt.Errorf("touch thread: %w", err)
		}
		if err := enqueueWake(ctx, tx, runID, wake); err != nil {
			return err
		}
		if res.Reply == "" {
			return nil
		}
		return insertMessage(ctx, tx,
			Message{ID: newID(), ThreadID: threadID, Role: RoleBot, Text: res.Reply, RunID: runID})
	})
}

// FailUnfinished marks every running or waiting run failed with reason and returns their ids. When
// wake is not nil, a failed helper run also holds the message wake returns for its boss thread, in
// the same transaction. Called once at start: no run outlives the process that ran it.
func (s *Store) FailUnfinished(
	ctx context.Context, reason string, wake func(Wake) string,
) ([]string, error) {
	var ids []string
	err := s.tx(ctx, func(tx *sql.Tx) error {
		var err error
		ids, err = query(ctx, tx,
			"UPDATE runs SET status = ?, error = ?, finished_at = ? WHERE status IN (?, ?) RETURNING id",
			func(r *sql.Rows) (id string, err error) { return id, r.Scan(&id) },
			StatusFailed, reason, time.Now().UTC().UnixMicro(), StatusRunning, StatusWaiting)
		if err != nil {
			return err
		}
		for _, id := range ids {
			if _, err := tx.ExecContext(ctx,
				"UPDATE threads SET last_activity = ? WHERE id = (SELECT thread_id FROM runs WHERE id = ?)",
				time.Now().UTC().UnixMicro(), id); err != nil {
				return fmt.Errorf("touch thread of run %s: %w", id, err)
			}
			if err := enqueueWake(ctx, tx, id, wake); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("fail unfinished runs: %w", err)
	}
	return ids, nil
}

func enqueueWake(ctx context.Context, tx *sql.Tx, runID string, wake func(Wake) string) error {
	if wake == nil {
		return nil
	}
	w := Wake{Run: Run{ID: runID}}
	var botID, bossID string
	var report sql.NullString
	err := tx.QueryRowContext(ctx,
		`SELECT b.id, b.name, t.id, t.branch, p.id, p.branch, r.status, r.reply, r.error, r.report
		 FROM runs r JOIN threads t ON t.id = r.thread_id
		 JOIN bots b ON b.id = t.bot_id AND b.removed = 0
		 JOIN threads p ON p.id = t.parent_thread_id
		 WHERE r.id = ?`, runID).
		Scan(&botID, &w.Helper, &w.ThreadID, &w.Branch, &bossID, &w.BossBranch,
			&w.Run.Status, &w.Run.Reply, &w.Run.Error, &report)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read helper run %s: %w", runID, err)
	}
	if report.Valid {
		if err := json.Unmarshal([]byte(report.String), &w.Run.Report); err != nil {
			return fmt.Errorf("decode report of run %s: %w", runID, err)
		}
	}
	return insertInbox(ctx, tx, bossID, botID, wake(w), "")
}

// Messages lists every message in the thread, oldest first.
func (s *Store) Messages(ctx context.Context, threadID string) ([]Message, error) {
	return s.messages(ctx,
		`SELECT m.id, m.thread_id, m.role, m.text, m.run_id, m.from_bot_id, m.kind,
            CASE WHEN m.role = 'bot' AND r.finished_at != 0 THEN r.finished_at ELSE r.started_at END
         FROM messages m JOIN runs r ON r.id = m.run_id WHERE m.thread_id = ? ORDER BY m.rowid`,
		threadID)
}

// MessagePage lists a page of messages, oldest first. An empty cursor returns the newest page.
func (s *Store) MessagePage(ctx context.Context, threadID, before, after string) ([]Message, error) {
	if after != "" {
		return s.messages(ctx,
			`SELECT m.id, m.thread_id, m.role, m.text, m.run_id, m.from_bot_id, m.kind,
                CASE WHEN m.role = 'bot' AND r.finished_at != 0 THEN r.finished_at ELSE r.started_at END
             FROM messages m JOIN runs r ON r.id = m.run_id
             WHERE m.thread_id = ? AND m.rowid > (
                 SELECT rowid FROM messages WHERE id = ? AND thread_id = ?
             ) ORDER BY m.rowid LIMIT `+strconv.Itoa(_messagePageSize),
			threadID, after, threadID)
	}
	return s.messages(ctx,
		`SELECT id, thread_id, role, text, run_id, from_bot_id, kind, at FROM (
            SELECT m.id, m.thread_id, m.role, m.text, m.run_id, m.from_bot_id, m.kind,
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
	msgs, err := query(ctx, s.db, querySQL,
		func(r *sql.Rows) (m Message, err error) {
			var at int64
			err = r.Scan(&m.ID, &m.ThreadID, &m.Role, &m.Text, &m.RunID, &m.FromBotID, &m.Kind, &at)
			if at != 0 {
				m.CreatedAt = time.UnixMicro(at).UTC()
			}
			m.Attachments = []Attachment{}
			return m, err
		}, args...)
	if err != nil || len(msgs) == 0 {
		return msgs, err
	}
	ids := make([]string, len(msgs))
	byID := make(map[string]*Message, len(msgs))
	for i := range msgs {
		ids[i] = msgs[i].ID
		byID[msgs[i].ID] = &msgs[i]
	}
	list, err := json.Marshal(ids)
	if err != nil {
		return nil, fmt.Errorf("encode message ids: %w", err)
	}
	type row struct {
		messageID string
		Attachment
	}
	rows, err := query(ctx, s.db,
		`SELECT message_id, id, mime FROM attachments
         WHERE message_id IN (SELECT value FROM json_each(?)) ORDER BY rowid`,
		func(r *sql.Rows) (a row, err error) {
			return a, r.Scan(&a.messageID, &a.ID, &a.Mime)
		}, string(list))
	if err != nil {
		return nil, fmt.Errorf("list attachments: %w", err)
	}
	for _, a := range rows {
		m := byID[a.messageID]
		m.Attachments = append(m.Attachments, a.Attachment)
	}
	return msgs, nil
}

// Attachment returns the image with id. It wraps ErrNotFound for an unknown id.
func (s *Store) Attachment(ctx context.Context, id string) (Attachment, error) {
	a := Attachment{ID: id}
	err := s.db.QueryRowContext(ctx, "SELECT mime, data FROM attachments WHERE id = ?", id).
		Scan(&a.Mime, &a.Data)
	if errors.Is(err, sql.ErrNoRows) {
		return Attachment{}, fmt.Errorf("attachment %s: %w", id, ErrNotFound)
	}
	if err != nil {
		return Attachment{}, fmt.Errorf("read attachment %s: %w", id, err)
	}
	return a, nil
}

func (s *Store) tx(ctx context.Context, fn func(*sql.Tx) error) error {
	if tx, ok := s.db.(*sql.Tx); ok {
		return fn(tx)
	}
	tx, err := s.root.BeginTx(ctx, nil)
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

// Begin starts a transaction that takes the write lock at once. Call Rollback when done; it does
// nothing after Commit.
func (s *Store) Begin(ctx context.Context) (*Tx, error) {
	tx, err := s.root.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin: %w", err)
	}
	return &Tx{Store: &Store{db: tx, root: s.root}, tx: tx}, nil
}

// Commit makes the writes made through t.Store permanent.
func (t *Tx) Commit() error {
	if err := t.tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

// Rollback drops the writes made through t.Store.
func (t *Tx) Rollback() { _ = t.tx.Rollback() }

// FirstWords is the title a thread's first message gives it: the first six words, without the
// punctuation that ends the last one.
func FirstWords(text string) string {
	words := strings.Fields(text)
	words = words[:min(6, len(words))]
	if len(words) > 0 {
		words[len(words)-1] = strings.TrimRight(words[len(words)-1], ".,;:!?")
	}
	return strings.Join(words, " ")
}

func insertInbox(ctx context.Context, db execer, threadID, fromBotID, text, kind string) error {
	_, err := db.ExecContext(ctx,
		"INSERT INTO inbox (id, thread_id, from_bot_id, text, kind, created) VALUES (?, ?, ?, ?, ?, ?)",
		newID(), threadID, fromBotID, text, kind, time.Now().UTC().UnixMicro())
	if err != nil {
		return fmt.Errorf("enqueue message: %w", err)
	}
	return nil
}

func insertMessage(ctx context.Context, tx *sql.Tx, m Message) error {
	_, err := tx.ExecContext(ctx,
		"INSERT INTO messages (id, thread_id, role, text, run_id, from_bot_id, kind) VALUES (?, ?, ?, ?, ?, ?, ?)",
		m.ID, m.ThreadID, m.Role, m.Text, m.RunID, m.FromBotID, m.Kind)
	if err != nil {
		return fmt.Errorf("insert message: %w", err)
	}
	for _, a := range m.Attachments {
		if _, err := tx.ExecContext(ctx,
			"INSERT INTO attachments (id, message_id, mime, data, created_at) VALUES (?, ?, ?, ?, ?)",
			a.ID, m.ID, a.Mime, a.Data, time.Now().UTC().UnixMicro()); err != nil {
			return fmt.Errorf("insert attachment: %w", err)
		}
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
	var removed, wake int
	var closedAt sql.NullInt64
	err := r.Scan(&t.ID, &t.BotID, &t.Session, &t.RepoID, &t.Branch, &t.Worktree, &t.MRURL,
		&t.MRState, &closed, &created, &lastActivity, &t.MRTitle, &removed, &closedAt, &t.SnapshotRef,
		&t.Title, &t.ParentThreadID, &t.SessionProvider, &t.CheckoutGitDir, &t.WorktreeGitDir, &t.MRCI, &t.MRWoke, &wake)
	if err != nil {
		return Thread{}, err
	}
	t.Closed = closed != 0
	t.WorktreeRemoved = removed != 0
	t.MRWake = wake != 0
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
	var conns, skills, mcp, delegates string
	err := r.Scan(&b.ID, &b.Name, &b.Persona, &b.Provider, &access, &conns, &skills,
		&b.CanManagePlatform, &b.Look, &b.Model, &mcp, &b.Mode, &delegates, &b.Pinned, &b.GroupID)
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
	if err := json.Unmarshal([]byte(mcp), &b.MCPServers); err != nil {
		return Bot{}, fmt.Errorf("decode MCP servers of bot %s: %w", b.ID, err)
	}
	if err := json.Unmarshal([]byte(delegates), &b.Delegates); err != nil {
		return Bot{}, fmt.Errorf("decode delegates of bot %s: %w", b.ID, err)
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

type dbConn interface {
	execer
	queryer
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
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
