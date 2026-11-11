// Package claude is the Claude Code provider: one long-lived `claude -p` process speaking
// stream-json on stdin and stdout.
package claude

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
}

type (
	inputMessage struct {
		Type    string       `json:"type"`
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
		Event     struct {
			Type  string `json:"type"`
			Delta struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"delta"`
		} `json:"event"`
		Message struct {
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
	}

	// state is what parse keeps between lines. A message that starts after text in the same turn
	// has its first text delta begin a new paragraph.
	state struct {
		wrote, breakNext, signedOut bool
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
	msg, err := json.Marshal(inputMessage{
		Type:    "user",
		Message: inputContent{Role: "user", Content: content},
	})
	if err != nil {
		return err
	}
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
func (s *Session) Answer(string, bool) error {
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
	if mcpConfig != nil {
		r, w, err := os.Pipe()
		if err != nil {
			return err
		}
		defer func() { _ = r.Close() }()
		_, err = w.Write(mcpConfig)
		if cerr := w.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			return err
		}
		cmd.ExtraFiles = []*os.File{r}
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start claude: %w", err)
	}

	s.cmd, s.stdin = cmd, stdin
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
	isolation, err := IsolationArgs(cfg.Cwd, cfg.NoTools || cfg.NoShell)
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
func IsolationArgs(cwd string, noTools bool) ([]string, error) {
	excludes := []string{}
	seen := map[string]bool{}
	paths := []string{cwd}
	if resolved, err := filepath.EvalSymlinks(cwd); err == nil {
		paths = append(paths, resolved)
	}
	replacer := strings.NewReplacer(`\`, "?", "*", "?", "?", "?", "[", "?", "]", "?", "{", "?", "}", "?", "(", "?", ")", "?")
	for _, path := range paths {
		if path == "" {
			continue
		}
		for dir := filepath.Dir(path); ; dir = filepath.Dir(dir) {
			for _, pattern := range []string{filepath.Join(replacer.Replace(dir), "*.md"), filepath.Join(replacer.Replace(dir), ".claude", "**")} {
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
	settings, err := json.Marshal(map[string]any{
		"disableAllHooks": true, "disableBundledSkills": true,
		"skillOverrides":    map[string]string{"design": "off", "doctor": "off", "plugin-authoring": "off"},
		"autoMemoryEnabled": false, "claudeMdExcludes": excludes,
	})
	if err != nil {
		return nil, err
	}
	tools := _builtinTools
	if noTools {
		tools = ""
	}
	return []string{"--setting-sources", "project", "--settings", string(settings), "--tools", tools}, nil
}

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

	r := bufio.NewReader(stdout)
	var last provider.Kind
	var st state
	for {
		line, err := r.ReadBytes('\n')
		for _, ev := range parse(line, &st) {
			if !s.emit(ev) {
				_ = s.cmd.Wait()
				return
			}
			last = ev.Kind
		}
		if err != nil {
			break
		}
	}
	err := s.cmd.Wait()
	if err == nil || last == provider.KindError {
		return
	}
	s.emit(provider.Event{
		Kind: provider.KindError,
		Text: fmt.Sprintf("claude exited: %v: %s", err, bytes.TrimSpace(s.stderr.b)),
	})
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
// assistant line, as the partial input is cut off. Unknown lines give none.
func parse(line []byte, st *state) []provider.Event {
	var l outputLine
	if err := json.Unmarshal(line, &l); err != nil {
		return nil
	}
	switch l.Type {
	case "system":
		if l.Subtype == "init" {
			return []provider.Event{{Kind: provider.KindSession, SessionID: l.SessionID}}
		}
	case "assistant":
		st.signedOut = st.signedOut || l.Error == "authentication_failed"
		var evs []provider.Event
		for _, c := range l.Message.Content {
			if c.Type == "tool_use" {
				evs = append(evs, provider.Event{
					Kind: provider.KindToolCall, ID: c.ID, Name: c.Name, Input: c.Input,
				})
			}
		}
		return evs
	case "stream_event":
		if l.Event.Type == "message_start" {
			st.breakNext = st.wrote
		}
		if l.Event.Type == "content_block_delta" && l.Event.Delta.Type == "text_delta" {
			text := l.Event.Delta.Text
			if st.breakNext {
				text, st.breakNext = _messageBreak+text, false
			}
			st.wrote = true
			return []provider.Event{{Kind: provider.KindText, Text: text}}
		}
	case "result":
		wrote, signedOut := st.wrote, st.signedOut
		*st = state{}
		if l.IsError {
			text := strings.Join(append([]string{l.Result}, l.Errors...), "\n")
			return []provider.Event{{Kind: provider.KindError, Text: strings.TrimSpace(text), SignedOut: signedOut}}
		}
		if !wrote && l.Result != "" {
			return []provider.Event{{Kind: provider.KindText, Text: l.Result}, {Kind: provider.KindTurnEnd}}
		}
		return []provider.Event{{Kind: provider.KindTurnEnd}}
	}
	return nil
}

// Write appends p and drops the oldest bytes beyond 4 KB.
func (t *tail) Write(p []byte) (int, error) {
	t.b = append(t.b, p...)
	if len(t.b) > _stderrKeep {
		t.b = t.b[len(t.b)-_stderrKeep:]
	}
	return len(p), nil
}
