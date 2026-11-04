// Package codex implements provider.Session over `codex app-server`, a JSON-RPC conversation on
// the child's stdin and stdout.
package codex

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"atoll/provider"
)

var _ provider.Session = (*Session)(nil)

const (
	_killAfter = time.Second
	_waitDelay = 2 * time.Second
	_tailKeep  = 4 << 10

	_messageBreak = "\n\n"
)

type (
	// Session is one codex app-server process holding one thread. Use New.
	Session struct {
		cmd      *exec.Cmd
		stdin    io.Writer
		stdout   *bufio.Reader
		stderr   tail
		threadID string
		nextID   int
		events   chan provider.Event
		done     chan struct{} // closed by Interrupt
		exited   chan struct{} // closed when run has returned and Events is closed
		writeMu  sync.Mutex    // serializes writes to stdin
		mu       sync.Mutex    // guards used, cmd and Interrupt
		used     bool
		msgID    string // agent message item the last text delta belonged to; owned by run
		wrote    bool   // the turn in progress has sent text; owned by run
	}

	message struct {
		ID     json.RawMessage `json:"id"`
		Method string          `json:"method"`
		Params struct {
			Delta  string `json:"delta"`
			ItemID string `json:"itemId"`
			Turn   struct {
				Status string `json:"status"`
				Error  *struct {
					Message string `json:"message"`
				} `json:"error"`
			} `json:"turn"`
		} `json:"params"`
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Message string `json:"message"`
		} `json:"error"`
	}

	// tail is an io.Writer that keeps the last 4 KB written to it.
	tail struct{ b []byte }
)

// New returns a Session that has not started.
func New() *Session {
	return &Session{
		events: make(chan provider.Event, 1),
		done:   make(chan struct{}),
		exited: make(chan struct{}),
	}
}

// SignedIn reports whether `codex login status` with CODEX_HOME=dir exits 0. A non-zero exit is
// signed out; an error means codex could not be run.
func SignedIn(ctx context.Context, bin, dir string) (bool, error) {
	cmd := exec.CommandContext(ctx, bin, "login", "status")
	cmd.Env = env(dir)
	err := cmd.Run()
	if ctx.Err() != nil {
		return false, ctx.Err()
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return false, nil
	}
	return err == nil, err
}

// Login returns the `codex login` command with CODEX_HOME=dir. The caller wires its stdio.
func Login(ctx context.Context, bin, dir string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, bin, "login")
	cmd.Env = env(dir)
	return cmd
}

// Start runs codex in cfg.Cwd on a new thread.
func (s *Session) Start(ctx context.Context, cfg provider.Config) error {
	params, err := threadParams(cfg)
	if err != nil {
		return err
	}
	return s.launch(ctx, cfg, "thread/start", params)
}

// Resume runs codex in cfg.Cwd on the thread with the given id.
func (s *Session) Resume(ctx context.Context, cfg provider.Config, id string) error {
	params, err := threadParams(cfg)
	if err != nil {
		return err
	}
	params["threadId"] = id
	return s.launch(ctx, cfg, "thread/resume", params)
}

// Send starts a turn on the thread.
func (s *Session) Send(_ context.Context, prompt string) error {
	if s.threadID == "" {
		return errors.New("codex not started")
	}
	return s.request("turn/start", map[string]any{
		"threadId": s.threadID,
		"input":    []map[string]string{{"type": "text", "text": prompt}},
	})
}

// Events streams events until codex exits, then closes the channel.
func (s *Session) Events() <-chan provider.Event { return s.events }

// Interrupt stops codex, waits for it, and closes Events. Codex gets SIGTERM so it can stop the
// commands it started, which run in their own process groups; SIGKILL follows after _killAfter.
func (s *Session) Interrupt() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.interrupted() {
		return nil
	}
	s.used = true
	close(s.done)
	if s.cmd == nil {
		close(s.events)
		return nil
	}
	select {
	case <-s.exited:
		return nil
	default:
	}
	err := s.signal(syscall.SIGTERM)
	select {
	case <-s.exited:
	case <-time.After(_killAfter):
		err = errors.Join(err, s.signal(syscall.SIGKILL))
		<-s.exited
	}
	return err
}

func (s *Session) launch(
	ctx context.Context, cfg provider.Config, method string, params map[string]any,
) error {
	ready, err := s.spawn(cfg, method, params)
	if err != nil {
		return err
	}
	stop := context.AfterFunc(ctx, func() { _ = s.Interrupt() })
	err = <-ready
	if !stop() || err != nil {
		_ = s.Interrupt()
		if ctx.Err() != nil {
			return fmt.Errorf("%s: %w", method, ctx.Err())
		}
		if errors.Is(err, io.EOF) {
			return fmt.Errorf("codex exited: %w: %s", err, strings.TrimSpace(string(s.stderr.b)))
		}
		return err
	}
	return nil
}

// spawn starts the process and the goroutine that runs the handshake. It holds mu so that
// Interrupt sees either no process or a started one.
func (s *Session) spawn(
	cfg provider.Config, method string, params map[string]any,
) (<-chan error, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.used {
		return nil, errors.New("codex session already used")
	}
	s.used = true
	cmd := exec.Command(cfg.Bin, "app-server")
	cmd.Dir = cfg.Cwd
	cmd.Env = env(cfg.Dir)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Stderr = &s.stderr
	cmd.WaitDelay = _waitDelay
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start codex: %w", err)
	}
	s.cmd, s.stdin, s.stdout = cmd, stdin, bufio.NewReader(stdout)
	ready := make(chan error, 1)
	go s.run(method, params, ready)
	return ready, nil
}

// run owns stdout: it does the handshake, streams events, then reaps the process.
func (s *Session) run(method string, params map[string]any, ready chan<- error) {
	defer close(s.exited)
	defer close(s.events)
	err := s.handshake(method, params)
	ready <- err
	if err != nil {
		_ = s.cmd.Wait()
		return
	}
	s.read()
	// Wait closes stdout, so it must come after the last read.
	err = s.cmd.Wait()
	if err == nil || s.interrupted() {
		return
	}
	s.emit(provider.Event{
		Kind: provider.KindError,
		Text: fmt.Sprintf("codex exited: %v: %s", err, strings.TrimSpace(string(s.stderr.b))),
	})
}

func (s *Session) handshake(method string, params map[string]any) error {
	clientInfo := map[string]any{"name": "atoll", "title": nil, "version": "0"}
	if _, err := s.call("initialize", map[string]any{"clientInfo": clientInfo}); err != nil {
		return fmt.Errorf("initialize: %w", err)
	}
	if err := s.write(map[string]any{"method": "initialized"}); err != nil {
		return err
	}
	res, err := s.call(method, params)
	if err != nil {
		return fmt.Errorf("%s: %w", method, err)
	}
	var thread struct {
		Thread struct {
			ID string `json:"id"`
		} `json:"thread"`
	}
	if err := json.Unmarshal(res, &thread); err != nil {
		return fmt.Errorf("%s result: %w", method, err)
	}
	s.threadID = thread.Thread.ID
	return nil
}

func (s *Session) call(method string, params any) (json.RawMessage, error) {
	if err := s.request(method, params); err != nil {
		return nil, err
	}
	want := strconv.Itoa(s.nextID)
	for {
		line, err := s.stdout.ReadBytes('\n')
		if err != nil {
			return nil, err
		}
		var m message
		if json.Unmarshal(line, &m) != nil || string(m.ID) != want || m.Method != "" {
			continue
		}
		if m.Error != nil {
			return nil, errors.New(m.Error.Message)
		}
		return m.Result, nil
	}
}

func (s *Session) request(method string, params any) error {
	s.nextID++
	return s.write(map[string]any{"id": s.nextID, "method": method, "params": params})
}

func (s *Session) write(v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	_, err = s.stdin.Write(append(b, '\n'))
	return err
}

func (s *Session) read() {
	if !s.emit(provider.Event{Kind: provider.KindSession, SessionID: s.threadID}) {
		return
	}
	for {
		line, err := s.stdout.ReadBytes('\n')
		var m message
		if json.Unmarshal(line, &m) == nil {
			if m.Method != "" && len(m.ID) > 0 {
				// Codex asks the client something; nobody is there to answer.
				_ = s.write(map[string]any{
					"id":    m.ID,
					"error": map[string]any{"code": -32601, "message": "unsupported"},
				})
			} else if !s.handle(m) {
				return
			}
		}
		if err != nil {
			return
		}
	}
}

// handle emits the event for m and reports whether to keep reading. A break goes before the text
// of a second agent message in a turn.
func (s *Session) handle(m message) bool {
	switch m.Method {
	case "turn/started":
		s.msgID, s.wrote = "", false
	case "item/agentMessage/delta":
		if s.wrote && m.Params.ItemID != s.msgID &&
			!s.emit(provider.Event{Kind: provider.KindText, Text: _messageBreak}) {
			return false
		}
		s.msgID, s.wrote = m.Params.ItemID, true
	}
	ev, ok := m.event()
	if !ok {
		return true
	}
	return s.emit(ev)
}

func (s *Session) emit(ev provider.Event) bool {
	select {
	case s.events <- ev:
		return true
	case <-s.done:
		return false
	}
}

func (s *Session) interrupted() bool {
	select {
	case <-s.done:
		return true
	default:
		return false
	}
}

func (s *Session) signal(sig syscall.Signal) error {
	err := syscall.Kill(-s.cmd.Process.Pid, sig)
	if errors.Is(err, syscall.ESRCH) {
		return nil
	}
	return err
}

func (m message) event() (ev provider.Event, ok bool) {
	p := m.Params
	switch {
	case m.Method == "" && m.Error != nil:
		return provider.Event{Kind: provider.KindError, Text: m.Error.Message}, true
	case m.Method == "item/agentMessage/delta":
		return provider.Event{Kind: provider.KindText, Text: p.Delta}, true
	case m.Method == "turn/completed" && p.Turn.Status == "failed":
		msg := "turn failed"
		if p.Turn.Error != nil {
			msg = p.Turn.Error.Message
		}
		return provider.Event{Kind: provider.KindError, Text: msg}, true
	case m.Method == "turn/completed":
		return provider.Event{Kind: provider.KindTurnEnd}, true
	}
	return provider.Event{}, false
}

func (t *tail) Write(p []byte) (int, error) {
	t.b = append(t.b, p...)
	if len(t.b) > _tailKeep {
		t.b = t.b[len(t.b)-_tailKeep:]
	}
	return len(p), nil
}

func env(dir string) []string {
	return append(os.Environ(), "CODEX_HOME="+dir)
}

func threadParams(cfg provider.Config) (map[string]any, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	config, err := threadConfig(home)
	if err != nil {
		return nil, err
	}
	return map[string]any{"cwd": cfg.Cwd, "approvalPolicy": "never", "config": config}, nil
}

// threadConfig turns off everything of the user's that Codex would load besides what CODEX_HOME
// holds. Codex finds ~/.agents/skills through HOME, which stays the user's, so each skill there
// is disabled by the path of its SKILL.md.
func threadConfig(home string) (map[string]any, error) {
	skills := map[string]any{"bundled": map[string]bool{"enabled": false}}
	entries, err := os.ReadDir(filepath.Join(home, ".agents", "skills"))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	var off []map[string]any
	for _, e := range entries {
		path := filepath.Join(home, ".agents", "skills", e.Name(), "SKILL.md")
		off = append(off, map[string]any{"path": path, "enabled": false})
	}
	if len(off) > 0 {
		skills["config"] = off
	}
	return map[string]any{
		"skills":   skills,
		"features": map[string]bool{"apps": false, "plugins": false, "remote_plugin": false},
	}, nil
}
