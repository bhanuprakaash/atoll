package server

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"

	"atoll/run"
	"atoll/store"
)

func newHandler(t *testing.T) http.Handler {
	t.Helper()
	h, _ := newHandlerWith(t, nil)
	return h
}

func newHandlerWith(t *testing.T, providers Providers) (http.Handler, *store.Store) {
	t.Helper()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	return New(st, nil, run.New(run.Config{Store: st, Log: log}), log, providers), st
}

func serve(h http.Handler, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequestWithContext(context.Background(), method, path, strings.NewReader(body))
	req.Host = "127.0.0.1:4317"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestAnswer_RequiresApprove(t *testing.T) {
	tests := []struct {
		name       string
		give       string
		wantStatus int
	}{
		{"no approve field", `{}`, 400},
		{"not JSON", `nope`, 400},
		{"approve true", `{"approve":true}`, 404},
		{"approve false", `{"approve":false}`, 404},
	}
	h := newHandler(t)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/approvals/x",
				strings.NewReader(tt.give))
			req.Host = "127.0.0.1:4317"
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != tt.wantStatus {
				t.Errorf("status %d, want %d; body: %s", rec.Code, tt.wantStatus, rec.Body)
			}
		})
	}
}

func TestLocalOnly(t *testing.T) {
	h := newHandler(t)

	tests := []struct {
		name       string
		path       string
		host       string
		origin     string
		wantStatus int
	}{
		{"127.0.0.1 with a port", "/api/bots", "127.0.0.1:4317", "", 200},
		{"localhost", "/api/bots", "localhost:4317", "", 200},
		{"IPv6 loopback", "/api/bots", "[::1]:4317", "", 200},
		{"same-origin page", "/api/bots", "127.0.0.1:4317", "http://127.0.0.1:4317", 200},
		{"rebinding host", "/api/bots", "evil.example:4317", "", 403},
		{"other origin", "/api/bots", "127.0.0.1:4317", "https://evil.example", 403},
		{"other port as origin", "/api/bots", "127.0.0.1:4317", "http://127.0.0.1:80", 403},
		{"null origin", "/api/bots", "127.0.0.1:4317", "null", 403},
		{"mcp rebinding host", MCPPath + "tok", "evil.example", "", 403},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, tt.path, nil)
			req.Host = tt.host
			if tt.origin != "" {
				req.Header.Set("Origin", tt.origin)
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != tt.wantStatus {
				t.Errorf("status %d, want %d; body: %s", rec.Code, tt.wantStatus, rec.Body)
			}
		})
	}
}

func TestGetSettings_DataDir(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := New(st, nil, run.New(run.Config{Store: st, Log: log, DataDir: "/data", WorktreeRoot: "/wt"}), log, nil)
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/settings", nil)
	req.Host = "127.0.0.1:4317"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	want := `{"worktree_root":"/wt","user_name":"","data_dir":"/data"}`
	if got := strings.TrimSpace(rec.Body.String()); got != want {
		t.Errorf("body %s, want %s", got, want)
	}
}

func TestShortCommand(t *testing.T) {
	tests := []struct {
		name string
		give string
		want string
	}{
		{name: "program and argument", give: "make e2e", want: "make e2e"},
		{name: "past cd", give: "cd /tmp/atoll && make check 2>&1 | tail -5", want: "make check"},
		{name: "path program", give: "/usr/bin/git status --short", want: "git status"},
		{name: "flag argument", give: "go test -count=1 ./e2e", want: "go test"},
		{name: "path argument", give: "ls ~/.claude/agents", want: "ls"},
		{name: "subshell", give: "cd x && (e2e/with-secret-store go test ./e2e)", want: "with-secret-store go"},
		{name: "only cd", give: "cd /tmp", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shortCommand(tt.give); got != tt.want {
				t.Errorf("shortCommand(%q) = %q, want %q", tt.give, got, tt.want)
			}
		})
	}
}

func TestCreateRepo_UnsafeName(t *testing.T) {
	h := newHandler(t)
	for _, name := range []string{"../x", ".", "..", "a/b"} {
		t.Run(name, func(t *testing.T) {
			rec := serve(h, http.MethodPost, "/api/repos", `{"name":"`+name+`","path":"/tmp"}`)
			if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "name must be one folder name") {
				t.Errorf("status %d, body %s; want 400 about the name", rec.Code, rec.Body)
			}
		})
	}
}

func TestBots_UnknownModel(t *testing.T) {
	h, st := newHandlerWith(t, fakeProviders{{
		Name: "claude", SignedIn: true, Models: []Model{{ID: "opus"}, {ID: "sonnet"}},
	}})
	bot, err := st.CreateBot(t.Context(), store.Bot{Name: "Ada", Provider: "claude", Model: "retired"})
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name       string
		method     string
		path       string
		give       string
		wantStatus int
		wantBody   string
	}{
		{"create", http.MethodPost, "/api/bots", `{"name":"Bo","provider":"claude","model":"gpt"}`, 400, `models: opus, sonnet`},
		{"edit", http.MethodPut, "/api/bots/" + bot.ID, `{"model":"gpt"}`, 400, `models: opus, sonnet`},
		{"edit keeping the model it has", http.MethodPut, "/api/bots/" + bot.ID, `{"name":"Ada 2"}`, 200, `"model":"retired"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := serve(h, tt.method, tt.path, tt.give)
			if rec.Code != tt.wantStatus || !strings.Contains(rec.Body.String(), tt.wantBody) {
				t.Errorf("status %d, body %s; want %d containing %s", rec.Code, rec.Body, tt.wantStatus, tt.wantBody)
			}
		})
	}
}

func TestPutBot_ConcurrentPartialSaves(t *testing.T) {
	h, st := newHandlerWith(t, nil)
	bot, err := st.CreateBot(t.Context(), store.Bot{Name: "Ada", Provider: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	modes := []string{"ask", "full"}
	for i := range 20 {
		name, mode := "Ada "+strconv.Itoa(i), modes[i%2]
		var wg sync.WaitGroup
		for _, give := range []string{`{"name":"` + name + `"}`, `{"mode":"` + mode + `"}`} {
			wg.Go(func() {
				if rec := serve(h, http.MethodPut, "/api/bots/"+bot.ID, give); rec.Code != http.StatusOK {
					t.Errorf("PUT %s: status %d, body %s", give, rec.Code, rec.Body)
				}
			})
		}
		wg.Wait()
		got, err := st.Bot(t.Context(), bot.ID)
		if err != nil || got.Name != name || got.Mode != mode {
			t.Fatalf("round %d: name %q mode %q (%v), want %q %q", i, got.Name, got.Mode, err, name, mode)
		}
	}
}
