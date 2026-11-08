package run

import (
	"context"
	"fmt"

	"atoll/memory"
)

// MemorySaved is sent when a run saves a memory item.
type MemorySaved struct {
	Item        memory.Item `json:"item"`
	SourceBotID string      `json:"source_bot_id"`
	ThreadID    string      `json:"thread_id"`
	RunID       string      `json:"run_id"`
}

// Remember saves an accepted item for the run with token. The run supplies the source and the
// scope id: the thread's repo for scope repo, its Bot for scope bot. It returns the saved item
// and the items that look like it.
func (m *Manager) Remember(
	ctx context.Context, token, kind, body, why, scope string, replaces []string,
) (memory.Item, []memory.Similar, error) {
	a, err := m.byToken(token)
	if err != nil {
		return memory.Item{}, nil, err
	}
	t, err := m.cfg.Store.Thread(ctx, a.threadID)
	if err != nil {
		return memory.Item{}, nil, fmt.Errorf("load thread: %w", err)
	}
	var scopeID string
	switch scope {
	case memory.ScopeRepo:
		if t.RepoID == "" {
			return memory.Item{}, nil, fmt.Errorf("%w: thread has no repo", memory.ErrInvalid)
		}
		scopeID = t.RepoID
	case memory.ScopeBot:
		scopeID = t.BotID
	}
	item, similar, err := m.cfg.Memory.Remember(ctx, memory.Note{
		Kind: kind, Body: body, Why: why, Scope: scope, ScopeID: scopeID, Replaces: replaces,
		SourceBotID: t.BotID, SourceThreadID: t.ID, SourceRunID: a.run.ID,
	})
	if err != nil {
		return memory.Item{}, nil, err
	}
	m.publish(a, EventMemorySaved,
		MemorySaved{Item: item, SourceBotID: t.BotID, ThreadID: t.ID, RunID: a.run.ID})
	m.show(token, item)
	return item, similar, nil
}

// Recall searches the memory the run with token may see.
func (m *Manager) Recall(
	ctx context.Context, token, query string, limit int,
) ([]memory.Item, error) {
	sh, err := m.shelf(ctx, token)
	if err != nil {
		return nil, err
	}
	items, err := m.cfg.Memory.Recall(ctx, sh, query, limit)
	if err != nil {
		return nil, err
	}
	m.show(token, items...)
	return items, nil
}

// ReadMemory returns item id. It wraps memory.ErrNotFound for an item off the run's shelf.
func (m *Manager) ReadMemory(ctx context.Context, token, id string) (memory.Item, error) {
	sh, err := m.shelf(ctx, token)
	if err != nil {
		return memory.Item{}, err
	}
	it, err := m.onShelf(ctx, sh, id)
	if err != nil {
		return memory.Item{}, err
	}
	m.show(token, it)
	return it, nil
}

// RetireMemory retires item id. It wraps memory.ErrNotFound for an item off the run's shelf.
func (m *Manager) RetireMemory(ctx context.Context, token, id string) (memory.Item, error) {
	sh, err := m.shelf(ctx, token)
	if err != nil {
		return memory.Item{}, err
	}
	if _, err := m.onShelf(ctx, sh, id); err != nil {
		return memory.Item{}, err
	}
	return m.cfg.Memory.Retire(ctx, id)
}

func (m *Manager) onShelf(ctx context.Context, sh memory.Shelf, id string) (memory.Item, error) {
	it, err := m.cfg.Memory.Read(ctx, id)
	if err != nil {
		return memory.Item{}, err
	}
	if !sh.Has(it) {
		return memory.Item{}, fmt.Errorf("memory %s: %w", id, memory.ErrNotFound)
	}
	return it, nil
}

// shelf is what the run with token may see: its thread's repo and its Bot.
func (m *Manager) shelf(ctx context.Context, token string) (memory.Shelf, error) {
	a, err := m.byToken(token)
	if err != nil {
		return memory.Shelf{}, err
	}
	t, err := m.cfg.Store.Thread(ctx, a.threadID)
	if err != nil {
		return memory.Shelf{}, fmt.Errorf("load thread: %w", err)
	}
	m.mu.Lock()
	allowed := a.bot.RepoAccess.Allows(t.RepoID)
	m.mu.Unlock()
	if !allowed {
		t.RepoID = ""
	}
	return memory.Shelf{RepoID: t.RepoID, BotID: t.BotID}, nil
}

// show records items the provider got from a memory tool, so resumed messages leave them out.
func (m *Manager) show(token string, items ...memory.Item) {
	a, err := m.byToken(token)
	if err != nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, it := range items {
		a.shown = append(a.shown, it.ID)
	}
}
