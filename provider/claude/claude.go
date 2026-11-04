// Package claude is the Claude Code provider: one long-lived `claude -p` process speaking
// stream-json on stdin and stdout.
package claude

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"

	"atoll/provider"
)

var _ provider.Session = (*Session)(nil)

const (
	// SIGTERM lets claude stop the tool processes it started; they are not in its group.
	_termGrace = time.Second
	// WaitDelay bounds Wait when a process that outlived claude holds its stderr pipe.
	_waitDelay = 2 * time.Second
	// _messageBreak goes before the text of a message that follows text in the same turn.
	_messageBreak = "\n\n"
	_stderrKeep   = 4 << 10
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
		Content string `json:"content"`
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
		IsError bool     `json:"is_error"`
		Result  string   `json:"result"`
		Errors  []string `json:"errors"`
	}

	// state is what parse keeps between lines. A message that starts after text in the same turn
	// has its first text delta begin a new paragraph.
	state struct {
		wrote, breakNext bool
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

// Resume launches claude in cfg.Cwd continuing the session with the given id.
func (s *Session) Resume(_ context.Context, cfg provider.Config, id string) error {
	return s.start(cfg, "--resume", id)
}

// Send writes the prompt to claude as one user message.
func (s *Session) Send(ctx context.Context, prompt string) error {
	if s.stdin == nil {
		return errors.New("claude not started")
	}
	msg, err := json.Marshal(inputMessage{
		Type:    "user",
		Message: inputContent{Role: "user", Content: prompt},
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

// Login returns the command that signs claude in to dir. The caller wires its stdio.
func Login(ctx context.Context, bin, dir string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, bin, "auth", "login")
	cmd.Env = configEnv(dir)
	return cmd
}

func configEnv(dir string) []string {
	return provider.Env("CLAUDE_CONFIG_DIR=" + dir)
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

	args := append([]string{
		"-p",
		"--input-format", "stream-json",
		"--output-format", "stream-json",
		"--verbose",
		"--include-partial-messages",
		"--strict-mcp-config",
		"--settings", `{"disableAllHooks":true}`,
	}, extra...)
	cmd := exec.Command(cfg.Bin, args...)
	cmd.Dir = cfg.Cwd
	cmd.Env = append(configEnv(cfg.Dir), "ENABLE_CLAUDEAI_MCP_SERVERS=false")
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
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start claude: %w", err)
	}

	s.cmd, s.stdin = cmd, stdin
	go s.read(stdout)
	return nil
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
// complete assistant text blocks that follow are skipped. Unknown lines give none.
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
		*st = state{}
		if l.IsError {
			text := strings.Join(append([]string{l.Result}, l.Errors...), "\n")
			return []provider.Event{{Kind: provider.KindError, Text: strings.TrimSpace(text)}}
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
