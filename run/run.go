// Package run handles one message as one run: it starts or resumes the Bot's provider session,
// streams the run's events to the thread, holds approvals open until the user answers, and stores
// the reply and the finish_run report.
package run

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"atoll/connection"
	"atoll/memory"
	"atoll/provider"
	"atoll/skill"
	"atoll/store"
)

var (
	// ErrBusy is returned when a thread already has a run going.
	ErrBusy = errors.New("thread has a run going")
	// ErrClosed is returned when a message is sent to a closed thread.
	ErrClosed = errors.New("thread is closed")
	// ErrNotFound is wrapped when an approval id or run token matches nothing open.
	ErrNotFound = errors.New("not found")
	// ErrNoGitHubConnection means the Bot has no GitHub connection.
	ErrNoGitHubConnection = errors.New("this repo is on GitHub and this Bot has no GitHub connection: " +
		"give this Bot the GitHub connection in its settings, then ask again")
)

// Event types on a thread's stream.
const (
	EventText        = "text"            // Text: a piece of the reply
	EventToolCall    = "tool_call"       // ToolCall
	EventApproval    = "approval"        // Approval
	EventApprovalEnd = "approval_closed" // ApprovalClosed
	EventRunFinished = "run_finished"    // Finished

	EventWorktreeCreated = "worktree_created" // WorktreeCreated
	EventMRUpdated       = "mr_updated"       // MRUpdated
	EventWorktreeRemoved = "worktree_removed" // WorktreeRemoved
	EventMemorySaved     = "memory_saved"     // MemorySaved
	EventPrompt          = "prompt"           // Prompt
)

const (
	// A slow SSE client must not stall the run: its channel holds this many events and the
	// subscriber is closed when it is full, so the client reconnects.
	_subscriberBuffer = 64
	_stopReason       = "Atoll stopped while the run was going"
	_atollTools       = "mcp__" + provider.MCPServer + "__"
	// _callWait bounds how long Ask waits for its tool call to come through the provider's
	// stdout, which races the permission prompt arriving over HTTP.
	_callWait = 2 * time.Second
	_nudge    = "You did not call finish_run. Call the finish_run tool of the atoll MCP server " +
		"now with done, tried, blocked and next. Do not reply with anything else."
)

const _promptItems = 8

// Whether stream shows the turn's text as the reply.
const (
	_hideText = false
	_showText = true
)

type (
	// Launch returns a provider session that has not started, and the config to start it with
	// (all but Cwd and MCPURL), for the provider name of a Bot.
	Launch func(ctx context.Context, name string) (provider.Session, provider.Config, error)

	// Config is what a Manager needs.
	Config struct {
		Store     *store.Store
		Memory    *memory.Store
		Launch    Launch
		Available func(context.Context, string) bool
		// SignedOut is told the provider name of a run that failed because it is signed out.
		SignedOut func(string)
		// MCPURL is the base of Atoll's MCP endpoint; a run's server is MCPURL + its token.
		MCPURL string
		// DataDir holds runs/<run id>.jsonl, each run's event stream, and threads/<thread id>,
		// the folder a thread's runs work in: a provider resumes a session only in its folder.
		DataDir string
		// WorktreeRoot is where worktrees go until the user sets another root.
		WorktreeRoot string
		// PollInterval is how often Start's watcher checks the MRs.
		PollInterval time.Duration
		Log          *slog.Logger
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
		approvals map[string]open                    // open approvals, by approval id
		subs      map[string]map[chan Event]struct{} // by thread id
	}

	// open is an approval waiting for the user; answer takes its one answer.
	open struct {
		approval Approval
		answer   chan bool
	}

	// active is a run in progress. The Manager's mu guards log and open.
	active struct {
		threadID string
		ctx      context.Context
		cancel   context.CancelFunc
		done     chan struct{}
		stopped  bool
		ending   bool
		token    string
		run      store.Run
		bot      store.Bot // settings as the run started
		log      *os.File
		open     int                      // approvals open
		calls    map[string]chan struct{} // closed once the tool call with that id is published
		tool     ToolCall
		shown    []string // memory ids the run's memory tools returned
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
		RunID  string          `json:"run_id"`
		ID     string          `json:"id"`
		Name   string          `json:"name"`
		Kind   string          `json:"kind"`
		Target string          `json:"target"`
		Verb   string          `json:"verb"`
		Input  json.RawMessage `json:"input"`
	}

	// Approval is a step that waits for the user; ID is what Answer takes.
	Approval struct {
		RunID string          `json:"run_id"`
		ID    string          `json:"id"`
		Tool  string          `json:"tool"`
		Input json.RawMessage `json:"input"`
	}

	// ApprovalClosed says the approval id is no longer open: answered, or its run or request ended.
	ApprovalClosed struct {
		RunID string `json:"run_id"`
		ID    string `json:"id"`
	}

	// Prompt is what Atoll sent the provider: its system addition and the message.
	Prompt struct {
		RunID   string `json:"run_id"`
		System  string `json:"system"`
		Message string `json:"message"`
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
		approvals: map[string]open{},
		subs:      map[string]map[chan Event]struct{}{},
	}
}

// Send stores text as the user's message and starts a run for it. It returns ErrBusy when the
// thread has a run going and wraps store.ErrNotFound for an unknown thread.
func (m *Manager) Send(ctx context.Context, threadID, text string) (store.Message, store.Run, error) {
	runCtx, cancel := context.WithCancel(m.ctx)
	a := &active{threadID: threadID, token: randHex(16), ctx: runCtx, cancel: cancel, done: make(chan struct{})}
	m.mu.Lock()
	if m.ctx.Err() != nil {
		cancel()
		m.mu.Unlock()
		return store.Message{}, store.Run{}, errors.New("atoll is stopping")
	}
	if _, busy := m.runs[threadID]; busy {
		cancel()
		m.mu.Unlock()
		return store.Message{}, store.Run{}, ErrBusy
	}
	m.runs[threadID] = a
	m.wg.Add(1)
	m.mu.Unlock()

	msg, run, err := m.start(ctx, threadID, text)
	if err != nil {
		m.drop(a)
		cancel()
		m.wg.Done()
		return store.Message{}, store.Run{}, err
	}
	m.mu.Lock()
	a.run = run
	m.tokens[a.token] = a
	m.mu.Unlock()
	go m.execute(a, text)
	return msg, run, nil
}

// start refuses a closed thread, then stores the message and its run.
func (m *Manager) start(ctx context.Context, threadID, text string) (store.Message, store.Run, error) {
	t, err := m.cfg.Store.Thread(ctx, threadID)
	if err != nil {
		return store.Message{}, store.Run{}, fmt.Errorf("start run: %w", err)
	}
	if t.Closed {
		return store.Message{}, store.Run{}, ErrClosed
	}
	msg, run, err := m.cfg.Store.StartRun(ctx, threadID, text)
	if err != nil {
		return store.Message{}, store.Run{}, fmt.Errorf("start run: %w", err)
	}
	return msg, run, nil
}

// Available reports whether the provider is installed and signed in.
func (m *Manager) Available(ctx context.Context, name string) bool {
	if m.cfg.Available == nil {
		return true
	}
	return m.cfg.Available(ctx, name)
}

// Stop ends an active run and waits for its provider to exit.
func (m *Manager) Stop(ctx context.Context, id string) error {
	ru, err := m.cfg.Store.Run(ctx, id)
	if err != nil {
		return err
	}
	m.mu.Lock()
	a := m.runs[ru.ThreadID]
	if a == nil || a.run.ID != id || a.stopped || a.ending {
		m.mu.Unlock()
		return ErrBusy
	}
	a.stopped = true
	a.cancel()
	m.mu.Unlock()
	<-a.done
	return nil
}

// Subscribe streams the thread's events from now on, until cancel is called.
func (m *Manager) Subscribe(threadID string) (events <-chan Event, cancel func()) {
	ch := make(chan Event, _subscriberBuffer)
	m.mu.Lock()
	if m.ctx.Err() != nil {
		close(ch)
		m.mu.Unlock()
		return ch, func() {}
	}
	if a := m.runs[threadID]; a != nil && a.log != nil {
		for _, ev := range m.replay(a.run.ID) {
			ch <- ev
		}
	}
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

// Active returns the id of the run going in the thread and its open approvals. ok is false when
// the thread has no run going.
func (m *Manager) Active(threadID string) (runID string, approvals []Approval, ok bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	a, ok := m.runs[threadID]
	if !ok || a.run.ID == "" {
		return "", nil, false
	}
	for _, o := range m.approvals {
		if o.approval.RunID == a.run.ID {
			approvals = append(approvals, o.approval)
		}
	}
	return a.run.ID, approvals, true
}

// ActiveTool returns the latest tool of an active run.
func (m *Manager) ActiveTool(threadID string) ToolCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	if a := m.runs[threadID]; a != nil {
		return a.tool
	}
	return ToolCall{}
}

// Answer gives the user's answer to the open approval id. It wraps ErrNotFound when none is open.
func (m *Manager) Answer(_ context.Context, id string, allow bool) error {
	m.mu.Lock()
	o, ok := m.approvals[id]
	delete(m.approvals, id)
	m.mu.Unlock()
	if !ok {
		return fmt.Errorf("approval %s: %w", id, ErrNotFound)
	}
	o.answer <- allow
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

// Close stops every run, marks it failed, waits for them, and closes every subscriber.
func (m *Manager) Close() error {
	m.mu.Lock()
	m.cancel()
	m.mu.Unlock()
	m.wg.Wait()
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, chs := range m.subs {
		for ch := range chs {
			delete(chs, ch)
			close(ch)
		}
	}
	return nil
}

// execute is the goroutine of one run.
func (m *Manager) execute(a *active, text string) {
	defer m.wg.Done()
	defer close(a.done)
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
	res := store.RunResult{Status: store.StatusDone, Reply: strings.TrimSpace(reply)}
	if err != nil {
		res = store.RunResult{Status: store.StatusFailed, Error: err.Error()}
		if m.ctx.Err() != nil {
			res.Error = _stopReason
		} else if errors.Is(err, provider.ErrSignedOut) {
			res.ErrorKind = store.ErrorSignedOut
			if m.cfg.SignedOut != nil {
				m.cfg.SignedOut(a.bot.Provider)
			}
		}
	}
	m.mu.Lock()
	a.ending = true
	if a.stopped {
		res = store.RunResult{Status: "stopped"}
	}
	m.mu.Unlock()
	res.Provider = a.bot.Provider
	// A background context: FinishRun must outlive Close's cancel.
	for range 2 {
		err = m.cfg.Store.FinishRun(context.Background(), a.run.ID, res)
		if err == nil {
			break
		}
	}
	if err != nil {
		m.cfg.Log.Error("finish run", "run_id", a.run.ID, "error", err)
		res.Status, res.Error = store.StatusFailed, "could not save the run: "+err.Error()
	}
	m.cfg.Log.Info("run finished", "run_id", a.run.ID, "status", res.Status, "error", res.Error,
		"duration", time.Since(start))
	// Dropped before run_finished: a client may send the next message the moment it sees it.
	m.drop(a)
	m.publish(a, EventRunFinished, Finished{RunID: a.run.ID, Status: res.Status, Error: res.Error})
}

// drive runs the provider session for one prompt and returns its reply.
func (m *Manager) drive(a *active, text string) (string, error) {
	thread, err := m.cfg.Store.Thread(a.ctx, a.threadID)
	if err != nil {
		return "", fmt.Errorf("load thread: %w", err)
	}
	bot, err := m.cfg.Store.Bot(a.ctx, thread.BotID)
	if err != nil {
		return "", fmt.Errorf("load bot: %w", err)
	}
	m.mu.Lock()
	a.bot = bot
	m.mu.Unlock()
	m.cfg.Log.Info("run started", "run_id", a.run.ID, "thread_id", a.threadID,
		"provider", bot.Provider)
	sess, cfg, err := m.cfg.Launch(a.ctx, bot.Provider)
	if err != nil {
		return "", fmt.Errorf("launch %s: %w", bot.Provider, err)
	}
	skillDir := filepath.Join(cfg.Dir, "runs", a.run.ID)
	defer func() {
		if err := os.RemoveAll(skillDir); err != nil {
			m.cfg.Log.Warn("remove skill dir", "run_id", a.run.ID, "error", err)
		}
	}()
	defer func() {
		if err := sess.Interrupt(); err != nil {
			m.cfg.Log.Warn("interrupt provider", "run_id", a.run.ID, "error", err)
		}
	}()
	var rp store.Repo
	if thread.RepoID != "" && bot.RepoAccess.Allows(thread.RepoID) {
		if rp, err = m.cfg.Store.Repo(a.ctx, thread.RepoID); err != nil {
			return "", fmt.Errorf("load repo: %w", err)
		}
		rp.Host = RepoHost(a.ctx, rp.Path)
	}
	if cfg.Cwd, err = m.workdir(thread, rp); err != nil {
		return "", err
	}
	user, err := m.cfg.Store.Setting(a.ctx, "user_name")
	if err != nil {
		return "", fmt.Errorf("load user name: %w", err)
	}
	cfg.Model = bot.Model
	cfg.SystemPrompt = systemPrompt(bot, user)
	skills := make([]store.Skill, 0, len(bot.Skills))
	for _, id := range bot.Skills {
		sk, err := m.cfg.Store.Skill(a.ctx, id)
		if errors.Is(err, store.ErrNotFound) {
			continue
		}
		if err != nil {
			return "", fmt.Errorf("load skill: %w", err)
		}
		skills = append(skills, sk)
	}
	if cfg.SkillDir, err = skill.Place(skillDir, bot.Provider, skills); err != nil {
		return "", fmt.Errorf("place skills: %w", err)
	}
	if cfg.MCPServers, err = m.mcpServers(a.ctx, bot); err != nil {
		return "", err
	}
	var items []memory.Item
	var counts memory.Counts
	if m.cfg.Memory != nil {
		shelf := memory.Shelf{RepoID: rp.ID, BotID: thread.BotID}
		var seen map[string]bool
		if thread.Session != "" {
			seen, err = m.cfg.Memory.Seen(a.ctx, thread.Session)
			if err != nil {
				return "", fmt.Errorf("load seen memory: %w", err)
			}
		} else {
			counts, err = m.cfg.Memory.Count(a.ctx, shelf)
			if err != nil {
				return "", fmt.Errorf("count memory: %w", err)
			}
		}
		items, err = m.cfg.Memory.Relevant(a.ctx, shelf, text, _promptItems, seen)
		if err != nil {
			return "", fmt.Errorf("select memory: %w", err)
		}
	}
	prompt := messagePrompt(text, items, counts, thread, rp, cfg.Cwd)
	var githubToken string
	cfg.Env, githubToken, err = m.runEnv(a.ctx, bot, true)
	if err != nil {
		return "", err
	}
	if githubToken != "" {
		cfg.GitHubMCPURL, cfg.GitHubToken = connection.GitHubMCPURL(), githubToken
		cfg.MCPServers = slices.DeleteFunc(cfg.MCPServers, func(s provider.UserMCPServer) bool {
			return strings.TrimSuffix(s.URL, "/") == strings.TrimSuffix(cfg.GitHubMCPURL, "/")
		})
	}
	cfg.MCPURL = m.cfg.MCPURL + a.token
	if thread.Session != "" {
		err = sess.Resume(a.ctx, cfg, thread.Session)
	} else {
		err = sess.Start(a.ctx, cfg)
	}
	if err != nil {
		return "", fmt.Errorf("start %s: %w", bot.Provider, err)
	}
	m.publish(a, EventPrompt, Prompt{RunID: a.run.ID, System: cfg.SystemPrompt, Message: prompt})
	if err := sess.Send(a.ctx, prompt); err != nil {
		return "", fmt.Errorf("send prompt: %w", err)
	}
	reply, err := m.stream(a, sess, _showText)
	if err != nil {
		return "", err
	}
	m.mu.Lock()
	ids := slices.Clone(a.shown)
	m.mu.Unlock()
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	if m.cfg.Memory != nil && len(ids) > 0 {
		// A resume may report a new session id, so the marks follow the reloaded thread.
		if updated, err := m.cfg.Store.Thread(a.ctx, a.threadID); err != nil {
			m.cfg.Log.Warn("reload thread", "run_id", a.run.ID, "error", err)
		} else if updated.Session != "" {
			if err := m.cfg.Memory.MarkSeen(a.ctx, updated.Session, ids); err != nil {
				m.cfg.Log.Warn("mark memory seen", "run_id", a.run.ID, "error", err)
			}
		}
	}
	run, err := m.cfg.Store.Run(a.ctx, a.run.ID)
	if err != nil {
		return "", fmt.Errorf("load run: %w", err)
	}
	if run.Report != nil {
		return reply, nil
	}
	// The nudge only asks for the report: its failure keeps the first turn's reply.
	nerr := sess.Send(a.ctx, _nudge)
	if nerr == nil {
		_, nerr = m.stream(a, sess, _hideText)
	}
	if a.ctx.Err() != nil {
		return "", a.ctx.Err()
	}
	if nerr != nil {
		m.cfg.Log.Warn("finish_run nudge failed", "run_id", a.run.ID, "error", nerr)
	}
	if run, err = m.cfg.Store.Run(a.ctx, a.run.ID); err != nil {
		return "", fmt.Errorf("load run: %w", err)
	}
	if run.Report == nil {
		m.cfg.Log.Warn("run ended without a finish_run report", "run_id", a.run.ID)
	}
	return reply, nil
}

// stream reads the turn's events until it ends and returns the reply. With showText false the
// turn's text is dropped.
func (m *Manager) stream(a *active, sess provider.Session, showText bool) (string, error) {
	var reply strings.Builder
	for {
		select {
		case <-a.ctx.Done():
			return "", a.ctx.Err()
		case ev, ok := <-sess.Events():
			if !ok {
				return "", errors.New("provider exited before the turn ended")
			}
			switch ev.Kind {
			case provider.KindText:
				if showText {
					reply.WriteString(ev.Text)
					m.publish(a, EventText, Text{RunID: a.run.ID, Text: ev.Text})
				}
			case provider.KindSession:
				if err := m.cfg.Store.SetSession(a.ctx, a.threadID, ev.SessionID); err != nil {
					return "", fmt.Errorf("save session: %w", err)
				}
			case provider.KindToolCall:
				kind, target := ToolKind(ev.Name, ev.Input)
				if strings.Contains(ev.Name, "read_memory") && m.cfg.Memory != nil {
					var input struct {
						ID        string `json:"id"`
						Arguments struct {
							ID string `json:"id"`
						} `json:"arguments"`
					}
					if json.Unmarshal(ev.Input, &input) == nil {
						if input.Arguments.ID != "" {
							input.ID = input.Arguments.ID
						}
						item, err := m.cfg.Memory.Read(a.ctx, input.ID)
						shelf, shelfErr := m.shelf(a.ctx, a.token)
						if err == nil && shelfErr == nil && shelf.Has(item) {
							target = shortMemory(item.Body)
						}
					}
				}
				if !strings.HasSuffix(ev.Name, "finish_run") {
					if err := m.cfg.Store.CountTool(a.ctx, a.run.ID, kind); err != nil {
						m.cfg.Log.Warn("count tool", "error", err)
					}
				}
				m.publish(a, EventToolCall,
					ToolCall{RunID: a.run.ID, ID: ev.ID, Name: ev.Name, Input: ev.Input, Kind: kind, Target: target, Verb: ToolVerb(ev.Name, kind)})
				if ch := m.called(a, ev.ID); !isClosed(ch) {
					close(ch)
				}
			case provider.KindApproval:
				allow, err := m.approve(a.ctx, a, ev.Name, ev.Input)
				if err != nil {
					return "", err
				}
				if err := sess.Answer(ev.ID, allow); err != nil {
					return "", fmt.Errorf("answer approval: %w", err)
				}
			case provider.KindTurnEnd:
				return reply.String(), nil
			case provider.KindError:
				if ev.SignedOut {
					return "", fmt.Errorf("%w: %s", provider.ErrSignedOut, ev.Text)
				}
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
	ap := Approval{RunID: a.run.ID, ID: id, Tool: tool, Input: input}
	m.mu.Lock()
	m.approvals[id] = open{approval: ap, answer: answer}
	a.open++
	m.mu.Unlock()
	defer m.resume(ctx, a, id)
	if err := m.cfg.Store.SetStatus(ctx, a.run.ID, store.StatusWaiting); err != nil {
		return false, fmt.Errorf("approval %s: %w", id, err)
	}
	m.publish(a, EventApproval, ap)
	select {
	case allow := <-answer:
		return allow, nil
	case <-ctx.Done():
		return false, fmt.Errorf("approval %s: %w", id, ctx.Err())
	case <-a.ctx.Done():
		return false, fmt.Errorf("approval %s: %w", id, a.ctx.Err())
	}
}

// resume closes approval id and sets the run back to running once none is open.
func (m *Manager) resume(ctx context.Context, a *active, id string) {
	m.mu.Lock()
	delete(m.approvals, id)
	a.open--
	idle := a.open == 0
	m.mu.Unlock()
	m.publish(a, EventApprovalEnd, ApprovalClosed{RunID: a.run.ID, ID: id})
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
	if call, ok := data.(ToolCall); ok {
		a.tool = call
	}
	m.deliver(a.threadID, a.run.ID, a.log, typ, data)
}

// announce is publish for an event outside a run: it goes to the file of the thread's latest run.
func (m *Manager) announce(threadID, runID, typ string, data any) {
	var f *os.File
	if runID != "" {
		var err error
		if f, err = m.openLog(runID); err != nil {
			m.cfg.Log.Warn("open event file", "run_id", runID, "error", err)
		} else {
			defer func() { _ = f.Close() }()
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.deliver(threadID, runID, f, typ, data)
}

// deliver writes the event to log, when there is one, and sends it to the thread's subscribers.
// The caller holds mu.
func (m *Manager) deliver(threadID, runID string, log *os.File, typ string, data any) {
	if log != nil {
		line, err := json.Marshal(eventLine{Type: typ, Data: data, At: time.Now().UTC()})
		if err == nil {
			_, err = log.Write(append(line, '\n'))
		}
		if err != nil {
			m.cfg.Log.Warn("write event", "run_id", runID, "error", err)
		}
	}
	for ch := range m.subs[threadID] {
		select {
		case ch <- Event{Type: typ, Data: data}:
		default:
			delete(m.subs[threadID], ch)
			close(ch)
			m.cfg.Log.Warn("subscriber full, stream closed", "run_id", runID, "type", typ)
		}
	}
}

// replay is the running run's events so far, so a subscriber that joins late (a page opened
// after the first message, or one reconnecting) sees the current tool and approvals. Text deltas
// are left out and only the latest events are kept, to fit the subscriber's buffer.
func (m *Manager) replay(runID string) []Event {
	b, err := os.ReadFile(filepath.Join(m.cfg.DataDir, "runs", runID+".jsonl"))
	if err != nil {
		m.cfg.Log.Warn("read event file", "run_id", runID, "error", err)
		return nil
	}
	var evs []Event
	for line := range bytes.Lines(b) {
		var ev struct {
			Type string          `json:"type"`
			Data json.RawMessage `json:"data"`
		}
		if json.Unmarshal(line, &ev) != nil || ev.Type == EventText {
			continue
		}
		evs = append(evs, Event{Type: ev.Type, Data: ev.Data})
	}
	return evs[max(0, len(evs)-_subscriberBuffer/2):]
}

func (m *Manager) openLog(runID string) (*os.File, error) {
	dir := filepath.Join(m.cfg.DataDir, "runs")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create runs folder: %w", err)
	}
	f, err := os.OpenFile(filepath.Join(dir, runID+".jsonl"),
		os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
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

// ToolVerb describes the current work without exposing provider tool names.
func ToolVerb(name, kind string) string {
	switch {
	case strings.Contains(name, "start_work"):
		return "Starting a branch"
	case strings.Contains(name, "track_mr"):
		return "Opening the MR"
	case strings.Contains(name, "retire_memory"), strings.Contains(name, "remember"):
		return "Taking a note"
	case strings.Contains(name, "recall"), strings.Contains(name, "read_memory"):
		return "Remembering"
	}
	switch kind {
	case "search":
		return "Searching"
	case "read":
		return "Reading"
	case "edit":
		return "Editing"
	case "command":
		return "Running"
	case "memory":
		return "Remembering"
	case "platform":
		return "Setting up"
	default:
		return "Thinking"
	}
}

// ToolKind classifies a provider tool and extracts its target.
func ToolKind(name string, input json.RawMessage) (kind, target string) {
	var v struct {
		Pattern  string `json:"pattern"`
		Query    string `json:"query"`
		Topic    string `json:"topic"`
		Name     string `json:"name"`
		FilePath string `json:"file_path"`
		Command  string `json:"command"`
		Desc     string `json:"description"`
		Actions  []struct {
			Type    string `json:"type"`
			Query   string `json:"query"`
			Path    string `json:"path"`
			Command string `json:"command"`
		} `json:"commandActions"`
		Changes []struct {
			Path string `json:"path"`
		} `json:"changes"`
	}
	_ = json.Unmarshal(input, &v)
	switch name {
	case "Grep", "Glob":
		return "search", lastPart(v.Pattern)
	case "Read":
		return "read", filepath.Base(v.FilePath)
	case "Edit", "Write":
		return "edit", filepath.Base(v.FilePath)
	case "Bash":
		if v.Desc != "" {
			return "command", v.Desc
		}
		return "command", program(v.Command)
	case "fileChange":
		if len(v.Changes) > 0 {
			return "edit", filepath.Base(v.Changes[0].Path)
		}
		return "edit", ""
	case "commandExecution":
		if len(v.Actions) > 0 {
			a := v.Actions[0]
			switch a.Type {
			case "search":
				return "search", lastPart(a.Query)
			case "read":
				return "read", filepath.Base(a.Path)
			default:
				return "command", program(a.Command)
			}
		}
		return "command", program(v.Command)
	}
	if strings.Contains(name, "memory") || strings.Contains(name, "remember") || strings.Contains(name, "recall") {
		if v.Query != "" {
			return "memory", v.Query
		}
		return "memory", v.Topic
	}
	if strings.Contains(name, "start_work") || strings.Contains(name, "track_mr") {
		return "work", ""
	}
	if strings.HasPrefix(name, "mcp__atoll__") || slices.Contains([]string{
		provider.ToolCreateBot, provider.ToolEditBot, provider.ToolCreateSkill,
		provider.ToolEditSkill, provider.ToolImportSkill, provider.ToolRegisterRepo,
		provider.ToolAttachConnection, provider.ToolListBots, provider.ToolListSkills, provider.ToolListConnections,
		provider.ToolListRepos, provider.ToolListModels, provider.ToolListMCPServers,
	}, name) {
		return "platform", v.Name
	}
	return "other", ""
}

func lastPart(s string) string {
	if s == "" {
		return ""
	}
	return filepath.Base(s)
}

func program(command string) string {
	fields := strings.Fields(command)
	if len(fields) == 0 {
		return ""
	}
	return filepath.Base(fields[0])
}

func shortMemory(body string) string {
	runes := []rune(strings.Join(strings.Fields(body), " "))
	if len(runes) > 80 {
		return string(runes[:80]) + "…"
	}
	return string(runes)
}
