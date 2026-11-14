// Package provider defines what every provider adapter implements: one Session and the Events it
// streams.
package provider

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"slices"
	"strings"
	"time"
)

// Atoll's own MCP server, as every provider's config names it, and its tools.
const (
	MCPServer       = "atoll"
	GitHubMCPServer = "github"
	// ToolApprove is Claude's permission prompt tool: it answers once the user does.
	ToolApprove = "approve"
	// ToolStartWork takes {type, slug} and returns the worktree {path, branch, base, note} of the thread.
	ToolStartWork = "start_work"
	// ToolTrackMR takes {url}: the merge request the thread opened.
	ToolTrackMR = "track_mr"
	// ToolRemember, ToolRecall, ToolReadMemory and ToolRetireMemory are the memory tools.
	ToolRemember     = "remember"
	ToolRecall       = "recall"
	ToolReadMemory   = "read_memory"
	ToolRetireMemory = "retire_memory"
	ToolDelegate     = "delegate"
	ToolContinue     = "continue"
	// ToolTellUser takes {text}: a message to the person, listed only in background runs.
	ToolTellUser = "tell_user"
	// Platform tools modify platform configuration.
	ToolCreateBot        = "create_bot"
	ToolEditBot          = "edit_bot"
	ToolCreateSkill      = "create_skill"
	ToolEditSkill        = "edit_skill"
	ToolImportSkill      = "import_skill"
	ToolImportAgent      = "import_agent"
	ToolRegisterRepo     = "register_repo"
	ToolAttachConnection = "attach_connection"
	ToolListBots         = "list_bots"
	ToolListSkills       = "list_skills"
	ToolListConnections  = "list_connections"
	ToolListRepos        = "list_repos"
	ToolListModels       = "list_models"
	ToolListMCPServers   = "list_mcp_servers"
)

// Bot modes.
const (
	ModeAuto = "auto"
	ModeAsk  = "ask"
	ModeFull = "full"
)

// ErrSignedOut is the error of a run that failed because the provider refused its sign-in.
var ErrSignedOut = errors.New("provider is signed out")

// ErrOff is the error of a run whose provider cannot start: its CLI is missing or it is turned off.
var ErrOff = errors.New("provider is off")

type offError struct{ error }

func (e offError) Unwrap() error      { return e.error }
func (offError) Is(target error) bool { return target == ErrOff }

// Off marks err as ErrOff without changing its text.
func Off(err error) error { return offError{err} }

// LimitError is the error of a run a provider's usage limit stopped. ResetsAt is zero when the
// provider gave no time.
type LimitError struct {
	Text     string
	ResetsAt time.Time
}

func (e *LimitError) Error() string { return e.Text }

// ErrApprovalEnded means the provider resolved an approval before Answer reached it.
var ErrApprovalEnded = errors.New("approval request has ended")

// Session drives one provider process holding one session. Call Start or Resume once; Send one
// prompt per turn; read Events until it closes. Interrupt is the only way to stop it.
type Session interface {
	// Start launches the provider on a new session.
	Start(ctx context.Context, cfg Config) error
	// Resume launches the provider on the session with id, from a KindSession event.
	Resume(ctx context.Context, cfg Config, id string) error
	// Send starts one request with the prompt and the images that came with it. The request ends
	// with exactly one KindTurnEnd or KindError event, for this request only. Send it one request at
	// a time: the next after that event.
	Send(ctx context.Context, prompt string, images []Image) error
	// Events streams events until the process exits, then closes.
	Events() <-chan Event
	// Answer replies to the KindApproval event with id. A non-empty reason with allow false is what
	// the user wrote instead; the provider is told it before the refusal.
	Answer(id string, allow bool, reason string) error
	// Interrupt stops the process. When it returns, the process is gone and Events is closed.
	Interrupt() error
}

// Image is a picture sent with a prompt.
type Image struct {
	Mime string
	Data []byte
}

// Config is how a session starts.
type Config struct {
	SystemPrompt  string
	Model         string
	Bin           string // provider executable
	Dir           string // Atoll's settings folder for this provider
	Cwd           string
	WritableRoots []string
	// Env is extra environment for the provider process, as KEY=value.
	Env []string
	// MCPURL is Atoll's MCP server for this run. Empty runs without it, and without approvals:
	// nobody is there to answer them.
	MCPURL string
	// Empty GitHubMCPURL runs without GitHub's MCP server.
	GitHubMCPURL string
	GitHubToken  string
	// SkillDir is the absolute root of this run's skill copies. Empty runs without them.
	SkillDir   string
	MCPServers []UserMCPServer
	// Mode is the Bot's mode; other values run as the provider's default.
	Mode string
	// NoTools disables tools, or confines them to a read-only, no-network sandbox.
	NoTools bool
	// NoShell disables Claude's built-in tools while retaining MCP tools.
	NoShell bool
	// HiddenPaths are absolute paths Claude's file tools and shell must not reach.
	HiddenPaths []string
	// Log receives the adapter's warnings. Nil logs nothing.
	Log *slog.Logger
}

// UserMCPServer is local with Command, Args and Env, or remote with URL and Headers.
type UserMCPServer struct {
	Name    string
	Command string
	Args    []string
	Env     map[string]string
	URL     string
	Headers map[string]string
}

// Kind says what an Event carries.
type Kind int

// Event kinds.
const (
	KindText    Kind = iota + 1 // Text is a piece of the reply
	KindSession                 // SessionID is the id Resume takes
	KindTurnEnd
	KindError       // Text is the error; SignedOut when the provider refused its sign-in; Limited when its usage limit stopped the turn
	KindToolCall    // ID, Server, Name and Input: a tool call started
	KindApproval    // ID, Name and Input: the provider waits for Answer(ID, ...)
	KindApprovalEnd // ID: the provider ended the request without an Answer
	KindUsage       // Usage: what the process used so far; it comes before the turn's end
)

// Event is one step of a session.
type Event struct {
	Kind Kind
	// Text is a piece of the request's text for KindText: progress. For KindTurnEnd it is the
	// request's final message, which is the reply.
	Text      string
	SessionID string
	ID        string
	Name      string
	Server    string
	Input     json.RawMessage
	SignedOut bool
	Limited   bool
	ResetsAt  time.Time // when Limited, the provider's reset time; zero if it gave none
	Usage     *Usage
	// Other marks activity the provider started on its own, not Reef's request: a finished
	// background shell, another turn of the thread. Only KindText, KindToolCall and KindUsage carry
	// it; a KindUsage then holds the session cost alone.
	Other bool
}

// Usage is what one provider process used, over all its turns. Tokens are the process's only,
// never the resumed session's past.
type Usage struct {
	Model            string
	Calls            int
	InputTokens      int
	OutputTokens     int
	CacheReadTokens  int
	CacheWriteTokens int
	// SessionCostUSD is what the whole session has cost so far, including runs before this process;
	// nil when the provider reports no price.
	SessionCostUSD *float64
}

// Env returns the parent environment without the variables that carry a provider's credentials or
// session state (ANTHROPIC_*, CLAUDE*, OPENAI_*, CODEX_*) or the user's own GitLab and GitHub
// tokens (GITLAB_TOKEN, GITLAB_ACCESS_TOKEN, GH_TOKEN, GITHUB_TOKEN, GH_ENTERPRISE_TOKEN,
// GITHUB_ENTERPRISE_TOKEN: the CLIs would prefer them to a Bot's own login), the settings that
// carry a login (OAUTH_TOKEN, GITLAB_HOST, GIT_CONFIG_PARAMETERS, SSH_AUTH_SOCK), and the local
// API credential (ATOLL_TOKEN), followed by kv.
func Env(kv ...string) []string {
	var env []string
	for _, e := range os.Environ() {
		if !Login(e) && !slices.ContainsFunc(_loginSettings, func(name string) bool { return strings.HasPrefix(e, name+"=") }) {
			env = append(env, e)
		}
	}
	return append(env, kv...)
}

// Login reports whether e, a NAME=value entry, is a login token or provider session state:
// ANTHROPIC_*, CLAUDE*, OPENAI_*, CODEX_*, or one of the GitHub, GitLab and OAuth tokens.
func Login(e string) bool {
	return strings.HasPrefix(e, "ANTHROPIC_") || strings.HasPrefix(e, "CLAUDE") ||
		strings.HasPrefix(e, "OPENAI_") || strings.HasPrefix(e, "CODEX_") ||
		slices.ContainsFunc(_loginTokens, func(name string) bool { return strings.HasPrefix(e, name+"=") })
}

var _loginTokens = []string{
	"GITLAB_TOKEN", "GITLAB_ACCESS_TOKEN", "GH_TOKEN", "GITHUB_TOKEN", "GH_ENTERPRISE_TOKEN", "GITHUB_ENTERPRISE_TOKEN",
	"OAUTH_TOKEN",
}

// _loginSettings are not tokens, but a Bot's CLIs must not inherit them either; Reef keeps them.
var _loginSettings = []string{
	"GITLAB_HOST", "GIT_CONFIG_PARAMETERS", "SSH_AUTH_SOCK", "ATOLL_TOKEN",
	"DBUS_SESSION_BUS_ADDRESS", "XDG_RUNTIME_DIR", "DISPLAY",
}
