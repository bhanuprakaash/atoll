package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"atoll/mcpserver"
	"atoll/store"
)

type errorReply struct {
	Error string `json:"error"`
	Code  string `json:"code"`
}

func TestAPIFail_Mapping(t *testing.T) {
	boom := errors.New("secret service said /home/me/.cache/x: permission denied")
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
		wantError  string
	}{
		{"rule", ruleError("name is required"), 400, "invalid", "name is required"},
		{
			"unreadable folder", fmt.Errorf("%w: %w", errUnreadable, boom), 400, "invalid",
			"Reef couldn't read that skill folder.",
		},
		{
			"keychain save", fmt.Errorf("save: %w: %w", mcpserver.ErrKeychain, boom), 500, "keychain",
			"Reef couldn't save that MCP server's secrets to the system keychain.",
		},
		{
			"keychain delete", fmt.Errorf("delete: %w: %w", mcpserver.ErrKeychainRemove, boom), 500, "keychain",
			"Reef couldn't remove that MCP server's secrets from the system keychain.",
		},
		{
			"secret store down", fmt.Errorf("read: %w: %w", store.ErrSecretStore, boom), 500, "keychain",
			"Reef couldn't read a secret from the system keychain.",
		},
		{"needs app", mcpserver.ErrNeedsApp, 409, "conflict", "This server needs a registered app."},
		{"anything else", boom, 500, "internal", _internalSentence},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := &api{log: slog.New(slog.NewTextHandler(io.Discard, nil))}
			rec := httptest.NewRecorder()
			a.fail(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/x", nil), tt.err)
			var got errorReply
			if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
				t.Fatal(err)
			}
			if rec.Code != tt.wantStatus || got.Code != tt.wantCode || got.Error != tt.wantError {
				t.Errorf("got %d %+v, want %d %s %q", rec.Code, got, tt.wantStatus, tt.wantCode, tt.wantError)
			}
		})
	}
}

func TestDesignedErrors(t *testing.T) {
	gitlab := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"invalid_client"}`))
	}))
	t.Cleanup(gitlab.Close)
	tests := []struct {
		name, method, path, body, host string
		wantStatus                     int
		wantCode, wantError            string
	}{
		{
			name: "repo the Bot cannot use", method: "POST", path: "/api/bots/bot/threads", body: `{"repo_id":"r"}`,
			wantStatus: 403, wantCode: "forbidden", wantError: "The Bot has no access to this repo.",
		},
		{
			name: "request from another host", method: "GET", path: "/api/bots", host: "evil.example",
			wantStatus: 403, wantCode: "forbidden", wantError: "That request didn't come from Reef.",
		},
		{
			name: "skill without a name", method: "POST", path: "/api/skills", body: `{}`,
			wantStatus: 400, wantCode: "invalid", wantError: "invalid skill: name is required",
		},
		{
			name: "skill folder Reef cannot read", method: "POST", path: "/api/skills/{skill}/reimport", body: `{}`,
			wantStatus: 400, wantCode: "invalid", wantError: "Reef couldn't read that skill folder.",
		},
		{
			name: "sign-in answered with an error", method: "POST", path: "/api/connections/gitlab/start",
			body:       `{"instance_url":"` + gitlab.URL + `"}`,
			wantStatus: 500, wantCode: "internal", wantError: _internalSentence,
		},
		{
			name: "sign-in not answered", method: "POST", path: "/api/connections/gitlab/start",
			body:       `{"instance_url":"http://127.0.0.1:1"}`,
			wantStatus: 502, wantCode: "unreachable", wantError: "Reef couldn't reach 127.0.0.1:1.",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			if err := os.MkdirAll(filepath.Join(home, ".claude", "skills", "gone"), 0o755); err != nil {
				t.Fatal(err)
			}
			h, st := newHandlerWith(t, nil)
			if _, err := st.DB().ExecContext(t.Context(),
				"INSERT INTO bots (id, name, persona, provider) VALUES ('bot', 'Ada', '', 'claude'); "+
					"INSERT INTO repos VALUES ('r', 'r', '/repo'); UPDATE bots SET repo_access = '[]'"); err != nil {
				t.Fatal(err)
			}
			sk, err := st.CreateSkill(t.Context(), store.Skill{
				Name: "gone", Source: filepath.Join(home, ".claude", "skills", "gone"),
			})
			if err != nil {
				t.Fatal(err)
			}
			path := strings.ReplaceAll(tt.path, "{skill}", sk.ID)
			req := httptest.NewRequestWithContext(t.Context(), tt.method, path, strings.NewReader(tt.body))
			req.Host = "127.0.0.1:4317"
			if tt.host != "" {
				req.Host = tt.host
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			var got errorReply
			if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
				t.Fatalf("body %s: %v", rec.Body, err)
			}
			if rec.Code != tt.wantStatus || got.Code != tt.wantCode || got.Error != tt.wantError {
				t.Errorf("got %d %+v, want %d %s %q", rec.Code, got, tt.wantStatus, tt.wantCode, tt.wantError)
			}
			if strings.Contains(rec.Body.String(), home) {
				t.Errorf("body %s names a path of the machine", rec.Body)
			}
		})
	}
}
