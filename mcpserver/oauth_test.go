package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"atoll/store"

	"github.com/zalando/go-keyring"
)

func TestToken(t *testing.T) {
	soon := time.Now().Add(5 * time.Minute)
	tests := []struct {
		name        string
		give        Tokens
		reply       map[string]any
		want        string
		wantStatus  string
		wantRefresh string
		wantCalls   int
		wantFail    bool
	}{
		{
			name: "no expiry is never refreshed",
			give: Tokens{ClientID: "c", AccessToken: "old", RefreshToken: "r"},
			want: "old", wantRefresh: "r",
		},
		{
			name: "no refresh token near expiry needs sign-in",
			give: Tokens{ClientID: "c", AccessToken: "old", ExpiresAt: soon},
			want: "", wantStatus: store.MCPNeedsSignIn,
		},
		{
			name:  "a reply without a refresh token keeps the old one",
			give:  Tokens{ClientID: "c", AccessToken: "old", RefreshToken: "r", ExpiresAt: soon},
			reply: map[string]any{"access_token": "new", "expires_in": 3600},
			want:  "new", wantRefresh: "r", wantCalls: 1,
		},
		{
			name:  "a revoked Slack grant needs sign-in",
			give:  Tokens{ClientID: "c", AccessToken: "old", RefreshToken: "r", ExpiresAt: soon},
			reply: map[string]any{"ok": false, "error": "token_revoked"},
			want:  "", wantStatus: store.MCPNeedsSignIn, wantCalls: 1,
		},
		{
			name:  "an expired Slack grant needs sign-in",
			give:  Tokens{ClientID: "c", AccessToken: "old", RefreshToken: "r", ExpiresAt: soon},
			reply: map[string]any{"ok": false, "error": "token_expired"},
			want:  "", wantStatus: store.MCPNeedsSignIn, wantCalls: 1,
		},
		{
			name:  "another Slack error is a plain failure",
			give:  Tokens{ClientID: "c", AccessToken: "old", RefreshToken: "r", ExpiresAt: soon},
			reply: map[string]any{"ok": false, "error": "ratelimited"},
			want:  "", wantRefresh: "r", wantCalls: 1, wantFail: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			keyring.MockInit()
			st, err := store.Open(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = st.Close() }()
			calls := 0
			var srv *httptest.Server
			srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/mcp":
					w.Header().Set("WWW-Authenticate", `Bearer resource_metadata="`+srv.URL+`/resource"`)
					w.WriteHeader(http.StatusUnauthorized)
				case "/resource":
					_ = json.NewEncoder(w).Encode(map[string]any{"authorization_servers": []string{srv.URL}})
				case "/.well-known/oauth-authorization-server":
					_ = json.NewEncoder(w).Encode(map[string]any{"token_endpoint": srv.URL + "/token"})
				case "/token":
					calls++
					_ = json.NewEncoder(w).Encode(tt.reply)
				}
			}))
			defer srv.Close()
			s := store.MCPServer{ID: "s1", Name: "docs", URL: srv.URL + "/mcp", Status: store.MCPConnected}
			if err := saveTokens(s.ID, tt.give); err != nil {
				t.Fatal(err)
			}

			got, err := Token(context.Background(), srv.Client(), st, s)
			if tt.wantStatus != "" && !errors.Is(err, ErrNeedsSignIn) || tt.wantStatus == "" && (err != nil) != tt.wantFail {
				t.Fatalf("Token error = %v, want needs sign-in: %t", err, tt.wantStatus != "")
			}
			if got != tt.want || calls != tt.wantCalls {
				t.Errorf("Token = %q with %d refresh requests, want %q and %d", got, calls, tt.want, tt.wantCalls)
			}
			signIns, err := st.MCPSignIns(context.Background())
			if err != nil || signIns[s.ID] != tt.wantStatus {
				t.Errorf("status = %q (%v), want %q", signIns[s.ID], err, tt.wantStatus)
			}
			if tt.wantRefresh != "" {
				var rec Tokens
				value, _ := keyring.Get("atoll", tokensUser(s.ID))
				if err := json.Unmarshal([]byte(value), &rec); err != nil || rec.RefreshToken != tt.wantRefresh {
					t.Errorf("stored refresh token = %q (%v), want %q", rec.RefreshToken, err, tt.wantRefresh)
				}
			}
		})
	}
}
