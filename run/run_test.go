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
	answers     []bool
	interrupted int
}

func (f *fakeSession) Start(context.Context, provider.Config) error          { return nil }
func (f *fakeSession) Resume(context.Context, provider.Config, string) error { return nil }
func (f *fakeSession) Events() <-chan provider.Event                         { return f.events }

func (f *fakeSession) Send(_ context.Context, prompt string) error {
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
	m := New(Config{
		Store: st,
		Launch: func(context.Context, string) (provider.Session, provider.Config, error) {
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
		wantReport string
		wantSends  int
		wantEvents []string
	}{
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
			wantEvents: []string{EventText, EventText, EventRunFinished},
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
			wantEvents: []string{EventText, EventToolCall, EventRunFinished},
		},
		{
			name:       "no report after the nudge",
			give:       &fakeSession{script: []provider.Event{{Kind: provider.KindText, Text: "hi"}, turnEnd}},
			wantStatus: store.StatusDone,
			wantReply:  "hi",
			wantSends:  2,
			wantEvents: []string{EventText, EventRunFinished},
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
			wantEvents: []string{EventText, EventRunFinished},
		},
		{
			name:       "provider error",
			give:       &fakeSession{script: []provider.Event{{Kind: provider.KindError, Text: "rate limited"}}},
			wantStatus: store.StatusFailed,
			wantError:  "rate limited",
			wantSends:  1,
			wantEvents: []string{EventRunFinished},
		},
		{
			name:       "events end early",
			give:       &fakeSession{end: true},
			wantStatus: store.StatusFailed,
			wantError:  "provider exited before the turn ended",
			wantSends:  1,
			wantEvents: []string{EventRunFinished},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t, tt.give)
			tt.give.onSend = func(n int) {
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
			_, run, err := e.m.Send(t.Context(), e.thread.ID, "hello")
			if err != nil {
				t.Fatal(err)
			}
			fin := nextAs[Finished](t, e, EventRunFinished)
			got, err := e.st.Run(t.Context(), run.ID)
			if err != nil {
				t.Fatal(err)
			}
			if fin.Status != tt.wantStatus || got.Status != tt.wantStatus || got.Reply != tt.wantReply ||
				got.Error != tt.wantError {
				t.Errorf("run_finished %+v, stored %+v; want %s, reply %q, error %q",
					fin, got, tt.wantStatus, tt.wantReply, tt.wantError)
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
			if got := eventTypes(t, e, run.ID); !slices.Equal(got, tt.wantEvents) {
				t.Errorf("event file holds %v, want %v", got, tt.wantEvents)
			}
		})
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
	_, _, err := e.m.Send(t.Context(), e.thread.ID, "hello")
	if err != nil {
		t.Fatal(err)
	}
	fin := nextAs[Finished](t, e, EventRunFinished)
	if fin.Status != store.StatusFailed || fin.Error != "could not save the run: begin: sql: database is closed" {
		t.Errorf("run_finished %+v, want failed with the save error", fin)
	}
	if _, _, err := e.m.Send(t.Context(), e.thread.ID, "again"); errors.Is(err, ErrBusy) {
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
			_, run, err := e.m.Send(t.Context(), e.thread.ID, "hello")
			if err != nil {
				t.Fatal(err)
			}
			ap := nextAs[Approval](t, e, EventApproval)
			gotRun, gotOpen, ok := e.m.Active(e.thread.ID)
			if !ok || gotRun != run.ID || !reflect.DeepEqual(gotOpen, []Approval{ap}) {
				t.Errorf("Active = %q, %v, %v; want %q, [%v], true", gotRun, gotOpen, ok, run.ID, ap)
			}
			if got, _ := e.st.Run(t.Context(), run.ID); got.Status != store.StatusWaiting {
				t.Errorf("status %q when the approval was published, want waiting", got.Status)
			}
			if _, _, err := e.m.Send(t.Context(), e.thread.ID, "again"); !errors.Is(err, ErrBusy) {
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
			_, run, err := e.m.Send(t.Context(), e.thread.ID, "hello")
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

func TestManager_Subscribers(t *testing.T) {
	tests := []struct {
		name       string
		do         func(*env)
		wantEvents int
	}{
		{"a full buffer closes the stream", func(e *env) {
			for range _subscriberBuffer + 1 {
				e.m.publish(&active{threadID: e.thread.ID}, EventText, Text{})
			}
		}, _subscriberBuffer},
		{"Close closes the stream", func(e *env) { _ = e.m.Close() }, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t, &fakeSession{})
			tt.do(e)
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
	_, run, err := e.m.Send(t.Context(), e.thread.ID, "hello")
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
