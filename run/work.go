package run

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
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
		Title string `json:"title"`
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

// DataDir returns the folder that holds Atoll's runs, threads and database.
func (m *Manager) DataDir() string { return m.cfg.DataDir }

// SetWorktreeRoot stores the folder worktrees are made under from now on.
func (m *Manager) SetWorktreeRoot(ctx context.Context, root string) error {
	return m.cfg.Store.SetSetting(ctx, _settingWorktreeRoot, root)
}

// StartWork gives the thread of the run with token its worktree, on a new branch typ/slug, and
// returns its path, branch and base. A thread that has a worktree gets that one back with an
// empty base: no new branch was started.
func (m *Manager) StartWork(
	ctx context.Context, token, typ, slug string,
) (path, branch string, base repo.Base, err error) {
	a, err := m.byToken(token)
	if err != nil {
		return "", "", repo.Base{}, err
	}
	t, err := m.cfg.Store.Thread(ctx, a.threadID)
	if err != nil {
		return "", "", repo.Base{}, fmt.Errorf("load thread: %w", err)
	}
	if t.Worktree != "" {
		return t.Worktree, t.Branch, repo.Base{}, nil
	}
	m.mu.Lock()
	access := a.bot.RepoAccess
	m.mu.Unlock()
	if t.RepoID == "" || !access.Allows(t.RepoID) {
		return "", "", repo.Base{}, errors.New("thread has no repo")
	}
	name, err := repo.Branch(typ, slug)
	if err != nil {
		return "", "", repo.Base{}, err
	}
	rp, err := m.cfg.Store.Repo(ctx, t.RepoID)
	if err != nil {
		return "", "", repo.Base{}, fmt.Errorf("load repo: %w", err)
	}
	root, err := m.WorktreeRoot(ctx)
	if err != nil {
		return "", "", repo.Base{}, err
	}
	if path, branch, base, err = repo.Create(ctx, rp.Path, root, rp.Name, name); err != nil {
		return "", "", repo.Base{}, err
	}
	if err := m.cfg.Store.SetWorktree(ctx, t.ID, branch, path); err != nil {
		return "", "", repo.Base{}, err
	}
	m.publish(a, EventWorktreeCreated,
		WorktreeCreated{RunID: a.run.ID, RepoID: t.RepoID, Path: path, Branch: branch})
	return path, branch, base, nil
}

// TrackMR records url as the MR of the thread of the run with token, opened.
func (m *Manager) TrackMR(ctx context.Context, token, url, title string) error {
	a, err := m.byToken(token)
	if err != nil {
		return err
	}
	if title == "" {
		thread, threadErr := m.cfg.Store.Thread(ctx, a.threadID)
		if threadErr == nil && thread.RepoID != "" {
			if rp, repoErr := m.cfg.Store.Repo(ctx, thread.RepoID); repoErr == nil {
				env, _, _ := m.runEnv(ctx, a.bot)
				_, title, _ = m.mrState(rp.Path, url, env)
			}
		}
	}
	if err := m.cfg.Store.SetMR(ctx, a.threadID, url, _mrOpened, title); err != nil {
		return err
	}
	m.publish(a, EventMRUpdated, MRUpdated{RunID: a.run.ID, URL: url, State: _mrOpened, Title: title})
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

// DeleteBot stops the runs of the Bot's threads, closes the threads, and deletes the Bot with its
// threads, runs and event files. Memory the Bot saved stays.
func (m *Manager) DeleteBot(ctx context.Context, botID string) error {
	if _, err := m.cfg.Store.Bot(ctx, botID); err != nil {
		return fmt.Errorf("delete bot: %w", err)
	}
	threads, err := m.cfg.Store.Threads(ctx, botID)
	if err != nil {
		return err
	}
	for _, t := range threads {
		if runID, _, ok := m.Active(t.ID); ok {
			if err := m.Stop(ctx, runID); err != nil && !errors.Is(err, ErrBusy) {
				return err
			}
		}
		if err := m.CloseThread(ctx, t.ID); err != nil {
			return err
		}
	}
	runIDs, err := m.cfg.Store.DeleteBot(ctx, botID)
	if err != nil {
		return err
	}
	for _, id := range runIDs {
		if err := os.Remove(filepath.Join(m.cfg.DataDir, "runs", id+".jsonl")); err != nil &&
			!errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove event file: %w", err)
		}
	}
	for _, t := range threads {
		if err := os.RemoveAll(m.threadDir(t.ID)); err != nil {
			return fmt.Errorf("remove thread folder: %w", err)
		}
	}
	return nil
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
	if err := m.cfg.Store.RemoveWorktree(ctx, t.ID, ref); err != nil {
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

// gitHelper is a git credential helper for the URL it is scoped to.
type gitHelper struct{ url, script string }

// runEnv is the environment of every run and of the CLIs Reef starts for a Bot: empty glab and gh
// config folders, so neither CLI finds the user's own login, and a cleared git credential helper
// list, followed by what the Bot's connections add: for each, a helper scoped to its URL. Only a
// connection fills these. It also returns the token of the Bot's GitHub connection, empty when
// it has none.
func (m *Manager) runEnv(ctx context.Context, b store.Bot) ([]string, string, error) {
	var env []string
	for _, cli := range []struct{ name, file string }{{"glab", "config.yml"}, {"gh", "hosts.yml"}} {
		dir := filepath.Join(m.cfg.DataDir, "empty", cli.name)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, "", fmt.Errorf("create empty %s config folder: %w", cli.name, err)
		}
		if err := os.WriteFile(filepath.Join(dir, cli.file), nil, 0o600); err != nil {
			return nil, "", fmt.Errorf("write empty %s config: %w", cli.name, err)
		}
		env = append(env, strings.ToUpper(cli.name)+"_CONFIG_DIR="+dir)
	}
	var helpers []gitHelper
	gitlab, helper, err := m.gitlabEnv(ctx, b)
	if err != nil {
		return nil, "", err
	}
	env = append(env, gitlab...)
	if helper.url != "" {
		helpers = append(helpers, helper)
	}
	github, token, err := m.githubEnv(ctx, b)
	if err != nil {
		return nil, "", err
	}
	env = append(env, github...)
	if token != "" {
		helpers = append(helpers, gitHelper{connection.GitHubURL(), _githubHelper})
	}
	env = append(env, "GIT_CONFIG_COUNT="+strconv.Itoa(1+len(helpers)),
		"GIT_CONFIG_KEY_0=credential.helper", "GIT_CONFIG_VALUE_0=")
	for i, h := range helpers {
		n := strconv.Itoa(i + 1)
		env = append(env, "GIT_CONFIG_KEY_"+n+"=credential."+h.url+".helper", "GIT_CONFIG_VALUE_"+n+"="+h.script)
	}
	return env, token, nil
}

// gitlabEnv is the environment that lets glab act as the Bot's first connected GitLab
// connection: the token as OAUTH_TOKEN with a glab config marking the host OAuth. It also returns
// the git credential helper that passes the token. Both are empty when the Bot has none. The
// token goes only into the environment.
func (m *Manager) gitlabEnv(ctx context.Context, b store.Bot) ([]string, gitHelper, error) {
	for _, id := range b.Connections {
		c, err := m.cfg.Store.Connection(ctx, id)
		if err != nil || c.Kind != store.KindGitLab {
			continue
		}
		if c.Status == store.ConnectionNeedsSignIn {
			return nil, gitHelper{}, connection.ErrNeedsSignIn
		}
		if c.Status != store.ConnectionConnected {
			continue
		}
		token, err := connection.UsableToken(ctx, &http.Client{Timeout: 30 * time.Second}, c.InstanceURL, id)
		if errors.Is(err, connection.ErrNeedsSignIn) {
			if setErr := m.cfg.Store.SetConnectionStatus(ctx, id, store.ConnectionNeedsSignIn); setErr != nil {
				return nil, gitHelper{}, fmt.Errorf("set GitLab connection needs sign-in: %w", setErr)
			}
			return nil, gitHelper{}, err
		}
		if err != nil {
			return nil, gitHelper{}, fmt.Errorf("read GitLab connection token: %w", err)
		}
		dir, err := m.glabConfig(id, c.InstanceURL)
		if err != nil {
			return nil, gitHelper{}, err
		}
		return []string{
			"OAUTH_TOKEN=" + token,
			"GLAB_CONFIG_DIR=" + dir,
			"GITLAB_HOST=" + c.InstanceURL,
		}, gitHelper{c.InstanceURL, `!f() { test "$1" = get && echo username=oauth2 && ` +
			`echo "password=$OAUTH_TOKEN"; }; f`}, nil
	}
	return nil, gitHelper{}, nil
}

const _githubHelper = `!f() { test "$1" = get && echo username=x-access-token && ` +
	`echo "password=$GH_TOKEN"; }; f`

// githubEnv is the environment that lets gh act as the Bot's first connected GitHub connection:
// the token as GH_TOKEN. It also returns the token, empty when the Bot has none. GitHub says
// whether it still accepts the token before every run: a rejected one sets the connection to
// needs sign-in.
func (m *Manager) githubEnv(ctx context.Context, b store.Bot) ([]string, string, error) {
	for _, id := range b.Connections {
		c, err := m.cfg.Store.Connection(ctx, id)
		if err != nil || c.Kind != store.KindGitHub {
			continue
		}
		if c.Status == store.ConnectionNeedsSignIn {
			return nil, "", connection.ErrGitHubNeedsSignIn
		}
		if c.Status != store.ConnectionConnected {
			continue
		}
		token, err := connection.GitHubToken(id)
		if err != nil {
			return nil, "", fmt.Errorf("read GitHub connection token: %w", err)
		}
		err = connection.CheckGitHubToken(ctx, &http.Client{Timeout: 30 * time.Second}, token)
		if errors.Is(err, connection.ErrGitHubNeedsSignIn) {
			if setErr := m.cfg.Store.SetConnectionStatus(ctx, id, store.ConnectionNeedsSignIn); setErr != nil {
				return nil, "", fmt.Errorf("set GitHub connection needs sign-in: %w", setErr)
			}
			return nil, "", err
		}
		if err != nil {
			return nil, "", fmt.Errorf("check GitHub connection token: %w", err)
		}
		return []string{"GH_TOKEN=" + token}, token, nil
	}
	return nil, "", nil
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
	body := fmt.Sprintf("hosts:\n  %s:\n    is_oauth2: \"true\"\n    oauth2_expiry_date: %q\n    api_protocol: %s\n    git_protocol: %s\n",
		u.Host, time.Now().UTC().Add(24*time.Hour).Format(time.RFC3339), u.Scheme, u.Scheme)
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
	env, _, err := m.runEnv(m.ctx, bot)
	if err != nil {
		return err
	}
	state, title, err := m.mrState(rp.Path, t.MRURL, env)
	if err != nil {
		return err
	}
	if t.MRTitle != "" {
		title = t.MRTitle
	}
	if state != t.MRState || title != t.MRTitle {
		runID, err := m.cfg.Store.LatestRun(m.ctx, t.ID)
		if err != nil {
			return err
		}
		if err := m.cfg.Store.SetMR(m.ctx, t.ID, t.MRURL, state, title); err != nil {
			return err
		}
		m.announce(t.ID, runID, EventMRUpdated, MRUpdated{RunID: runID, URL: t.MRURL, State: state, Title: title})
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
func (m *Manager) mrState(dir, url string, env []string) (string, string, error) {
	cmd := exec.CommandContext(m.ctx, "glab", "mr", "view", url, "--output", "json")
	cmd.Dir = dir
	cmd.Env = provider.Env(env...)
	out, err := cmd.Output()
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		err = fmt.Errorf("%w: %s", err, bytes.TrimSpace(exit.Stderr))
	}
	if err != nil {
		return "", "", fmt.Errorf("glab mr view: %w", err)
	}
	var mr struct {
		State string `json:"state"`
		Title string `json:"title"`
	}
	if err := json.Unmarshal(out, &mr); err != nil || strings.TrimSpace(mr.State) == "" {
		return "", "", fmt.Errorf("glab mr view: no state in %q", out)
	}
	return mr.State, mr.Title, nil
}
