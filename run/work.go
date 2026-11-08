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
	_hostGitHub          = "github"
	_hostGitLab          = "gitlab"
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

// RepoHost is github or gitlab.
func RepoHost(ctx context.Context, path string) string {
	if connection.OnGitHub(repo.Origin(ctx, path)) {
		return _hostGitHub
	}
	return _hostGitLab
}

// Start begins checking the MR of every thread that has one and a worktree, at once and then
// every PollInterval, until Close, and delivers held messages. Call it once.
func (m *Manager) Start() {
	m.recoverInbox()
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
	bot := a.bot
	m.mu.Unlock()
	access := bot.RepoAccess
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
	if RepoHost(ctx, rp.Path) == _hostGitHub {
		_, token, err := m.githubEnv(ctx, bot)
		if err != nil {
			return "", "", repo.Base{}, err
		}
		if token == "" {
			return "", "", repo.Base{}, ErrNoGitHubConnection
		}
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
			rp, repoErr := m.cfg.Store.Repo(ctx, thread.RepoID)
			if repoErr == nil && RepoHost(ctx, rp.Path) == _hostGitLab {
				if env, _, envErr := m.runEnv(ctx, a.bot, false); envErr == nil {
					_, title, _ = m.mrState(rp.Path, url, env)
				}
			}
		}
	}
	if err := m.cfg.Store.SetMR(ctx, a.threadID, url, _mrOpened, title); err != nil {
		return err
	}
	m.publish(a, EventMRUpdated, MRUpdated{RunID: a.run.ID, URL: url, State: _mrOpened, Title: title})
	return nil
}

// CloseThread closes the thread and its helper threads: it saves the unfinished work of its
// worktree, removes the worktree and the read-only checkout, and refuses new messages. It returns
// ErrBusy while a run is going. When the save fails nothing is removed and the thread stays open.
func (m *Manager) CloseThread(ctx context.Context, threadID string) error {
	slot := m.reserve(threadID)
	if slot == nil {
		return ErrBusy
	}
	defer m.free(slot)
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
	if err := m.closeHelpers(ctx, threadID); err != nil {
		return err
	}
	return m.cfg.Store.SetClosed(ctx, threadID)
}

// DeleteBot deletes the Bot with its threads, runs and event files. Memory the Bot saved stays.
func (m *Manager) DeleteBot(ctx context.Context, botID string) error {
	bot, err := m.cfg.Store.Bot(ctx, botID)
	if err != nil {
		return fmt.Errorf("delete bot: %w", err)
	}
	threads, err := m.cfg.Store.Threads(ctx, botID)
	if err != nil {
		return err
	}
	var wakes []store.InboxItem
	for _, t := range threads {
		stopped, err := m.stopQuietly(ctx, t.ID)
		if err != nil {
			return err
		}
		if stopped && t.ParentThreadID != "" {
			wakes = append(wakes, store.InboxItem{
				ThreadID: t.ParentThreadID, FromBotID: botID,
				Text: fmt.Sprintf("%s was deleted\n(thread %s)", bot.Name, t.ID),
			})
		}
		if err := m.CloseThread(ctx, t.ID); err != nil {
			return err
		}
	}
	runIDs, err := m.cfg.Store.DeleteBot(ctx, botID, wakes)
	if err != nil {
		return err
	}
	for _, w := range wakes {
		m.flushLog(w.ThreadID)
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

func (m *Manager) closeHelpers(ctx context.Context, threadID string) error {
	helpers, err := m.cfg.Store.HelperThreads(ctx, threadID)
	if err != nil {
		return fmt.Errorf("list helper threads: %w", err)
	}
	for _, h := range helpers {
		if _, err := m.stopQuietly(ctx, h.ID); err != nil {
			return err
		}
		if err := m.CloseThread(ctx, h.ID); err != nil {
			return err
		}
	}
	return nil
}

func (m *Manager) stopQuietly(ctx context.Context, threadID string) (bool, error) {
	runID, _, ok := m.Active(threadID)
	if !ok {
		return false, nil
	}
	err := m.stop(ctx, runID, _stopByReef)
	if errors.Is(err, ErrBusy) {
		return false, nil
	}
	return err == nil, err
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
// a run is going or the Manager is closing; give the slot back with free.
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

// WorkDir is the folder the thread's runs work in: its worktree, else its own folder.
func (m *Manager) WorkDir(t store.Thread) string {
	if t.Worktree != "" {
		return t.Worktree
	}
	return m.threadDir(t.ID)
}

func (m *Manager) threadDir(threadID string) string {
	return filepath.Join(m.cfg.DataDir, "threads", threadID)
}

type gitHelper struct {
	url, script string
	ssh         []string
}

// runEnv empties the glab and gh config folders and the git credential helper list, so neither CLI
// finds the user's own login, and gives git's ssh no key, agent or config; each connection then
// adds a helper scoped to its URL and maps its host's SSH remotes to it. GitHub's connection joins
// only when github is set, so the MR watcher never depends on GitHub. It also returns the GitHub
// token, empty when there is none.
func (m *Manager) runEnv(ctx context.Context, b store.Bot, github bool) ([]string, string, error) {
	env := []string{
		"GIT_SSH_COMMAND=ssh -F /dev/null -o IdentitiesOnly=yes -o IdentityFile=/dev/null -o IdentityAgent=none",
	}
	for _, cli := range []struct{ name, file string }{{"glab", "config.yml"}, {"gh", "hosts.yml"}} {
		dir := filepath.Join(m.cfg.DataDir, "empty", cli.name)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, "", fmt.Errorf("create empty %s config folder: %w", cli.name, err)
		}
		if err := writeFileAtomic(filepath.Join(dir, cli.file), nil); err != nil {
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
	var token string
	if github {
		githubEnv, githubToken, err := m.githubEnv(ctx, b)
		if err != nil {
			return nil, "", err
		}
		env, token = append(env, githubEnv...), githubToken
		if token != "" {
			ssh, err := sshRemotes(connection.GitHubURL())
			if err != nil {
				return nil, "", err
			}
			helpers = append(helpers, gitHelper{connection.GitHubURL(), _githubHelper, ssh})
		}
	}
	config := [][2]string{{"credential.helper", ""}}
	for _, h := range helpers {
		config = append(config, [2]string{"credential." + h.url + ".helper", h.script})
		for _, remote := range h.ssh {
			config = append(config, [2]string{"url." + strings.TrimRight(h.url, "/") + "/.insteadOf", remote})
		}
	}
	env = append(env, "GIT_CONFIG_COUNT="+strconv.Itoa(len(config)))
	for i, kv := range config {
		n := strconv.Itoa(i)
		env = append(env, "GIT_CONFIG_KEY_"+n+"="+kv[0], "GIT_CONFIG_VALUE_"+n+"="+kv[1])
	}
	return env, token, nil
}

// gitlabEnv makes glab act as the Bot's first GitLab connection. The token goes only into the
// environment.
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
		ssh, err := sshRemotes(c.InstanceURL)
		if err != nil {
			return nil, gitHelper{}, err
		}
		return []string{
			"OAUTH_TOKEN=" + token,
			"GLAB_CONFIG_DIR=" + dir,
			"GITLAB_HOST=" + c.InstanceURL,
		}, gitHelper{c.InstanceURL, `!f() { test "$1" = get && echo username=oauth2 && ` +
			`echo "password=$OAUTH_TOKEN"; }; f`, ssh}, nil
	}
	return nil, gitHelper{}, nil
}

func sshRemotes(rawURL string) ([]string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("URL %q: %w", rawURL, err)
	}
	return []string{"git@" + u.Hostname() + ":", "ssh://git@" + u.Hostname() + "/"}, nil
}

const _githubHelper = `!f() { test "$1" = get && echo username=x-access-token && ` +
	`echo "password=$GH_TOKEN"; }; f`

// githubEnv makes gh act as the Bot's first GitHub connection. It checks the token before every run
// and marks a rejected one needs sign-in.
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
	if err := writeFileAtomic(filepath.Join(dir, "config.yml"), []byte(body)); err != nil {
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
	if RepoHost(m.ctx, rp.Path) == _hostGitHub {
		return nil
	}
	env, _, err := m.runEnv(m.ctx, bot, false)
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
	defer m.free(slot)
	if t, err = m.cfg.Store.Thread(m.ctx, t.ID); err != nil || t.Worktree == "" {
		return err
	}
	if ok, err := m.releaseHelpers(t, rp); !ok || err != nil {
		return err
	}
	return m.release(m.ctx, t, rp, "merged")
}

func (m *Manager) releaseHelpers(boss store.Thread, rp store.Repo) (bool, error) {
	helpers, err := m.cfg.Store.HelperThreads(m.ctx, boss.ID)
	if err != nil {
		return false, fmt.Errorf("list helper threads: %w", err)
	}
	var slots []*active
	defer func() {
		for _, slot := range slots {
			m.free(slot)
		}
	}()
	var owned []store.Thread
	for _, h := range helpers {
		if h.Worktree == "" || !strings.HasPrefix(h.Branch, boss.Branch+"--") {
			continue
		}
		slot := m.reserve(h.ID)
		if slot == nil {
			return false, nil
		}
		slots = append(slots, slot)
		owned = append(owned, h)
	}
	for _, h := range owned {
		if err := m.release(m.ctx, h, rp, "merged"); err != nil {
			return false, err
		}
	}
	return true, nil
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

// writeFileAtomic replaces path in one rename, so a CLI reading the file never sees it half
// written.
func writeFileAtomic(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	_, err = tmp.Write(data)
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
