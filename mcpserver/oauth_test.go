package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
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
			if tt.wantStatus != "" && !errors.Is(err, ErrNeedsSignIn) || tt.wantStatus == "" && err != nil {
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

func TestReplaceSignIn(t *testing.T) {
	tests := []struct {
		name     string
		in       Input
		wantKept bool
	}{
		{name: "same URL keeps the sign-in", in: Input{Name: "docs", URL: "https://a.example/mcp"}, wantKept: true},
		{name: "new URL drops it", in: Input{Name: "docs", URL: "https://b.example/mcp"}},
		{name: "local kind drops it", in: Input{Name: "docs", Command: "npx"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			keyring.MockInit()
			st, err := store.Open(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = st.Close() }()
			ctx := context.Background()
			s, err := st.CreateMCPServer(ctx, store.MCPServer{Name: "docs", Kind: store.MCPRemote, URL: "https://a.example/mcp"})
			if err != nil {
				t.Fatal(err)
			}
			if err := errors.Join(saveTokens(s.ID, Tokens{AccessToken: "a"}), st.SetMCPSignIn(ctx, s.ID, store.MCPConnected)); err != nil {
				t.Fatal(err)
			}
			offline := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				return nil, errors.New("offline")
			})}

			if _, err := Replace(ctx, st, offline, s.ID, tt.in); err != nil {
				t.Fatal(err)
			}
			_, tokenErr := keyring.Get("atoll", tokensUser(s.ID))
			signIns, err := st.MCPSignIns(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if kept := tokenErr == nil && signIns[s.ID] == store.MCPConnected; kept != tt.wantKept {
				t.Errorf("sign-in kept = %t (token error %v, status %q), want %t", kept, tokenErr, signIns[s.ID], tt.wantKept)
			}
		})
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestTokenRefreshesOnceAtATime(t *testing.T) {
	keyring.MockInit()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	var mu sync.Mutex
	refreshes, used := 0, map[string]bool{}
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
			_ = r.ParseForm()
			mu.Lock()
			defer mu.Unlock()
			refreshes++
			if used[r.PostForm.Get("refresh_token")] {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
				return
			}
			used[r.PostForm.Get("refresh_token")] = true
			time.Sleep(50 * time.Millisecond)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token": "new", "refresh_token": "r2", "expires_in": 3600,
			})
		}
	}))
	defer srv.Close()
	s := store.MCPServer{ID: "s1", Name: "docs", URL: srv.URL + "/mcp", Status: store.MCPConnected}
	give := Tokens{ClientID: "c", AccessToken: "old", RefreshToken: "r1", ExpiresAt: time.Now().Add(time.Minute)}
	if err := saveTokens(s.ID, give); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	got, errs := make([]string, 2), make([]error, 2)
	for i := range got {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got[i], errs[i] = Token(context.Background(), srv.Client(), st, s)
		}()
	}
	wg.Wait()

	for i := range got {
		if errs[i] != nil || got[i] != "new" {
			t.Errorf("Token #%d = %q, %v, want \"new\"", i, got[i], errs[i])
		}
	}
	if refreshes != 1 {
		t.Errorf("refresh requests = %d, want 1", refreshes)
	}
	signIns, err := st.MCPSignIns(context.Background())
	if err != nil || signIns[s.ID] == store.MCPNeedsSignIn {
		t.Errorf("status = %q (%v), want not needs_sign_in", signIns[s.ID], err)
	}
}
