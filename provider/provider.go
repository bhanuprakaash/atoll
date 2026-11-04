// Package provider defines what every provider adapter implements: one Session and the Events it
// streams.
package provider

import (
	"context"
	"encoding/json"
	"os"
	"strings"
)

// Atoll's own MCP server, as every provider's config names it, and its tools.
const (
	MCPServer = "atoll"
	// ToolFinishRun takes the run's report: {done, tried, blocked, next}.
	ToolFinishRun = "finish_run"
	// ToolApprove is Claude's permission prompt tool: it answers once the user does.
	ToolApprove = "approve"
	// ToolStartWork takes {type, slug} and returns the worktree {path, branch} of the thread.
	ToolStartWork = "start_work"
	// ToolTrackMR takes {url}: the merge request the thread opened.
	ToolTrackMR = "track_mr"
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
	// Answer replies to the KindApproval event with id.
	Answer(id string, allow bool) error
	// Interrupt stops the process. When it returns, the process is gone and Events is closed.
	Interrupt() error
}

// Config is how a session starts.
type Config struct {
	SystemPrompt string
	Bin          string // provider executable
	Dir          string // Atoll's settings folder for this provider
	Cwd          string
	// Env is extra environment for the provider process, as KEY=value.
	Env []string
	// MCPURL is Atoll's MCP server for this run. Empty runs without it, and without approvals:
	// nobody is there to answer them.
	MCPURL string
}

// Kind says what an Event carries.
type Kind int

// Event kinds.
const (
	KindText    Kind = iota + 1 // Text is a piece of the reply
	KindSession                 // SessionID is the id Resume takes
	KindTurnEnd
	KindError    // Text is the error
	KindToolCall // ID, Name and Input: a tool call started
	KindApproval // ID, Name and Input: the provider waits for Answer(ID, ...)
)

// Event is one step of a session.
type Event struct {
	Kind      Kind
	Text      string
	SessionID string
	ID        string
	Name      string
	Input     json.RawMessage
}

// Env returns the parent environment without the variables that carry a provider's credentials or
// session state (ANTHROPIC_*, CLAUDE*, OPENAI_*, CODEX_*) or the user's own GitLab token
// (GITLAB_TOKEN, GITLAB_ACCESS_TOKEN: glab would prefer it to a Bot's OAUTH_TOKEN), followed by kv.
func Env(kv ...string) []string {
	var env []string
	for _, e := range os.Environ() {
		if !strings.HasPrefix(e, "ANTHROPIC_") && !strings.HasPrefix(e, "CLAUDE") &&
			!strings.HasPrefix(e, "OPENAI_") && !strings.HasPrefix(e, "CODEX_") &&
			!strings.HasPrefix(e, "GITLAB_TOKEN=") && !strings.HasPrefix(e, "GITLAB_ACCESS_TOKEN=") {
			env = append(env, e)
		}
	}
	return append(env, kv...)
}
