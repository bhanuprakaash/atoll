// Package provider defines what every provider adapter implements: one Session and the Events it
// streams.
package provider

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"slices"
	"strings"
)

// Atoll's own MCP server, as every provider's config names it, and its tools.
const (
	MCPServer       = "atoll"
	GitHubMCPServer = "github"
	// ToolFinishRun takes the run's report: {done, tried, blocked, next}.
	ToolFinishRun = "finish_run"
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
	// Platform tools modify platform configuration.
	ToolCreateBot        = "create_bot"
	ToolEditBot          = "edit_bot"
	ToolCreateSkill      = "create_skill"
	ToolEditSkill        = "edit_skill"
	ToolImportSkill      = "import_skill"
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

// ErrApprovalEnded means the provider resolved an approval before Answer reached it.
var ErrApprovalEnded = errors.New("approval request has ended")

// Session drives one provider process holding one session. Call Start or Resume once; Send one
// prompt per turn; read Events until it closes. Interrupt is the only way to stop it.
type Session interface {
	// Start launches the provider on a new session.
	Start(ctx context.Context, cfg Config) error
	// Resume launches the provider on the session with id, from a KindSession event.
	Resume(ctx context.Context, cfg Config, id string) error
	// Send starts one turn with the prompt and the images that came with it. The turn ends with a
	// KindTurnEnd or KindError event.
	Send(ctx context.Context, prompt string, images []Image) error
	// Events streams events until the process exits, then closes.
	Events() <-chan Event
	// Answer replies to the KindApproval event with id.
	Answer(id string, allow bool) error
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
	KindError       // Text is the error; SignedOut when the provider refused its sign-in
	KindToolCall    // ID, Server, Name and Input: a tool call started
	KindApproval    // ID, Name and Input: the provider waits for Answer(ID, ...)
	KindApprovalEnd // ID: the provider ended the request without an Answer
)

// Event is one step of a session.
type Event struct {
	Kind      Kind
	Text      string
	SessionID string
	ID        string
	Name      string
	Server    string
	Input     json.RawMessage
	SignedOut bool
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
		if !strings.HasPrefix(e, "ANTHROPIC_") && !strings.HasPrefix(e, "CLAUDE") &&
			!strings.HasPrefix(e, "OPENAI_") && !strings.HasPrefix(e, "CODEX_") &&
			!slices.ContainsFunc(_loginTokens, func(name string) bool { return strings.HasPrefix(e, name+"=") }) {
			env = append(env, e)
		}
	}
	return append(env, kv...)
}

var _loginTokens = []string{
	"GITLAB_TOKEN", "GITLAB_ACCESS_TOKEN", "GH_TOKEN", "GITHUB_TOKEN", "GH_ENTERPRISE_TOKEN", "GITHUB_ENTERPRISE_TOKEN",
	"OAUTH_TOKEN", "GITLAB_HOST", "GIT_CONFIG_PARAMETERS", "SSH_AUTH_SOCK", "ATOLL_TOKEN",
}
