package run

import (
	"context"
	"strings"

	"atoll/store"
)

// backgroundLine starts the prompt of a background run: a run whose messages all come from Reef.
func backgroundLine(user string) string {
	if user == "" {
		user = "The user"
	}
	return "Reef: background run. " + user + " does not see your final message, so end with one line " +
		"of status. If " + user + " must act (a PR to merge, a decision, a failure you cannot fix), " +
		"tell them with tell_user."
}

// failure reports whether a notice holds a failure Reef knows of: a helper failed, was stopped, hit
// its limit or was deleted, a restart cut helpers short, or CI failed. It reads the notice kind Reef
// stored where it wrote the wake, never the words.
func failure(it store.InboxItem) bool {
	switch it.Notice {
	case store.NoticeHelperFailed, store.NoticeHelperStopped, store.NoticeHelperLimited,
		store.NoticeHelperDeleted, store.NoticeRestart, store.NoticeCIFailed:
		return true
	}
	return false
}

// Background reports whether the run with token is a background run: it lists tell_user.
func (m *Manager) Background(token string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	a, ok := m.tokens[token]
	return ok && a.background
}

// TellUser adds text to the chat as a message of the Bot, for the background run with token.
func (m *Manager) TellUser(ctx context.Context, token, text string) error {
	a, err := m.byToken(token)
	if err != nil {
		return err
	}
	m.mu.Lock()
	if !a.background {
		m.mu.Unlock()
		return RefusedError("tell_user is only for a run that starts with Reef notices")
	}
	m.mu.Unlock()
	if err := m.cfg.Store.AddBotMessage(ctx, a.threadID, a.run.ID, strings.TrimSpace(text)); err != nil {
		return err
	}
	m.mu.Lock()
	a.told = true
	m.mu.Unlock()
	return nil
}
