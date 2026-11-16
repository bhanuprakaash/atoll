package run

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"
	"time"

	"atoll/repo"
	"atoll/store"
)

const _maxHelpers = 3

// A helper's first message is a preface of one or two paragraphs, then the brief.
const (
	_delegatedTo = " delegated this to you; your replies go to "
	_workOn      = "Work on branch "
)

// RefusedError is a tool error the Bot sees, so its run goes on.
type RefusedError string

func (e RefusedError) Error() string { return string(e) }

// HelperStarted is a helper thread made for the boss's run, so the boss's page reads its helpers.
type HelperStarted struct {
	RunID    string `json:"run_id"`
	ThreadID string `json:"thread_id"`
}

// Handoff is the boss's result for a delegate.
type Handoff struct {
	ThreadID string `json:"thread_id"`
	Branch   string `json:"branch,omitempty"`
	Note     string `json:"note,omitempty"`
	Helpers  string `json:"helpers"`
}

// Delegate starts a helper thread for the Bot botName and sends it the brief.
func (m *Manager) Delegate(
	ctx context.Context, token, botName, brief, repoName string, readOnly bool,
) (Handoff, error) {
	a, err := m.byToken(token)
	if err != nil {
		return Handoff{}, err
	}
	m.mu.Lock()
	boss := a.bot
	m.mu.Unlock()
	t, err := m.cfg.Store.Thread(ctx, a.threadID)
	if err != nil {
		return Handoff{}, fmt.Errorf("load thread: %w", err)
	}
	if t.ParentThreadID != "" {
		return Handoff{}, RefusedError("a helper cannot delegate")
	}
	if t.RepoID != "" && !boss.RepoAccess.Allows(t.RepoID) {
		t.RepoID, t.Worktree, t.Branch = "", "", ""
	}
	helper, err := m.helper(ctx, boss, botName)
	if err != nil {
		return Handoff{}, err
	}
	rp, err := m.helperRepo(ctx, t.RepoID, repoName)
	if err != nil {
		return Handoff{}, err
	}
	if rp.ID != "" {
		if !helper.RepoAccess.Allows(rp.ID) {
			return Handoff{}, RefusedError(helper.Name + " has no access to " + rp.Name)
		}
		if !repo.HasCommit(ctx, rp.Path) {
			return Handoff{}, RefusedError(rp.Name + " has no commit yet")
		}
		if t.Worktree == "" && !readOnly && RepoHost(ctx, rp.Path) == _hostGitHub && !m.hasGitHub(ctx, helper) {
			return Handoff{}, RefusedError(helper.Name + " has no GitHub connection, so it cannot edit " +
				rp.Name + ". Add it in " + helper.Name + "'s settings, or pass read_only for a read-only task.")
		}
	}
	h, path, err := m.startHelper(ctx, boss, helper, t, rp, brief)
	if err != nil {
		m.abandonHelper(ctx, rp, h, path)
		return Handoff{}, err
	}
	if h.Helpers, err = m.helpersLine(ctx, t.ID, ""); err != nil {
		m.cfg.Log.Error("count helpers", "thread_id", t.ID, "error", err)
	}
	m.publish(a, EventHelperStarted, HelperStarted{RunID: a.run.ID, ThreadID: h.ThreadID})
	return h, nil
}

// startHelper makes the helper's worktree and thread and starts its run. On an error it returns
// what it made so far.
func (m *Manager) startHelper(
	ctx context.Context, boss, helper store.Bot, t store.Thread, rp store.Repo, brief string,
) (h Handoff, path string, err error) {
	m.delegateMu.Lock()
	defer m.delegateMu.Unlock()
	going, err := m.helpersGoing(ctx, t.ID)
	if err != nil {
		return h, "", err
	}
	if going >= _maxHelpers {
		return h, "", RefusedError(fmt.Sprintf("%d helpers running. Tell the user, "+
			"and hand this off when one of them reports back.", _maxHelpers))
	}
	first := boss.Name + _delegatedTo + boss.Name + ".\n\n"
	if t.Worktree != "" {
		root, err := m.WorktreeRoot(ctx)
		if err != nil {
			return h, "", err
		}
		name := t.Branch + "--" + repo.Slug(helper.Name)
		if path, h.Branch, err = repo.CreateFrom(ctx, rp.Path, root, rp.Name, name, t.Branch); err != nil {
			return h, "", err
		}
		h.Note = "Uncommitted changes in your worktree are not included."
		first += _workOn + h.Branch + " and commit there. Do not push or open a PR: " +
			boss.Name + " merges it.\n\n"
	}
	ht, err := m.cfg.Store.CreateHelperThread(ctx, t.ID, helper.ID, rp.ID)
	if err != nil {
		return h, path, fmt.Errorf("create helper thread: %w", err)
	}
	h.ThreadID = ht.ID
	if path != "" {
		if err := m.cfg.Store.SetWorktree(ctx, ht.ID, h.Branch, path); err != nil {
			return h, path, err
		}
		common, err := repo.GitCommonDir(ctx, rp.Path)
		if err != nil {
			return h, path, err
		}
		id, err := repo.WorktreeID(common, path)
		if err != nil {
			return h, path, err
		}
		if err := m.cfg.Store.SetGitDirs(ctx, ht.ID, "", id); err != nil {
			return h, path, err
		}
	}
	if err := m.cfg.Store.SetTitle(ctx, ht.ID, store.FirstWords(brief)); err != nil {
		return h, path, err
	}
	it := store.InboxItem{ThreadID: ht.ID, FromBotID: boss.ID, Text: first + brief}
	if _, _, err := m.send(ctx, it); err != nil {
		return h, path, fmt.Errorf("start helper run: %w", err)
	}
	return h, path, nil
}

// abandonHelper undoes a delegate that failed: it closes the helper thread, if one was made, and
// removes the worktree and branch. Closing the thread removes the worktree too.
func (m *Manager) abandonHelper(ctx context.Context, rp store.Repo, h Handoff, path string) {
	ctx = context.WithoutCancel(ctx)
	if h.ThreadID != "" {
		if err := m.CloseThread(ctx, h.ThreadID); err != nil {
			m.cfg.Log.Error("close helper thread", "thread_id", h.ThreadID, "error", err)
		}
	}
	if path == "" {
		return
	}
	if _, err := os.Lstat(path); !errors.Is(err, fs.ErrNotExist) {
		if err := m.checkWorktree(path, rp.Name); err != nil {
			m.cfg.Log.Error("remove helper worktree", "path", path, "error", err)
			return
		}
		if err := repo.Remove(ctx, rp.Path, path); err != nil {
			m.cfg.Log.Error("remove helper worktree", "path", path, "error", err)
			return
		}
	}
	if err := repo.DeleteBranch(ctx, rp.Path, h.Branch); err != nil {
		m.cfg.Log.Error("delete helper branch", "branch", h.Branch, "error", err)
	}
}

// Continue sends message to a helper thread of the boss run with token. When the helper's run is
// going the message is queued and Continue returns the helper's name.
func (m *Manager) Continue(ctx context.Context, token, threadID, message string) (queuedFor string, err error) {
	a, err := m.byToken(token)
	if err != nil {
		return "", err
	}
	t, err := m.cfg.Store.Thread(ctx, threadID)
	if errors.Is(err, store.ErrNotFound) {
		return "", RefusedError("no thread " + threadID)
	}
	if err != nil {
		return "", fmt.Errorf("load thread: %w", err)
	}
	if t.ParentThreadID != a.threadID {
		return "", RefusedError("thread " + threadID + " is not a helper thread of this thread")
	}
	if t.Closed {
		return "", ErrClosed
	}
	busy, err := m.post(ctx, threadID, message)
	if err != nil || !busy {
		return "", err
	}
	helper, err := m.cfg.Store.Bot(ctx, t.BotID)
	if err != nil {
		return "", fmt.Errorf("load helper: %w", err)
	}
	return helper.Name, nil
}

// StopHelper stops the run of threadID, a helper thread of the calling boss thread, as the user's
// Stop does but without waking the boss. It returns the helper Bot's name and whether a run was
// stopped.
func (m *Manager) StopHelper(ctx context.Context, token, threadID string) (name string, stopped bool, err error) {
	a, err := m.byToken(token)
	if err != nil {
		return "", false, err
	}
	t, err := m.cfg.Store.Thread(ctx, threadID)
	if errors.Is(err, store.ErrNotFound) || err == nil && t.ParentThreadID != a.threadID {
		return "", false, RefusedError(threadID + " is not one of your helper threads.")
	}
	if err != nil {
		return "", false, fmt.Errorf("load thread: %w", err)
	}
	helper, err := m.cfg.Store.Bot(ctx, t.BotID)
	if err != nil {
		return "", false, fmt.Errorf("load helper: %w", err)
	}
	stopped, err = m.stopQuietly(ctx, threadID)
	if stopped {
		m.flushHelpers(t.ParentThreadID, threadID)
	}
	return helper.Name, stopped, err
}

// hasGitHub reports whether b has a connected GitHub connection.
func (m *Manager) hasGitHub(ctx context.Context, b store.Bot) bool {
	for _, id := range b.Connections {
		c, err := m.cfg.Store.Connection(ctx, id)
		if err == nil && c.Kind == store.KindGitHub && c.Status == store.ConnectionConnected {
			return true
		}
	}
	return false
}

func (m *Manager) helpersGoing(ctx context.Context, bossID string) (int, error) {
	running, err := m.helpersRunning(ctx, bossID, "")
	return len(running), err
}

// helpersRunning lists the helper threads of bossID that have a run going, oldest first, except
// skip.
func (m *Manager) helpersRunning(ctx context.Context, bossID, skip string) ([]store.Thread, error) {
	helpers, err := m.cfg.Store.HelperThreads(ctx, bossID)
	if err != nil {
		return nil, fmt.Errorf("list helper threads: %w", err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	var running []store.Thread
	for _, h := range helpers {
		if _, ok := m.runs[h.ID]; ok && h.ID != skip {
			running = append(running, h)
		}
	}
	return running, nil
}

// HelpersLine is the line that tells a boss how many of its helper slots are free: running holds
// one "{Bot} on {title}" per helper going.
func HelpersLine(running []string) string {
	free := _maxHelpers - len(running)
	if len(running) == 0 {
		return fmt.Sprintf("Helpers: 0 of %d running. %d free.", _maxHelpers, free)
	}
	return fmt.Sprintf("Helpers: %d of %d running (%s). %d free.",
		len(running), _maxHelpers, strings.Join(running, "; "), free)
}

// helpersLine is HelpersLine for the helper runs going under bossID, except the one on skip.
func (m *Manager) helpersLine(ctx context.Context, bossID, skip string) (string, error) {
	running, err := m.helpersRunning(ctx, bossID, skip)
	if err != nil {
		return "", err
	}
	names := make([]string, len(running))
	for i, h := range running {
		b, err := m.cfg.Store.Bot(ctx, h.BotID)
		if err != nil {
			return "", fmt.Errorf("load helper: %w", err)
		}
		names[i] = b.Name + " on " + h.Title
	}
	return HelpersLine(names), nil
}

// HelperRow is one helper thread in the result of helpers(): no text from the thread.
type HelperRow struct {
	Bot      string `json:"bot"`
	ThreadID string `json:"thread_id"`
	Title    string `json:"title"`
	State    string `json:"state"`
	IdleS    *int   `json:"idle_s,omitempty"`
	Branch   string `json:"branch"`
	Ahead    int    `json:"ahead"`
}

// Helpers lists the open helper threads of the run with token's thread, oldest first, with the
// helpers line. It changes nothing.
func (m *Manager) Helpers(ctx context.Context, token string) ([]HelperRow, string, error) {
	_, boss, _, err := m.Source(token)
	if err != nil {
		return nil, "", err
	}
	threads, err := m.cfg.Store.HelperThreads(ctx, boss.ID)
	if err != nil {
		return nil, "", fmt.Errorf("list helper threads: %w", err)
	}
	rows := []HelperRow{}
	for _, h := range threads {
		if h.Closed {
			continue
		}
		bot, err := m.cfg.Store.Bot(ctx, h.BotID)
		if err != nil {
			return nil, "", fmt.Errorf("load helper: %w", err)
		}
		m.mu.Lock()
		var runID string
		var started time.Time
		act, held := m.runs[h.ID]
		if held {
			runID, started = act.run.ID, act.run.StartedAt
		}
		m.mu.Unlock()
		going := held && runID != ""
		id, err := m.cfg.Store.LatestRun(ctx, h.ID)
		if err != nil {
			return nil, "", err
		}
		row := HelperRow{Bot: bot.Name, ThreadID: h.ID, Title: h.Title, Branch: h.Branch}
		var last store.Run
		if id != "" {
			if last, err = m.cfg.Store.Run(ctx, id); err != nil {
				return nil, "", fmt.Errorf("load helper run: %w", err)
			}
		}
		switch {
		case going, id == "":
			// A helper whose run has not started yet counts as running, as in the helpers line.
			row.State = store.StatusRunning
			idle := 0
			if going {
				since := started
				if n := act.lastCall.Load(); n != 0 {
					since = time.Unix(0, n)
				}
				idle = int(time.Since(since).Seconds())
			}
			row.IdleS = &idle
		case last.ErrorKind == store.ErrorRateLimited:
			row.State = "limited"
		default:
			row.State = last.Status
		}
		if boss.Worktree != "" && h.Worktree != "" && h.RepoID != "" {
			rp, err := m.cfg.Store.Repo(ctx, h.RepoID)
			if err != nil {
				return nil, "", fmt.Errorf("load repo: %w", err)
			}
			row.Ahead = repo.Ahead(ctx, rp.Path, boss.Branch, h.Branch)
		}
		rows = append(rows, row)
	}
	line, err := m.helpersLine(ctx, boss.ID, "")
	return rows, line, err
}

// BossHelpers is the helpers line for the run with token, or "" unless the run lists delegate:
// the Bot has delegates and its thread is not a helper thread.
func (m *Manager) BossHelpers(ctx context.Context, token string) (string, error) {
	bot, t, _, err := m.Source(token)
	if err != nil {
		return "", err
	}
	if len(bot.Delegates) == 0 || t.ParentThreadID != "" {
		return "", nil
	}
	return m.helpersLine(ctx, t.ID, "")
}

func (m *Manager) helper(ctx context.Context, boss store.Bot, name string) (store.Bot, error) {
	var names []string
	for _, id := range boss.Delegates {
		b, err := m.cfg.Store.Bot(ctx, id)
		if errors.Is(err, store.ErrNotFound) {
			continue
		}
		if err != nil {
			return store.Bot{}, fmt.Errorf("load delegate: %w", err)
		}
		if b.Name == name {
			return b, nil
		}
		names = append(names, b.Name)
	}
	return store.Bot{}, RefusedError(fmt.Sprintf("%q is not on your delegate list; it has: %s",
		name, strings.Join(names, ", ")))
}

func (m *Manager) helperRepo(ctx context.Context, bossRepoID, name string) (store.Repo, error) {
	if bossRepoID != "" {
		rp, err := m.cfg.Store.Repo(ctx, bossRepoID)
		if err != nil {
			return store.Repo{}, fmt.Errorf("load repo: %w", err)
		}
		return rp, nil
	}
	if name == "" {
		return store.Repo{}, nil
	}
	repos, err := m.cfg.Store.Repos(ctx)
	if err != nil {
		return store.Repo{}, fmt.Errorf("list repos: %w", err)
	}
	var names []string
	for _, rp := range repos {
		if rp.Name == name {
			return rp, nil
		}
		names = append(names, rp.Name)
	}
	return store.Repo{}, RefusedError(fmt.Sprintf("no repo %q; registered repos: %s",
		name, strings.Join(names, ", ")))
}

// helperBrief is the brief of a helper's first message: text without its preface.
func helperBrief(text string) string {
	if first, rest, ok := strings.Cut(text, "\n\n"); ok && strings.Contains(first, _delegatedTo) {
		text = rest
		if first, rest, ok := strings.Cut(text, "\n\n"); ok && strings.HasPrefix(first, _workOn) {
			text = rest
		}
	}
	return text
}
