package server

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"atoll/run"
	"atoll/store"
)

func TestLocalOnly(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := New(st, run.New(run.Config{Store: st, Log: log}), log)

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
