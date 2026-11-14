package server

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"atoll/provider"
	"atoll/run"
	"atoll/store"
)

func TestSidebar_Routes(t *testing.T) {
	ctx := t.Context()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := New(st, nil, run.New(run.Config{Store: st, Log: log}), log, nil, "")
	bot, err := st.CreateBot(ctx, store.Bot{Name: "Ada", Persona: "p", Provider: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for range 4 {
		th, err := st.CreateThread(ctx, bot.ID, "")
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, th.ID)
	}
	_, ru, err := st.StartRunFrom(ctx, store.InboxItem{ThreadID: ids[0], Text: "hello there"})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.FinishRun(ctx, ru.ID, store.RunResult{Status: store.StatusDone, Reply: "ok"}, nil); err != nil {
		t.Fatal(err)
	}
	do := func(method, path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequestWithContext(ctx, method, path, strings.NewReader(body))
		req.Host = "127.0.0.1:4317"
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}
	pin := func(id string) string { return "/api/threads/" + id + "/pin" }

	type row struct {
		ID     string `json:"id"`
		Title  string `json:"title"`
		State  string `json:"state"`
		Pinned bool   `json:"pinned"`
	}
	t.Run("bots list marks the finished thread New", func(t *testing.T) {
		rec := do("GET", "/api/bots", "")
		var bots []struct {
			ID      string `json:"id"`
			Threads []row  `json:"threads"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &bots); rec.Code != 200 || err != nil {
			t.Fatalf("status %d body %s: %v", rec.Code, rec.Body, err)
		}
		want := []row{{ids[0], "hello there", "new", false}, {ids[3], "", "", false}, {ids[2], "", "", false}, {ids[1], "", "", false}}
		var got []row
		for _, b := range bots {
			if b.ID == bot.ID {
				got = b.Threads
			}
		}
		if !slices.Equal(got, want) {
			t.Errorf("threads %+v, want %+v", got, want)
		}
	})

	tests := []struct {
		name       string
		method     string
		path       string
		body       string
		wantStatus int
		wantBody   string
	}{
		{"list has group_id and threads", "GET", "/api/bots", "", 200,
			`"can_manage_platform":false,"group_id":"","threads":[{`},
		{"one Bot has group_id and no threads", "GET", "/api/bots/" + bot.ID, "", 200,
			`"can_manage_platform":false,"group_id":""}`},
		{"open it", "POST", "/api/threads/" + ids[0] + "/opened", "", 204, ""},
		{"opened clears New", "GET", "/api/bots", "", 200,
			`{"id":"` + ids[0] + `","title":"hello there","state":"","pinned":false,`},
		{"pin 1", "POST", pin(ids[0]), "", 204, ""},
		{"pin 2", "POST", pin(ids[1]), `{}`, 204, ""},
		{"pin 3", "POST", pin(ids[2]), "", 204, ""},
		{"pin 4 is refused", "POST", pin(ids[3]), "", 409,
			`"error":"3 threads of this Bot are pinned","code":"conflict"}`},
		{"pin 4 replacing 1", "POST", pin(ids[3]),
			`{"replace":"` + ids[1] + `"}`, 204, ""},
		{"unpin", "DELETE", pin(ids[3]), "", 204, ""},
		{"opened on an unknown thread", "POST", "/api/threads/x/opened", "", 404, ""},
		{"group needs a name", "POST", "/api/groups", `{"name":" "}`, 400, ""},
		{"Chief has no group", "PUT", "/api/bots/chief/group", `{"group_id":null}`, 409, ""},
		{"unknown group", "PUT", "/api/bots/" + bot.ID + "/group", `{"group_id":"x"}`, 404, ""},
		{"move needs up", "POST", "/api/groups/x/move", `{}`, 400, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := do(tt.method, tt.path, tt.body)
			if rec.Code != tt.wantStatus || !strings.Contains(rec.Body.String(), tt.wantBody) {
				t.Errorf("status %d body %s, want %d containing %s", rec.Code, rec.Body, tt.wantStatus, tt.wantBody)
			}
		})
	}
}

func TestNotices_ParentThread(t *testing.T) {
	ctx := t.Context()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	a := &api{st: st}
	bot, err := st.CreateBot(ctx, store.Bot{Name: "Ada", Persona: "p", Provider: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	boss, err := st.CreateThread(ctx, bot.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	helper, err := st.CreateHelperThread(ctx, boss.ID, bot.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name     string
		threadID string
		want     string
	}{
		{"a boss thread has no parent", boss.ID, ""},
		{"a helper's notice names its boss", helper.ID, boss.ID},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			n, err := a.noticeOf(ctx, run.Event{Type: run.EventApproval, ThreadID: tt.threadID})
			if err != nil {
				t.Fatal(err)
			}
			if n.ThreadID != tt.threadID || n.ParentThreadID != tt.want {
				t.Errorf("notice thread %q parent %q, want %q parent %q",
					n.ThreadID, n.ParentThreadID, tt.threadID, tt.want)
			}
		})
	}
}

type heldSession struct {
	events  chan provider.Event
	started chan struct{}
	once    sync.Once
}

func (s *heldSession) Start(context.Context, provider.Config) error {
	close(s.started)
	return nil
}
func (s *heldSession) Resume(context.Context, provider.Config, string) error { return nil }
func (s *heldSession) Events() <-chan provider.Event                         { return s.events }
func (s *heldSession) Send(context.Context, string, []provider.Image) error  { return nil }
func (s *heldSession) Answer(string, bool, string) error                     { return nil }
func (s *heldSession) Interrupt() error {
	s.once.Do(func() { close(s.events) })
	return nil
}

func TestNotices_ApprovalsFirstAndRunOnce(t *testing.T) {
	ctx := t.Context()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	bot, err := st.CreateBot(ctx, store.Bot{Name: "Ada", Provider: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	waiting, err := st.CreateThread(ctx, bot.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	finishing, err := st.CreateThread(ctx, bot.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	sessions := make(chan *heldSession, 2)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	runs := run.New(run.Config{Store: st, DataDir: t.TempDir(), Log: log,
		Launch: func(context.Context, string) (provider.Session, provider.Config, error) {
			return <-sessions, provider.Config{}, nil
		}})
	t.Cleanup(func() { _ = runs.Close() })
	srv := httptest.NewServer(New(st, nil, runs, log, nil, ""))
	t.Cleanup(srv.Close)

	start := func(threadID string) (*heldSession, string) {
		sess := &heldSession{events: make(chan provider.Event), started: make(chan struct{})}
		sessions <- sess
		_, ru, err := runs.Send(ctx, threadID, "go", nil)
		if err != nil {
			t.Fatal(err)
		}
		<-sess.started
		return sess, ru.ID
	}
	asking, _ := start(waiting.ID)
	asking.events <- provider.Event{Kind: provider.KindApproval, ID: "p1", Name: "Write", Input: json.RawMessage(`{}`)}
	var approval string
	for approval == "" {
		if _, open, _ := runs.Active(waiting.ID); len(open) > 0 {
			approval = open[0].ID
		}
		time.Sleep(time.Millisecond)
	}
	ending, endingRun := start(finishing.ID)
	if err := st.FinishRun(ctx, endingRun, store.RunResult{Status: store.StatusDone, Reply: "ok"}, nil); err != nil {
		t.Fatal(err)
	}
	thread, cancelThread := runs.Subscribe(finishing.ID)
	t.Cleanup(cancelThread)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/api/events", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	type frame struct{ typ, data string }
	frames := make(chan frame)
	var wg sync.WaitGroup
	wg.Go(func() {
		sc := bufio.NewScanner(resp.Body)
		var typ string
		for sc.Scan() {
			if v, ok := strings.CutPrefix(sc.Text(), "event: "); ok {
				typ = v
			} else if v, ok := strings.CutPrefix(sc.Text(), "data: "); ok {
				select {
				case frames <- frame{typ, v}:
				case <-ctx.Done():
					return
				}
			}
		}
	})
	t.Cleanup(wg.Wait)
	next := func() frame {
		t.Helper()
		select {
		case f := <-frames:
			return f
		case <-time.After(5 * time.Second):
			t.Fatal("no event")
			return frame{}
		}
	}

	if f := next(); f.typ != run.EventApproval {
		t.Fatalf("first event %s %s, want the open approval", f.typ, f.data)
	}
	f := next()
	var listed []struct {
		Type string    `json:"type"`
		At   time.Time `json:"at"`
		Data struct {
			RunID string `json:"run_id"`
		} `json:"data"`
	}
	if f.typ != "earlier" || json.Unmarshal([]byte(f.data), &listed) != nil {
		t.Fatalf("second event %s %s, want earlier with RFC 3339 times", f.typ, f.data)
	}
	if len(listed) != 1 || listed[0].Type != run.EventRunFinished || listed[0].Data.RunID != endingRun ||
		listed[0].At.IsZero() {
		t.Fatalf("earlier %s, want run %s once", f.data, endingRun)
	}

	ending.events <- provider.Event{Kind: provider.KindTurnEnd}
	for ev := range thread {
		if ev.Type == run.EventRunFinished {
			break
		}
	}
	if err := runs.Answer(ctx, approval, true); err != nil {
		t.Fatal(err)
	}
	if f := next(); f.typ != run.EventApprovalEnd {
		t.Fatalf("event after the run finished %s %s, want approval_closed: the run is in earlier already", f.typ, f.data)
	}
}
