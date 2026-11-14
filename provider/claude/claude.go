// Package claude is the Claude Code provider: one long-lived `claude -p` process speaking
// stream-json on stdin and stdout.
package claude

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"

	"atoll/provider"
)

var _ provider.Session = (*Session)(nil)

var _unsafe = regexp.MustCompile(`[^A-Za-z0-9]`)

const (
	// SIGTERM lets claude stop the tool processes it started; they are not in its group.
	_termGrace = time.Second
	// WaitDelay bounds Wait when a process that outlived claude holds its stderr pipe.
	_waitDelay = 2 * time.Second
	// _messageBreak goes before the text of a message that follows text in the same turn.
	_messageBreak = "\n\n"
	_stderrKeep   = 4 << 10
	// _resultWait is how long a request waits for its result after its terminal state.
	_resultWait   = 5 * time.Second
	_builtinTools = "Bash,Edit,Glob,Grep,NotebookEdit,Read,Skill,WebFetch,WebSearch,Write"
)

// Session drives one claude process. Use one Session per process.
type Session struct {
	cmd     *exec.Cmd
	stdin   io.Writer
	stderr  tail
	events  chan provider.Event
	quit    chan struct{}
	done    chan struct{}
	mu      sync.Mutex // guards started, stopped and the fields start sets
	started bool
	stopped bool
	log     *slog.Logger // set by start; nil logs nothing
	sentMu  sync.Mutex   // guards sent
	sent    string       // uuid of the user line Send wrote last
}

type (
	inputMessage struct {
		Type    string       `json:"type"`
		UUID    string       `json:"uuid"`
		Message inputContent `json:"message"`
	}

	inputContent struct {
		Role    string `json:"role"`
		Content any    `json:"content"`
	}

	inputBlock struct {
		Type   string       `json:"type"`
		Text   string       `json:"text,omitempty"`
		Source *inputSource `json:"source,omitempty"`
	}

	inputSource struct {
		Type      string `json:"type"`
		MediaType string `json:"media_type"`
		Data      string `json:"data"`
	}

	outputLine struct {
		Type      string `json:"type"`
		Subtype   string `json:"subtype"`
		SessionID string `json:"session_id"`
		Model     string `json:"model"`
		Event     struct {
			Type  string `json:"type"`
			Delta struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"delta"`
		} `json:"event"`
		Message struct {
			ID      string `json:"id"`
			Content []struct {
				Type  string          `json:"type"`
				ID    string          `json:"id"`
				Name  string          `json:"name"`
				Input json.RawMessage `json:"input"`
			} `json:"content"`
		} `json:"message"`
		IsError bool     `json:"is_error"`
		Result  string   `json:"result"`
		Errors  []string `json:"errors"`
		Error   string   `json:"error"`
		// TotalCostUSD and Usage: the cost is the session's, the tokens the process's.
		TotalCostUSD *float64 `json:"total_cost_usd"`
		Usage        struct {
			InputTokens      int `json:"input_tokens"`
			OutputTokens     int `json:"output_tokens"`
			CacheReadTokens  int `json:"cache_read_input_tokens"`
			CacheWriteTokens int `json:"cache_creation_input_tokens"`
		} `json:"usage"`
		// CommandUUID and State are of a command_lifecycle line; Origin is set on a result claude
		// wrote for activity Reef did not ask for.
		CommandUUID    string          `json:"command_uuid"`
		State          string          `json:"state"`
		Origin         json.RawMessage `json:"origin"`
		APIError       string          `json:"api_error"`
		APIErrorParams struct {
			RateLimitInfo rateLimitInfo `json:"rate_limit_info"`
		} `json:"api_error_params"`
		RateLimitInfo rateLimitInfo `json:"rate_limit_info"`
	}

	rateLimitInfo struct {
		Status   string `json:"status"`
		ResetsAt int64  `json:"resetsAt"`
	}

	// state is what parse keeps between lines. A message that starts after text in the same turn
	// has its first text delta begin a new paragraph. model, calls (the distinct assistant message
	// ids of Reef's requests) and noLifecycle last the whole process.
	state struct {
		wrote, breakNext, signedOut, limited bool
		resetsAt                             time.Time
		model                                string
		calls                                map[string]bool
		noLifecycle                          bool // a request ended on a result with no command_lifecycle line
		req                                  request
	}

	// request is Reef's current request, found in claude's output by the uuid Send wrote.
	request struct {
		uuid            string
		queued, started bool
		terminal        string          // the command_lifecycle state that ended it, once it came
		final           *provider.Event // the end of the request, held until both signals are in
		ended           bool
	}

	readResult struct {
		line []byte
		err  error
	}

	// tail keeps the last 4 KB written to it, for claude's stderr.
	tail struct{ b []byte }
)

// New returns a Session that has not started. Its Events channel is valid at once.
func New() *Session {
	return &Session{
		events: make(chan provider.Event),
		quit:   make(chan struct{}),
		done:   make(chan struct{}),
	}
}

// Start launches claude in cfg.Cwd on a new session.
func (s *Session) Start(_ context.Context, cfg provider.Config) error {
	return s.start(cfg)
}

// Resume launches claude in cfg.Cwd continuing the session with the given id. Claude resumes only
// the sessions of its own folder, so a session file made in another folder is copied here first.
func (s *Session) Resume(_ context.Context, cfg provider.Config, id string) error {
	if err := adoptSession(cfg, id); err != nil {
		return fmt.Errorf("move session: %w", err)
	}
	return s.start(cfg, "--resume", id)
}

// Send writes the prompt and its images to claude as one user message.
func (s *Session) Send(ctx context.Context, prompt string, images []provider.Image) error {
	if s.stdin == nil {
		return errors.New("claude not started")
	}
	var content any = prompt
	if len(images) > 0 {
		blocks := make([]inputBlock, 0, len(images)+1)
		if prompt != "" {
			blocks = append(blocks, inputBlock{Type: "text", Text: prompt})
		}
		for _, img := range images {
			blocks = append(blocks, inputBlock{Type: "image", Source: &inputSource{
				Type: "base64", MediaType: img.Mime, Data: base64.StdEncoding.EncodeToString(img.Data),
			}})
		}
		content = blocks
	}
	id := newUUID()
	msg, err := json.Marshal(inputMessage{
		Type:    "user",
		UUID:    id,
		Message: inputContent{Role: "user", Content: content},
	})
	if err != nil {
		return err
	}
	s.sentMu.Lock()
	s.sent = id
	s.sentMu.Unlock()
	if _, err := s.stdin.Write(append(msg, '\n')); err != nil {
		err = fmt.Errorf("write prompt: %w", err)
		if why := s.exitReason(ctx); why != "" {
			err = fmt.Errorf("%w: %s", err, why)
		}
		return err
	}
	return nil
}

// Events streams events until claude exits, then closes the channel.
func (s *Session) Events() <-chan provider.Event { return s.events }

// Answer always fails: claude asks through the permission prompt tool on Atoll's MCP server and
// never sends a KindApproval event.
func (s *Session) Answer(string, bool, string) error {
	return errors.New("claude asks for approval through its permission prompt tool")
}

// Interrupt kills claude and anything it spawned, and waits until Events is closed.
func (s *Session) Interrupt() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped {
		return nil
	}
	s.stopped = true
	close(s.quit)
	if s.cmd == nil {
		close(s.events)
		return nil
	}
	select {
	case <-s.done:
		return nil
	default:
	}
	_ = syscall.Kill(-s.cmd.Process.Pid, syscall.SIGTERM)
	select {
	case <-s.done:
	case <-time.After(_termGrace):
		_ = syscall.Kill(-s.cmd.Process.Pid, syscall.SIGKILL)
		<-s.done
	}
	return nil
}

// SignedIn reports whether `claude auth status` finds a login in dir. A non-zero exit is signed
// out; an error means claude could not be run.
func SignedIn(ctx context.Context, bin, dir string) (bool, error) {
	cmd := exec.CommandContext(ctx, bin, "auth", "status")
	cmd.Env = configEnv(dir)
	err := cmd.Run()
	var exit *exec.ExitError
	if errors.As(err, &exit) && ctx.Err() == nil {
		return false, nil
	}
	return err == nil, err
}

// Own names the MCP servers and skills of the user's own Claude Code setup under home.
func Own(home string) provider.Own {
	own := provider.Own{MCPServers: []string{}, Skills: provider.SkillNames(filepath.Join(home, ".claude", "skills"))}
	var cfg struct {
		MCPServers map[string]json.RawMessage `json:"mcpServers"`
	}
	if b, err := os.ReadFile(filepath.Join(home, ".claude.json")); err == nil && json.Unmarshal(b, &cfg) == nil {
		own.MCPServers = append(own.MCPServers, slices.Sorted(maps.Keys(cfg.MCPServers))...)
	}
	return own
}

// Login returns the command that signs claude in to dir. The caller wires its stdio.
func Login(ctx context.Context, bin, dir string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, bin, "auth", "login")
	cmd.Env = configEnv(dir)
	return cmd
}

func configEnv(dir string) []string {
	return provider.Env("CLAUDE_CONFIG_DIR=" + dir)
}

// adoptSession copies the file of session id into the project folder claude uses for cfg.Cwd,
// named after the path with every character outside [A-Za-z0-9] as "-". It copies the newest
// file of that name, and does nothing when that is the one there already or none exists.
func adoptSession(cfg provider.Config, id string) error {
	projects := filepath.Join(cfg.Dir, "projects")
	name := id + ".jsonl"
	dst := filepath.Join(projects, _unsafe.ReplaceAllString(cfg.Cwd, "-"), name)
	newest, newestAt := "", time.Time{}
	if info, err := os.Stat(dst); err == nil {
		newest, newestAt = dst, info.ModTime()
	}
	found, err := filepath.Glob(filepath.Join(projects, "*", name))
	if err != nil {
		return err
	}
	for _, f := range found {
		if info, err := os.Stat(f); err == nil && (newest == "" || info.ModTime().After(newestAt)) {
			newest, newestAt = f, info.ModTime()
		}
	}
	if newest == "" || newest == dst {
		return nil
	}
	body, err := os.ReadFile(newest)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return err
	}
	return os.WriteFile(dst, body, 0o600)
}

// exitReason is the text of the error event claude left when it exited before it read the
// prompt, such as on a session it could not resume. The caller of a failed write is not
// reading Events, so the reason would otherwise never reach it.
func (s *Session) exitReason(ctx context.Context) string {
	for {
		select {
		case ev, ok := <-s.events:
			if !ok {
				return ""
			}
			if ev.Kind == provider.KindError {
				return ev.Text
			}
		case <-ctx.Done():
			return ""
		}
	}
}

func (s *Session) start(cfg provider.Config, extra ...string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.started || s.stopped {
		return errors.New("claude already started or interrupted")
	}
	s.started = true

	args, mcpConfig, err := argv(cfg, extra...)
	if err != nil {
		return err
	}
	cmd := exec.Command(cfg.Bin, args...)
	cmd.Dir = cfg.Cwd
	cmd.Env = append(configEnv(cfg.Dir), "ENABLE_CLAUDEAI_MCP_SERVERS=false", "MCP_TOOL_TIMEOUT=1800000", "CLAUDE_CODE_SHELL_PREFIX="+filepath.Join(cfg.Dir, "..", "..", "bin", "claude-shell-prefix"))
	cmd.Env = append(cmd.Env, cfg.Env...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.WaitDelay = _waitDelay
	cmd.Stderr = &s.stderr
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	var w *os.File
	if mcpConfig != nil {
		var r *os.File
		r, w, err = os.Pipe()
		if err != nil {
			return err
		}
		defer func() { _ = r.Close() }()
		cmd.ExtraFiles = []*os.File{r}
	}
	if err := cmd.Start(); err != nil {
		if w != nil {
			_ = w.Close()
		}
		return fmt.Errorf("start claude: %w", err)
	}
	if w != nil {
		// Not waited for: it ends when the child reads the config or exits, and a grandchild that
		// holds fd 3 open could make a wait hang.
		go func() {
			_, _ = w.Write(mcpConfig)
			_ = w.Close()
		}()
	}

	s.cmd, s.stdin, s.log = cmd, stdin, cfg.Log
	go s.read(stdout)
	return nil
}

// _mcpConfigPath is the first extra file of the process (fd 3): a pipe holding the MCP config.
const _mcpConfigPath = "/dev/fd/3"

// argv returns the arguments for claude and the MCP config they name, or nil when there is none.
// The config holds every URL, header value and env value; it goes to claude on a pipe, so none
// shows in the process list or the environment.
func argv(cfg provider.Config, extra ...string) ([]string, []byte, error) {
	var mcpConfig []byte
	isolation, err := IsolationArgs(cfg.Cwd, cfg.NoTools || cfg.NoShell, cfg.HiddenPaths)
	if err != nil {
		return nil, nil, err
	}
	args := []string{
		"-p",
		"--input-format", "stream-json",
		"--output-format", "stream-json",
		"--verbose",
		"--include-partial-messages",
		"--strict-mcp-config",
	}
	args = append(args, isolation...)
	if cfg.Model != "" {
		args = append(args, "--model", cfg.Model)
	}
	if cfg.SystemPrompt != "" {
		args = append(args, "--append-system-prompt", cfg.SystemPrompt)
	}
	if cfg.MCPURL != "" && !cfg.NoTools {
		mcpServers := map[string]any{
			provider.MCPServer: map[string]any{"type": "http", "url": cfg.MCPURL},
		}
		if cfg.GitHubMCPURL != "" {
			mcpServers[provider.GitHubMCPServer] = map[string]any{
				"type": "http", "url": cfg.GitHubMCPURL,
				"headers": map[string]string{"Authorization": "Bearer " + cfg.GitHubToken},
			}
		}
		for _, m := range cfg.MCPServers {
			mcpServers[m.Name] = userServer(m)
		}
		mcpConfig, err = json.Marshal(map[string]any{"mcpServers": mcpServers})
		if err != nil {
			return nil, nil, err
		}
		mcp := "mcp__" + provider.MCPServer
		args = append(args,
			"--mcp-config", _mcpConfigPath,
			"--permission-prompt-tool", mcp+"__"+provider.ToolApprove,
			"--allowedTools", mcp,
		)
	}
	switch cfg.Mode {
	case provider.ModeAuto:
		args = append(args, "--permission-mode", "auto")
	case provider.ModeFull:
		args = append(args, "--permission-mode", "bypassPermissions")
	}
	if cfg.SkillDir != "" {
		args = append(args, "--add-dir", cfg.SkillDir)
	}
	return append(args, extra...), mcpConfig, nil
}

// IsolationArgs excludes ancestor instructions, so cwd must be the repo root to keep its CLAUDE.md.
// With tools on, hidden paths are refused to the file tools and, through a sandbox that only hides
// reads, to Bash.
func IsolationArgs(cwd string, noTools bool, hidden []string) ([]string, error) {
	excludes := []string{}
	seen := map[string]bool{}
	paths := []string{cwd}
	if resolved, err := filepath.EvalSymlinks(cwd); err == nil {
		paths = append(paths, resolved)
	}
	for _, path := range paths {
		if path == "" {
			continue
		}
		for dir := filepath.Dir(path); ; dir = filepath.Dir(dir) {
			for _, pattern := range []string{filepath.Join(_globChars.Replace(dir), "*.md"), filepath.Join(_globChars.Replace(dir), ".claude", "**")} {
				if !seen[pattern] {
					excludes = append(excludes, pattern)
					seen[pattern] = true
				}
			}
			if dir == filepath.Dir(dir) {
				break
			}
		}
	}
	base := map[string]any{
		"disableAllHooks": true, "disableBundledSkills": true,
		"skillOverrides":    map[string]string{"design": "off", "doctor": "off", "plugin-authoring": "off"},
		"autoMemoryEnabled": false, "claudeMdExcludes": excludes,
	}
	if len(hidden) > 0 && !noTools {
		var deny []string
		for _, path := range hidden {
			rule := "(/" + _globChars.Replace(path) + "/**)"
			deny = append(deny, "Read"+rule, "Edit"+rule)
		}
		base["permissions"] = map[string]any{"deny": deny}
		base["sandbox"] = map[string]any{
			"enabled": true, "failIfUnavailable": true,
			"allowUnsandboxedCommands": false, "autoAllowBashIfSandboxed": false,
			"network":    map[string]any{"allowedDomains": []string{"*"}},
			"filesystem": map[string]any{"allowWrite": []string{"/"}, "denyRead": hidden},
		}
	}
	settings, err := json.Marshal(base)
	if err != nil {
		return nil, err
	}
	tools := _builtinTools
	if noTools {
		tools = ""
	}
	return []string{"--setting-sources", "project", "--settings", string(settings), "--tools", tools}, nil
}

var _globChars = strings.NewReplacer(`\`, "?", "*", "?", "?", "?", "[", "?", "]", "?", "{", "?", "}", "?", "(", "?", ")", "?")

func userServer(m provider.UserMCPServer) map[string]any {
	e := map[string]any{"type": "stdio", "command": m.Command}
	if m.Command == "" {
		e = map[string]any{"type": "http", "url": m.URL}
	}
	if len(m.Args) > 0 {
		e["args"] = m.Args
	}
	if len(m.Env) > 0 {
		e["env"] = m.Env
	}
	if len(m.Headers) > 0 {
		e["headers"] = m.Headers
	}
	return e
}

func (s *Session) read(stdout io.Reader) {
	defer close(s.done)
	defer close(s.events)

	lines := make(chan readResult)
	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		r := bufio.NewReader(stdout)
		for {
			line, err := r.ReadBytes('\n')
			select {
			case lines <- readResult{line, err}:
			case <-s.quit:
				return
			}
			if err != nil {
				return
			}
		}
	}()

	var last provider.Kind
	var st state
	var warned bool
	var deadline time.Time
	// emit reports whether to go on; last is the kind of the latest event that was Reef's.
	emit := func(evs []provider.Event) bool {
		for _, ev := range evs {
			if !s.emit(ev) {
				return false
			}
			if !ev.Other {
				last = ev.Kind
			}
		}
		return true
	}
	for eof := false; !eof; {
		var timeout <-chan time.Time
		if st.req.waiting() {
			if deadline.IsZero() {
				deadline = time.Now().Add(_resultWait)
			}
			timeout = time.After(time.Until(deadline))
		} else {
			deadline = time.Time{}
		}
		var evs []provider.Event
		select {
		case l := <-lines:
			if id := s.sentUUID(); id != st.req.uuid {
				st.req = request{uuid: id}
			}
			evs, eof = parse(l.line, &st), l.err != nil
		case <-s.quit:
			_ = s.cmd.Wait()
			<-readDone
			return
		case <-timeout:
			evs = st.req.expire()
		}
		if st.noLifecycle && !warned {
			warned = true
			if s.log != nil {
				s.log.Warn("claude sent no lifecycle events; ending requests on the first result")
			}
		}
		if !emit(evs) {
			_ = s.cmd.Wait()
			<-readDone
			return
		}
	}
	err := s.cmd.Wait()
	<-readDone
	if f := st.req.final; f != nil && f.Kind == provider.KindError && !st.req.ended && !emit([]provider.Event{*f}) {
		return
	}
	if err == nil || last == provider.KindError {
		return
	}
	text := fmt.Sprintf("claude exited: %v: %s", err, bytes.TrimSpace(s.stderr.b))
	if bytes.Contains(s.stderr.b, []byte("sandbox required but unavailable")) {
		text = "This Bot cannot run: it needs bubblewrap and socat to keep the repos it lacks out of its shell. " +
			"Install them (on Debian or Ubuntu: sudo apt install bubblewrap socat) and ask again."
	}
	s.emit(provider.Event{Kind: provider.KindError, Text: text})
}

// emit reports whether ev was delivered. Once Interrupt has run it delivers nothing.
func (s *Session) emit(ev provider.Event) bool {
	select {
	case <-s.quit:
		return false
	default:
	}
	select {
	case s.events <- ev:
		return true
	case <-s.quit:
		return false
	}
}

// parse maps one stdout line to Events. Text comes from the partial-message deltas, so the
// complete assistant text blocks that follow are skipped; tool calls come from the complete
// assistant line, as the partial input is cut off. Unknown lines give none. What claude does
// outside Reef's request (see request) is Other and leaves Reef's state alone.
func parse(line []byte, st *state) []provider.Event {
	var l outputLine
	if err := json.Unmarshal(line, &l); err != nil {
		return nil
	}
	other := st.req.other()
	switch l.Type {
	case "system":
		if l.Subtype == "init" {
			st.model = l.Model
			return []provider.Event{{Kind: provider.KindSession, SessionID: l.SessionID}}
		}
	case "command_lifecycle":
		return st.lifecycle(l)
	case "assistant":
		if !other {
			st.signedOut = st.signedOut || l.Error == "authentication_failed"
			if l.Message.ID != "" {
				if st.calls == nil {
					st.calls = map[string]bool{}
				}
				st.calls[l.Message.ID] = true
			}
			if l.Error == "rate_limit" && l.APIError == "usage_limit_reached" {
				st.limited = true
				st.setReset(l.APIErrorParams.RateLimitInfo)
			}
		}
		var evs []provider.Event
		for _, c := range l.Message.Content {
			if c.Type == "tool_use" {
				evs = append(evs, provider.Event{
					Kind: provider.KindToolCall, ID: c.ID, Name: c.Name, Input: c.Input, Other: other,
				})
			}
		}
		return evs
	case "rate_limit_event":
		if l.RateLimitInfo.Status == "rejected" {
			st.setReset(l.RateLimitInfo)
		}
	case "stream_event":
		if l.Event.Type == "message_start" && !other {
			st.breakNext = st.wrote
		}
		if l.Event.Type == "content_block_delta" && l.Event.Delta.Type == "text_delta" {
			text := l.Event.Delta.Text
			if other {
				return []provider.Event{{Kind: provider.KindText, Text: text, Other: true}}
			}
			if st.breakNext {
				text, st.breakNext = _messageBreak+text, false
			}
			st.wrote = true
			return []provider.Event{{Kind: provider.KindText, Text: text}}
		}
	case "result":
		if other || len(l.Origin) > 0 && string(l.Origin) != "null" {
			if l.TotalCostUSD == nil {
				return nil
			}
			return []provider.Event{{Kind: provider.KindUsage, Other: true,
				Usage: &provider.Usage{SessionCostUSD: l.TotalCostUSD}}}
		}
		wrote, prev := st.wrote, *st
		var evs []provider.Event
		if u := (provider.Usage{
			Model: st.model, Calls: len(st.calls), InputTokens: l.Usage.InputTokens,
			OutputTokens: l.Usage.OutputTokens, CacheReadTokens: l.Usage.CacheReadTokens,
			CacheWriteTokens: l.Usage.CacheWriteTokens, SessionCostUSD: l.TotalCostUSD,
		}); u.Calls > 0 || u.SessionCostUSD != nil {
			evs = append(evs, provider.Event{Kind: provider.KindUsage, Usage: &u})
		}
		*st = state{model: st.model, calls: st.calls, noLifecycle: st.noLifecycle, req: st.req}
		final := provider.Event{Kind: provider.KindTurnEnd}
		if l.IsError {
			text := strings.Join(append([]string{l.Result}, l.Errors...), "\n")
			final = provider.Event{Kind: provider.KindError, Text: strings.TrimSpace(text), SignedOut: prev.signedOut,
				Limited: prev.limited, ResetsAt: prev.resetsAt}
		} else if !wrote && l.Result != "" {
			evs = append(evs, provider.Event{Kind: provider.KindText, Text: l.Result})
		}
		if !st.req.queued && !st.req.started {
			st.noLifecycle, st.req.ended = true, true
			return append(evs, final)
		}
		st.req.final = &final
		return append(evs, st.req.finish()...)
	}
	return nil
}

// lifecycle takes a command_lifecycle line. Claude reports Reef's uuid as queued, started, then a
// terminal state: any state but those two.
func (st *state) lifecycle(l outputLine) []provider.Event {
	r := &st.req
	if r.uuid == "" || l.CommandUUID != r.uuid {
		return nil
	}
	switch l.State {
	case "queued":
		r.queued = true
	case "started":
		r.started = true
	default:
		r.terminal = l.State
	}
	return r.finish()
}

// other reports whether the line being read is not Reef's: the request has ended, or claude has
// queued it and not yet started it. Before claude says anything of the request, and for a build
// that never does, lines are Reef's.
func (r *request) other() bool {
	return r.ended || r.queued && !r.started
}

// waiting reports whether claude has said the request ended and its result has not come.
func (r *request) waiting() bool {
	return !r.ended && r.final == nil && r.terminal != ""
}

// finish ends the request once its result and its terminal state are both in.
func (r *request) finish() []provider.Event {
	if r.ended || r.final == nil || r.terminal == "" {
		return nil
	}
	r.ended = true
	return []provider.Event{*r.final}
}

// expire fails a request claude said ended and gave no result for.
func (r *request) expire() []provider.Event {
	r.ended = true
	return []provider.Event{{Kind: provider.KindError, Text: "claude ended the request (" + r.terminal + ") without a result"}}
}

func newUUID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6], b[8] = b[6]&0x0f|0x40, b[8]&0x3f|0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[:4], b[4:6], b[6:8], b[8:10], b[10:])
}

func (s *Session) sentUUID() string {
	s.sentMu.Lock()
	defer s.sentMu.Unlock()
	return s.sent
}

func (st *state) setReset(info rateLimitInfo) {
	if info.ResetsAt > 0 {
		st.resetsAt = time.Unix(info.ResetsAt, 0).UTC()
	}
}

// Write appends p and drops the oldest bytes beyond 4 KB.
func (t *tail) Write(p []byte) (int, error) {
	t.b = append(t.b, p...)
	if len(t.b) > _stderrKeep {
		t.b = t.b[len(t.b)-_stderrKeep:]
	}
	return len(p), nil
}
