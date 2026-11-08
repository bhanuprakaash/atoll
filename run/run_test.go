package run

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"atoll/provider"
	"atoll/store"
)

// fakeSession plays script when the first prompt is sent and nudge for each later one (a bare
// turn end when nudge is empty), then closes Events if end is set. onSend gets the number of
// prompts sent so far, first prompt included. Every
// answer it gets is recorded and ends the turn; when answered is set, the turn ends only after
// release is closed.
type fakeSession struct {
	script   []provider.Event
	nudge    []provider.Event
	onSend   func(n int)
	end      bool
	answered chan struct{}
	release  chan struct{}

	mu          sync.Mutex
	events      chan provider.Event
	closed      bool
	prompts     []string
	cfgs        []provider.Config
	answers     []bool
	interrupted int
	resumed     string
}

func (f *fakeSession) Start(_ context.Context, cfg provider.Config) error {
	f.cfgs = append(f.cfgs, cfg)
	return nil
}

func (f *fakeSession) Resume(ctx context.Context, cfg provider.Config, id string) error {
	f.resumed = id
	return f.Start(ctx, cfg)
}

func (f *fakeSession) Events() <-chan provider.Event { return f.events }

func (f *fakeSession) Send(_ context.Context, prompt string, _ []provider.Image) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.prompts = append(f.prompts, prompt)
	if f.onSend != nil {
		f.onSend(len(f.prompts))
	}
	script := f.script
	if len(f.prompts) > 1 {
		script = []provider.Event{{Kind: provider.KindTurnEnd}}
		if len(f.nudge) > 0 {
			script = f.nudge
		}
	}
	for _, ev := range script {
		f.events <- ev
	}
	if f.end {
		f.close()
	}
	return nil
}

func (f *fakeSession) Answer(_ string, allow bool) error {
	f.mu.Lock()
	f.answers = append(f.answers, allow)
	f.mu.Unlock()
	if f.answered != nil {
		f.answered <- struct{}{}
		<-f.release
	}
	f.events <- provider.Event{Kind: provider.KindTurnEnd}
	return nil
}

func (f *fakeSession) Interrupt() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.interrupted++
	f.close()
	return nil
}

func (f *fakeSession) close() {
	if !f.closed {
		f.closed = true
		close(f.events)
	}
}

// exited is a session whose provider has exited: a later launch, such as a title session.
func exited() *fakeSession {
	f := &fakeSession{events: make(chan provider.Event)}
	f.close()
	return f
}

type env struct {
	m      *Manager
	st     *store.Store
	thread store.Thread
	sess   *fakeSession
	events <-chan Event
}

func newEnv(t *testing.T, sess *fakeSession) *env {
	t.Helper()
	sess.events = make(chan provider.Event, 8)
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	bot, err := st.CreateBot(t.Context(), store.Bot{Name: "Ada", Persona: "Terse.", Provider: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	thread, err := st.CreateThread(t.Context(), bot.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	var launched atomic.Bool
	m := New(Config{
		Store: st,
		Launch: func(context.Context, string) (provider.Session, provider.Config, error) {
			if launched.Swap(true) {
				return exited(), provider.Config{}, nil
			}
			return sess, provider.Config{}, nil
		},
		MCPURL:  "http://atoll/mcp/",
		DataDir: t.TempDir(),
		Log:     slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	t.Cleanup(func() { _ = m.Close() })
	events, cancel := m.Subscribe(thread.ID)
	t.Cleanup(cancel)
	return &env{m: m, st: st, thread: thread, sess: sess, events: events}
}

// next returns the next event of type typ.
func (e *env) next(t *testing.T, typ string) Event {
	t.Helper()
	timeout := time.After(5 * time.Second)
	for {
		select {
		case ev := <-e.events:
			if ev.Type == typ {
				return ev
			}
		case <-timeout:
			t.Fatalf("no %s event", typ)
		}
	}
}

func nextAs[T any](t *testing.T, e *env, typ string) T {
	t.Helper()
	v, ok := e.next(t, typ).Data.(T)
	if !ok {
		t.Fatalf("%s event data is not %T", typ, v)
	}
	return v
}

func TestRun_Finish(t *testing.T) {
	turnEnd := provider.Event{Kind: provider.KindTurnEnd}
	tests := []struct {
		name       string
		give       *fakeSession
		reportOn   int // the prompt that gets a finish_run report; 0 for none
		wantStatus string
		wantReply  string
		wantError  string
		wantKind   string
		stopped    bool // the run is stopped while the provider fails
		wantReport string
		wantSends  int
		wantEvents []string
		provider   string
		session    string
		owner      string
		wantResume string
		wantTools  map[string]int
	}{
		{
			name: "MCP tools are classified by server",
			give: &fakeSession{script: []provider.Event{
				{Kind: provider.KindToolCall, ID: "c1", Name: "mcp__memory__remember"},
				{Kind: provider.KindToolCall, ID: "c2", Server: "memory", Name: "remember"},
				{Kind: provider.KindToolCall, ID: "c3", Server: "atoll", Name: "remember"},
				{Kind: provider.KindToolCall, ID: "c4", Name: "mcp__atoll__remember"},
				{Kind: provider.KindToolCall, ID: "c5", Server: "atoll", Name: "finish_run"},
				{Kind: provider.KindToolCall, ID: "c6", Server: "memory", Name: "finish_run"},
				turnEnd,
			}},
			wantStatus: store.StatusDone,
			wantSends:  2,
			wantTools:  map[string]int{"memory": 2, "other": 3},
			wantEvents: []string{EventPrompt, EventToolCall, EventToolCall, EventToolCall,
				EventToolCall, EventToolCall, EventToolCall, EventRunFinished},
		},
		{
			name: "provider switch starts fresh",
			give: &fakeSession{script: []provider.Event{
				{Kind: provider.KindSession, SessionID: "new-session"},
				{Kind: provider.KindText, Text: "hi"}, turnEnd,
			}},
			provider: "codex", session: "old-session", owner: "claude",
			wantStatus: store.StatusDone,
			wantReply:  "Provider changed to codex; starting a fresh session.\n\nhi",
			wantSends:  2,
			wantEvents: []string{EventPrompt, EventText, EventText, EventRunFinished},
		},
		{
			name: "same provider resumes",
			give: &fakeSession{script: []provider.Event{
				{Kind: provider.KindText, Text: "hi"}, turnEnd,
			}},
			session: "old-session", owner: "claude", wantResume: "old-session",
			wantStatus: store.StatusDone, wantReply: "hi", wantSends: 2,
			wantEvents: []string{EventPrompt, EventText, EventRunFinished},
		},
		{
			name: "switch from codex starts fresh",
			give: &fakeSession{script: []provider.Event{
				{Kind: provider.KindText, Text: "hi"}, turnEnd,
			}},
			session: "old-session", owner: "codex",
			wantStatus: store.StatusDone,
			wantReply:  "Provider changed to claude; starting a fresh session.\n\nhi",
			wantSends:  2,
			wantEvents: []string{EventPrompt, EventText, EventText, EventRunFinished},
		},
		{
			name: "session without a provider starts fresh",
			give: &fakeSession{script: []provider.Event{
				{Kind: provider.KindText, Text: "hi"}, turnEnd,
			}},
			session:    "old-session",
			wantStatus: store.StatusDone,
			wantReply:  "Provider changed to claude; starting a fresh session.\n\nhi",
			wantSends:  2,
			wantEvents: []string{EventPrompt, EventText, EventText, EventRunFinished},
		},
		{
			name: "reply",
			give: &fakeSession{script: []provider.Event{
				{Kind: provider.KindText, Text: " hi "}, {Kind: provider.KindText, Text: "there\n"}, turnEnd,
			}},
			reportOn:   1,
			wantStatus: store.StatusDone,
			wantReply:  "hi there",
			wantReport: "prompt 1",
			wantSends:  1,
			wantEvents: []string{EventPrompt, EventText, EventText, EventRunFinished},
		},
		{
			name: "report after the nudge",
			give: &fakeSession{
				script: []provider.Event{{Kind: provider.KindText, Text: "hi"}, turnEnd},
				nudge: []provider.Event{
					{Kind: provider.KindToolCall, ID: "tu1", Name: "mcp__atoll__finish_run"},
					{Kind: provider.KindText, Text: "reported"}, turnEnd,
				},
			},
			reportOn:   2,
			wantStatus: store.StatusDone,
			wantReply:  "hi",
			wantReport: "prompt 2",
			wantSends:  2,
			wantEvents: []string{EventPrompt, EventText, EventToolCall, EventRunFinished},
		},
		{
			name:       "no report after the nudge",
			give:       &fakeSession{script: []provider.Event{{Kind: provider.KindText, Text: "hi"}, turnEnd}},
			wantStatus: store.StatusDone,
			wantReply:  "hi",
			wantSends:  2,
			wantEvents: []string{EventPrompt, EventText, EventRunFinished},
		},
		{
			name: "the nudge turn fails",
			give: &fakeSession{
				script: []provider.Event{{Kind: provider.KindText, Text: "hi"}, turnEnd},
				nudge:  []provider.Event{{Kind: provider.KindError, Text: "rate limited"}},
			},
			wantStatus: store.StatusDone,
			wantReply:  "hi",
			wantSends:  2,
			wantEvents: []string{EventPrompt, EventText, EventRunFinished},
		},
		{
			name:       "provider error",
			give:       &fakeSession{script: []provider.Event{{Kind: provider.KindError, Text: "rate limited"}}},
			wantStatus: store.StatusFailed,
			wantError:  "rate limited",
			wantSends:  1,
			wantEvents: []string{EventPrompt, EventRunFinished},
		},
		{
			name: "provider signed out",
			give: &fakeSession{script: []provider.Event{
				{Kind: provider.KindError, Text: "token revoked", SignedOut: true},
			}},
			wantStatus: store.StatusFailed,
			wantError:  "provider is signed out: token revoked",
			wantKind:   store.ErrorSignedOut,
			wantSends:  1,
			wantEvents: []string{EventPrompt, EventRunFinished},
		},
		{
			name: "stopped while signed out",
			give: &fakeSession{script: []provider.Event{
				{Kind: provider.KindError, Text: "token revoked", SignedOut: true},
			}},
			stopped:    true,
			wantStatus: store.StatusStopped,
			wantSends:  1,
			wantEvents: []string{EventPrompt, EventRunFinished},
		},
		{
			name:       "events end early",
			give:       &fakeSession{end: true},
			wantStatus: store.StatusFailed,
			wantError:  "provider exited before the turn ended",
			wantSends:  1,
			wantEvents: []string{EventPrompt, EventRunFinished},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t, tt.give)
			if tt.session != "" {
				if err := e.st.SetSession(t.Context(), e.thread.ID, tt.session, tt.owner); err != nil {
					t.Fatal(err)
				}
			}
			if tt.provider != "" {
				bot, err := e.st.Bot(t.Context(), e.thread.BotID)
				if err != nil {
					t.Fatal(err)
				}
				bot.Provider = tt.provider
				if _, err := e.st.UpdateBot(t.Context(), bot); err != nil {
					t.Fatal(err)
				}
			}
			tt.give.onSend = func(n int) {
				if tt.stopped {
					e.m.mu.Lock()
					e.m.runs[e.thread.ID].stopped = true
					e.m.mu.Unlock()
				}
				if n != tt.reportOn {
					return
				}
				e.m.mu.Lock()
				token := slices.Collect(maps.Keys(e.m.tokens))[0]
				e.m.mu.Unlock()
				rep := store.Report{Done: "prompt " + strconv.Itoa(n)}
				if err := e.m.Report(t.Context(), token, rep); err != nil {
					t.Error(err)
				}
			}
			_, run, err := e.m.Send(t.Context(), e.thread.ID, "hello", nil)
			if err != nil {
				t.Fatal(err)
			}
			fin := nextAs[Finished](t, e, EventRunFinished)
			got, err := e.st.Run(t.Context(), run.ID)
			if err != nil {
				t.Fatal(err)
			}
			if fin.Status != tt.wantStatus || got.Status != tt.wantStatus || got.Reply != tt.wantReply ||
				got.Error != tt.wantError || got.ErrorKind != tt.wantKind {
				t.Errorf("run_finished %+v, stored %+v; want %s, reply %q, error %q, kind %q",
					fin, got, tt.wantStatus, tt.wantReply, tt.wantError, tt.wantKind)
			}
			if (got.Report == nil) != (tt.wantReport == "") ||
				got.Report != nil && got.Report.Done != tt.wantReport {
				t.Errorf("stored report %+v, want done %q", got.Report, tt.wantReport)
			}
			if len(tt.give.prompts) != tt.wantSends {
				t.Errorf("%d prompts sent, want %d", len(tt.give.prompts), tt.wantSends)
			}
			if tt.wantSends == 2 && tt.give.prompts[1] != _nudge {
				t.Errorf("second prompt %q is not the nudge", tt.give.prompts[1])
			}
			if tt.give.interrupted != 1 {
				t.Errorf("Interrupt called %d times, want 1", tt.give.interrupted)
			}
			if tt.give.resumed != tt.wantResume {
				t.Errorf("resumed %q, want %q", tt.give.resumed, tt.wantResume)
			}
			if tt.wantTools != nil && !maps.Equal(got.Tools, tt.wantTools) {
				t.Errorf("tool counts = %v, want %v", got.Tools, tt.wantTools)
			}
			if got := eventTypes(t, e, run.ID); !slices.Equal(got, tt.wantEvents) {
				t.Errorf("event file holds %v, want %v", got, tt.wantEvents)
			}
		})
	}
}

func TestRun_ActivityWrites(t *testing.T) {
	script := make([]provider.Event, 1000, 1001)
	for i := range script {
		script[i] = provider.Event{Kind: provider.KindText, Text: "x"}
	}
	e := newEnv(t, &fakeSession{script: append(script, provider.Event{Kind: provider.KindTurnEnd})})
	e.sess.events = make(chan provider.Event, len(script)+1)
	_, err := e.st.DB().ExecContext(t.Context(), `
		CREATE TABLE touches (n INTEGER);
		CREATE TRIGGER touched AFTER UPDATE OF last_activity ON threads
		BEGIN INSERT INTO touches VALUES (1); END`)
	if err != nil {
		t.Fatal(err)
	}
	_, ru, err := e.m.Send(t.Context(), e.thread.ID, "hello", nil)
	if err != nil {
		t.Fatal(err)
	}
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(10 * time.Millisecond) {
		got, err := e.st.Run(t.Context(), ru.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.Status == store.StatusDone {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("run still %s", got.Status)
		}
	}
	var writes int
	if err := e.st.DB().QueryRowContext(t.Context(), "SELECT count(*) FROM touches").Scan(&writes); err != nil {
		t.Fatal(err)
	}
	t.Logf("1000 text deltas wrote last_activity %d times", writes)
	if writes != 2 {
		t.Errorf("last_activity written %d times, want 2 (run start, run finish)", writes)
	}
}

func TestRun_FinishNotSaved(t *testing.T) {
	e := newEnv(t, &fakeSession{script: []provider.Event{
		{Kind: provider.KindText, Text: "hi"}, {Kind: provider.KindTurnEnd},
	}})
	e.m.cfg.Launch = func(context.Context, string) (provider.Session, provider.Config, error) {
		_ = e.st.Close()
		return e.sess, provider.Config{}, nil
	}
	_, _, err := e.m.Send(t.Context(), e.thread.ID, "hello", nil)
	if err != nil {
		t.Fatal(err)
	}
	fin := nextAs[Finished](t, e, EventRunFinished)
	if fin.Status != store.StatusFailed || fin.Error != "could not save the run: begin: sql: database is closed" {
		t.Errorf("run_finished %+v, want failed with the save error", fin)
	}
	if _, _, err := e.m.Send(t.Context(), e.thread.ID, "again", nil); errors.Is(err, ErrBusy) {
		t.Error("thread is still busy after the run finished")
	}
}

func TestRun_Approval(t *testing.T) {
	tests := []struct {
		name  string
		allow bool
	}{{"allow", true}, {"deny", false}}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t, &fakeSession{
				script: []provider.Event{
					{Kind: provider.KindApproval, ID: "p1", Name: "Write", Input: json.RawMessage(`{"a":1}`)},
				},
				answered: make(chan struct{}),
				release:  make(chan struct{}),
			})
			_, run, err := e.m.Send(t.Context(), e.thread.ID, "hello", nil)
			if err != nil {
				t.Fatal(err)
			}
			ap := nextAs[Approval](t, e, EventApproval)
			gotRun, gotOpen, ok := e.m.Active(e.thread.ID)
			if !ok || gotRun != run.ID || !reflect.DeepEqual(gotOpen, []Approval{ap}) {
				t.Errorf("Active = %q, %v, %v; want %q, [%v], true", gotRun, gotOpen, ok, run.ID, ap)
			}
			gotOpen[0].Input[0] = 'x'
			if _, again, _ := e.m.Active(e.thread.ID); string(again[0].Input) != `{"a":1}` {
				t.Errorf("Active input mutated through its caller: %s", again[0].Input)
			}
			if got, _ := e.st.Run(t.Context(), run.ID); got.Status != store.StatusWaiting {
				t.Errorf("status %q when the approval was published, want waiting", got.Status)
			}
			if _, _, err := e.m.Send(t.Context(), e.thread.ID, "again", nil); !errors.Is(err, ErrBusy) {
				t.Errorf("Send during a run: %v, want ErrBusy", err)
			}
			if err := e.m.Answer(t.Context(), ap.ID, tt.allow); err != nil {
				t.Fatal(err)
			}
			if _, gotOpen, _ := e.m.Active(e.thread.ID); len(gotOpen) != 0 {
				t.Errorf("Active lists %v after the answer, want none", gotOpen)
			}
			if err := e.m.Answer(t.Context(), ap.ID, tt.allow); !errors.Is(err, ErrNotFound) {
				t.Errorf("second Answer: %v, want ErrNotFound", err)
			}
			<-e.sess.answered
			if got, _ := e.st.Run(t.Context(), run.ID); got.Status != store.StatusRunning {
				t.Errorf("status %q after the answer, want running", got.Status)
			}
			close(e.sess.release)
			e.next(t, EventRunFinished)
			e.sess.mu.Lock()
			defer e.sess.mu.Unlock()
			if len(e.sess.answers) != 1 || e.sess.answers[0] != tt.allow {
				t.Errorf("provider answers %v, want [%v]", e.sess.answers, tt.allow)
			}
		})
	}
}

func TestRun_ApprovalEndedByProvider(t *testing.T) {
	e := newEnv(t, &fakeSession{script: []provider.Event{
		{Kind: provider.KindApproval, ID: "p1", Name: "Write", Input: json.RawMessage(`{"a":1}`)},
	}})
	_, run, err := e.m.Send(t.Context(), e.thread.ID, "hello", nil)
	if err != nil {
		t.Fatal(err)
	}
	ap := nextAs[Approval](t, e, EventApproval)
	e.sess.events <- provider.Event{Kind: provider.KindApprovalEnd, ID: "p1"}
	if closed := nextAs[ApprovalClosed](t, e, EventApprovalEnd); closed.ID != ap.ID {
		t.Errorf("approval_closed id %q, want %q", closed.ID, ap.ID)
	}
	if err := e.m.Answer(t.Context(), ap.ID, true); !errors.Is(err, ErrNotFound) {
		t.Errorf("Answer after the provider ended the request: %v, want ErrNotFound", err)
	}
	e.sess.events <- provider.Event{Kind: provider.KindTurnEnd}
	e.next(t, EventRunFinished)
	if got, _ := e.st.Run(t.Context(), run.ID); got.Status != store.StatusDone {
		t.Errorf("status %q, want done", got.Status)
	}
	e.sess.mu.Lock()
	defer e.sess.mu.Unlock()
	if len(e.sess.answers) != 0 {
		t.Errorf("provider answers %v, want none", e.sess.answers)
	}
}

func TestRun_ApprovalOutlivesRun(t *testing.T) {
	tests := []struct {
		name       string
		finish     func(*fakeSession)
		wantStatus string
	}{
		{"provider exits", func(f *fakeSession) { f.close() }, store.StatusFailed},
		{
			"turn ends",
			func(f *fakeSession) { f.events <- provider.Event{Kind: provider.KindTurnEnd} },
			store.StatusDone,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t, &fakeSession{script: []provider.Event{
				{Kind: provider.KindToolCall, ID: "tu1", Name: "Write"},
			}})
			_, run, err := e.m.Send(t.Context(), e.thread.ID, "hello", nil)
			if err != nil {
				t.Fatal(err)
			}
			e.next(t, EventToolCall)
			e.m.mu.Lock()
			token := slices.Collect(maps.Keys(e.m.tokens))[0]
			e.m.mu.Unlock()
			asked := make(chan error, 1)
			go func() {
				_, err := e.m.Ask(t.Context(), token, "tu1", "Write", nil)
				asked <- err
			}()
			ap := nextAs[Approval](t, e, EventApproval)
			e.sess.mu.Lock()
			tt.finish(e.sess)
			e.sess.mu.Unlock()
			e.next(t, EventRunFinished)
			if err := e.m.Answer(t.Context(), ap.ID, true); err != nil {
				t.Fatal(err)
			}
			if err := <-asked; err != nil {
				t.Fatal(err)
			}
			if got, _ := e.st.Run(t.Context(), run.ID); got.Status != tt.wantStatus {
				t.Errorf("status %q after the approval was answered, want %s", got.Status, tt.wantStatus)
			}
		})
	}
}

type readFunc func([]byte) (int, error)

func (f readFunc) Read(p []byte) (int, error) { return f(p) }

func TestManager_Subscribers(t *testing.T) {
	tests := []struct {
		name       string
		do         func(*testing.T, *env)
		wantEvents int
	}{
		{"a full buffer closes the stream", func(_ *testing.T, e *env) {
			for range _subscriberBuffer + 1 {
				e.m.publish(&active{threadID: e.thread.ID}, EventText, Text{})
			}
		}, _subscriberBuffer},
		{"Close closes the stream", func(_ *testing.T, e *env) { _ = e.m.Close() }, 0},
		{"publish during replay is delivered once", func(t *testing.T, e *env) {
			file, err := e.m.openLog("replay")
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = file.Close() }()
			a := &active{threadID: e.thread.ID, log: file}
			e.m.publish(a, EventToolCall, ToolCall{ID: "before"})
			e.next(t, EventToolCall)
			source, err := os.Open(file.Name())
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = source.Close() }()
			info, err := source.Stat()
			if err != nil {
				t.Fatal(err)
			}
			snapshot := io.NewSectionReader(source, 0, info.Size())
			published := false
			for ch := range e.m.subs[e.thread.ID] {
				e.m.replay(e.thread.ID, ch, readFunc(func(p []byte) (int, error) {
					if !published {
						e.m.publish(a, EventToolCall, ToolCall{ID: "during"})
						published = true
					}
					return snapshot.Read(p)
				}))
			}
			for _, want := range []string{"before", "during"} {
				ev := e.next(t, EventToolCall)
				body, err := json.Marshal(ev.Data)
				if err != nil {
					t.Fatal(err)
				}
				var call ToolCall
				if err := json.Unmarshal(body, &call); err != nil || call.ID != want {
					t.Fatalf("replayed event = %s, %v; want %s", body, err, want)
				}
			}
			_ = e.m.Close()
		}, 0},
		{"late subscriber replays the log without text", func(t *testing.T, e *env) {
			file, err := e.m.openLog("replay")
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = file.Close() }()
			a := &active{threadID: e.thread.ID, run: store.Run{ID: "replay"}, log: file}
			e.m.runs[e.thread.ID] = a
			e.m.publish(a, EventToolCall, ToolCall{ID: "before"})
			e.m.publish(a, EventText, Text{Text: "omitted"})
			late, cancel := e.m.Subscribe(e.thread.ID)
			defer cancel()
			e.m.publish(a, EventToolCall, ToolCall{ID: "after"})
			delete(e.m.runs, e.thread.ID)
			_ = e.m.Close()
			var ids []string
			for ev := range late {
				body, err := json.Marshal(ev.Data)
				if err != nil {
					t.Fatal(err)
				}
				var call ToolCall
				if err := json.Unmarshal(body, &call); err != nil || ev.Type != EventToolCall {
					t.Fatalf("replay = %s %s, %v; want tool call", ev.Type, body, err)
				}
				ids = append(ids, call.ID)
			}
			if !slices.Equal(ids, []string{"before", "after"}) {
				t.Errorf("late subscriber IDs = %v, want [before after]", ids)
			}
		}, 3},
		{"ActiveTool input is a copy", func(t *testing.T, e *env) {
			a := &active{threadID: e.thread.ID, tool: ToolCall{Input: json.RawMessage(`{"a":1}`)}}
			e.m.runs[e.thread.ID] = a
			got := e.m.ActiveTool(e.thread.ID)
			got.Input[0] = 'x'
			if again := e.m.ActiveTool(e.thread.ID); string(again.Input) != `{"a":1}` {
				t.Errorf("ActiveTool input mutated through its caller: %s", again.Input)
			}
			delete(e.m.runs, e.thread.ID)
			_ = e.m.Close()
		}, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t, &fakeSession{})
			tt.do(t, e)
			got := 0
			for range e.events {
				got++
			}
			if got != tt.wantEvents {
				t.Errorf("subscriber got %d events before its stream closed, want %d", got, tt.wantEvents)
			}
		})
	}
}

func TestRun_PrivateFiles(t *testing.T) {
	e := newEnv(t, &fakeSession{script: []provider.Event{{Kind: provider.KindTurnEnd}}})
	_, run, err := e.m.Send(t.Context(), e.thread.ID, "hello", nil)
	if err != nil {
		t.Fatal(err)
	}
	e.next(t, EventRunFinished)
	data := e.m.cfg.DataDir
	for path, want := range map[string]os.FileMode{
		filepath.Join(data, "runs"):                  0o700,
		filepath.Join(data, "runs", run.ID+".jsonl"): 0o600,
		filepath.Join(data, "threads", e.thread.ID):  0o700,
	} {
		fi, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if got := fi.Mode().Perm(); got != want {
			t.Errorf("%s mode %v, want %v", path, got, want)
		}
	}
	for _, tt := range []struct {
		name, provider string
		files          []string
	}{
		{"delete Claude copies", "claude", []string{
			"projects/old/session.jsonl", "projects/new/session.jsonl",
		}},
		{"delete Codex nested rollout", "codex", []string{
			"sessions/2026/11/08/rollout-2026-11-08T12-00-00-session.jsonl",
		}},
		{"delete missing transcripts", "claude", nil},
	} {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t, &fakeSession{script: []provider.Event{
				{Kind: provider.KindSession, SessionID: "session"},
				{Kind: provider.KindTurnEnd},
			}})
			bot, err := e.st.Bot(t.Context(), e.thread.BotID)
			if err != nil {
				t.Fatal(err)
			}
			bot.Provider = tt.provider
			if _, err := e.st.UpdateBot(t.Context(), bot); err != nil {
				t.Fatal(err)
			}
			_, run, err := e.m.Send(t.Context(), e.thread.ID, "hello", nil)
			if err != nil {
				t.Fatal(err)
			}
			e.next(t, EventRunFinished)
			providerDir := filepath.Join(e.m.cfg.DataDir, "providers", tt.provider)
			keep := filepath.Join(providerDir, "projects", "old", "unrelated.jsonl")
			if tt.provider == "codex" {
				keep = filepath.Join(providerDir, "sessions", "2026", "11", "08", "rollout-time-unrelated.jsonl")
			}
			personal := filepath.Join(t.TempDir(), "session.jsonl")
			paths := []string{keep, personal}
			for _, file := range tt.files {
				paths = append(paths, filepath.Join(providerDir, file))
			}
			for _, path := range paths {
				if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte("transcript"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if err := e.m.DeleteBot(t.Context(), bot.ID); err != nil {
				t.Fatal(err)
			}
			for _, path := range append(paths[2:],
				filepath.Join(e.m.cfg.DataDir, "runs", run.ID+".jsonl"),
				e.m.threadDir(e.thread.ID)) {
				if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
					t.Errorf("deleted Bot file %s still exists: %v", path, err)
				}
			}
			for _, path := range []string{keep, personal} {
				if _, err := os.Stat(path); err != nil {
					t.Errorf("unrelated transcript %s removed: %v", path, err)
				}
			}
		})
	}
}

// eventTypes returns the types of the events in the run's event file, in order.
func eventTypes(t *testing.T, e *env, runID string) []string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(e.m.cfg.DataDir, "runs", runID+".jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	var types []string
	for _, line := range strings.Split(strings.TrimSpace(string(body)), "\n") {
		var ev eventLine
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			t.Fatal(err)
		}
		types = append(types, ev.Type)
	}
	return types
}

func TestToolVerb(t *testing.T) {
	tests := []struct{ name, tool, kind, want string }{
		{"search", "Grep", "search", "Searching"},
		{"recall", "mcp__atoll__recall", "memory", "Remembering"},
		{"read memory", "mcp__atoll__read_memory", "memory", "Remembering"},
		{"retire", "mcp__atoll__retire_memory", "memory", "Taking a note"},
		{"remember", "mcp__atoll__remember", "memory", "Taking a note"},
		{"branch", "mcp__atoll__start_work", "work", "Starting a branch"},
		{"mr", "mcp__atoll__track_mr", "work", "Opening the MR"},
		{"platform", "create_bot", "platform", "Setting up"},
		{"edit", "Edit", "edit", "Editing"},
		{"command", "Bash", "command", "Running"},
		{"unknown", "unknown", "other", "Thinking"},
		{"user memory server", "mcp__memory__remember", "other", "Thinking"},
		{"user start work", "mcp__user__start_work", "other", "Thinking"},
		{"similar Reef tool", "mcp__atoll__recall_extra", "platform", "Setting up"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ToolVerb(tt.tool, tt.kind); got != tt.want {
				t.Errorf("ToolVerb(%q, %q) = %q, want %q", tt.tool, tt.kind, got, tt.want)
			}
		})
	}
}

func TestToolKind(t *testing.T) {
	tests := []struct{ name, tool, input, kind, target string }{
		{"bash description", "Bash", `{"command":"ls ~/.claude/agents/ 2>/dev/null","description":"List agent files"}`, "command", "List agent files"},
		{"bash program", "Bash", `{"command":"/usr/bin/grep -rn agent ~/.claude"}`, "command", "grep"},
		{"Reef memory", "mcp__atoll__recall", `{"query":"style"}`, "memory", "style"},
		{"Reef note", "mcp__atoll__remember", `{"topic":"design"}`, "memory", "design"},
		{"Reef work", "mcp__atoll__start_work", `{}`, "work", ""},
		{"Reef platform", "mcp__atoll__create_bot", `{"name":"Ada"}`, "platform", "Ada"},
		{"user memory server", "mcp__memory__recall", `{}`, "other", ""},
		{"user tool", "mcp__user__remember", `{}`, "other", ""},
		{"bare memory tool", "remember", `{}`, "other", ""},
		{"user start work", "mcp__user__start_work", `{}`, "other", ""},
		{"similar Reef tool", "mcp__atoll__recall_extra", `{}`, "platform", ""},
		{"glob", "Glob", `{"pattern":"web/src/**/*.tsx"}`, "search", "*.tsx"},
		{"codex command", "commandExecution", `{"commandActions":[{"type":"unknown","command":"pnpm test"}]}`, "command", "pnpm"},
		{"codex search", "commandExecution", `{"commandActions":[{"type":"search","query":"web/src/app"}]}`, "search", "app"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			kind, target := ToolKind(tt.tool, json.RawMessage(tt.input))
			if kind != tt.kind || target != tt.target {
				t.Errorf("ToolKind(%q, %s) = %q, %q, want %q, %q", tt.tool, tt.input, kind, target, tt.kind, tt.target)
			}
		})
	}
}

func TestShortMemory(t *testing.T) {
	tests := []struct{ name, body, want string }{
		{name: "whitespace", body: "first\n  second", want: "first second"},
		{name: "rune boundary", body: strings.Repeat("界", 81), want: strings.Repeat("界", 80) + "…"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shortMemory(tt.body); got != tt.want {
				t.Errorf("shortMemory() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRun_SubscribeAll(t *testing.T) {
	e := newEnv(t, &fakeSession{
		script: []provider.Event{
			{Kind: provider.KindApproval, ID: "p1", Name: "Write", Input: json.RawMessage(`{"a":1}`)},
		},
		answered: make(chan struct{}),
		release:  make(chan struct{}),
	})
	if _, _, err := e.m.Send(t.Context(), e.thread.ID, "hello", nil); err != nil {
		t.Fatal(err)
	}
	ap := nextAs[Approval](t, e, EventApproval)
	all, cancel := e.m.SubscribeAll()
	defer cancel()
	if err := e.m.Answer(t.Context(), ap.ID, true); err != nil {
		t.Fatal(err)
	}
	<-e.sess.answered
	close(e.sess.release)
	var got []string
	for len(got) < 3 {
		select {
		case ev := <-all:
			got = append(got, ev.Type+" "+ev.ThreadID)
		case <-time.After(5 * time.Second):
			t.Fatalf("events so far: %v", got)
		}
	}
	want := []string{
		EventApproval + " " + e.thread.ID,
		EventApprovalEnd + " " + e.thread.ID,
		EventRunFinished + " " + e.thread.ID,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("events %v, want %v", got, want)
	}
}
