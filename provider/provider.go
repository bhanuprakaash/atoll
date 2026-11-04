// Package provider defines what every provider adapter implements: one Session and the Events it
// streams.
package provider

import (
	"context"
	"os"
	"strings"
)

// Session drives one provider process holding one session. Call Start or Resume once; Send one
// prompt per turn; read Events until it closes. Interrupt is the only way to stop it.
type Session interface {
	// Start launches the provider on a new session.
	Start(ctx context.Context, cfg Config) error
	// Resume launches the provider on the session with id, from a KindSession event.
	Resume(ctx context.Context, cfg Config, id string) error
	// Send starts one turn. The turn ends with a KindTurnEnd or KindError event.
	Send(ctx context.Context, prompt string) error
	// Events streams events until the process exits, then closes.
	Events() <-chan Event
	// Interrupt stops the process. When it returns, the process is gone and Events is closed.
	Interrupt() error
}

// Config is how a session starts.
type Config struct {
	Bin string // provider executable
	Dir string // Atoll's settings folder for this provider
	Cwd string
}

// Kind says what an Event carries.
type Kind int

// Event kinds.
const (
	KindText    Kind = iota + 1 // Text is a piece of the reply
	KindSession                 // SessionID is the id Resume takes
	KindTurnEnd
	KindError // Text is the error
)

// Event is one step of a session.
type Event struct {
	Kind      Kind
	Text      string
	SessionID string
}

// Env returns the parent environment without the variables that carry a provider's credentials or
// session state (ANTHROPIC_*, CLAUDE*, OPENAI_*, CODEX_*), followed by kv.
func Env(kv ...string) []string {
	var env []string
	for _, e := range os.Environ() {
		if !strings.HasPrefix(e, "ANTHROPIC_") && !strings.HasPrefix(e, "CLAUDE") &&
			!strings.HasPrefix(e, "OPENAI_") && !strings.HasPrefix(e, "CODEX_") {
			env = append(env, e)
		}
	}
	return append(env, kv...)
}
