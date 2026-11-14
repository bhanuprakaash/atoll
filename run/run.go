// Package run handles one message as one run: it starts or resumes the Bot's provider session,
// streams the run's events to the thread, holds approvals open until the user answers, and stores
// the reply.
package run

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"atoll/connection"
	"atoll/memory"
	"atoll/provider"
	"atoll/repo"
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
	// ErrNotOffered is returned when Allow in this thread answers an approval that does not offer it.
	ErrNotOffered = errors.New("allow in this thread is not offered for this approval")
	// ErrNoGitHubConnection means the Bot has no GitHub connection.
	ErrNoGitHubConnection = errors.New("this repo is on GitHub and this Bot has no GitHub connection: " +
		"give this Bot the GitHub connection in its settings, then ask again")
)

var errRequestEnded = errors.New("the provider ended the request")

// Event types on a thread's stream.
const (
	EventText        = "text"            // Text: a piece of the reply
	EventToolCall    = "tool_call"       // ToolCall
	EventApproval    = "approval"        // Approval
	EventApprovalEnd = "approval_closed" // ApprovalClosed
	EventRunFinished = "run_finished"    // Finished
	EventThreadRead  = "thread_read"     // nothing: the user opened the thread

	EventWorktreeCreated = "worktree_created" // WorktreeCreated
	EventMRUpdated       = "mr_updated"       // MRUpdated
	EventWorktreeRemoved = "worktree_removed" // WorktreeRemoved
	EventMemorySaved     = "memory_saved"     // MemorySaved
	EventPrompt          = "prompt"           // Prompt
	EventHelperStarted   = "helper_started"   // HelperStarted

	// EventOther is the provider's own activity, not Reef's request. It is in the run's event file
	// only, never on a stream.
	EventOther = "other" // Other
)

const (
	// A slow SSE client must not stall the run: its channel holds this many events and the
	// subscriber is closed when it is full, so the client reconnects.
	_subscriberBuffer  = 64
	_stopReason        = "Atoll stopped while the run was going"
	_secretStoreReason = "Reef can't reach its secret store. Restart Reef, or unlock it, then send again."
	_atollTools        = "mcp__" + provider.MCPServer + "__"
	// _callWait bounds how long Ask waits for its tool call to come through the provider's
	// stdout, which races the permission prompt arriving over HTTP.
	_callWait = 2 * time.Second
)

const _promptItems = 8

// Whether stop tells the boss and lets held messages start: a user's stop does, Reef's does not.
const (
	_stopByUser = false
	_stopByReef = true
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
		// WakeWindow is how long held helper messages alone wait for company before a run starts;
		// zero starts one at once.
		WakeWindow time.Duration
		Log        *slog.Logger
	}

	// Manager runs messages and fans their events out to the thread's subscribers.
	Manager struct {
		cfg    Config
		ctx    context.Context // ends when Close is called
		cancel context.CancelFunc
		wg     sync.WaitGroup

		// inboxMu and delegateMu make take-start-remove and count-start atomic. Neither is taken under
		// mu; delegateMu is taken under inboxMu, never the reverse.
		inboxMu    sync.Mutex
		delegateMu sync.Mutex
		windows    map[string]*time.Timer // wake windows open, by thread id; inboxMu guards it

		mu        sync.Mutex
		runs      map[string]*active                 // by thread id
		tokens    map[string]*active                 // by run token
		approvals map[string]open                    // open approvals, by approval id
		subs      map[string]map[chan Event]struct{} // by thread id
		all       map[chan Event]struct{}            // SubscribeAll's subscribers
	}

	// open is an approval waiting for the user; answer takes its one answer.
	open struct {
		approval Approval
		command  string
		run      *active
		answer   chan Decision
	}

	// Decision is the user's answer to an approval. Reason is what a reply told the provider
	// instead; it is empty for a plain Allow or Deny.
	Decision struct {
		Allow  bool
		Reason string
	}

	// active is a run in progress. The Manager's mu guards log and open.
	active struct {
		workMu   sync.Mutex
		statusMu sync.Mutex // orders the open count with the run status it implies
		threadID string
		ctx      context.Context
		cancel   context.CancelFunc
		done     chan struct{}
		stopped  bool
		quiet    bool // stopped by Reef, not by the user: no wake, no held message
		ending   bool
		token    string
		run      store.Run
		bot      store.Bot // settings as the run started
		log      *os.File
		open     int                      // approvals open
		calls    map[string]chan struct{} // closed once the tool call with that id is published
		tool     ToolCall
		shown    []string // memory ids the run's memory tools returned
		// usage and priorCostUSD are the run goroutine's: the provider's last report, and what the
		// session had cost before this run.
		usage        *provider.Usage
		priorCostUSD float64
		kind         string // of the wake the run answers; empty when any message is not a wake
		msgs         []store.InboxItem
		// background: every message is a Reef notice, so the final message is a status line. guard:
		// a notice holds a failure Reef knows of. told: the run called tell_user. The Manager's mu
		// guards them.
		background, guard, told bool
	}

	// Event is one event on a thread's stream. Data is one of the types below. ThreadID is the
	// thread it belongs to.
	Event struct {
		Type     string
		ThreadID string
		Data     any
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
		RunID         string          `json:"run_id"`
		ID            string          `json:"id"`
		Tool          string          `json:"tool"`
		Input         json.RawMessage `json:"input"`
		AllowInThread bool            `json:"allow_in_thread"`
	}

	// ApprovalClosed says an approval is no longer open.
	ApprovalClosed struct {
		RunID string `json:"run_id"`
		ID    string `json:"id"`
	}

	// Other is a piece of text or a tool call of the provider's own activity.
	Other struct {
		RunID string          `json:"run_id"`
		Text  string          `json:"text,omitempty"`
		Name  string          `json:"name,omitempty"`
		Input json.RawMessage `json:"input,omitempty"`
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
		windows:   map[string]*time.Timer{},
		runs:      map[string]*active{},
		tokens:    map[string]*active{},
		approvals: map[string]open{},
		subs:      map[string]map[chan Event]struct{}{},
		all:       map[chan Event]struct{}{},
	}
}

// Send stores text and images as the user's message and starts a run for it. When the thread's
// run is going it queues the message instead (the message has Queued set, the run is zero) and
// logs it. It wraps store.ErrNotFound for an unknown thread.
func (m *Manager) Send(
	ctx context.Context, threadID, text string, images []store.Attachment,
) (store.Message, store.Run, error) {
	msg, run, err := m.sendWithHeld(ctx, store.InboxItem{ThreadID: threadID, Text: text, Images: images})
	if !errors.Is(err, ErrBusy) {
		if err != nil {
			m.flushLog(threadID)
		}
		return msg, run, err
	}
	it, err := m.cfg.Store.Enqueue(ctx, store.InboxItem{
		ThreadID: threadID, Text: text, Images: images, Queued: true, FromUser: true,
	})
	if err != nil {
		return store.Message{}, store.Run{}, err
	}
	m.cfg.Log.Info("queued", "thread_id", threadID, "message_id", it.ID)
	// The run may have ended after send saw it going and before the message was stored.
	m.flushLog(threadID)
	return store.Message{
		ID: it.ID, ThreadID: threadID, Role: store.RoleUser, Text: text, CreatedAt: it.CreatedAt,
		Queued: true, Attachments: it.Images,
	}, store.Run{}, nil
}

// EditQueued sets the text of the thread's queued message id; see store.EditQueued for the errors.
func (m *Manager) EditQueued(ctx context.Context, threadID, id, text string) (store.InboxItem, error) {
	m.inboxMu.Lock()
	defer m.inboxMu.Unlock()
	return m.cfg.Store.EditQueued(ctx, threadID, id, text)
}

// RemoveQueued removes the thread's queued message id.
func (m *Manager) RemoveQueued(ctx context.Context, threadID, id string) error {
	m.inboxMu.Lock()
	defer m.inboxMu.Unlock()
	return m.cfg.Store.DeleteQueued(ctx, threadID, id)
}

// SendNow stops the thread's run, if one is going, and starts the one run of the person's queued
// messages. It returns store.ErrSent when message id is no longer queued.
func (m *Manager) SendNow(ctx context.Context, threadID, id string) (store.Run, error) {
	if err := m.cfg.Store.CheckQueued(ctx, threadID, id); err != nil {
		return store.Run{}, err
	}
	if runID, _, ok := m.Active(threadID); ok {
		if err := m.Stop(ctx, runID); err != nil && !errors.Is(err, ErrBusy) {
			return store.Run{}, err
		}
	}
	m.inboxMu.Lock()
	defer m.inboxMu.Unlock()
	items, err := m.cfg.Store.UserInbox(ctx, threadID)
	if err == nil {
		items, err = m.takeAll(ctx, threadID, items)
	}
	if err != nil {
		return store.Run{}, err
	}
	if !slices.ContainsFunc(items, func(it store.InboxItem) bool { return it.ID == id }) {
		return store.Run{}, store.ErrSent
	}
	_, run, err := m.sendHeld(ctx, items...)
	if errors.Is(err, store.ErrNoneHeld) {
		return store.Run{}, store.ErrSent
	}
	return run, err
}

func (m *Manager) send(ctx context.Context, items ...store.InboxItem) (store.Message, store.Run, error) {
	threadID := items[0].ThreadID
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

	msgs, run, err := m.start(ctx, items)
	if err != nil {
		m.drop(a)
		cancel()
		m.wg.Done()
		return store.Message{}, store.Run{}, err
	}
	items = slices.DeleteFunc(slices.Clone(items), func(it store.InboxItem) bool {
		return it.ID != "" && !slices.ContainsFunc(msgs, func(msg store.Message) bool { return msg.ID == it.ID })
	})
	texts := make([]string, len(items))
	var images []provider.Image
	kind := items[0].Kind
	for i, it := range items {
		texts[i] = it.Text
		if it.Kind != kind {
			kind = ""
		}
		for _, img := range it.Images {
			images = append(images, provider.Image{Mime: img.Mime, Data: img.Data})
		}
	}
	m.mu.Lock()
	a.run, a.kind, a.msgs = run, kind, items
	m.tokens[a.token] = a
	m.mu.Unlock()
	go m.execute(a, strings.Join(texts, "\n\n"), images)
	return msgs[0], run, nil
}

// start refuses a closed thread, then stores the messages and their run. It returns the messages
// stored, which leave out a held message removed meanwhile.
func (m *Manager) start(ctx context.Context, items []store.InboxItem) ([]store.Message, store.Run, error) {
	t, err := m.cfg.Store.Thread(ctx, items[0].ThreadID)
	if err != nil {
		return nil, store.Run{}, fmt.Errorf("start run: %w", err)
	}
	if t.Closed {
		return nil, store.Run{}, ErrClosed
	}
	msgs, run, err := m.cfg.Store.StartRunFrom(ctx, items...)
	if err != nil {
		return nil, store.Run{}, fmt.Errorf("start run: %w", err)
	}
	return msgs, run, nil
}

// Available reports whether the provider is installed and signed in.
func (m *Manager) Available(ctx context.Context, name string) bool {
	if m.cfg.Available == nil {
		return true
	}
	return m.cfg.Available(ctx, name)
}

// Stop ends an active run for the user and waits for its provider to exit.
func (m *Manager) Stop(ctx context.Context, id string) error {
	return m.stop(ctx, id, _stopByUser)
}

func (m *Manager) stop(ctx context.Context, id string, quiet bool) error {
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
	a.stopped, a.quiet = true, quiet
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
	var snapshot *io.SectionReader
	var file *os.File
	if a := m.runs[threadID]; a != nil && a.log != nil {
		var err error
		file, err = os.Open(a.log.Name())
		if err == nil {
			var info os.FileInfo
			info, err = file.Stat()
			if err == nil {
				snapshot = io.NewSectionReader(file, 0, info.Size())
			}
		}
		if err != nil {
			m.cfg.Log.Warn("open replay snapshot", "run_id", a.run.ID, "error", err)
		}
	}
	if m.subs[threadID] == nil {
		m.subs[threadID] = map[chan Event]struct{}{}
	}
	m.subs[threadID][ch] = struct{}{}
	m.mu.Unlock()
	if file != nil {
		defer func() { _ = file.Close() }()
	}
	if snapshot != nil {
		m.replay(threadID, ch, snapshot)
	}
	return ch, func() {
		m.mu.Lock()
		defer m.mu.Unlock()
		if _, ok := m.subs[threadID][ch]; ok {
			delete(m.subs[threadID], ch)
			close(ch)
		}
	}
}

// SubscribeAll streams the notice events of every thread from now on, until cancel is called.
// approvals holds an approval event for each approval open now; events carries what follows them.
func (m *Manager) SubscribeAll() (approvals []Event, events <-chan Event, cancel func()) {
	ch := make(chan Event, _subscriberBuffer)
	m.mu.Lock()
	if m.ctx.Err() != nil {
		close(ch)
		m.mu.Unlock()
		return nil, ch, func() {}
	}
	for threadID, a := range m.runs {
		for _, o := range m.approvals {
			if o.approval.RunID == a.run.ID {
				approvals = append(approvals, Event{Type: EventApproval, ThreadID: threadID, Data: o.approval})
			}
		}
	}
	m.all[ch] = struct{}{}
	m.mu.Unlock()
	return approvals, ch, func() {
		m.mu.Lock()
		defer m.mu.Unlock()
		if _, ok := m.all[ch]; ok {
			delete(m.all, ch)
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
			ap := o.approval
			ap.Input = slices.Clone(ap.Input)
			approvals = append(approvals, ap)
		}
	}
	return a.run.ID, approvals, true
}

// ActiveTool returns the latest tool of an active run.
func (m *Manager) ActiveTool(threadID string) ToolCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	if a := m.runs[threadID]; a != nil {
		tool := a.tool
		tool.Input = slices.Clone(tool.Input)
		return tool
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
	o.answer <- Decision{Allow: allow}
	return nil
}

// AnswerInThread allows the open approval id and every later ask for the same command in its
// thread. It wraps ErrNotFound when none is open, and leaves the approval open and returns
// ErrNotOffered when allow is false or the approval does not offer it. When saving the allow
// fails it leaves the approval open and the command unanswered.
func (m *Manager) AnswerInThread(ctx context.Context, id string, allow bool) error {
	m.mu.Lock()
	o, ok := m.approvals[id]
	offered := ok && allow && o.approval.AllowInThread
	if offered {
		delete(m.approvals, id)
	}
	m.mu.Unlock()
	switch {
	case !ok:
		return fmt.Errorf("approval %s: %w", id, ErrNotFound)
	case !offered:
		return ErrNotOffered
	}
	after, err := json.Marshal(map[string]string{"command": o.command})
	if err == nil {
		var c store.Change
		c, err = m.cfg.Store.RecordChange(ctx, store.Change{
			Entity: store.EntityAllow, EntityID: id, Action: store.ActionCreate, After: after,
			SourceBotID: o.run.bot.ID, SourceThreadID: o.run.threadID, SourceRunID: o.run.run.ID,
		})
		if err == nil {
			m.publish(o.run, EventPlatformChanged, PlatformChanged{
				SourceBotID: c.SourceBotID, ThreadID: c.SourceThreadID, RunID: c.SourceRunID, Change: c,
			})
		}
	}
	if err != nil {
		m.mu.Lock()
		if m.runs[o.run.threadID] == o.run && o.run.ctx.Err() == nil {
			m.approvals[id] = o
		}
		m.mu.Unlock()
		return fmt.Errorf("record allow: %w", err)
	}
	o.answer <- Decision{Allow: true}
	return nil
}

// Ask is Claude's permission prompt for the run with token: it opens an approval and blocks
// until the user answers or ctx ends. Atoll's own tools are allowed without asking. The approval
// follows the tool call toolUseID on the thread's stream.
func (m *Manager) Ask(
	ctx context.Context, token, toolUseID, tool string, input json.RawMessage,
) (Decision, error) {
	a, err := m.byToken(token)
	if err != nil {
		return Decision{}, err
	}
	if strings.HasPrefix(tool, _atollTools) {
		return Decision{Allow: true}, nil
	}
	select {
	case <-m.called(a, toolUseID):
	case <-time.After(_callWait):
	case <-ctx.Done():
		return Decision{}, ctx.Err()
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
	m.inboxMu.Lock()
	for id, tm := range m.windows {
		tm.Stop()
		delete(m.windows, id)
	}
	m.inboxMu.Unlock()
	m.wg.Wait()
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, chs := range m.subs {
		for ch := range chs {
			delete(chs, ch)
			close(ch)
		}
	}
	for ch := range m.all {
		delete(m.all, ch)
		close(ch)
	}
	return nil
}

// execute is the goroutine of one run.
func (m *Manager) execute(a *active, text string, images []provider.Image) {
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
		reply, err = m.drive(a, text, images)
	}
	res := store.RunResult{Status: store.StatusDone, Reply: strings.TrimSpace(reply)}
	if err != nil {
		res = store.RunResult{Status: store.StatusFailed, Error: err.Error()}
		if m.ctx.Err() != nil {
			res.Error = _stopReason
		} else if errors.Is(err, store.ErrSecretStore) {
			res.Error, res.ErrorKind = _secretStoreReason, store.ErrorSecretStore
			m.cfg.Log.Warn("secret store unreachable", "run_id", a.run.ID,
				"error_kind", res.ErrorKind, "error", err)
		} else if limit := (*provider.LimitError)(nil); errors.As(err, &limit) {
			res.ErrorKind, res.ResetsAt = store.ErrorRateLimited, limit.ResetsAt
		} else if errors.Is(err, provider.ErrOff) {
			res.ErrorKind = store.ErrorProviderOff
		} else if errors.Is(err, connection.ErrNeedsSignIn) || errors.Is(err, connection.ErrGitHubNeedsSignIn) {
			res.ErrorKind = store.ErrorNeedsSignIn
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
		res = store.RunResult{Status: store.StatusStopped}
	}
	quiet := a.quiet
	res.StatusLine = a.background && (a.told || !a.guard)
	m.mu.Unlock()
	res.Provider = a.bot.Provider
	res.Usage, res.SessionCostUSD = a.usageResult()
	var wake func(store.Wake) string
	if !quiet {
		wake = m.wakeText(a.threadID)
	}
	// A background context: FinishRun must outlive Close's cancel.
	for range 2 {
		err = m.cfg.Store.FinishRun(context.Background(), a.run.ID, res, wake)
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
	if res.Status == store.StatusDone && res.Reply != "" && !res.StatusLine {
		m.wg.Add(1)
		go func() {
			defer m.wg.Done()
			m.nameThread(a.threadID, a.bot.Provider)
		}()
	}
	if !quiet {
		m.flushBoss(a.threadID)
		// A user's Stop must not start the next held message at once.
		if res.Status != store.StatusStopped {
			m.flushLog(a.threadID)
		}
	}
}

// usageResult is what the run used, and what the session has cost in total. A session's cost
// carries into its next process, so the run's own cost is the total less what came before.
func (a *active) usageResult() (*store.Usage, *float64) {
	u := a.usage
	if u == nil {
		return nil, nil
	}
	used := &store.Usage{
		Model: u.Model, Calls: u.Calls, InputTokens: u.InputTokens, OutputTokens: u.OutputTokens,
		CacheReadTokens: u.CacheReadTokens, CacheWriteTokens: u.CacheWriteTokens,
	}
	if u.SessionCostUSD == nil {
		return used, nil
	}
	cost := math.Round((*u.SessionCostUSD-a.priorCostUSD)*1e6) / 1e6
	used.CostUSD = &cost
	return used, u.SessionCostUSD
}

// wakeText makes the wake text of a helper run on threadID, with the helpers line of its boss: the
// helper whose run ended is not counted.
func (m *Manager) wakeText(threadID string) func(store.Wake) string {
	ctx := context.Background()
	line := ""
	t, err := m.cfg.Store.Thread(ctx, threadID)
	if err == nil && t.ParentThreadID != "" {
		line, err = m.helpersLine(ctx, t.ParentThreadID, threadID)
	}
	if err != nil {
		m.cfg.Log.Error("count helpers", "thread_id", threadID, "error", err)
	}
	return func(w store.Wake) string { return WakeText(w, line) }
}

func (m *Manager) codexAutoCache(botID string) (string, []string, error) {
	cache := filepath.Join(m.cfg.DataDir, "cache", botID)
	var env []string
	for name, dir := range map[string]string{
		"XDG_CACHE_HOME": "xdg-cache", "XDG_DATA_HOME": "xdg-data", "XDG_STATE_HOME": "xdg-state",
		"GOCACHE": "go-build", "GOLANGCI_LINT_CACHE": "golangci-lint",
		"pnpm_config_store_dir": "pnpm-store", "PNPM_HOME": "pnpm-home",
	} {
		path := filepath.Join(cache, dir)
		if err := os.MkdirAll(path, 0o755); err != nil {
			return "", nil, fmt.Errorf("make bot cache folder: %w", err)
		}
		env = append(env, name+"="+path)
	}
	return cache, env, nil
}

func (m *Manager) codexAutoRoots(ctx context.Context, thread store.Thread, rp store.Repo) ([]string, error) {
	var roots []string
	root, err := m.WorktreeRoot(ctx)
	if err != nil {
		return nil, fmt.Errorf("load worktree root: %w", err)
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, fmt.Errorf("make worktree root: %w", err)
	}
	if _, err := os.Stat(rp.Path); !errors.Is(err, os.ErrNotExist) {
		common, err := repo.GitCommonDir(ctx, rp.Path)
		if err != nil {
			return nil, fmt.Errorf("find git directories: %w", err)
		}
		resolvedRoot, err := filepath.EvalSymlinks(root)
		if err != nil {
			return nil, fmt.Errorf("resolve worktree root: %w", err)
		}
		resolvedCommon, err := filepath.EvalSymlinks(common)
		if err != nil {
			return nil, fmt.Errorf("resolve git directory: %w", err)
		}
		rel, err := filepath.Rel(resolvedRoot, resolvedCommon)
		if err != nil {
			return nil, fmt.Errorf("compare git directory and worktree root: %w", err)
		}
		resolvedRepo, err := filepath.EvalSymlinks(rp.Path)
		if err != nil {
			return nil, fmt.Errorf("resolve repo: %w", err)
		}
		repoRel, err := filepath.Rel(resolvedRoot, resolvedRepo)
		if err != nil {
			return nil, fmt.Errorf("compare repo and worktree root: %w", err)
		}
		if (rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator))) && (repoRel == ".." || strings.HasPrefix(repoRel, ".."+string(filepath.Separator))) {
			roots = []string{root}
		}
		roots = append(roots, filepath.Join(common, "objects"), filepath.Join(common, "refs"), filepath.Join(common, "logs"))
		if !repo.WorktreeConfig(ctx, rp.Path) {
			roots = append(roots, filepath.Join(common, "worktrees"))
			current, err := m.cfg.Store.Thread(ctx, thread.ID)
			if err != nil {
				return nil, fmt.Errorf("load thread git directory: %w", err)
			}
			id := current.CheckoutGitDir
			if current.Worktree != "" {
				id = current.WorktreeGitDir
			}
			gitDir, err := filepath.EvalSymlinks(filepath.Join(common, "worktrees", id))
			if err != nil {
				return nil, fmt.Errorf("resolve thread git directory: %w", err)
			}
			if id == "" || filepath.Dir(gitDir) != filepath.Join(resolvedCommon, "worktrees") {
				return nil, errors.New("thread git directory is outside the registered repo")
			}
			if !slices.Contains(roots, gitDir) {
				roots = append(roots, gitDir)
			}
		}
	}
	return roots, nil
}

// drive runs the provider session for one prompt and returns its reply.
func (m *Manager) drive(a *active, text string, images []provider.Image) (string, error) {
	thread, err := m.cfg.Store.Thread(a.ctx, a.threadID)
	if err != nil {
		return "", fmt.Errorf("load thread: %w", err)
	}
	bot, err := m.cfg.Store.Bot(a.ctx, thread.BotID)
	if err != nil {
		return "", fmt.Errorf("load bot: %w", err)
	}
	var notice string
	if thread.Session != "" && thread.SessionProvider != bot.Provider {
		thread.Session = ""
		notice = "Provider changed to " + bot.Provider + "; starting a fresh session.\n\n"
	}
	if thread.Session != "" {
		a.priorCostUSD = thread.SessionCostUSD
	}
	msgs, background := asNotices(thread, a.msgs)
	m.mu.Lock()
	a.bot, a.background = bot, background
	a.guard = background && slices.ContainsFunc(msgs, failure)
	m.mu.Unlock()
	m.cfg.Log.Info("run started", "run_id", a.run.ID, "thread_id", a.threadID,
		"provider", bot.Provider)
	sess, cfg, err := m.cfg.Launch(a.ctx, bot.Provider)
	if err != nil {
		return "", fmt.Errorf("launch %s: %w", bot.Provider, err)
	}
	skillDir := filepath.Join(cfg.Dir, "runs", a.run.ID)
	if !skill.Safe(skillDir) {
		skillDir = safeSkillDir(a.run.ID)
	}
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
	cwd, stale, err := m.workdir(thread, rp)
	if err != nil {
		return "", err
	}
	cfg.Cwd = cwd
	user, err := m.cfg.Store.Setting(a.ctx, "user_name")
	if err != nil {
		return "", fmt.Errorf("load user name: %w", err)
	}
	cfg.Model = bot.Model
	cfg.Mode = bot.Mode
	cfg.Log = m.cfg.Log
	cfg.NoShell = bot.ID == "chief" && bot.Provider == "claude"
	if bot.Provider == "claude" && !cfg.NoShell {
		if cfg.HiddenPaths, err = m.hiddenPaths(a.ctx, bot); err != nil {
			return "", err
		}
	}
	if bot.Provider == "codex" && bot.Mode == provider.ModeAuto && rp.ID != "" {
		if cfg.WritableRoots, err = m.codexAutoRoots(a.ctx, thread, rp); err != nil {
			return "", err
		}
	}
	onBossBranch := false
	if thread.ParentThreadID != "" && thread.Branch != "" {
		boss, err := m.cfg.Store.Thread(a.ctx, thread.ParentThreadID)
		if err != nil {
			return "", fmt.Errorf("load boss thread: %w", err)
		}
		onBossBranch = boss.Branch != "" && strings.HasPrefix(thread.Branch, boss.Branch+"--")
	}
	cfg.SystemPrompt = systemPrompt(bot, user, onBossBranch,
		len(bot.Delegates) > 0 && thread.ParentThreadID == "")
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
	if m.cfg.Memory != nil && a.kind == "" {
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
	var behind string
	if rp.ID != "" && thread.Session == "" && thread.Worktree == "" {
		behind = repo.BehindNote(a.ctx, rp.Path)
	}
	prompt := messagePrompt(msgs, items, counts, thread, rp, cfg.Cwd, behind, stale)
	if background {
		prompt = backgroundLine(user) + "\n\n" + prompt
	}
	var githubToken string
	cfg.Env, githubToken, err = m.runEnv(a.ctx, bot, true)
	if err != nil {
		return "", err
	}
	if bot.Provider == "codex" && bot.Mode == provider.ModeAuto && rp.ID != "" {
		cache, env, err := m.codexAutoCache(bot.ID)
		if err != nil {
			return "", err
		}
		cfg.WritableRoots = append(cfg.WritableRoots, cache)
		cfg.Env = append(cfg.Env, env...)
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
	if notice != "" {
		m.publish(a, EventText, Text{RunID: a.run.ID, Text: notice})
	}
	if err := sess.Send(a.ctx, prompt, images); err != nil {
		return "", fmt.Errorf("send prompt: %w", err)
	}
	reply, err := m.stream(a, sess)
	if err != nil {
		return "", err
	}
	reply = notice + reply
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
	return reply, nil
}

// stream reads the request's events until it ends and returns its final message, the reply. The
// text before it is progress: published, never the reply.
func (m *Manager) stream(a *active, sess provider.Session) (string, error) {
	var wg sync.WaitGroup
	asks := map[string]context.CancelCauseFunc{}
	failed := make(chan error, 1)
	defer func() {
		for _, cancel := range asks {
			cancel(nil)
		}
		wg.Wait()
	}()
	for {
		select {
		case <-a.ctx.Done():
			return "", a.ctx.Err()
		case err := <-failed:
			return "", err
		case ev, ok := <-sess.Events():
			if !ok {
				return "", errors.New("provider exited before the turn ended")
			}
			if ev.Other {
				m.logOther(a, ev)
				continue
			}
			switch ev.Kind {
			case provider.KindText:
				m.publish(a, EventText, Text{RunID: a.run.ID, Text: ev.Text})
			case provider.KindUsage:
				a.usage = ev.Usage
			case provider.KindSession:
				if err := m.cfg.Store.SetSession(a.ctx, a.threadID, ev.SessionID, a.bot.Provider); err != nil {
					return "", fmt.Errorf("save session: %w", err)
				}
			case provider.KindToolCall:
				name := ev.Name
				if ev.Server != "" {
					name = "mcp__" + ev.Server + "__" + name
				}
				kind, target := ToolKind(name, ev.Input)
				if name == _atollTools+provider.ToolReadMemory && m.cfg.Memory != nil {
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
				if err := m.cfg.Store.CountTool(a.ctx, a.run.ID, kind); err != nil {
					m.cfg.Log.Warn("count tool", "error", err)
				}
				m.publish(a, EventToolCall,
					ToolCall{RunID: a.run.ID, ID: ev.ID, Name: ev.Name, Input: ev.Input, Kind: kind, Target: target, Verb: ToolVerb(name, kind)})
				if ch := m.called(a, ev.ID); !isClosed(ch) {
					close(ch)
				}
			case provider.KindApproval:
				ctx, cancel := context.WithCancelCause(a.ctx)
				asks[ev.ID] = cancel
				wg.Go(func() {
					d, err := m.approve(ctx, a, ev.Name, ev.Input)
					if err == nil {
						if err = sess.Answer(ev.ID, d.Allow, d.Reason); err != nil {
							err = fmt.Errorf("answer approval: %w", err)
						}
					}
					if err != nil && !errors.Is(err, provider.ErrApprovalEnded) &&
						!errors.Is(context.Cause(ctx), errRequestEnded) {
						select {
						case failed <- err:
						default:
						}
					}
				})
			case provider.KindApprovalEnd:
				if cancel, ok := asks[ev.ID]; ok {
					cancel(errRequestEnded)
					delete(asks, ev.ID)
				}
			case provider.KindTurnEnd:
				return ev.Text, nil
			case provider.KindError:
				if ev.SignedOut {
					return "", fmt.Errorf("%w: %s", provider.ErrSignedOut, ev.Text)
				}
				if ev.Limited {
					return "", &provider.LimitError{Text: ev.Text, ResetsAt: ev.ResetsAt}
				}
				return "", errors.New(ev.Text)
			}
		}
	}
}

// approve opens an approval and blocks until it is answered, ctx ends or the Manager closes.
func (m *Manager) approve(
	ctx context.Context, a *active, tool string, input json.RawMessage,
) (Decision, error) {
	command, isCommand := commandText(tool, input)
	if isCommand && m.allowedInThread(ctx, a.threadID, command) {
		return Decision{Allow: true}, nil
	}
	id := randHex(8)
	answer := make(chan Decision, 1) // Answer must not wait for the run to read
	ap := Approval{RunID: a.run.ID, ID: id, Tool: tool, Input: input}
	if isCommand {
		if thread, err := m.cfg.Store.Thread(ctx, a.threadID); err == nil {
			ap.AllowInThread = canAllowInThread(command, allowRoots(thread))
		}
	}
	a.statusMu.Lock()
	m.mu.Lock()
	m.approvals[id] = open{approval: ap, command: command, run: a, answer: answer}
	a.open++
	m.mu.Unlock()
	err := m.cfg.Store.SetStatus(ctx, a.run.ID, store.StatusWaiting)
	a.statusMu.Unlock()
	defer m.resume(ctx, a, id)
	if err != nil {
		return Decision{}, fmt.Errorf("approval %s: %w", id, err)
	}
	m.publish(a, EventApproval, ap)
	select {
	case d := <-answer:
		return d, nil
	case <-ctx.Done():
		return Decision{}, fmt.Errorf("approval %s: %w", id, ctx.Err())
	case <-a.ctx.Done():
		return Decision{}, fmt.Errorf("approval %s: %w", id, a.ctx.Err())
	}
}

// allowedInThread reports whether the user chose Allow in this thread for command.
func (m *Manager) allowedInThread(ctx context.Context, threadID, command string) bool {
	changes, err := m.cfg.Store.Changes(ctx, threadID)
	if err != nil {
		m.cfg.Log.Warn("read thread allows", "thread_id", threadID, "error", err)
		return false
	}
	return slices.ContainsFunc(changes, func(c store.Change) bool {
		var after struct {
			Command string `json:"command"`
		}
		return c.Entity == store.EntityAllow && json.Unmarshal(c.After, &after) == nil &&
			after.Command == command
	})
}

// resume closes approval id and sets the run back to running once none is open.
func (m *Manager) resume(ctx context.Context, a *active, id string) {
	a.statusMu.Lock()
	m.mu.Lock()
	delete(m.approvals, id)
	a.open--
	idle := a.open == 0
	m.mu.Unlock()
	var err error
	if idle {
		err = m.cfg.Store.SetStatus(context.WithoutCancel(ctx), a.run.ID, store.StatusRunning)
	}
	a.statusMu.Unlock()
	m.publish(a, EventApprovalEnd, ApprovalClosed{RunID: a.run.ID, ID: id})
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

// logOther writes the provider's own activity to the run's event file and nothing else. Its cost
// is in the session total, which the run's cost is read from.
func (m *Manager) logOther(a *active, ev provider.Event) {
	if ev.Kind == provider.KindUsage {
		if a.usage != nil {
			a.usage.SessionCostUSD = ev.Usage.SessionCostUSD
		}
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.writeEvent(a.run.ID, a.log, EventOther,
		Other{RunID: a.run.ID, Text: ev.Text, Name: ev.Name, Input: ev.Input})
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

// deliver writes the event to log, when there is one, and sends it to the thread's subscribers
// and, for a notice, to every SubscribeAll subscriber.
// The caller holds mu.
func (m *Manager) deliver(threadID, runID string, log *os.File, typ string, data any) {
	m.writeEvent(runID, log, typ, data)
	ev := Event{Type: typ, ThreadID: threadID, Data: data}
	for ch := range m.subs[threadID] {
		select {
		case ch <- ev:
		default:
			delete(m.subs[threadID], ch)
			close(ch)
			m.cfg.Log.Warn("subscriber full, stream closed", "run_id", runID, "type", typ)
		}
	}
	switch typ {
	case EventApproval, EventApprovalEnd, EventRunFinished, EventMRUpdated, EventPlatformChanged, EventThreadRead:
	default:
		return
	}
	for ch := range m.all {
		select {
		case ch <- ev:
		default:
			delete(m.all, ch)
			close(ch)
			m.cfg.Log.Warn("subscriber full, stream closed", "run_id", runID, "type", typ)
		}
	}
}

// writeEvent appends the event to log, when there is one.
func (m *Manager) writeEvent(runID string, log *os.File, typ string, data any) {
	if log == nil {
		return
	}
	line, err := json.Marshal(eventLine{Type: typ, Data: data, At: time.Now().UTC()})
	if err == nil {
		_, err = log.Write(append(line, '\n'))
	}
	if err != nil {
		m.cfg.Log.Warn("write event", "run_id", runID, "error", err)
	}
}

// The snapshot ends at registration; events after it are already buffered in ch.
func (m *Manager) replay(threadID string, ch chan Event, snapshot io.Reader) {
	b, err := io.ReadAll(snapshot)
	if err != nil {
		m.cfg.Log.Warn("read event file", "thread_id", threadID, "error", err)
		return
	}
	var evs []Event
	for line := range bytes.Lines(b) {
		var ev struct {
			Type string          `json:"type"`
			Data json.RawMessage `json:"data"`
		}
		if json.Unmarshal(line, &ev) != nil || ev.Type == EventOther {
			continue
		}
		if ev.Type == EventText {
			var piece Text
			if json.Unmarshal(ev.Data, &piece) != nil {
				continue
			}
			if n := len(evs); n > 0 && evs[n-1].Type == EventText {
				piece.Text = evs[n-1].Data.(Text).Text + piece.Text
				evs = evs[:n-1]
			}
			evs = append(evs, Event{Type: EventText, ThreadID: threadID, Data: piece})
			continue
		}
		evs = append(evs, Event{Type: ev.Type, ThreadID: threadID, Data: ev.Data})
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.subs[threadID][ch]; !ok {
		return
	}
	live := make([]Event, len(ch))
	for i := range live {
		live[i] = <-ch
	}
	keep := min(_subscriberBuffer/2, cap(ch)-len(live))
	for _, ev := range evs[max(0, len(evs)-keep):] {
		ch <- ev
	}
	for _, ev := range live {
		ch <- ev
	}
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

func (m *Manager) free(a *active) {
	m.drop(a)
	m.flushLog(a.threadID)
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
	if strings.HasPrefix(name, _atollTools) {
		switch strings.TrimPrefix(name, _atollTools) {
		case provider.ToolStartWork:
			return "Starting a branch"
		case provider.ToolTrackMR:
			return "Opening the MR"
		case provider.ToolRetireMemory, provider.ToolRemember:
			return "Taking a note"
		case provider.ToolRecall, provider.ToolReadMemory:
			return "Remembering"
		case provider.ToolTellUser:
			return "Writing to you"
		case provider.ToolDelegate, provider.ToolContinue:
			return "Handing off"
		}
		if kind == "platform" && strings.HasPrefix(strings.TrimPrefix(name, _atollTools), "list_") {
			return "Looking around"
		}
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
	case "work":
		return "Waiting on a background task"
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
		if _backgroundOutput.MatchString(v.Command) {
			return "work", ""
		}
		if v.Desc != "" {
			return "command", v.Desc
		}
		return "command", CommandTarget(v.Command)
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
	if strings.HasPrefix(name, _atollTools) {
		switch strings.TrimPrefix(name, _atollTools) {
		case provider.ToolRemember, provider.ToolRecall, provider.ToolReadMemory, provider.ToolRetireMemory:
			if v.Query != "" {
				return "memory", v.Query
			}
			return "memory", v.Topic
		case provider.ToolStartWork, provider.ToolTrackMR:
			return "work", ""
		case provider.ToolDelegate, provider.ToolContinue:
			return "platform", ""
		default:
			return "platform", v.Name
		}
	}
	return "other", ""
}

func lastPart(s string) string {
	if s == "" {
		return ""
	}
	return filepath.Base(s)
}

var (
	_backgroundOutput = regexp.MustCompile(`/tasks/[^\s;&|"']*\.output\b`)
	_plainWord        = regexp.MustCompile(`^[A-Za-z][\w-]*$`)
	_assignment       = regexp.MustCompile(`^\w+=`)
	_quoted           = regexp.MustCompile(`\$\([^)]*\)|"[^"]*"|'[^']*'`)
	_heredoc          = regexp.MustCompile(`<<-?\s*['"]?(\w+)`)
	_writeRedirect    = regexp.MustCompile(`^\d?>`)
	_timeout          = regexp.MustCompile(`^\d+[smhd]?$`)
	_leadingWords     = map[string]bool{"if": true, "elif": true, "while": true, "until": true, "then": true, "do": true, "else": true, "{": true, "!": true}
	_noCommand        = map[string]bool{"for": true, "case": true, "select": true, "}": true, "done": true, "fi": true, "esac": true, "[": true, "[[": true}
)

// stripHeredocs drops the text of heredocs: it is input, never a command.
func stripHeredocs(command string) string {
	var out []string
	end := ""
	for _, line := range strings.Split(command, "\n") {
		if end != "" {
			if strings.TrimSpace(line) == end {
				end = ""
			}
			continue
		}
		out = append(out, line)
		if m := _heredoc.FindStringSubmatch(line); m != nil {
			end = m[1]
		}
	}
	return strings.Join(out, "\n")
}

// commandWords returns the words of the first real command: past cd, assignments, a leading
// timeout, function definitions, loop headers and a cat that only writes a file (which is the
// answer only when nothing else runs).
func commandWords(command string) []string {
	command = _quoted.ReplaceAllString(stripHeredocs(unwrapShell(command)), "_")
	var write []string
	for _, part := range strings.FieldsFunc(command, func(r rune) bool { return r == ';' || r == '&' || r == '|' || r == '\n' }) {
		fields := strings.Fields(part)
		for len(fields) > 0 {
			head := fields[0]
			switch {
			case _assignment.MatchString(head), _leadingWords[head]:
				fields = fields[1:]
			case len(fields) > 1 && strings.HasPrefix(fields[1], "()"):
				fields = fields[2:]
			case strings.HasSuffix(head, "()") || strings.Contains(head, "(){"):
				fields = fields[1:]
			case head == "timeout" && len(fields) > 1 && _timeout.MatchString(fields[1]):
				fields = fields[2:]
			case strings.HasPrefix(head, "("):
				fields[0] = strings.TrimLeft(head, "(")
				if fields[0] == "" {
					fields = fields[1:]
				}
			default:
				goto first
			}
		}
	first:
		if len(fields) == 0 || fields[0] == "cd" || _noCommand[fields[0]] {
			continue
		}
		if fields[0] == "cat" && len(fields) > 1 && _writeRedirect.MatchString(fields[1]) {
			write = fields[:1]
			continue
		}
		if fields[0] == "_" {
			return nil
		}
		return fields
	}
	return write
}

// CommandTarget is a command's program and plain first argument: "make e2e".
func CommandTarget(command string) string {
	fields := commandWords(command)
	if len(fields) == 0 {
		return ""
	}
	target := filepath.Base(fields[0])
	if len(fields) > 1 && _plainWord.MatchString(fields[1]) {
		target += " " + fields[1]
	}
	return target
}

func program(command string) string {
	fields := commandWords(command)
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

// safeSkillDir is a run's skill folder under the temp folder, for a data dir whose path a shell
// would split or expand (macOS `Application Support`): skill scripts write the placed path unquoted.
func safeSkillDir(runID string) string {
	root := os.TempDir()
	if !skill.Safe(root) {
		root = "/tmp"
	}
	return filepath.Join(root, "atoll-run-"+runID)
}
