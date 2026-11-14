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

// failure reports whether a notice holds a failure Reef knows of: a helper failed, was stopped or hit
// its limit, a restart cut helpers short, or CI failed. It reads the notice's first line, which Reef
// wrote; a helper's reply and the names of checks come after it.
func failure(it store.InboxItem) bool {
	first, _, _ := strings.Cut(it.Text, "\n")
	switch it.Kind {
	case "restart":
		return true
	case "ci":
		return strings.Contains(first, _ciFailed)
	case _helperNotice:
		return strings.Contains(first, "'s run failed: ") || strings.HasPrefix(first, "The user stopped ") ||
			strings.Contains(first, " is out until ")
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
	a.told = true
	m.mu.Unlock()
	return m.cfg.Store.AddBotMessage(ctx, a.threadID, a.run.ID, strings.TrimSpace(text))
}
