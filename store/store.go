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
	"maps"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/zalando/go-keyring"
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
	// ErrSent is returned when a queued message has already been delivered.
	ErrSent = errors.New("already sent")
	// ErrNoneHeld is returned when every held message of a run to start was removed meanwhile.
	ErrNoneHeld = errors.New("no held message is left")
	// ErrNoRerun is returned when a run cannot be continued: it did not fail, is not its thread's
	// newest or has no message of the person's.
	ErrNoRerun = errors.New("run cannot be continued")
	// ErrEmpty is returned when a message would have neither text nor an image.
	ErrEmpty = errors.New("text or an image is required")
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

// ErrorNeedsSignIn is the error kind of a run that failed because a GitHub or GitLab connection
// needs sign-in; its error is Reef's own plain sentence.
const ErrorNeedsSignIn = "needs_sign_in"

// ErrorProviderOff is the error kind of a run whose provider is not installed or is turned off.
const ErrorProviderOff = "provider_off"

// ErrorFolderGone is the error kind of a run whose thread's worktree folder was deleted; its error
// is Reef's own plain sentence with the folder and the next step.
const ErrorFolderGone = "folder_gone"

// ErrorSecretStore is the error kind of a run that failed because a secret could not be read.
const ErrorSecretStore = "secret_store"

// ErrSecretStore is wrapped when the OS secret store cannot be read; a secret that is simply
// missing does not wrap it.
var ErrSecretStore = errors.New("secret store unreachable")

// SecretReadError marks a failed secret read as an unreachable store, unless the secret is only
// missing.
func SecretReadError(err error) error {
	if err == nil || errors.Is(err, keyring.ErrNotFound) {
		return err
	}
	return fmt.Errorf("%w: %w", ErrSecretStore, err)
}

// ErrorRateLimited is the error kind of a run a provider's usage limit stopped.
const ErrorRateLimited = "rate_limited"

// ErrorStuck is the error kind of a run the idle limit stopped.
const ErrorStuck = "stuck"

// ErrorRestarted is the error kind of a run the server's restart cut short.
const ErrorRestarted = "restarted"

// Notices: what a held Reef notice is, kept in the inbox apart from its kind, which the chat and the
// prompt read. The background guard reads the failures among them.
const (
	NoticeHelperDone    = "helper_done"
	NoticeHelperFailed  = "helper_failed"
	NoticeHelperStopped = "helper_stopped"
	NoticeHelperLimited = "helper_limited"
	NoticeHelperStuck   = "helper_stuck"
	NoticeHelperDeleted = "helper_deleted"
	NoticeRestart       = "restart"
	NoticeCIPassed      = "ci_passed"
	NoticeCIFailed      = "ci_failed"
	NoticeMerged        = "merged"
	NoticeClosed        = "closed"
	// NoticeRelease marks a boss's continue: it ends the hold of a stopped helper thread.
	NoticeRelease = "release"
)

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

// _islandSeenKey is the setting that holds the island's seen time, in microseconds.
const _islandSeenKey = "island_seen_at"

// _titlesCleanedKey is the setting that marks the one cleaning of the titles stored before it.
const _titlesCleanedKey = "titles_cleaned"

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
		Colour            string     `json:"colour,omitempty"`
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
		// GroupID is the sidebar group, empty when Ungrouped. Only SetGroup and PlaceBot write it.
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
		// SignedOutAt is when the connection went needs_sign_in; nil for any other status.
		SignedOutAt *time.Time `json:"signed_out_at,omitempty"`
	}

	// Thread is a chat with one Bot. Session is the provider session its runs resume; empty
	// until the first run starts one. SessionCostUSD is what that session has cost in total as its
	// last run reported it. The repo, branch and worktree fields are empty until set.
	Thread struct {
		ID              string     `json:"id"`
		BotID           string     `json:"bot_id"`
		ParentThreadID  string     `json:"parent_thread_id"`
		Title           string     `json:"title"`
		Session         string     `json:"-"`
		SessionProvider string     `json:"-"`
		SessionCostUSD  float64    `json:"-"`
		RepoID          string     `json:"repo_id"`
		Branch          string     `json:"branch"`
		Worktree        string     `json:"worktree"`
		CheckoutGitDir  string     `json:"-"`
		WorktreeGitDir  string     `json:"-"`
		WorktreeRemoved bool       `json:"worktree_removed"`
		SnapshotRef     string     `json:"snapshot_ref"`
		ClosedAt        *time.Time `json:"closed_at"`
		Closed          bool       `json:"closed"`
		Pinned          bool       `json:"pinned"`
		// HoldsReports is set by the person's Stop on the thread's run: helper notices wait for the
		// person's next message. A stopped helper thread holds all its messages for its boss's next
		// continue.
		HoldsReports bool      `json:"holds_reports"`
		Created      time.Time `json:"created_at"`
		LastActivity time.Time `json:"last_activity"`
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
		// Queued is set on a message that waits for the thread's run to end.
		Queued bool `json:"queued,omitempty"`
		// Attachments are the message's images. Data is set only on a message just stored.
		Attachments []Attachment `json:"attachments"`
		// NotRead is set on a user message whose run failed because a secret could not be read.
		NotRead bool `json:"not_read"`
		// RunStatus is the status of the message's run.
		RunStatus string `json:"run_status"`
		// Answered are the approvals this message denied by replying.
		Answered []AnsweredApproval `json:"answered_approvals,omitempty"`
	}

	// AnsweredApproval is an approval a reply denied.
	AnsweredApproval struct {
		ID    string          `json:"id"`
		Tool  string          `json:"tool"`
		Input json.RawMessage `json:"input"`
	}

	// Attachment is an image sent with a message.
	Attachment struct {
		ID   string `json:"id"`
		Mime string `json:"mime"`
		Data []byte `json:"-"`
	}

	// InboxItem is a held message. Queued ones are listed in the thread's queue: a person's
	// (FromUser) and a boss's continue.
	InboxItem struct {
		ID        string
		ThreadID  string
		FromBotID string
		Text      string
		Queued    bool
		FromUser  bool
		CreatedAt time.Time
		Kind      string
		Notice    string
		Images    []Attachment
		// HelperThread is the thread of the helper whose run a held wake reports; its text ends
		// where the helpers line goes.
		HelperThread string
	}

	// PR is one pull request or MR a thread tracks, as the last poll read it.
	PR struct {
		ThreadID, Woke, Head string
		Wake                 bool
		URL                  string `json:"url"`
		State                string `json:"state"`
		Title                string `json:"title"`
		CI                   string `json:"ci"`
	}

	// WatchedPR is a tracked PR with the thread that tracks it.
	WatchedPR struct {
		Thread Thread
		PR     PR
	}

	// MRRead is what one poll of a PR changes: the values to store, and the wake they report.
	// Prev is the state the poll started from.
	MRRead struct {
		URL, Prev, State, Title, CI, Woke, Head string
		// Quiet stores the wake as a message in the thread, with no inbox row and no run.
		Quiet bool
		// Label names the PR as its wakes do ("PR #7"). DropCI deletes that PR's held ci wake: its
		// commit is no longer the head or the PR is done.
		DropCI   bool
		Label    string
		WakeKind string
		WakeText string
		Notice   string
	}

	// Run is one message handled.
	Run struct {
		ID        string `json:"id"`
		ThreadID  string `json:"thread_id"`
		Status    string `json:"status"`
		Reply     string `json:"reply"`
		Error     string `json:"error"`
		ErrorKind string `json:"error_kind"`
		// ErrorDetail is the raw error behind a plain Error; empty when Error is the raw one.
		ErrorDetail string         `json:"error_detail"`
		ResetsAt    string         `json:"resets_at"`
		Provider    string         `json:"provider"`
		StartedAt   time.Time      `json:"started_at"`
		FinishedAt  time.Time      `json:"finished_at"`
		Tools       map[string]int `json:"tools"`
		Usage       *Usage         `json:"usage"`
		// Streamed is set once the run streamed model output: a text or a tool call.
		Streamed bool `json:"streamed"`
	}

	// Usage is what one run used; it counts that run only. CostUSD is nil when the provider
	// reports no price.
	Usage struct {
		Model            string   `json:"model"`
		Calls            int      `json:"calls"`
		InputTokens      int      `json:"input_tokens"`
		OutputTokens     int      `json:"output_tokens"`
		CacheReadTokens  int      `json:"cache_read_tokens"`
		CacheWriteTokens int      `json:"cache_write_tokens"`
		CostUSD          *float64 `json:"cost_usd,omitempty"`
	}
)

// TitleSource says how a title reached Reef.
type TitleSource uint8

// The title sources CleanTitle tells apart.
const (
	TitleGenerated TitleSource = iota
	TitleFirstWords
	TitleStored
)

const (
	_botColumns = "id, name, persona, provider, repo_access, connections, skills, " +
		"can_manage_platform, look, model, mcp_servers, mode, delegates, colour"
	_insertBot = "INSERT INTO bots (" + _botColumns + ", position) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, " +
		"(SELECT coalesce(max(position), 0) + 1 FROM bots))"
	_threadColumns   = "id, bot_id, session, repo_id, branch, worktree, closed, created, last_activity, worktree_removed, closed_at, snapshot_ref, title, parent_thread_id, session_provider, checkout_git_dir, worktree_git_dir, session_cost_usd, pinned_at IS NOT NULL, holds_reports"
	_messagePageSize = 50
)

// Looks are the creatures a Bot can be, and Colours the colours it can take.
var (
	Looks   = []string{"jellyfish", "octopus", "whale", "starfish", "sponge", "coral", "turtle", "clam", "seal"}
	Colours = []string{"brown", "orange", "amber", "green", "teal", "sky", "blue", "purple", "pink"}
)

var (
	_lookColours = map[string]string{
		"jellyfish": "orange", "octopus": "purple", "whale": "blue", "starfish": "amber", "sponge": "brown",
		"coral": "pink", "turtle": "teal", "clam": "green", "seal": "sky",
	}
	// _movedLooks are the creatures the design dropped, each to the nearest one kept.
	_movedLooks = map[string]string{
		"shark": "whale", "manta": "whale", "penguin": "seal", "anglerfish": "octopus", "pufferfish": "starfish",
		"crab": "coral", "scallop": "clam", "narwhal": "turtle", "sand_dollar": "seal", "kelp": "turtle",
		"shell": "octopus", "wave": "whale", "island": "seal", "sun": "starfish", "sailboat": "seal",
		"seahorse": "starfish", "otter": "seal", "clownfish": "clam",
	}
	// _oldColours keep a Bot that had no colour of its own close to the colour its old look drew.
	_oldColours = map[string]string{
		"jellyfish": "orange", "whale": "blue", "shark": "purple", "penguin": "teal", "anglerfish": "purple",
		"pufferfish": "amber", "octopus": "pink", "crab": "orange", "seal": "amber", "manta": "sky",
		"scallop": "amber", "narwhal": "green",
	}
)

// LookColour is the colour a Bot of look takes when it has none of its own.
func LookColour(look string) string {
	if c, ok := _lookColours[look]; ok {
		return c
	}
	return "orange"
}

// retireLooks moves Bots off dropped looks and gives every Bot without a colour one.
func retireLooks() string {
	sqlCase := func(expr string, m map[string]string, orElse string) string {
		keys := slices.Sorted(maps.Keys(m))
		var b strings.Builder
		b.WriteString("CASE " + expr)
		for _, k := range keys {
			fmt.Fprintf(&b, " WHEN '%s' THEN '%s'", k, m[k])
		}
		return b.String() + " ELSE " + orElse + " END"
	}
	moved := sqlCase("look", _movedLooks, "look")
	keys := slices.Sorted(maps.Keys(_movedLooks))
	return "UPDATE bots SET colour = CASE WHEN colour != '' THEN colour ELSE " +
		sqlCase("look", _oldColours, sqlCase(moved, _lookColours, "'orange'")) + " END, " +
		"look = " + moved + " WHERE colour = '' OR look IN ('" + strings.Join(keys, "', '") + "')"
}

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
	if _, err := db.Exec(retireLooks()); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("retire looks: %w", err)
	}
	if err := cleanStoredTitles(context.Background(), db); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("clean titles: %w", err)
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
	_, err = s.db.ExecContext(ctx, _insertBot, args...)
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

// Bots lists every Bot: Chief, then the pinned Bots, then the rest, each in its position.
func (s *Store) Bots(ctx context.Context) ([]Bot, error) {
	return query(ctx, s.db,
		"SELECT "+_botColumns+", pinned_at IS NOT NULL, coalesce(group_id, '') FROM bots "+
			"WHERE removed = 0 "+
			"ORDER BY id = 'chief' DESC, pinned_at IS NULL, position, rowid",
		scanBot)
}

// BotCosts24h is what each Bot's runs that ended in the last 24 hours cost, by Bot id; a Bot with
// no priced run is absent.
func (s *Store) BotCosts24h(ctx context.Context) (map[string]float64, error) {
	rows, err := query(ctx, s.db,
		`SELECT t.bot_id, sum(r.cost_usd) FROM runs r JOIN threads t ON t.id = r.thread_id
		 WHERE r.cost_usd IS NOT NULL AND r.finished_at >= ? GROUP BY t.bot_id`,
		func(r *sql.Rows) (botCost, error) {
			var c botCost
			return c, r.Scan(&c.id, &c.usd)
		}, time.Now().Add(-24*time.Hour).UnixMicro())
	if err != nil {
		return nil, fmt.Errorf("read bot costs: %w", err)
	}
	costs := make(map[string]float64, len(rows))
	for _, c := range rows {
		costs[c.id] = c.usd
	}
	return costs, nil
}

type botCost struct {
	id  string
	usd float64
}

// SetPinned pins the Bot with id, or unpins it. Pinning puts it last in Pinned; pinning a pinned
// Bot keeps its place.
func (s *Store) SetPinned(ctx context.Context, id string, pinned bool) error {
	stmt, args := "UPDATE bots SET pinned_at = NULL WHERE id = ? AND removed = 0", []any{id}
	if pinned {
		stmt = "UPDATE bots SET pinned_at = coalesce(pinned_at, ?), position = CASE WHEN pinned_at IS NULL " +
			"THEN (SELECT coalesce(max(position), 0) + 1 FROM bots) ELSE position END " +
			"WHERE id = ? AND removed = 0"
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
			if err := insertInbox(ctx, tx, w.ThreadID, w.FromBotID, w.Text, "", w.Notice); err != nil {
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

// IslandSeenAt is when the user last read the island's Earlier list. Migration 022 starts it at the
// time the file first ran that version.
func (s *Store) IslandSeenAt(ctx context.Context) (time.Time, error) {
	value, err := s.Setting(ctx, _islandSeenKey)
	if err != nil {
		return time.Time{}, err
	}
	micros, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return time.Time{}, fmt.Errorf("read setting %s: %w", _islandSeenKey, err)
	}
	return time.UnixMicro(micros).UTC(), nil
}

// MarkIslandSeen sets the island's seen time to now.
func (s *Store) MarkIslandSeen(ctx context.Context) error {
	return s.SetSetting(ctx, _islandSeenKey, strconv.FormatInt(time.Now().UTC().UnixMicro(), 10))
}

// ThreadEvent is something that happened to a thread at At: a run that ended (RunID, Status,
// Error), or an MR that opened (URL, Title).
type ThreadEvent struct {
	ThreadID string
	RunID    string
	At       time.Time
	Status   string
	Error    string
	URL      string
	Title    string
}

// FinishedRunsSince lists, for each thread that is still open, its newest ended run when it ended
// after since and after the user last opened the thread.
func (s *Store) FinishedRunsSince(ctx context.Context, since time.Time) ([]ThreadEvent, error) {
	return query(ctx, s.db,
		`SELECT r.thread_id, r.id, r.finished_at, r.status, r.error
		 FROM runs r JOIN threads t ON t.id = r.thread_id
		 WHERE r.finished_at > ? AND r.finished_at > t.last_opened_at AND t.closed = 0
		   AND r.rowid = (SELECT r2.rowid FROM runs r2
		                  WHERE r2.thread_id = r.thread_id AND r2.status IN (?, ?, ?)
		                    AND (r2.status != ? OR EXISTS (SELECT 1 FROM messages m
		                         WHERE m.thread_id = r2.thread_id AND m.run_id = r2.id AND m.role = ?))
		                  ORDER BY r2.finished_at DESC, r2.rowid DESC LIMIT 1)`,
		func(r *sql.Rows) (e ThreadEvent, err error) {
			var at int64
			err = r.Scan(&e.ThreadID, &e.RunID, &at, &e.Status, &e.Error)
			e.At = time.UnixMicro(at).UTC()
			return e, err
		}, since.UnixMicro(), StatusDone, StatusFailed, StatusStopped, StatusDone, RoleBot)
}

// ToldSince lists, for each open thread whose run goes on, that run when it stored a message of the
// Bot after since and after the user last opened the thread, with the time of the newest.
func (s *Store) ToldSince(ctx context.Context, since time.Time) ([]ThreadEvent, error) {
	return query(ctx, s.db,
		`SELECT r.thread_id, r.id, max(m.created)
		 FROM runs r JOIN threads t ON t.id = r.thread_id JOIN messages m ON m.run_id = r.id AND m.role = ?
		 WHERE r.status IN (?, ?) AND t.closed = 0 AND m.created > ? AND m.created > t.last_opened_at
		 GROUP BY r.id`,
		func(r *sql.Rows) (e ThreadEvent, err error) {
			var at int64
			err = r.Scan(&e.ThreadID, &e.RunID, &at)
			e.At = time.UnixMicro(at).UTC()
			return e, err
		}, RoleBot, StatusRunning, StatusWaiting, since.UnixMicro())
}

// OpenedMRsSince lists the PRs that are open, became so after since and have not been dismissed
// from the island since.
func (s *Store) OpenedMRsSince(ctx context.Context, since time.Time) ([]ThreadEvent, error) {
	return query(ctx, s.db,
		`SELECT p.thread_id, p.changed_at, p.url, p.title FROM thread_prs p
		 LEFT JOIN pr_dismissals d ON d.thread_id = p.thread_id AND d.url = p.url
		 WHERE p.state = 'opened' AND p.changed_at > ? AND p.changed_at > coalesce(d.dismissed_at, 0)`,
		func(r *sql.Rows) (e ThreadEvent, err error) {
			var at int64
			err = r.Scan(&e.ThreadID, &at, &e.URL, &e.Title)
			e.At = time.UnixMicro(at).UTC()
			return e, err
		}, since.UnixMicro())
}

// DismissMR hides the PR at url from the island until the PR changes again.
func (s *Store) DismissMR(ctx context.Context, url string) error {
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO pr_dismissals (thread_id, url, dismissed_at) SELECT thread_id, url, ? FROM thread_prs WHERE url = ?
		 ON CONFLICT (thread_id, url) DO UPDATE SET dismissed_at = excluded.dismissed_at`,
		time.Now().UTC().UnixMicro(), url)
	if err != nil {
		return fmt.Errorf("dismiss mr: %w", err)
	}
	return found(res, "PR", url)
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
		"SELECT id, kind, instance_url, status, signed_out_at FROM connections WHERE id = ?", scanConnection, id)
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
		"SELECT id, kind, instance_url, status, signed_out_at FROM connections ORDER BY rowid", scanConnection)
}

// SetConnectionStatus sets the status of the connection with id. Going needs_sign_in records the
// time; staying there keeps the first one.
func (s *Store) SetConnectionStatus(ctx context.Context, id, status string) error {
	res, err := s.db.ExecContext(ctx, `UPDATE connections SET status = ?1,
		signed_out_at = CASE WHEN ?1 != ?2 THEN NULL WHEN status = ?2 THEN signed_out_at ELSE ?3 END
		WHERE id = ?4`, status, ConnectionNeedsSignIn, time.Now().UTC().UnixMicro(), id)
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

// ThreadIDsOnRepo lists the ids of the open threads on the repo repoID.
func (s *Store) ThreadIDsOnRepo(ctx context.Context, repoID string) ([]string, error) {
	return query(ctx, s.db, "SELECT id FROM threads WHERE repo_id = ? AND closed = 0 ORDER BY rowid",
		func(r *sql.Rows) (string, error) {
			var id string
			return id, r.Scan(&id)
		}, repoID)
}

// HelperThreads lists the threads that help parentID, oldest first.
func (s *Store) HelperThreads(ctx context.Context, parentID string) ([]Thread, error) {
	return query(ctx, s.db,
		"SELECT "+_threadColumns+" FROM threads WHERE parent_thread_id = ? ORDER BY rowid",
		scanThread, parentID)
}

// Watched lists the opened PRs of the open threads that have a repo: the ones that are polled, with
// their thread. A merged or closed PR is final and is not read again.
func (s *Store) Watched(ctx context.Context) ([]WatchedPR, error) {
	threads, err := query(ctx, s.db,
		"SELECT "+_threadColumns+" FROM threads WHERE closed = 0 AND repo_id != '' AND id IN (SELECT thread_id FROM thread_prs) ORDER BY rowid",
		scanThread)
	if err != nil {
		return nil, err
	}
	prs, err := s.AllPRs(ctx)
	if err != nil {
		return nil, err
	}
	var out []WatchedPR
	for _, t := range threads {
		for _, pr := range prs[t.ID] {
			if pr.State == "opened" {
				out = append(out, WatchedPR{Thread: t, PR: pr})
			}
		}
	}
	return out, nil
}

// Done lists the threads with a worktree whose PRs are all merged or closed, at least one merged.
func (s *Store) Done(ctx context.Context) ([]Thread, error) {
	return query(ctx, s.db,
		"SELECT "+_threadColumns+" FROM threads WHERE worktree != '' AND id IN (SELECT thread_id FROM thread_prs WHERE state = 'merged')"+
			" AND id NOT IN (SELECT thread_id FROM thread_prs WHERE state = 'opened') ORDER BY rowid",
		scanThread)
}

const _prColumns = "thread_id, url, state, title, ci, woke, wake, head"

func scanPR(r *sql.Rows) (pr PR, err error) {
	err = r.Scan(&pr.ThreadID, &pr.URL, &pr.State, &pr.Title, &pr.CI, &pr.Woke, &pr.Wake, &pr.Head)
	return pr, err
}

// PRs lists the PRs the thread tracks, in the order tracked.
func (s *Store) PRs(ctx context.Context, threadID string) ([]PR, error) {
	return query(ctx, s.db,
		"SELECT "+_prColumns+" FROM thread_prs WHERE thread_id = ? ORDER BY tracked_at, rowid", scanPR, threadID)
}

// AllPRs lists every thread's PRs, in the order tracked, by thread.
func (s *Store) AllPRs(ctx context.Context) (map[string][]PR, error) {
	prs, err := query(ctx, s.db,
		"SELECT "+_prColumns+" FROM thread_prs ORDER BY tracked_at, rowid", scanPR)
	if err != nil {
		return nil, err
	}
	out := map[string][]PR{}
	for _, pr := range prs {
		out[pr.ThreadID] = append(out[pr.ThreadID], pr)
	}
	return out, nil
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

// SetMR adds the PR at url to the thread's, opened, and returns the state the row holds. A URL the
// thread already tracks keeps its row as it is, so a re-track after a merge never wakes twice.
func (s *Store) SetMR(ctx context.Context, threadID, url, state, title string) (string, error) {
	var kept string
	now := time.Now().UTC().UnixMicro()
	err := s.db.QueryRowContext(ctx, `INSERT INTO thread_prs (thread_id, url, state, title, wake, changed_at, tracked_at)
		SELECT id, ?2, ?3, ?4, 1, ?5, ?5 FROM threads WHERE id = ?1
		ON CONFLICT (thread_id, url) DO UPDATE SET url = url RETURNING state`,
		threadID, url, state, title, now).Scan(&kept)
	if errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("thread %s: %w", threadID, ErrNotFound)
	}
	if err != nil {
		return "", fmt.Errorf("set MR: %w", err)
	}
	return kept, nil
}

// StoreMR stores what a poll read and, with it, the wake it reports, in one transaction: a restart
// cannot repeat the wake or lose it. It reports false and stores nothing when the thread is
// closed, does not track the PR or no longer has the state the poll started from.
func (s *Store) StoreMR(ctx context.Context, threadID string, r MRRead) (bool, error) {
	stored := false
	err := s.tx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `UPDATE thread_prs SET state = ?, title = ?, ci = ?, woke = ?, head = ?, wake = 1,
			changed_at = CASE WHEN state = ? THEN changed_at ELSE ? END
			WHERE thread_id = ? AND url = ? AND state = ? AND thread_id IN (SELECT id FROM threads WHERE closed = 0)`,
			r.State, r.Title, r.CI, r.Woke, r.Head, r.State, time.Now().UTC().UnixMicro(),
			threadID, r.URL, r.Prev)
		if err != nil {
			return fmt.Errorf("store MR: %w", err)
		}
		if n, err := res.RowsAffected(); err != nil || n == 0 {
			return err
		}
		stored = true
		if r.DropCI || r.WakeKind == "ci" {
			if _, err := tx.ExecContext(ctx, "DELETE FROM inbox WHERE thread_id = ? AND kind = 'ci' AND text LIKE ?",
				threadID, "CI finished on "+r.Label+":%"); err != nil {
				return fmt.Errorf("drop held ci wake: %w", err)
			}
		}
		switch {
		case r.WakeKind == "":
			return nil
		case r.Quiet:
			return insertQuietLine(ctx, tx, threadID, r.WakeText, r.WakeKind)
		}
		return insertInbox(ctx, tx, threadID, "", r.WakeText, r.WakeKind, r.Notice)
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
// Unseen is set when the thread was heard from after the user last opened it and has no live run:
// a reply, a tell_user message or a guard post sets the thread's last heard; a run that ends done
// with no reply, a stop and a quiet background run do not. Failed is set when a run failed after the
// user last opened it. LimitedUntil is when the provider's usage limit, which the thread's newest
// finished run hit, resets; zero when it has none.
type ThreadRun struct {
	BotID          string
	ThreadID       string
	ParentThreadID string
	Title          string
	Branch         string
	PRs            []PR
	Closed         bool
	Pinned         bool
	LastActivity   time.Time
	Unseen         bool
	Failed         bool
	LimitedUntil   time.Time
	// Merged is set for an open thread of the person's whose PR merged after the person's last
	// message in it.
	Merged bool
	Status string
	Reply  string
}

// latestRun is the SQL for the newest run of the thread threadID names that the person did not
// miss: a done run that posted nothing to the chat is a quiet one and does not count.
func latestRun(threadID string) string {
	return `(SELECT max(q.rowid) FROM runs q WHERE q.thread_id = ` + threadID + `
		   AND (q.status != 'done' OR EXISTS (SELECT 1 FROM messages m
		        WHERE m.thread_id = q.thread_id AND m.run_id = q.id AND m.role = 'bot')))`
}

// ThreadRuns lists every thread with its newest run (see latestRun), newest activity first. botID limits it to
// one Bot; empty means every Bot.
func (s *Store) ThreadRuns(ctx context.Context, botID string) ([]ThreadRun, error) {
	return s.threadRuns(ctx,
		`polled AS MATERIALIZED (
		   SELECT t.id AS thread_id, r.status, substr(r.reply, 1, 300) AS reply FROM threads t
		   JOIN runs r ON r.rowid = `+latestRun("t.id")+`)`,
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
		   JOIN runs r ON r.rowid = `+latestRun("n.thread_id")+`)`,
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
		 SELECT t.bot_id, t.id, t.parent_thread_id, t.title, t.branch, t.closed, t.pinned_at IS NOT NULL,
		        t.last_activity,
		        t.last_heard > t.last_opened_at AND t.last_heard > t.created
		          AND coalesce(p.status, '') NOT IN ('running', 'waiting'),
		        t.last_failed > t.last_opened_at, t.limited_until,
		        CASE WHEN t.closed = 0 AND t.parent_thread_id = ''
		          AND EXISTS (SELECT 1 FROM thread_prs pr WHERE pr.thread_id = t.id AND pr.state = 'merged')
		          AND NOT EXISTS (SELECT 1 FROM thread_prs pr WHERE pr.thread_id = t.id AND pr.state = 'opened')
		          THEN (SELECT max(pr.changed_at) FROM thread_prs pr WHERE pr.thread_id = t.id AND pr.state = 'merged') > coalesce((SELECT max(m.created) FROM messages m
		            WHERE m.thread_id = t.id AND m.role = 'user' AND m.from_bot_id = '' AND m.kind = ''), 0)
		          ELSE 0 END,
		        coalesce(p.status, ''), coalesce(p.reply, ''),
		        (SELECT json_group_array(json_object('url', url, 'state', state, 'title', title, 'ci', ci))
		         FROM (SELECT * FROM thread_prs WHERE thread_id = t.id ORDER BY tracked_at, rowid))
		 FROM threads t
		 LEFT JOIN polled p ON p.thread_id = t.id
		 WHERE `+where+`
		 ORDER BY t.last_activity DESC, t.rowid DESC`,
		func(r *sql.Rows) (tr ThreadRun, err error) {
			var active, limited int64
			var prs string
			err = r.Scan(&tr.BotID, &tr.ThreadID, &tr.ParentThreadID, &tr.Title, &tr.Branch, &tr.Closed,
				&tr.Pinned, &active, &tr.Unseen, &tr.Failed, &limited, &tr.Merged, &tr.Status, &tr.Reply, &prs)
			if err == nil {
				err = json.Unmarshal([]byte(prs), &tr.PRs)
			}
			tr.LastActivity = time.UnixMicro(active).UTC()
			if limited != 0 {
				tr.LimitedUntil = time.UnixMicro(limited).UTC()
			}
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

// MoveGroup puts the group before the group before, or last when before is empty.
func (s *Store) MoveGroup(ctx context.Context, id, before string) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		return reposition(ctx, tx, "bot_groups", "1", id, before)
	})
}

// PlaceBot puts the Bot in Pinned, or in the group groupID (Ungrouped when empty), before the
// Bot before of that section, or last when before is empty. It wraps ErrNotFound for an unknown
// Bot, group or before.
func (s *Store) PlaceBot(ctx context.Context, id string, pinned bool, groupID, before string) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx,
			"UPDATE bots SET pinned_at = CASE WHEN ?1 THEN coalesce(pinned_at, ?2) END, "+
				"group_id = nullif(?3, '') WHERE id = ?4 AND removed = 0 "+
				"AND (?3 = '' OR EXISTS (SELECT 1 FROM bot_groups WHERE id = ?3))",
			pinned, time.Now().UTC().UnixMicro(), groupID, id)
		if err != nil {
			return fmt.Errorf("place bot: %w", err)
		}
		if err := found(res, "bot or group", id); err != nil {
			return err
		}
		if before != "" {
			var n int
			if err := tx.QueryRowContext(ctx,
				"SELECT count(*) FROM bots WHERE id = ?1 AND id != ?2 AND removed = 0 AND "+
					"CASE WHEN ?3 THEN pinned_at IS NOT NULL "+
					"ELSE pinned_at IS NULL AND coalesce(group_id, '') = ?4 END",
				before, id, pinned, groupID).Scan(&n); err != nil {
				return fmt.Errorf("read before: %w", err)
			}
			if n == 0 {
				return fmt.Errorf("bot %s in that section: %w", before, ErrNotFound)
			}
		}
		return reposition(ctx, tx, "bots", "removed = 0", id, before)
	})
}

// reposition renumbers the live rows of table so that id sits before the row before, or last
// when before is empty.
func reposition(ctx context.Context, tx *sql.Tx, table, live, id, before string) error {
	ids, err := query(ctx, tx, "SELECT id FROM "+table+" WHERE "+live+" ORDER BY position, rowid",
		func(r *sql.Rows) (id string, err error) { return id, r.Scan(&id) })
	if err != nil {
		return fmt.Errorf("list %s: %w", table, err)
	}
	if !slices.Contains(ids, id) {
		return fmt.Errorf("%s %s: %w", table, id, ErrNotFound)
	}
	ids = slices.DeleteFunc(ids, func(other string) bool { return other == id })
	i := len(ids)
	if before != "" {
		if i = slices.Index(ids, before); i < 0 {
			return fmt.Errorf("%s %s: %w", table, before, ErrNotFound)
		}
	}
	for pos, other := range slices.Insert(ids, i, id) {
		if _, err := tx.ExecContext(ctx, "UPDATE "+table+" SET position = ? WHERE id = ?",
			pos+1, other); err != nil {
			return fmt.Errorf("renumber %s: %w", table, err)
		}
	}
	return nil
}

// DeleteGroup removes the group; its Bots become Ungrouped.
func (s *Store) DeleteGroup(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, "DELETE FROM bot_groups WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("delete group: %w", err)
	}
	return found(res, "group", id)
}

// SetGroup puts the Bot in the group groupID, or in Ungrouped when groupID is empty; an unpinned
// Bot goes last there. It wraps
// ErrNotFound for an unknown group.
func (s *Store) SetGroup(ctx context.Context, botID, groupID string) error {
	res, err := s.db.ExecContext(ctx,
		"UPDATE bots SET group_id = nullif(?1, ''), position = CASE WHEN pinned_at IS NULL "+
			"THEN (SELECT coalesce(max(position), 0) + 1 FROM bots) ELSE position END "+
			"WHERE id = ?2 AND removed = 0 "+
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

// StartRunFrom stores the items as messages of one running run, removing each held item that has
// an ID, in one transaction. A held item's message keeps its ID and its images. A held item that
// was removed meanwhile is skipped; it returns ErrNoneHeld when none is left. It returns the
// stored messages.
func (s *Store) StartRunFrom(ctx context.Context, items ...InboxItem) ([]Message, Run, error) {
	threadID := items[0].ThreadID
	r := Run{ID: newID(), ThreadID: threadID, Status: StatusRunning, StartedAt: time.Now().UTC(), Tools: map[string]int{}}
	msgs := make([]Message, len(items))
	for i, it := range items {
		id := it.ID
		if id == "" {
			id = newID()
		}
		msgs[i] = Message{ID: id, ThreadID: threadID, Role: RoleUser, Text: it.Text, RunID: r.ID,
			FromBotID: it.FromBotID, Kind: it.Kind, Attachments: []Attachment{}}
		for _, img := range it.Images {
			if img.ID == "" {
				img.ID = newID()
			}
			msgs[i].Attachments = append(msgs[i].Attachments, img)
		}
	}
	stored := make([]Message, 0, len(msgs))
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
		for i, it := range items {
			if it.ID != "" {
				res, err := tx.ExecContext(ctx, "DELETE FROM inbox WHERE id = ?", it.ID)
				if err != nil {
					return fmt.Errorf("delete inbox item: %w", err)
				}
				n, err := res.RowsAffected()
				if err != nil {
					return fmt.Errorf("count rows: %w", err)
				}
				if n == 0 {
					continue
				}
			}
			if err := insertMessage(ctx, tx, msgs[i]); err != nil {
				return err
			}
			stored = append(stored, msgs[i])
		}
		if len(stored) == 0 {
			return ErrNoneHeld
		}
		// Notices from Reef alone start a background run, which keeps the thread's place unless it
		// posts.
		released := slices.ContainsFunc(items, func(it InboxItem) bool { return it.Notice == NoticeRelease })
		notices := !slices.ContainsFunc(stored, func(m Message) bool { return m.Kind == "" && m.FromBotID == "" })
		_, err = tx.ExecContext(ctx,
			`UPDATE threads SET last_activity = CASE WHEN ?1 AND parent_thread_id = '' THEN last_activity ELSE ?2 END,
			   title = CASE WHEN title = '' THEN ?3 ELSE title END,
			   holds_reports = CASE WHEN ?1 AND NOT ?5 THEN holds_reports ELSE 0 END WHERE id = ?4`,
			notices, time.Now().UTC().UnixMicro(), FirstWords(stored[0].Text), threadID, released)
		return err
	})
	if err != nil {
		return nil, Run{}, err
	}
	return stored, r, nil
}

// Rerun starts a new run for the failed run runID and moves the run's messages onto it, so the
// thread gains no message. It returns the messages with their images' data. It returns ErrNoRerun
// unless the run failed, is its thread's newest and has a message of the person's. The new run
// starts as streamed when the failed one was, so a failed Continue offers Continue again.
func (s *Store) Rerun(ctx context.Context, runID string) ([]Message, Run, error) {
	var msgs []Message
	r := Run{ID: newID(), Status: StatusRunning, StartedAt: time.Now().UTC(), Tools: map[string]int{}}
	err := s.tx(ctx, func(tx *sql.Tx) error {
		var status, newest string
		err := tx.QueryRowContext(ctx,
			`SELECT thread_id, status, streamed, (SELECT id FROM runs WHERE thread_id = r.thread_id ORDER BY rowid DESC LIMIT 1)
			 FROM runs r WHERE id = ?`, runID).Scan(&r.ThreadID, &status, &r.Streamed, &newest)
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("run %s: %w", runID, ErrNotFound)
		}
		if err != nil {
			return fmt.Errorf("read run %s: %w", runID, err)
		}
		if status != StatusFailed || newest != runID {
			return ErrNoRerun
		}
		msgs, err = query(ctx, tx,
			"SELECT id, text, kind, from_bot_id FROM messages WHERE run_id = ? AND role = ? ORDER BY rowid",
			func(rows *sql.Rows) (m Message, err error) {
				m.ThreadID, m.Role, m.RunID = r.ThreadID, RoleUser, r.ID
				return m, rows.Scan(&m.ID, &m.Text, &m.Kind, &m.FromBotID)
			}, runID, RoleUser)
		if err != nil {
			return fmt.Errorf("list messages of run %s: %w", runID, err)
		}
		if !slices.ContainsFunc(msgs, func(m Message) bool { return m.Kind == "" && m.FromBotID == "" }) {
			return ErrNoRerun
		}
		for i := range msgs {
			msgs[i].Attachments, err = query(ctx, tx,
				"SELECT id, mime, data FROM attachments WHERE message_id = ? ORDER BY rowid",
				func(rows *sql.Rows) (a Attachment, err error) { return a, rows.Scan(&a.ID, &a.Mime, &a.Data) },
				msgs[i].ID)
			if err != nil {
				return fmt.Errorf("list attachments: %w", err)
			}
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO runs (id, thread_id, status, started_at, streamed) VALUES (?, ?, ?, ?, ?)",
			r.ID, r.ThreadID, r.Status, r.StartedAt.UnixMicro(), r.Streamed); err != nil {
			return fmt.Errorf("insert run: %w", err)
		}
		if _, err := tx.ExecContext(ctx, "UPDATE messages SET run_id = ? WHERE run_id = ? AND role = ?",
			r.ID, runID, RoleUser); err != nil {
			return fmt.Errorf("move messages: %w", err)
		}
		_, err = tx.ExecContext(ctx, "UPDATE threads SET last_activity = ?, holds_reports = 0 WHERE id = ?",
			time.Now().UTC().UnixMicro(), r.ThreadID)
		return err
	})
	if err != nil {
		return nil, Run{}, err
	}
	return msgs, r, nil
}

// MarkStreamed records that the run streamed model output.
func (s *Store) MarkStreamed(ctx context.Context, runID string) error {
	_, err := s.db.ExecContext(ctx, "UPDATE runs SET streamed = 1 WHERE id = ?", runID)
	return err
}

// AddReply stores text as the user's message in the thread, under run runID, with the approvals it
// denied.
func (s *Store) AddReply(
	ctx context.Context, threadID, runID, text string, answered []AnsweredApproval,
) (Message, error) {
	m := Message{
		ID: newID(), ThreadID: threadID, Role: RoleUser, Text: text, RunID: runID,
		CreatedAt: time.Now().UTC(), Attachments: []Attachment{}, Answered: answered,
	}
	err := s.tx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, "UPDATE threads SET last_activity = ? WHERE id = ?",
			m.CreatedAt.UnixMicro(), threadID); err != nil {
			return fmt.Errorf("touch thread: %w", err)
		}
		if err := insertMessage(ctx, tx, m); err != nil {
			return err
		}
		for _, a := range answered {
			if _, err := tx.ExecContext(ctx,
				"INSERT INTO answered_approvals (id, message_id, tool, input) VALUES (?, ?, ?, ?)",
				a.ID, m.ID, a.Tool, string(a.Input)); err != nil {
				return fmt.Errorf("insert answered approval: %w", err)
			}
		}
		return nil
	})
	if err != nil {
		return Message{}, fmt.Errorf("add reply: %w", err)
	}
	return m, nil
}

// AddBotMessage stores text as a message of the Bot in the thread, under the run runID, and marks
// the thread as heard from.
func (s *Store) AddBotMessage(ctx context.Context, threadID, runID, text string) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, "UPDATE threads SET last_activity = ?1, last_heard = ?1 WHERE id = ?2",
			time.Now().UTC().UnixMicro(), threadID)
		if err != nil {
			return fmt.Errorf("touch thread: %w", err)
		}
		if err := found(res, "thread", threadID); err != nil {
			return err
		}
		return insertMessage(ctx, tx, Message{ID: newID(), ThreadID: threadID, Role: RoleBot, Text: text, RunID: runID})
	})
}

// OldestInbox returns the oldest held message; ok is false when none.
func (s *Store) OldestInbox(ctx context.Context, threadID string) (it InboxItem, ok bool, err error) {
	items, err := s.inbox(ctx, "data", "WHERE thread_id = ? ORDER BY rowid LIMIT 1", threadID)
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

// HoldReports makes the thread hold the notices of its helpers until the person writes to it: a
// run that takes a message of the person clears it. A helper thread holds all its messages until
// a continue of its boss starts a run. Holding makes what is already held plain: only a later
// continue releases it.
func (s *Store) HoldReports(ctx context.Context, threadID string) error {
	err := s.tx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, "UPDATE threads SET holds_reports = 1 WHERE id = ?", threadID); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, "UPDATE inbox SET notice = '' WHERE thread_id = ? AND notice = ?", threadID, NoticeRelease)
		return err
	})
	if err != nil {
		return fmt.Errorf("hold reports: %w", err)
	}
	return nil
}

// HeldNotices counts the helper notices held for the thread.
func (s *Store) HeldNotices(ctx context.Context, threadID string) (n int, err error) {
	items, err := s.inbox(ctx, "NULL", "WHERE thread_id = ?", threadID)
	for _, it := range items {
		if IsHelperNotice(it) {
			n++
		}
	}
	return n, err
}

// IsHelperNotice reports whether the held message tells a boss about its helper: the notices a
// Stop on the boss holds.
func IsHelperNotice(it InboxItem) bool {
	switch it.Notice {
	case NoticeHelperDone, NoticeHelperFailed, NoticeHelperStopped, NoticeHelperLimited,
		NoticeHelperStuck, NoticeHelperDeleted, NoticeRestart:
		return true
	}
	return false
}

// InboxThreads lists the threads that have a held message.
func (s *Store) InboxThreads(ctx context.Context) ([]string, error) {
	return query(ctx, s.db, "SELECT DISTINCT thread_id FROM inbox ORDER BY thread_id",
		func(r *sql.Rows) (id string, err error) { return id, r.Scan(&id) })
}

// Run returns the run with id.
func (s *Store) Run(ctx context.Context, id string) (Run, error) {
	var r Run
	var usage sql.NullString
	var started, finished int64
	var tools string
	err := s.db.QueryRowContext(ctx,
		"SELECT id, thread_id, status, reply, error, error_kind, resets_at, provider, started_at, finished_at, tools, usage, streamed FROM runs WHERE id = ?", id).
		Scan(&r.ID, &r.ThreadID, &r.Status, &r.Reply, &r.Error, &r.ErrorKind, &r.ResetsAt, &r.Provider, &started, &finished, &tools, &usage, &r.Streamed)
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
	if usage.Valid {
		if err := json.Unmarshal([]byte(usage.String), &r.Usage); err != nil {
			return Run{}, fmt.Errorf("decode usage of run %s: %w", id, err)
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

// RunResult is how a run ended.
type RunResult struct {
	Status    string
	Reply     string
	Error     string
	ErrorKind string
	ResetsAt  time.Time // when the provider's usage limit resets, if it said
	Provider  string    // the provider the run used
	// StatusLine is set for a background run that posts nothing: its reply is the thread's status
	// line, not a message, and the thread has not been heard from.
	StatusLine bool
	// Usage is what the run used, nil when the provider reported nothing. SessionCostUSD is what
	// the session has cost in total after it, nil when the provider reports no price.
	Usage          *Usage
	SessionCostUSD *float64
}

// Wake is a finished helper run with what the boss's message is made of.
type Wake struct {
	Helper     string // the helper Bot's name
	ThreadID   string // the helper's thread
	Branch     string // the helper thread's branch
	BossBranch string // the boss thread's branch
	// RepoPath, Worktree and WorktreeGitDir place the helper's checkout; empty when it has none.
	RepoPath, Worktree, WorktreeGitDir string
	// StuckBefore: the helper thread's run before this one was stuck too.
	StuckBefore bool
	Run         Run
}

// Notice is the notice kind of the wake: how the helper's run ended.
func (w Wake) Notice() string {
	switch {
	case w.Run.Status == StatusDone:
		return NoticeHelperDone
	case w.Run.Status == StatusStopped:
		return NoticeHelperStopped
	case w.Run.ErrorKind == ErrorRateLimited:
		return NoticeHelperLimited
	case w.Run.ErrorKind == ErrorStuck:
		return NoticeHelperStuck
	}
	return NoticeHelperFailed
}

// FinishRun ends a run with res. A non-empty reply also becomes the thread's bot message, in the
// same transaction, unless it is the run's status line. When wake is not nil and the run is a helper's, the message wake returns is
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
		resetsAt := ""
		if !res.ResetsAt.IsZero() {
			resetsAt = res.ResetsAt.UTC().Format(time.RFC3339)
		}
		var usage sql.NullString
		var cost *float64
		if res.Usage != nil {
			body, err := json.Marshal(res.Usage)
			if err != nil {
				return fmt.Errorf("encode usage: %w", err)
			}
			usage, cost = sql.NullString{String: string(body), Valid: true}, res.Usage.CostUSD
		}
		_, err = tx.ExecContext(ctx,
			"UPDATE runs SET status = ?, reply = ?, error = ?, error_kind = ?, resets_at = ?, provider = ?, usage = ?, cost_usd = ?, finished_at = ? WHERE id = ?",
			res.Status, res.Reply, res.Error, res.ErrorKind, resetsAt, res.Provider, usage, cost, time.Now().UTC().UnixMicro(), runID)
		if err != nil {
			return fmt.Errorf("finish run: %w", err)
		}
		var limited *int64
		switch {
		case res.ErrorKind == ErrorRateLimited:
			until := int64(0)
			if !res.ResetsAt.IsZero() {
				until = res.ResetsAt.UnixMicro()
			}
			limited = &until
		case res.Status == StatusDone:
			limited = new(int64)
		}
		posts := res.Reply != "" && !res.StatusLine
		_, err = tx.ExecContext(ctx,
			`UPDATE threads SET last_activity = CASE WHEN ?7 THEN last_activity ELSE ?1 END,
			   session_cost_usd = coalesce(?2, session_cost_usd),
			   last_heard = CASE WHEN ?3 THEN ?1 ELSE last_heard END,
			   last_failed = CASE WHEN ?4 THEN ?1 ELSE last_failed END,
			   limited_until = coalesce(?5, limited_until) WHERE id = ?6`,
			time.Now().UTC().UnixMicro(), res.SessionCostUSD, res.Status == StatusDone && posts,
			res.Status == StatusFailed, limited, threadID, res.Status == StatusDone && res.StatusLine)
		if err != nil {
			return fmt.Errorf("touch thread: %w", err)
		}
		if err := enqueueWake(ctx, tx, runID, wake); err != nil {
			return err
		}
		if !posts {
			return nil
		}
		return insertMessage(ctx, tx,
			Message{ID: newID(), ThreadID: threadID, Role: RoleBot, Text: res.Reply, RunID: runID})
	})
}

// FailUnfinished marks every running or waiting run failed with reason and returns their ids. When
// wake is not nil, the failed helper runs of one boss thread hold one message for it, which wake
// builds from them in the order the runs began; it is from the helper Bot when one run failed, else
// from Reef. All in the same transaction. Called once at start: no run outlives the process that
// ran it.
func (s *Store) FailUnfinished(
	ctx context.Context, reason string, wake func([]Wake) string,
) ([]string, error) {
	var ids []string
	err := s.tx(ctx, func(tx *sql.Tx) error {
		var err error
		ids, err = query(ctx, tx, "SELECT id FROM runs WHERE status IN (?, ?) ORDER BY rowid",
			func(r *sql.Rows) (id string, err error) { return id, r.Scan(&id) },
			StatusRunning, StatusWaiting)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx,
			"UPDATE runs SET status = ?, error = ?, error_kind = ?, finished_at = ? WHERE status IN (?, ?)",
			StatusFailed, reason, ErrorRestarted, time.Now().UTC().UnixMicro(), StatusRunning, StatusWaiting); err != nil {
			return err
		}
		now := time.Now().UTC().UnixMicro()
		for _, id := range ids {
			if _, err := tx.ExecContext(ctx,
				"UPDATE threads SET last_activity = ?, last_failed = ? WHERE id = (SELECT thread_id FROM runs WHERE id = ?)",
				now, now, id); err != nil {
				return fmt.Errorf("touch thread of run %s: %w", id, err)
			}
		}
		if wake == nil {
			return nil
		}
		var bosses []string
		byBoss := map[string][]heldWake{}
		for _, id := range ids {
			w, ok, err := readWake(ctx, tx, id)
			if err != nil {
				return err
			}
			if !ok {
				continue
			}
			if _, seen := byBoss[w.bossID]; !seen {
				bosses = append(bosses, w.bossID)
			}
			byBoss[w.bossID] = append(byBoss[w.bossID], w)
		}
		for _, bossID := range bosses {
			held := byBoss[bossID]
			wakes := make([]Wake, len(held))
			for i, w := range held {
				wakes[i] = w.Wake
			}
			from, kind, notice := "", "restart", NoticeRestart
			if len(held) == 1 {
				from, kind, notice = held[0].botID, "", held[0].Notice()
			}
			if err := insertInbox(ctx, tx, bossID, from, wake(wakes), kind, notice); err != nil {
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

type heldWake struct {
	Wake
	botID, bossID string
}

func enqueueWake(ctx context.Context, tx *sql.Tx, runID string, wake func(Wake) string) error {
	if wake == nil {
		return nil
	}
	w, ok, err := readWake(ctx, tx, runID)
	if err != nil || !ok {
		return err
	}
	if err := insertInbox(ctx, tx, w.bossID, w.botID, wake(w.Wake), "", w.Notice()); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, "UPDATE inbox SET helper_thread_id = ? WHERE rowid = last_insert_rowid()", w.ThreadID)
	return err
}

// readWake reads the wake of a helper run; ok is false for a run that is not a helper's.
func readWake(ctx context.Context, tx *sql.Tx, runID string) (w heldWake, ok bool, err error) {
	w.Run.ID = runID
	err = tx.QueryRowContext(ctx,
		`SELECT b.id, b.name, t.id, t.branch, p.id, p.branch, r.status, r.reply, r.error, r.error_kind, r.resets_at, r.provider,
		   coalesce(rp.path, ''), t.worktree, t.worktree_git_dir,
		   coalesce((SELECT q.error_kind = 'stuck' FROM runs q WHERE q.thread_id = r.thread_id AND q.rowid < r.rowid
		     ORDER BY q.rowid DESC LIMIT 1), 0)
		 FROM runs r JOIN threads t ON t.id = r.thread_id
		 LEFT JOIN repos rp ON rp.id = t.repo_id
		 JOIN bots b ON b.id = t.bot_id AND b.removed = 0
		 JOIN threads p ON p.id = t.parent_thread_id
		 WHERE r.id = ?`, runID).
		Scan(&w.botID, &w.Helper, &w.ThreadID, &w.Branch, &w.bossID, &w.BossBranch,
			&w.Run.Status, &w.Run.Reply, &w.Run.Error, &w.Run.ErrorKind, &w.Run.ResetsAt, &w.Run.Provider,
			&w.RepoPath, &w.Worktree, &w.WorktreeGitDir, &w.StuckBefore)
	if errors.Is(err, sql.ErrNoRows) {
		return w, false, nil
	}
	if err != nil {
		return w, false, fmt.Errorf("read helper run %s: %w", runID, err)
	}
	return w, true, nil
}

// Messages lists every message in the thread, oldest first.
func (s *Store) Messages(ctx context.Context, threadID string) ([]Message, error) {
	return s.messages(ctx,
		`SELECT m.id, m.thread_id, m.role, m.text, m.run_id, m.from_bot_id, m.kind,
            CASE WHEN m.kind != '' AND m.created != 0 THEN m.created WHEN m.role = 'bot' AND r.finished_at != 0 THEN r.finished_at ELSE r.started_at END,
            m.role = 'user' AND m.kind = '' AND r.error_kind = 'secret_store',
            r.status
         FROM messages m JOIN runs r ON r.id = m.run_id WHERE m.thread_id = ? ORDER BY m.rowid`,
		threadID)
}

// MessagePage lists a page of messages, oldest first. An empty cursor returns the newest page.
func (s *Store) MessagePage(ctx context.Context, threadID, before, after string) ([]Message, error) {
	if after != "" {
		return s.messages(ctx,
			`SELECT m.id, m.thread_id, m.role, m.text, m.run_id, m.from_bot_id, m.kind,
                CASE WHEN m.kind != '' AND m.created != 0 THEN m.created WHEN m.role = 'bot' AND r.finished_at != 0 THEN r.finished_at ELSE r.started_at END,
                m.role = 'user' AND m.kind = '' AND r.error_kind = 'secret_store',
                r.status
             FROM messages m JOIN runs r ON r.id = m.run_id
             WHERE m.thread_id = ? AND m.rowid > (
                 SELECT rowid FROM messages WHERE id = ? AND thread_id = ?
             ) ORDER BY m.rowid LIMIT `+strconv.Itoa(_messagePageSize),
			threadID, after, threadID)
	}
	return s.messages(ctx,
		`SELECT id, thread_id, role, text, run_id, from_bot_id, kind, at, not_read, status FROM (
            SELECT m.id, m.thread_id, m.role, m.text, m.run_id, m.from_bot_id, m.kind,
                CASE WHEN m.kind != '' AND m.created != 0 THEN m.created WHEN m.role = 'bot' AND r.finished_at != 0 THEN r.finished_at ELSE r.started_at END AS at,
                m.role = 'user' AND m.kind = '' AND r.error_kind = 'secret_store' AS not_read,
                r.status AS status,
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
			err = r.Scan(&m.ID, &m.ThreadID, &m.Role, &m.Text, &m.RunID, &m.FromBotID, &m.Kind, &at, &m.NotRead, &m.RunStatus)
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
	type answered struct {
		messageID string
		AnsweredApproval
	}
	replies, err := query(ctx, s.db,
		`SELECT message_id, id, tool, input FROM answered_approvals
         WHERE message_id IN (SELECT value FROM json_each(?)) ORDER BY rowid`,
		func(r *sql.Rows) (a answered, err error) {
			var input string
			err = r.Scan(&a.messageID, &a.ID, &a.Tool, &input)
			a.Input = json.RawMessage(input)
			return a, err
		}, string(list))
	if err != nil {
		return nil, fmt.Errorf("list answered approvals: %w", err)
	}
	for _, a := range replies {
		m := byID[a.messageID]
		m.Answered = append(m.Answered, a.AnsweredApproval)
	}
	return msgs, nil
}

// Attachment returns the image with id. It wraps ErrNotFound for an unknown id.
func (s *Store) Attachment(ctx context.Context, id string) (Attachment, error) {
	a := Attachment{ID: id}
	err := s.db.QueryRowContext(ctx,
		`SELECT mime, data FROM attachments WHERE id = ?
		 UNION ALL SELECT mime, data FROM inbox_images WHERE id = ?`, id, id).
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

// FirstWords is the title a thread's first message gives it.
func FirstWords(text string) string {
	return CleanTitle(text, TitleFirstWords)
}

// CleanTitle applies the title rules for source without changing storage or calling a provider.
func CleanTitle(text string, source TitleSource) string {
	text = strings.TrimSpace(text)
	if source != TitleFirstWords {
		text = dropTitleLabel(text)
	}
	words := strings.Fields(text)
	cut := len(words) > 6
	if source == TitleStored {
		cut = len(words) == 6
	} else if cut {
		words = words[:6]
	}
	if cut {
		for len(words) > 0 && isTitleStopWord(words[len(words)-1]) {
			words = words[:len(words)-1]
		}
	}
	if len(words) > 0 && (source != TitleStored || cut) {
		words[len(words)-1] = strings.TrimRight(words[len(words)-1], ".,;:!?")
	}
	return strings.TrimSpace(strings.Join(words, " "))
}

func dropTitleLabel(text string) string {
	for _, label := range []string{"Title:", "Conversation title:", "Thread title:", "Chat title:", "Status line:"} {
		bare := strings.TrimSuffix(label, ":")
		for _, prefix := range []string{label, "**" + label + "**", "**" + bare + "**:"} {
			if len(text) >= len(prefix) && strings.EqualFold(text[:len(prefix)], prefix) {
				return strings.TrimSpace(text[len(prefix):])
			}
		}
	}
	return text
}

func isTitleStopWord(word string) bool {
	switch strings.ToLower(word) {
	case "on", "of", "and", "to", "in", "the", "a", "an", "for", "with", "at", "by", "from", "or":
		return true
	}
	return false
}

func cleanStoredTitles(ctx context.Context, db *sql.DB) error {
	var done int
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM settings WHERE key = ?", _titlesCleanedKey).Scan(&done); err != nil {
		return err
	}
	if done > 0 {
		return nil
	}
	rows, err := db.QueryContext(ctx, "SELECT id, title FROM threads")
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	var titles []struct{ id, old, clean string }
	for rows.Next() {
		var title struct{ id, old, clean string }
		if err := rows.Scan(&title.id, &title.old); err != nil {
			return err
		}
		title.clean = CleanTitle(title.old, TitleStored)
		if title.clean != title.old {
			titles = append(titles, title)
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, title := range titles {
		if _, err := db.ExecContext(ctx, "UPDATE threads SET title = ? WHERE id = ?", title.clean, title.id); err != nil {
			return err
		}
	}
	// A file with no thread has nothing stored to clean: the marker waits for the first launch that
	// finds one.
	_, err = db.ExecContext(ctx,
		"INSERT INTO settings (key, value) SELECT ?, '1' WHERE EXISTS (SELECT 1 FROM threads)", _titlesCleanedKey)
	return err
}

func insertInbox(ctx context.Context, db execer, threadID, fromBotID, text, kind, notice string) error {
	_, err := db.ExecContext(ctx,
		"INSERT INTO inbox (id, thread_id, from_bot_id, text, kind, notice, created) VALUES (?, ?, ?, ?, ?, ?, ?)",
		newID(), threadID, fromBotID, text, kind, notice, time.Now().UTC().UnixMicro())
	if err != nil {
		return fmt.Errorf("enqueue message: %w", err)
	}
	return nil
}

// insertQuietLine adds a Reef line to the thread, after its newest run: it starts no run.
func insertQuietLine(ctx context.Context, tx *sql.Tx, threadID, text, kind string) error {
	var runID string
	err := tx.QueryRowContext(ctx, "SELECT id FROM runs WHERE thread_id = ? ORDER BY rowid DESC LIMIT 1", threadID).Scan(&runID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read latest run of thread %s: %w", threadID, err)
	}
	return insertMessage(ctx, tx, Message{ID: newID(), ThreadID: threadID, Role: RoleUser, Text: text, RunID: runID, Kind: kind})
}

func insertMessage(ctx context.Context, tx *sql.Tx, m Message) error {
	_, err := tx.ExecContext(ctx,
		"INSERT INTO messages (id, thread_id, role, text, run_id, from_bot_id, kind, created) VALUES (?, ?, ?, ?, ?, ?, ?, ?)",
		m.ID, m.ThreadID, m.Role, m.Text, m.RunID, m.FromBotID, m.Kind, time.Now().UTC().UnixMicro())
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
	var removed int
	var closedAt sql.NullInt64
	err := r.Scan(&t.ID, &t.BotID, &t.Session, &t.RepoID, &t.Branch, &t.Worktree,
		&closed, &created, &lastActivity, &removed, &closedAt, &t.SnapshotRef,
		&t.Title, &t.ParentThreadID, &t.SessionProvider, &t.CheckoutGitDir, &t.WorktreeGitDir,
		&t.SessionCostUSD, &t.Pinned, &t.HoldsReports)
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

func scanBot(r *sql.Rows) (Bot, error) { return scanBotWith(r) }

// scanBotWith also scans the columns after a Bot's into extra.
func scanBotWith(r *sql.Rows, extra ...any) (Bot, error) {
	var b Bot
	var access sql.NullString
	var conns, skills, mcp, delegates string
	err := r.Scan(append([]any{&b.ID, &b.Name, &b.Persona, &b.Provider, &access, &conns, &skills,
		&b.CanManagePlatform, &b.Look, &b.Model, &mcp, &b.Mode, &delegates, &b.Colour, &b.Pinned, &b.GroupID}, extra...)...)
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
	var signedOut sql.NullInt64
	if err := r.Scan(&c.ID, &c.Kind, &c.InstanceURL, &c.Status, &signedOut); err != nil {
		return Connection{}, err
	}
	if signedOut.Valid {
		when := time.UnixMicro(signedOut.Int64).UTC()
		c.SignedOutAt = &when
	}
	return c, nil
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
	last := 0
	for _, name := range names {
		n, err := strconv.Atoi(strings.SplitN(name, "_", 2)[0])
		if err != nil {
			return fmt.Errorf("migration %s: bad number: %w", name, err)
		}
		if n == last {
			return fmt.Errorf("migration %s: number %d is used twice", name, n)
		}
		last = n
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
