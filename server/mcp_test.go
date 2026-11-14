package server

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path"
	"slices"
	"strings"
	"testing"

	"atoll/provider"
	"atoll/run"
	"atoll/store"
)

func TestMCP(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv := httptest.NewServer(New(st, nil, run.New(run.Config{Store: st, Log: log}), log, nil, ""))
	t.Cleanup(srv.Close)

	tests := []struct {
		name       string
		method     string
		give       string
		wantStatus int
		wantBody   string
	}{
		{
			name: "initialize echoes the protocol version", method: "POST",
			give:       `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}`,
			wantStatus: 200,
			wantBody:   `{"jsonrpc":"2.0","id":1,"result":{"capabilities":{"tools":{}},"protocolVersion":"2025-06-18","serverInfo":{"name":"atoll","version":"0"}}}`,
		},
		{
			name: "notification", method: "POST",
			give:       `{"jsonrpc":"2.0","method":"notifications/initialized"}`,
			wantStatus: 202,
		},
		{
			name: "tools/list", method: "POST",
			give:       `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
			wantStatus: 200,
			wantBody:   `"name":"start_work"`,
		},
		{
			name: "start_work with an unknown token", method: "POST",
			give:       `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"start_work","arguments":{"type":"feat","slug":"x"}}}`,
			wantStatus: 200,
			wantBody:   `"error":{"code":-32603`,
		},
		{
			name: "unknown tool", method: "POST",
			give:       `{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"nope","arguments":{}}}`,
			wantStatus: 200,
			wantBody:   `"error":{"code":-32602,"message":"unknown tool: nope"}`,
		},
		{name: "GET is refused", method: "GET", wantStatus: 405},
		{name: "DELETE ends the session", method: "DELETE", wantStatus: 204},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, err := http.NewRequestWithContext(t.Context(), tt.method,
				srv.URL+MCPPath+"tok", strings.NewReader(tt.give))
			if err != nil {
				t.Fatal(err)
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = resp.Body.Close() }()
			body, _ := io.ReadAll(resp.Body)
			if resp.StatusCode != tt.wantStatus {
				t.Fatalf("status %d, want %d; body: %s", resp.StatusCode, tt.wantStatus, body)
			}
			if !strings.Contains(strings.TrimSpace(string(body)), tt.wantBody) {
				t.Fatalf("body %s, want it to contain %s", body, tt.wantBody)
			}
		})
	}
}

type urlSession struct {
	events chan provider.Event
	url    chan string
}

func (s *urlSession) Start(_ context.Context, cfg provider.Config) error {
	s.url <- cfg.MCPURL
	return nil
}
func (s *urlSession) Resume(ctx context.Context, cfg provider.Config, _ string) error {
	return s.Start(ctx, cfg)
}
func (s *urlSession) Events() <-chan provider.Event                        { return s.events }
func (s *urlSession) Send(context.Context, string, []provider.Image) error { return nil }
func (s *urlSession) Answer(string, bool, string) error                    { return nil }
func (s *urlSession) Interrupt() error                                     { close(s.events); return nil }

type platformEnv struct {
	st     *store.Store
	srv    *httptest.Server
	token  string
	thread store.Thread
}

// newPlatform starts a server with Chief running a thread; the token is the one its run holds.
func newPlatform(t *testing.T, providers Providers) platformEnv {
	t.Helper()
	ctx := t.Context()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	chief, err := st.CreateBot(ctx, store.Bot{Name: "Chief", Provider: "claude", CanManagePlatform: true})
	if err != nil {
		t.Fatal(err)
	}
	thread, err := st.CreateThread(ctx, chief.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	sess := &urlSession{events: make(chan provider.Event), url: make(chan string, 1)}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	runs := run.New(run.Config{
		Store: st, Log: log, DataDir: t.TempDir(), MCPURL: "http://atoll" + MCPPath,
		Launch: func(context.Context, string) (provider.Session, provider.Config, error) {
			return sess, provider.Config{}, nil
		},
	})
	t.Cleanup(func() { _ = runs.Close() })
	srv := httptest.NewServer(New(st, nil, runs, log, providers, "secret"))
	t.Cleanup(srv.Close)
	if _, _, err := runs.Send(ctx, thread.ID, "hi", nil); err != nil {
		t.Fatal(err)
	}
	return platformEnv{st: st, srv: srv, token: path.Base(<-sess.url), thread: thread}
}

// call runs one MCP tool and returns the response body.
func (e platformEnv) call(t *testing.T, tool, args string) string {
	t.Helper()
	body := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"` + tool +
		`","arguments":` + args + `}}`
	resp, err := http.Post(e.srv.URL+MCPPath+e.token, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	out, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

func TestPlatform_EditBotUndo(t *testing.T) {
	ctx := t.Context()
	e := newPlatform(t, nil)
	st, thread := e.st, e.thread
	if _, err := st.DB().ExecContext(ctx,
		"INSERT INTO skills (id, name, content) VALUES ('a', 'a', ''), ('b', 'b', ''), ('c', 'c', '')"); err != nil {
		t.Fatal(err)
	}
	target, err := st.CreateBot(ctx, store.Bot{Name: "Ada", Provider: "claude", Skills: []string{"a", "b"}})
	if err != nil {
		t.Fatal(err)
	}
	e.call(t, "edit_bot", `{"id":"`+target.ID+`","skills":["c"]}`)
	got, err := st.Bot(ctx, target.ID)
	if err != nil || !slices.Equal(got.Skills, []string{"c"}) {
		t.Fatalf("after edit = %v, %v; want [c]", got.Skills, err)
	}
	changes, err := st.Changes(ctx, thread.ID)
	if err != nil || len(changes) != 1 {
		t.Fatalf("changes = %v, %v", changes, err)
	}
	if _, err := st.UndoChange(ctx, changes[0].ID, []string{"context7"}); err != nil {
		t.Fatal(err)
	}
	got, err = st.Bot(ctx, target.ID)
	if err != nil || !slices.Equal(got.Skills, []string{"a", "b"}) {
		t.Fatalf("after undo = %v, %v; want [a b]", got.Skills, err)
	}
}

func TestPlatform_Refusals(t *testing.T) {
	e := newPlatform(t, fakeProviders{
		{Name: "claude", Enabled: true, SignedIn: true, Models: []Model{{ID: "opus"}, {ID: "sonnet"}}},
		{Name: "codex", SignedIn: true},
	})
	tests := []struct {
		name     string
		tool     string
		give     string
		wantBody string
	}{
		{"repo name up a folder", "register_repo", `{"name":"../x","path":"/tmp"}`, "name must be one folder name"},
		{"repo name dot", "register_repo", `{"name":".","path":"/tmp"}`, "name must be one folder name"},
		{"repo name dot dot", "register_repo", `{"name":"..","path":"/tmp"}`, "name must be one folder name"},
		{"repo name with a slash", "register_repo", `{"name":"a/b","path":"/tmp"}`, "name must be one folder name"},
		{"provider that is off, no model", "create_bot", `{"name":"Ada","provider":"codex"}`, "codex is turned off in Settings"},
		{"unknown model", "create_bot", `{"name":"Ada","provider":"claude","model":"gpt"}`, `unknown model \"gpt\"; models: opus, sonnet`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := e.call(t, tt.tool, tt.give); !strings.Contains(got, tt.wantBody) {
				t.Fatalf("response %s, want it to contain %s", got, tt.wantBody)
			}
		})
	}
	bots, err := e.st.Bots(t.Context())
	if err != nil || len(bots) != 1 {
		t.Fatalf("bots = %v, %v; want only Chief", bots, err)
	}
}

func TestPlatform_ChangeRecordFailsWithEntity(t *testing.T) {
	ctx := t.Context()
	e := newPlatform(t, nil)
	_, err := e.st.DB().ExecContext(ctx,
		"CREATE TRIGGER no_changes BEFORE INSERT ON changes BEGIN SELECT RAISE(ABORT, 'no changes'); END")
	if err != nil {
		t.Fatal(err)
	}
	got := e.call(t, "create_bot", `{"name":"Ada","provider":"claude"}`)
	if !strings.Contains(got, "no changes") {
		t.Fatalf("response %s, want the record's error", got)
	}
	bots, err := e.st.Bots(ctx)
	if err != nil || len(bots) != 1 {
		t.Fatalf("bots = %v, %v; want only Chief", bots, err)
	}
}

type fakeProviders []Provider

func (fakeProviders) ServeHTTP(http.ResponseWriter, *http.Request) {}

func (f fakeProviders) Cached(context.Context) []Provider { return f }
