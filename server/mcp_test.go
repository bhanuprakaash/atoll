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

func TestMCP(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv := httptest.NewServer(New(st, nil, run.New(run.Config{Store: st, Log: log}), log))
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
			wantBody:   `"name":"finish_run"`,
		},
		{
			name: "finish_run with an unknown token", method: "POST",
			give:       `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"finish_run","arguments":{"done":"a","tried":"b","blocked":"c","next":"d"}}}`,
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
