// Package run handles one message as one run: it starts or resumes the Bot's provider session,
// streams the run's events to the thread, holds approvals open until the user answers, and stores
// the reply and the finish_run report.
package run

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"

	"atoll/provider"
	"atoll/store"
)

var (
	// ErrBusy is returned when a thread already has a run going.
	ErrBusy = errors.New("thread has a run going")
	// ErrNotFound is wrapped when an approval id or run token matches nothing open.
	ErrNotFound = errors.New("not found")
)

// Event types on a thread's stream.
const (
	EventText        = "text"         // Text: a piece of the reply
	EventToolCall    = "tool_call"    // ToolCall
	EventApproval    = "approval"     // Approval
	EventRunFinished = "run_finished" // Finished
)

type (
	// Launch returns a provider session that has not started, and the config to start it with
	// (all but Cwd and MCPURL), for the provider name of a Bot.
	Launch func(ctx context.Context, name string) (provider.Session, provider.Config, error)

	// Config is what a Manager needs.
	Config struct {
		Store  *store.Store
		Launch Launch
		// MCPURL is the base of Atoll's MCP endpoint; a run's server is MCPURL + its token.
		MCPURL string
		// DataDir holds runs/<run id>.jsonl, each run's event stream, and threads/<thread id>,
		// the folder a thread's runs work in: a provider resumes a session only in its folder.
		DataDir string
		Log     *slog.Logger
	}

	// Manager runs messages and fans their events out to the thread's subscribers.
	Manager struct{}

	// Event is one event on a thread's stream. Data is one of the types below.
	Event struct {
		Type string
		Data any
	}

	// Text is a piece of the reply.
	Text struct {
		RunID string `json:"run_id"`
		Text  string `json:"text"`
	}

	// ToolCall is a tool call the provider started, Atoll's own included.
	ToolCall struct {
		RunID string          `json:"run_id"`
		ID    string          `json:"id"`
		Name  string          `json:"name"`
		Input json.RawMessage `json:"input"`
	}

	// Approval is a step that waits for the user; ID is what Answer takes.
	Approval struct {
		RunID string          `json:"run_id"`
		ID    string          `json:"id"`
		Tool  string          `json:"tool"`
		Input json.RawMessage `json:"input"`
	}

	// Finished ends a run.
	Finished struct {
		RunID  string `json:"run_id"`
		Status string `json:"status"`
		Error  string `json:"error"`
	}
)

// New returns a Manager with no runs going.
func New(cfg Config) *Manager { return &Manager{} }

// Send stores text as the user's message and starts a run for it. It returns ErrBusy when the
// thread has a run going and wraps store.ErrNotFound for an unknown thread.
func (m *Manager) Send(ctx context.Context, threadID, text string) (store.Message, store.Run, error) {
	return store.Message{}, store.Run{}, errors.New("not built yet")
}

// Subscribe streams the thread's events from now on, until cancel is called.
func (m *Manager) Subscribe(threadID string) (events <-chan Event, cancel func()) {
	return nil, func() {}
}

// Answer gives the user's answer to the open approval id. It wraps ErrNotFound when none is open.
func (m *Manager) Answer(ctx context.Context, id string, allow bool) error {
	return errors.New("not built yet")
}

// Report stores the finish_run report of the run with token.
func (m *Manager) Report(ctx context.Context, token string, r store.Report) error {
	return errors.New("not built yet")
}

// Ask is Claude's permission prompt for the run with token: it opens an approval and blocks
// until the user answers or ctx ends. Atoll's own tools are allowed without asking.
func (m *Manager) Ask(ctx context.Context, token, tool string, input json.RawMessage) (bool, error) {
	return false, errors.New("not built yet")
}

// Close stops every run, marks it failed, and waits for them.
func (m *Manager) Close() error { return nil }
