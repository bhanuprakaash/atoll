// Package store keeps Atoll's data in one SQLite file: Bots, threads, messages and runs.
package store

import (
	"context"
	"errors"
	"time"
)

// ErrNotFound is wrapped when the id asked for matches no row.
var ErrNotFound = errors.New("not found")

// Run statuses.
const (
	StatusRunning = "running"
	StatusWaiting = "waiting" // an approval is open
	StatusDone    = "done"
	StatusFailed  = "failed"
)

// Message roles.
const (
	RoleUser = "user"
	RoleBot  = "bot"
)

type (
	// Store is Atoll's data, read and written through one SQLite file.
	Store struct{}

	// Bot is a configured agent. Provider is "claude" or "codex".
	Bot struct {
		ID       string `json:"id"`
		Name     string `json:"name"`
		Persona  string `json:"persona"`
		Provider string `json:"provider"`
	}

	// Thread is a chat with one Bot. Session is the provider session its runs resume; empty
	// until the first run starts one.
	Thread struct {
		ID      string    `json:"id"`
		BotID   string    `json:"bot_id"`
		Session string    `json:"-"`
		Created time.Time `json:"created_at"`
	}

	// Message is one chat line. A bot message holds a run's reply.
	Message struct {
		ID       string `json:"id"`
		ThreadID string `json:"-"`
		Role     string `json:"role"`
		Text     string `json:"text"`
		RunID    string `json:"run_id"`
	}

	// Run is one message handled.
	Run struct {
		ID       string  `json:"id"`
		ThreadID string  `json:"thread_id"`
		Status   string  `json:"status"`
		Reply    string  `json:"reply"`
		Error    string  `json:"error"`
		Report   *Report `json:"report"`
	}

	// Report is what the provider passed to finish_run, exactly.
	Report struct {
		Done    string `json:"done"`
		Tried   string `json:"tried"`
		Blocked string `json:"blocked"`
		Next    string `json:"next"`
	}
)

// Open opens dir/atoll.db, creating dir and the file if needed, and applies any migration the
// file lacks.
func Open(dir string) (*Store, error) { return nil, errors.New("not built yet") }

// Close closes the database.
func (s *Store) Close() error { return nil }

// CreateBot stores b with a new id and returns it.
func (s *Store) CreateBot(ctx context.Context, b Bot) (Bot, error) {
	return Bot{}, errors.New("not built yet")
}

// Bot returns the Bot with id.
func (s *Store) Bot(ctx context.Context, id string) (Bot, error) {
	return Bot{}, errors.New("not built yet")
}

// Bots lists every Bot, oldest first.
func (s *Store) Bots(ctx context.Context) ([]Bot, error) { return nil, errors.New("not built yet") }

// CreateThread starts a thread with the Bot botID.
func (s *Store) CreateThread(ctx context.Context, botID string) (Thread, error) {
	return Thread{}, errors.New("not built yet")
}

// Thread returns the thread with id.
func (s *Store) Thread(ctx context.Context, id string) (Thread, error) {
	return Thread{}, errors.New("not built yet")
}

// Threads lists the threads of the Bot botID, newest first.
func (s *Store) Threads(ctx context.Context, botID string) ([]Thread, error) {
	return nil, errors.New("not built yet")
}

// SetSession records the provider session the thread's next runs resume.
func (s *Store) SetSession(ctx context.Context, threadID, session string) error {
	return errors.New("not built yet")
}

// StartRun stores the user's message and a running run for it, in one transaction.
func (s *Store) StartRun(ctx context.Context, threadID, text string) (Message, Run, error) {
	return Message{}, Run{}, errors.New("not built yet")
}

// Run returns the run with id.
func (s *Store) Run(ctx context.Context, id string) (Run, error) {
	return Run{}, errors.New("not built yet")
}

// SetStatus sets the status of an unfinished run: running or waiting.
func (s *Store) SetStatus(ctx context.Context, runID, status string) error {
	return errors.New("not built yet")
}

// SetReport stores the run's finish_run report. A second call replaces the first.
func (s *Store) SetReport(ctx context.Context, runID string, r Report) error {
	return errors.New("not built yet")
}

// FinishRun ends a run as done with reply, or as failed with errText. A non-empty reply also
// becomes the thread's bot message, in the same transaction.
func (s *Store) FinishRun(ctx context.Context, runID, status, reply, errText string) error {
	return errors.New("not built yet")
}

// FailUnfinished marks every running or waiting run failed with reason and returns their ids.
// Called once at start: no run outlives the process that ran it.
func (s *Store) FailUnfinished(ctx context.Context, reason string) ([]string, error) {
	return nil, errors.New("not built yet")
}

// Messages lists the thread's messages, oldest first.
func (s *Store) Messages(ctx context.Context, threadID string) ([]Message, error) {
	return nil, errors.New("not built yet")
}
