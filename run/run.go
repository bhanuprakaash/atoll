// Package run handles one message as one run: it starts or resumes the Bot's provider session,
// streams the run's events to the thread, holds approvals open until the user answers, and stores
// the reply and the finish_run report.
package run

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

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

const (
	// A slow SSE client must not stall the run: its channel holds this many events and an event
	// is dropped when it is full.
	_subscriberBuffer = 64
	_stopReason       = "Atoll stopped while the run was going"
	_atollTools       = "mcp__" + provider.MCPServer + "__"
	// _callWait bounds how long Ask waits for its tool call to come through the provider's
	// stdout, which races the permission prompt arriving over HTTP.
	_callWait = 2 * time.Second
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
	Manager struct {
		cfg    Config
		ctx    context.Context // ends when Close is called
		cancel context.CancelFunc
		wg     sync.WaitGroup

		mu        sync.Mutex
		runs      map[string]*active                 // by thread id
		tokens    map[string]*active                 // by run token
		approvals map[string]chan bool               // open approvals; each channel takes one answer
		subs      map[string]map[chan Event]struct{} // by thread id
	}

	// active is a run in progress. The Manager's mu guards log and open.
	active struct {
		threadID string
		token    string
		run      store.Run
		log      *os.File
		open     int                      // approvals open
		calls    map[string]chan struct{} // closed once the tool call with that id is published
	}

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

	eventLine struct {
		Type string    `json:"type"`
		Data any       `json:"data"`
		At   time.Time `json:"at"`
	}
)

// New returns a Manager with no runs going.
func New(cfg Config) *Manager {
	ctx, cancel := context.WithCancel(context.Background())
	return &Manager{
		cfg:       cfg,
		ctx:       ctx,
		cancel:    cancel,
		runs:      map[string]*active{},
		tokens:    map[string]*active{},
		approvals: map[string]chan bool{},
		subs:      map[string]map[chan Event]struct{}{},
	}
}

// Send stores text as the user's message and starts a run for it. It returns ErrBusy when the
// thread has a run going and wraps store.ErrNotFound for an unknown thread.
func (m *Manager) Send(ctx context.Context, threadID, text string) (store.Message, store.Run, error) {
	a := &active{threadID: threadID, token: randHex(16)}
	m.mu.Lock()
	if m.ctx.Err() != nil {
		m.mu.Unlock()
		return store.Message{}, store.Run{}, errors.New("atoll is stopping")
	}
	if _, busy := m.runs[threadID]; busy {
		m.mu.Unlock()
		return store.Message{}, store.Run{}, ErrBusy
	}
	m.runs[threadID] = a
	m.wg.Add(1)
	m.mu.Unlock()

	msg, run, err := m.cfg.Store.StartRun(ctx, threadID, text)
	if err != nil {
		m.drop(a)
		m.wg.Done()
		return store.Message{}, store.Run{}, fmt.Errorf("start run: %w", err)
	}
	a.run = run
	m.mu.Lock()
	m.tokens[a.token] = a
	m.mu.Unlock()
	go m.execute(a, text)
	return msg, run, nil
}

// Subscribe streams the thread's events from now on, until cancel is called.
func (m *Manager) Subscribe(threadID string) (events <-chan Event, cancel func()) {
	ch := make(chan Event, _subscriberBuffer)
	m.mu.Lock()
	if m.subs[threadID] == nil {
		m.subs[threadID] = map[chan Event]struct{}{}
	}
	m.subs[threadID][ch] = struct{}{}
	m.mu.Unlock()
	return ch, func() {
		m.mu.Lock()
		defer m.mu.Unlock()
		if _, ok := m.subs[threadID][ch]; ok {
			delete(m.subs[threadID], ch)
			close(ch)
		}
	}
}

// Answer gives the user's answer to the open approval id. It wraps ErrNotFound when none is open.
func (m *Manager) Answer(_ context.Context, id string, allow bool) error {
	m.mu.Lock()
	ch, ok := m.approvals[id]
	delete(m.approvals, id)
	m.mu.Unlock()
	if !ok {
		return fmt.Errorf("approval %s: %w", id, ErrNotFound)
	}
	ch <- allow
	return nil
}

// Report stores the finish_run report of the run with token.
func (m *Manager) Report(ctx context.Context, token string, r store.Report) error {
	a, err := m.byToken(token)
	if err != nil {
		return err
	}
	if err := m.cfg.Store.SetReport(ctx, a.run.ID, r); err != nil {
		return fmt.Errorf("report run %s: %w", a.run.ID, err)
	}
	return nil
}

// Ask is Claude's permission prompt for the run with token: it opens an approval and blocks
// until the user answers or ctx ends. Atoll's own tools are allowed without asking. The approval
// follows the tool call toolUseID on the thread's stream.
func (m *Manager) Ask(
	ctx context.Context, token, toolUseID, tool string, input json.RawMessage,
) (bool, error) {
	a, err := m.byToken(token)
	if err != nil {
		return false, err
	}
	if strings.HasPrefix(tool, _atollTools) {
		return true, nil
	}
	select {
	case <-m.called(a, toolUseID):
	case <-time.After(_callWait):
	case <-ctx.Done():
		return false, ctx.Err()
	}
	return m.approve(ctx, a, tool, input)
}

// called returns the channel closed once the tool call id of run a is published.
func (m *Manager) called(a *active, id string) chan struct{} {
	m.mu.Lock()
	defer m.mu.Unlock()
	if a.calls == nil {
		a.calls = map[string]chan struct{}{}
	}
	ch, ok := a.calls[id]
	if !ok {
		ch = make(chan struct{})
		a.calls[id] = ch
	}
	return ch
}

// Close stops every run, marks it failed, and waits for them.
func (m *Manager) Close() error {
	m.mu.Lock()
	m.cancel()
	m.mu.Unlock()
	m.wg.Wait()
	return nil
}

// execute is the goroutine of one run.
func (m *Manager) execute(a *active, text string) {
	defer m.wg.Done()
	start := time.Now()
	var reply string
	f, err := m.openLog(a.run.ID)
	if err == nil {
		defer func() { _ = f.Close() }()
		m.mu.Lock()
		a.log = f
		m.mu.Unlock()
		reply, err = m.drive(a, text)
	}
	status, errText := store.StatusDone, ""
	if err != nil {
		status, reply, errText = store.StatusFailed, "", err.Error()
		if m.ctx.Err() != nil {
			errText = _stopReason
		}
	}
	// A background context: FinishRun must outlive Close's cancel.
	err = m.cfg.Store.FinishRun(context.Background(), a.run.ID, status, strings.TrimSpace(reply), errText)
	if err != nil {
		m.cfg.Log.Error("finish run", "run_id", a.run.ID, "error", err)
	}
	m.cfg.Log.Info("run finished", "run_id", a.run.ID, "status", status, "error", errText,
		"duration", time.Since(start))
	// Dropped before run_finished: a client may send the next message the moment it sees it.
	m.drop(a)
	m.publish(a, EventRunFinished, Finished{RunID: a.run.ID, Status: status, Error: errText})
}

// drive runs the provider session for one prompt and returns its reply.
func (m *Manager) drive(a *active, text string) (string, error) {
	thread, err := m.cfg.Store.Thread(m.ctx, a.threadID)
	if err != nil {
		return "", fmt.Errorf("load thread: %w", err)
	}
	bot, err := m.cfg.Store.Bot(m.ctx, thread.BotID)
	if err != nil {
		return "", fmt.Errorf("load bot: %w", err)
	}
	m.cfg.Log.Info("run started", "run_id", a.run.ID, "thread_id", a.threadID,
		"provider", bot.Provider)
	sess, cfg, err := m.cfg.Launch(m.ctx, bot.Provider)
	if err != nil {
		return "", fmt.Errorf("launch %s: %w", bot.Provider, err)
	}
	defer func() {
		if err := sess.Interrupt(); err != nil {
			m.cfg.Log.Warn("interrupt provider", "run_id", a.run.ID, "error", err)
		}
	}()
	cfg.Cwd = filepath.Join(m.cfg.DataDir, "threads", thread.ID)
	if err := os.MkdirAll(cfg.Cwd, 0o755); err != nil {
		return "", fmt.Errorf("create thread folder: %w", err)
	}
	cfg.MCPURL = m.cfg.MCPURL + a.token
	if thread.Session != "" {
		err = sess.Resume(m.ctx, cfg, thread.Session)
	} else {
		err = sess.Start(m.ctx, cfg)
	}
	if err != nil {
		return "", fmt.Errorf("start %s: %w", bot.Provider, err)
	}
	prompt := fmt.Sprintf("You are %s, a Bot in Reef. %s\n"+
		"For every message, even a trivial one: first call the finish_run tool of the atoll "+
		"MCP server with done, tried, blocked and next, then reply in 2-3 plain lines.\n\n%s",
		bot.Name, bot.Persona, text)
	if err := sess.Send(m.ctx, prompt); err != nil {
		return "", fmt.Errorf("send prompt: %w", err)
	}
	return m.stream(a, sess)
}

// stream reads the turn's events until it ends and returns the reply.
func (m *Manager) stream(a *active, sess provider.Session) (string, error) {
	var reply strings.Builder
	for {
		select {
		case <-m.ctx.Done():
			return "", m.ctx.Err()
		case ev, ok := <-sess.Events():
			if !ok {
				return "", errors.New("provider exited before the turn ended")
			}
			switch ev.Kind {
			case provider.KindText:
				reply.WriteString(ev.Text)
				m.publish(a, EventText, Text{RunID: a.run.ID, Text: ev.Text})
			case provider.KindSession:
				if err := m.cfg.Store.SetSession(m.ctx, a.threadID, ev.SessionID); err != nil {
					return "", fmt.Errorf("save session: %w", err)
				}
			case provider.KindToolCall:
				m.publish(a, EventToolCall,
					ToolCall{RunID: a.run.ID, ID: ev.ID, Name: ev.Name, Input: ev.Input})
				if ch := m.called(a, ev.ID); !isClosed(ch) {
					close(ch)
				}
			case provider.KindApproval:
				allow, err := m.approve(m.ctx, a, ev.Name, ev.Input)
				if err != nil {
					return "", err
				}
				if err := sess.Answer(ev.ID, allow); err != nil {
					return "", fmt.Errorf("answer approval: %w", err)
				}
			case provider.KindTurnEnd:
				return reply.String(), nil
			case provider.KindError:
				return "", errors.New(ev.Text)
			}
		}
	}
}

// approve opens an approval and blocks until it is answered, ctx ends or the Manager closes.
func (m *Manager) approve(
	ctx context.Context, a *active, tool string, input json.RawMessage,
) (bool, error) {
	id := randHex(8)
	answer := make(chan bool, 1) // Answer must not wait for the run to read
	m.mu.Lock()
	m.approvals[id] = answer
	a.open++
	m.mu.Unlock()
	defer m.resume(ctx, a, id)
	if err := m.cfg.Store.SetStatus(ctx, a.run.ID, store.StatusWaiting); err != nil {
		return false, fmt.Errorf("approval %s: %w", id, err)
	}
	m.publish(a, EventApproval, Approval{RunID: a.run.ID, ID: id, Tool: tool, Input: input})
	select {
	case allow := <-answer:
		return allow, nil
	case <-ctx.Done():
		return false, fmt.Errorf("approval %s: %w", id, ctx.Err())
	case <-m.ctx.Done():
		return false, fmt.Errorf("approval %s: %w", id, m.ctx.Err())
	}
}

// resume closes approval id and sets the run back to running once none is open.
func (m *Manager) resume(ctx context.Context, a *active, id string) {
	m.mu.Lock()
	delete(m.approvals, id)
	a.open--
	idle := a.open == 0
	m.mu.Unlock()
	if !idle {
		return
	}
	err := m.cfg.Store.SetStatus(context.WithoutCancel(ctx), a.run.ID, store.StatusRunning)
	if err != nil {
		m.cfg.Log.Warn("set status running", "run_id", a.run.ID, "error", err)
	}
}

// publish sends an event to the thread's subscribers and appends it to the run's event file.
func (m *Manager) publish(a *active, typ string, data any) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if a.log != nil {
		line, err := json.Marshal(eventLine{Type: typ, Data: data, At: time.Now().UTC()})
		if err == nil {
			_, err = a.log.Write(append(line, '\n'))
		}
		if err != nil {
			m.cfg.Log.Warn("write event", "run_id", a.run.ID, "error", err)
		}
	}
	for ch := range m.subs[a.threadID] {
		select {
		case ch <- Event{Type: typ, Data: data}:
		default:
			m.cfg.Log.Warn("subscriber full, event dropped", "run_id", a.run.ID, "type", typ)
		}
	}
}

func (m *Manager) openLog(runID string) (*os.File, error) {
	dir := filepath.Join(m.cfg.DataDir, "runs")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create runs folder: %w", err)
	}
	f, err := os.OpenFile(filepath.Join(dir, runID+".jsonl"),
		os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open event file: %w", err)
	}
	return f, nil
}

func (m *Manager) byToken(token string) (*active, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	a, ok := m.tokens[token]
	if !ok {
		return nil, fmt.Errorf("run token: %w", ErrNotFound)
	}
	return a, nil
}

func (m *Manager) drop(a *active) {
	m.mu.Lock()
	delete(m.runs, a.threadID)
	delete(m.tokens, a.token)
	m.mu.Unlock()
}

func randHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func isClosed(ch chan struct{}) bool {
	select {
	case <-ch:
		return true
	default:
		return false
	}
}
