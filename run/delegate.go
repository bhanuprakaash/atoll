package run

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"atoll/repo"
	"atoll/store"
)

// _maxHelpers is how many helper runs one boss thread may have going.
const _maxHelpers = 3

// RefusedError is a delegate or continue the Bot is told about as a tool error, so its run goes on.
type RefusedError string

func (e RefusedError) Error() string { return string(e) }

// Delegate starts a helper thread for the Bot botName on the delegate list of the run with token,
// and sends it the brief. The helper works in the boss thread's repo, else in the registered
// repo repoName, else in none. It returns the helper thread's id without waiting for its run.
func (m *Manager) Delegate(
	ctx context.Context, token, botName, brief, repoName string,
) (string, error) {
	a, err := m.byToken(token)
	if err != nil {
		return "", err
	}
	m.mu.Lock()
	boss := a.bot
	m.mu.Unlock()
	t, err := m.cfg.Store.Thread(ctx, a.threadID)
	if err != nil {
		return "", fmt.Errorf("load thread: %w", err)
	}
	if t.ParentThreadID != "" {
		return "", RefusedError("a helper cannot delegate")
	}
	helper, err := m.helper(ctx, boss, botName)
	if err != nil {
		return "", err
	}
	rp, err := m.helperRepo(ctx, t.RepoID, repoName)
	if err != nil {
		return "", err
	}
	if rp.ID != "" {
		if !helper.RepoAccess.Allows(rp.ID) {
			return "", RefusedError(helper.Name + " has no access to " + rp.Name)
		}
		if !repo.HasCommit(ctx, rp.Path) {
			return "", RefusedError(rp.Name + " has no commit yet")
		}
	}
	m.delegateMu.Lock()
	defer m.delegateMu.Unlock()
	helpers, err := m.cfg.Store.HelperThreads(ctx, t.ID)
	if err != nil {
		return "", fmt.Errorf("list helper threads: %w", err)
	}
	m.mu.Lock()
	going := 0
	for _, h := range helpers {
		if _, ok := m.runs[h.ID]; ok {
			going++
		}
	}
	m.mu.Unlock()
	if going >= _maxHelpers {
		return "", RefusedError(fmt.Sprintf("%d helpers running. Tell the user, "+
			"and hand this off when one of them reports back.", _maxHelpers))
	}
	ht, err := m.cfg.Store.CreateHelperThread(ctx, t.ID, helper.ID, rp.ID)
	if err != nil {
		return "", fmt.Errorf("create helper thread: %w", err)
	}
	first := boss.Name + " delegated this to you; your replies go to " + boss.Name + ".\n\n" + brief
	if _, _, err := m.Send(ctx, ht.ID, first); err != nil {
		return "", fmt.Errorf("start helper run: %w", err)
	}
	return ht.ID, nil
}

// Continue sends message to the helper thread, which only the boss thread of the run with token
// may do. A message for a helper whose run is going is held until that run ends.
func (m *Manager) Continue(ctx context.Context, token, threadID, message string) error {
	a, err := m.byToken(token)
	if err != nil {
		return err
	}
	t, err := m.cfg.Store.Thread(ctx, threadID)
	if errors.Is(err, store.ErrNotFound) {
		return RefusedError("no thread " + threadID)
	}
	if err != nil {
		return fmt.Errorf("load thread: %w", err)
	}
	if t.ParentThreadID != a.threadID {
		return RefusedError("thread " + threadID + " is not a helper thread of this thread")
	}
	if t.Closed {
		return ErrClosed
	}
	return m.post(ctx, threadID, "", message)
}

// helper returns the Bot called name on the boss's delegate list.
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

// helperRepo is the boss thread's repo, else the registered repo called name, else no repo.
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
