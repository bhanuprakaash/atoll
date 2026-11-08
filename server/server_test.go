package server

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"atoll/run"
	"atoll/store"
)

func newHandler(t *testing.T) http.Handler {
	t.Helper()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	return New(st, nil, run.New(run.Config{Store: st, Log: log}), log, nil)
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
