package run

import (
	"fmt"
	"slices"

	"atoll/store"
)

// EventPlatformChanged announces a platform change made by a run.
const EventPlatformChanged = "platform_changed"

// PlatformChanged identifies a change and the run that made it.
type PlatformChanged struct {
	SourceBotID string       `json:"source_bot_id"`
	ThreadID    string       `json:"thread_id"`
	RunID       string       `json:"run_id"`
	Change      store.Change `json:"change"`
}

// Source returns the run's bot snapshot, thread and run ID.
func (m *Manager) Source(token string) (store.Bot, store.Thread, string, error) {
	m.mu.Lock()
	a, ok := m.tokens[token]
	if !ok {
		m.mu.Unlock()
		return store.Bot{}, store.Thread{}, "", fmt.Errorf("run token: %w", ErrNotFound)
	}
	bot, threadID, id := a.bot, a.threadID, a.run.ID
	bot.Skills = slices.Clone(bot.Skills)
	bot.Connections = slices.Clone(bot.Connections)
	bot.MCPServers = slices.Clone(bot.MCPServers)
	bot.Delegates = slices.Clone(bot.Delegates)
	bot.RepoAccess = slices.Clone(bot.RepoAccess)
	m.mu.Unlock()
	thread, err := m.cfg.Store.Thread(m.ctx, threadID)
	if err != nil {
		return store.Bot{}, store.Thread{}, "", fmt.Errorf("load thread: %w", err)
	}
	return bot, thread, id, nil
}

// Changed publishes a recorded platform change to the run's thread.
func (m *Manager) Changed(token string, c store.Change) {
	a, err := m.byToken(token)
	if err != nil {
		return
	}
	m.publish(a, EventPlatformChanged, PlatformChanged{SourceBotID: c.SourceBotID, ThreadID: c.SourceThreadID, RunID: c.SourceRunID, Change: c})
}

// ThreadRead tells every notice stream that the user opened the thread.
func (m *Manager) ThreadRead(threadID string) {
	m.announce(threadID, "", EventThreadRead, struct{}{})
}
