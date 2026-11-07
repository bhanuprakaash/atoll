package run

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"atoll/repo"
	"atoll/store"
)

const _maxHelpers = 3

// RefusedError is a tool error the Bot sees, so its run goes on.
type RefusedError string

func (e RefusedError) Error() string { return string(e) }

// Handoff is the boss's result for a delegate.
type Handoff struct {
	ThreadID string `json:"thread_id"`
	Branch   string `json:"branch,omitempty"`
	Note     string `json:"note,omitempty"`
}

// Delegate starts a helper thread for the Bot botName and sends it the brief.
func (m *Manager) Delegate(
	ctx context.Context, token, botName, brief, repoName string,
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
	}
	m.delegateMu.Lock()
	defer m.delegateMu.Unlock()
	helpers, err := m.cfg.Store.HelperThreads(ctx, t.ID)
	if err != nil {
		return Handoff{}, fmt.Errorf("list helper threads: %w", err)
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
		return Handoff{}, RefusedError(fmt.Sprintf("%d helpers running. Tell the user, "+
			"and hand this off when one of them reports back.", _maxHelpers))
	}
	var h Handoff
	first := boss.Name + " delegated this to you; your replies go to " + boss.Name + ".\n\n"
	var path string
	if t.Worktree != "" {
		root, err := m.WorktreeRoot(ctx)
		if err != nil {
			return Handoff{}, err
		}
		name := t.Branch + "--" + repo.Slug(helper.Name)
		if path, h.Branch, err = repo.CreateFrom(ctx, rp.Path, root, rp.Name, name, t.Branch); err != nil {
			return Handoff{}, err
		}
		h.Note = "Uncommitted changes in your worktree are not included."
		first += "Work on branch " + h.Branch + " and commit there. Do not push or open a PR: " +
			boss.Name + " merges it.\n\n"
	}
	ht, err := m.cfg.Store.CreateHelperThread(ctx, t.ID, helper.ID, rp.ID)
	if err != nil {
		return Handoff{}, fmt.Errorf("create helper thread: %w", err)
	}
	h.ThreadID = ht.ID
	if path != "" {
		if err := m.cfg.Store.SetWorktree(ctx, ht.ID, h.Branch, path); err != nil {
			return Handoff{}, err
		}
	}
	if _, _, err := m.Send(ctx, ht.ID, first+brief); err != nil {
		return Handoff{}, fmt.Errorf("start helper run: %w", err)
	}
	return h, nil
}

// Continue sends message to a helper thread of the boss run with token.
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
