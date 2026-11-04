package run

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"atoll/connection"
	"atoll/provider"
	"atoll/repo"
	"atoll/store"
)

const (
	_settingWorktreeRoot = "worktree_root"
	_snapshotRefPrefix   = "refs/atoll/unfinished/"
	_mrOpened            = "opened"
	_mrMerged            = "merged"

	// _repoNotes goes before the prompt of a repo thread; %s is the repo name.
	_repoNotes = "You work in the repo %s, starting in a clean read-only checkout. " +
		"Before your first edit, call the start_work tool of the atoll MCP server with type " +
		"(feat, fix, chore or docs) and slug, and cd to the path it returns. " +
		"When the change is ready, commit, push, run glab mr create, call the track_mr tool of " +
		"the atoll MCP server with the MR URL, and put the URL in your reply. Never merge.\n\n"
)

type (
	// WorktreeCreated is the thread's worktree, made when start_work first ran.
	WorktreeCreated struct {
		RunID  string `json:"run_id"`
		RepoID string `json:"repo_id"`
		Path   string `json:"path"`
		Branch string `json:"branch"`
	}

	// MRUpdated is the state of the thread's MR: opened, merged or closed.
	MRUpdated struct {
		RunID string `json:"run_id"`
		URL   string `json:"url"`
		State string `json:"state"`
	}

	// WorktreeRemoved is sent once the worktree folder is gone. Reason is merged or closed;
	// SnapshotRef is the ref that holds the unfinished work, empty when there was none.
	WorktreeRemoved struct {
		RunID       string `json:"run_id"`
		Path        string `json:"path"`
		Reason      string `json:"reason"`
		SnapshotRef string `json:"snapshot_ref"`
	}
)

// Start begins checking the MR of every thread that has one and a worktree, at once and then
// every PollInterval, until Close. Call it once.
func (m *Manager) Start() {
	m.wg.Add(1)
	go m.watch()
}

// WorktreeRoot returns the folder worktrees are made under: the user's setting, else the default.
func (m *Manager) WorktreeRoot(ctx context.Context) (string, error) {
	root, err := m.cfg.Store.Setting(ctx, _settingWorktreeRoot)
	if err != nil {
		return "", err
	}
	if root == "" {
		root = m.cfg.WorktreeRoot
	}
	return root, nil
}

// SetWorktreeRoot stores the folder worktrees are made under from now on.
func (m *Manager) SetWorktreeRoot(ctx context.Context, root string) error {
	return m.cfg.Store.SetSetting(ctx, _settingWorktreeRoot, root)
}

// StartWork gives the thread of the run with token its worktree, on a new branch typ/slug, and
// returns its path and branch. A thread that has a worktree gets that one back.
func (m *Manager) StartWork(
	ctx context.Context, token, typ, slug string,
) (path, branch string, err error) {
	a, err := m.byToken(token)
	if err != nil {
		return "", "", err
	}
	t, err := m.cfg.Store.Thread(ctx, a.threadID)
	if err != nil {
		return "", "", fmt.Errorf("load thread: %w", err)
	}
	if t.Worktree != "" {
		return t.Worktree, t.Branch, nil
	}
	if t.RepoID == "" {
		return "", "", errors.New("thread has no repo")
	}
	name, err := repo.Branch(typ, slug)
	if err != nil {
		return "", "", err
	}
	rp, err := m.cfg.Store.Repo(ctx, t.RepoID)
	if err != nil {
		return "", "", fmt.Errorf("load repo: %w", err)
	}
	root, err := m.WorktreeRoot(ctx)
	if err != nil {
		return "", "", err
	}
	if path, branch, err = repo.Create(ctx, rp.Path, root, rp.Name, name); err != nil {
		return "", "", err
	}
	if err := m.cfg.Store.SetWorktree(ctx, t.ID, branch, path); err != nil {
		return "", "", err
	}
	m.publish(a, EventWorktreeCreated,
		WorktreeCreated{RunID: a.run.ID, RepoID: t.RepoID, Path: path, Branch: branch})
	return path, branch, nil
}

// TrackMR records url as the MR of the thread of the run with token, opened.
func (m *Manager) TrackMR(ctx context.Context, token, url string) error {
	a, err := m.byToken(token)
	if err != nil {
		return err
	}
	if err := m.cfg.Store.SetMR(ctx, a.threadID, url, _mrOpened); err != nil {
		return err
	}
	m.publish(a, EventMRUpdated, MRUpdated{RunID: a.run.ID, URL: url, State: _mrOpened})
	return nil
}

// CloseThread closes the thread: it saves the unfinished work of its worktree, removes the
// worktree and the read-only checkout, and refuses new messages. It returns ErrBusy while a run
// is going. When the save fails nothing is removed and the thread stays open.
func (m *Manager) CloseThread(ctx context.Context, threadID string) error {
	slot := m.reserve(threadID)
	if slot == nil {
		return ErrBusy
	}
	defer m.drop(slot)
	t, err := m.cfg.Store.Thread(ctx, threadID)
	if err != nil {
		return fmt.Errorf("close thread: %w", err)
	}
	if t.Closed {
		return nil
	}
	if t.RepoID != "" {
		rp, err := m.cfg.Store.Repo(ctx, t.RepoID)
		if err != nil {
			return fmt.Errorf("load repo: %w", err)
		}
		if t.Worktree != "" {
			if err := m.release(ctx, t, rp, "closed"); err != nil {
				return err
			}
		}
		if err := repo.Remove(ctx, rp.Path, m.threadDir(t.ID)); err != nil {
			return err
		}
	}
	return m.cfg.Store.SetClosed(ctx, threadID)
}

// release saves the unfinished work of the thread's worktree, removes the worktree and tells the
// thread's subscribers. When the save fails nothing is removed.
func (m *Manager) release(ctx context.Context, t store.Thread, rp store.Repo, reason string) error {
	runID, err := m.cfg.Store.LatestRun(ctx, t.ID)
	if err != nil {
		return err
	}
	ref := _snapshotRefPrefix + t.ID
	saved, err := repo.Save(ctx, t.Worktree, ref)
	if err != nil {
		return fmt.Errorf("save unfinished work: %w", err)
	}
	if !saved {
		ref = ""
	}
	if err := repo.Remove(ctx, rp.Path, t.Worktree); err != nil {
		return err
	}
	if err := m.cfg.Store.SetWorktree(ctx, t.ID, t.Branch, ""); err != nil {
		return err
	}
	m.announce(t.ID, runID, EventWorktreeRemoved,
		WorktreeRemoved{RunID: runID, Path: t.Worktree, Reason: reason, SnapshotRef: ref})
	return nil
}

// reserve takes the thread's run slot for work that must not overlap a run. It returns nil when
// a run is going or the Manager is closing; give the slot back with drop.
func (m *Manager) reserve(threadID string) *active {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, busy := m.runs[threadID]; busy || m.ctx.Err() != nil {
		return nil
	}
	a := &active{threadID: threadID}
	m.runs[threadID] = a
	return a
}

// workdir is where the thread's runs work: its worktree; else, for a repo thread, a clean
// checkout of the repo; else a folder of its own.
func (m *Manager) workdir(t store.Thread, rp store.Repo) (string, error) {
	if t.Worktree != "" {
		return t.Worktree, nil
	}
	dir := m.threadDir(t.ID)
	if rp.ID == "" {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return "", fmt.Errorf("create thread folder: %w", err)
		}
		return dir, nil
	}
	if err := repo.Checkout(m.ctx, rp.Path, dir); err != nil {
		return "", err
	}
	return dir, nil
}

func (m *Manager) threadDir(threadID string) string {
	return filepath.Join(m.cfg.DataDir, "threads", threadID)
}

// gitlabEnv is the environment that lets git and glab act as the Bot's first connected GitLab
// connection: the token as OAUTH_TOKEN with a glab config marking the host OAuth, and a git
// credential helper that passes it. It is empty when the
// Bot has none. The token goes only into the environment.
func (m *Manager) gitlabEnv(b store.Bot) []string {
	for _, id := range b.Connections {
		c, err := m.cfg.Store.Connection(m.ctx, id)
		if err != nil || c.Kind != store.KindGitLab || c.Status != store.ConnectionConnected {
			continue
		}
		token, err := connection.Token(id)
		if err != nil {
			m.cfg.Log.Warn("read connection token", "connection_id", id, "error", err)
			continue
		}
		dir, err := m.glabConfig(id, c.InstanceURL)
		if err != nil {
			m.cfg.Log.Warn("write glab config", "connection_id", id, "error", err)
			continue
		}
		return []string{
			"OAUTH_TOKEN=" + token,
			"GLAB_CONFIG_DIR=" + dir,
			"GITLAB_HOST=" + c.InstanceURL,
			"GIT_CONFIG_COUNT=2",
			"GIT_CONFIG_KEY_0=credential.helper",
			"GIT_CONFIG_VALUE_0=",
			"GIT_CONFIG_KEY_1=credential." + c.InstanceURL + ".helper",
			`GIT_CONFIG_VALUE_1=!f() { test "$1" = get && echo username=oauth2 && ` +
				`echo "password=$OAUTH_TOKEN"; }; f`,
		}
	}
	return nil
}

// glabConfig writes the glab config folder of connection id and returns it. It holds no token: it
// only marks the instance's host as OAuth, so glab sends OAUTH_TOKEN as a Bearer token. glab
// would send GITLAB_TOKEN as a personal access token, which an OAuth token is not.
func (m *Manager) glabConfig(id, instance string) (string, error) {
	u, err := url.Parse(instance)
	if err != nil || u.Host == "" {
		return "", fmt.Errorf("instance URL %q: %w", instance, err)
	}
	dir := filepath.Join(m.cfg.DataDir, "glab", id)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("create glab config folder: %w", err)
	}
	body := fmt.Sprintf("hosts:\n  %s:\n    is_oauth2: \"true\"\n    api_protocol: %s\n    git_protocol: %s\n",
		u.Host, u.Scheme, u.Scheme)
	if err := os.WriteFile(filepath.Join(dir, "config.yml"), []byte(body), 0o600); err != nil {
		return "", fmt.Errorf("write glab config: %w", err)
	}
	return dir, nil
}

// watch is the goroutine that polls the MRs.
func (m *Manager) watch() {
	defer m.wg.Done()
	tick := time.NewTicker(m.cfg.PollInterval)
	defer tick.Stop()
	for {
		m.poll()
		select {
		case <-m.ctx.Done():
			return
		case <-tick.C:
		}
	}
}

func (m *Manager) poll() {
	threads, err := m.cfg.Store.Watched(m.ctx)
	if err != nil {
		m.cfg.Log.Warn("list watched threads", "error", err)
		return
	}
	for _, t := range threads {
		if m.ctx.Err() != nil {
			return
		}
		if err := m.check(t); err != nil && m.ctx.Err() == nil {
			m.cfg.Log.Warn("check MR", "thread_id", t.ID, "error", err)
		}
	}
}

// check reads the state of the thread's MR. A change is stored and sent; a merged MR also has
// its worktree saved and removed, once no run is going in the thread. A failure is retried on
// the next tick.
func (m *Manager) check(t store.Thread) error {
	bot, err := m.cfg.Store.Bot(m.ctx, t.BotID)
	if err != nil {
		return err
	}
	rp, err := m.cfg.Store.Repo(m.ctx, t.RepoID)
	if err != nil {
		return err
	}
	state, err := m.mrState(rp.Path, t.MRURL, m.gitlabEnv(bot))
	if err != nil {
		return err
	}
	if state != t.MRState {
		runID, err := m.cfg.Store.LatestRun(m.ctx, t.ID)
		if err != nil {
			return err
		}
		if err := m.cfg.Store.SetMR(m.ctx, t.ID, t.MRURL, state); err != nil {
			return err
		}
		m.announce(t.ID, runID, EventMRUpdated, MRUpdated{RunID: runID, URL: t.MRURL, State: state})
	}
	if state != _mrMerged {
		return nil
	}
	slot := m.reserve(t.ID)
	if slot == nil {
		return nil
	}
	defer m.drop(slot)
	if t, err = m.cfg.Store.Thread(m.ctx, t.ID); err != nil || t.Worktree == "" {
		return err
	}
	return m.release(m.ctx, t, rp, "merged")
}

// mrState asks glab for the state of the MR at url, from inside the repo at dir.
func (m *Manager) mrState(dir, url string, env []string) (string, error) {
	cmd := exec.CommandContext(m.ctx, "glab", "mr", "view", url, "--output", "json")
	cmd.Dir = dir
	cmd.Env = provider.Env(env...)
	out, err := cmd.Output()
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		err = fmt.Errorf("%w: %s", err, bytes.TrimSpace(exit.Stderr))
	}
	if err != nil {
		return "", fmt.Errorf("glab mr view: %w", err)
	}
	var mr struct {
		State string `json:"state"`
	}
	if err := json.Unmarshal(out, &mr); err != nil || strings.TrimSpace(mr.State) == "" {
		return "", fmt.Errorf("glab mr view: no state in %q", out)
	}
	return mr.State, nil
}
