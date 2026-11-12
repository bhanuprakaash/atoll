package e2e

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Issue #149. Contract: designed-errors-contract.md.

type failure struct {
	Error string `json:"error"`
	Code  string `json:"code"`
}

// fails sends a request that must fail with status and reads the reply as {error, code}.
func (s *server) fails(t *testing.T, method, path string, body any, status int) failure {
	t.Helper()
	var f failure
	s.do(t, method, path, body, &f, status)
	return f
}

func wantFailure(t *testing.T, got failure, code, sentence string) {
	t.Helper()
	if got.Code != code || got.Error != sentence {
		t.Errorf("reply {error: %q, code: %q}, want {error: %q, code: %q}", got.Error, got.Code, sentence, code)
	}
}

func TestErrors_KnownCausesHaveCodeAndSentence(t *testing.T) {
	t.Parallel()
	_, s := start(t, "claude")
	closed := s.newThread(t, s.newBot(t, "Tern", "claude"))
	s.do(t, "POST", "/api/threads/"+closed.ID+"/close", nil, nil, http.StatusNoContent)
	s.do(t, "POST", "/api/mcp-servers", map[string]any{"name": "docs", "command": "echo"}, nil, http.StatusCreated)

	t.Run("not found", func(t *testing.T) {
		wantFailure(t, s.fails(t, "GET", "/api/bots/zzz", nil, http.StatusNotFound), "not_found", "That no longer exists.")
	})
	t.Run("not found, wrapped by the run package", func(t *testing.T) {
		got := s.fails(t, "POST", "/api/threads/zzz/messages", map[string]string{"text": "hi"}, http.StatusNotFound)
		wantFailure(t, got, "not_found", "That no longer exists.")
	})
	t.Run("unknown route", func(t *testing.T) {
		wantFailure(t, s.fails(t, "GET", "/api/zzz", nil, http.StatusNotFound), "not_found", "That no longer exists.")
	})
	t.Run("unknown method", func(t *testing.T) {
		wantFailure(t, s.fails(t, "DELETE", "/api/bots", nil, http.StatusMethodNotAllowed), "not_found", "That no longer exists.")
	})
	t.Run("closed thread", func(t *testing.T) {
		got := s.fails(t, "POST", "/api/threads/"+closed.ID+"/messages", map[string]string{"text": "hi"}, http.StatusConflict)
		wantFailure(t, got, "closed", "This thread is closed.")
	})
	t.Run("name taken", func(t *testing.T) {
		got := s.fails(t, "POST", "/api/mcp-servers", map[string]any{"name": "docs", "command": "echo"}, http.StatusConflict)
		wantFailure(t, got, "exists", "Something with that name already exists.")
	})
	t.Run("a rule that refuses keeps its own sentence", func(t *testing.T) {
		wantFailure(t, s.fails(t, "DELETE", "/api/bots/chief", nil, http.StatusConflict), "conflict", "Chief cannot be deleted")
	})
	t.Run("input rule", func(t *testing.T) {
		for name, tc := range map[string]struct {
			method, path string
			body         any
			status       int
		}{
			"bot without a name":     {"POST", "/api/bots", map[string]string{"provider": "claude"}, http.StatusBadRequest},
			"mcp server without how": {"POST", "/api/mcp-servers", map[string]string{"name": "x"}, http.StatusBadRequest},
			"relative worktree root": {"PUT", "/api/settings", map[string]string{"worktree_root": "rel"}, http.StatusBadRequest},
		} {
			got := s.fails(t, tc.method, tc.path, tc.body, tc.status)
			if got.Code != "invalid" || got.Error == "" || strings.Contains(got.Error, "invalid MCP server") {
				t.Errorf("%s: reply %+v, want code invalid and a plain sentence without the package's prefix", name, got)
			}
		}
	})
}

func TestErrors_UnreachableNamesTheHostNotTheChain(t *testing.T) {
	t.Parallel()
	_, s := start(t, "claude")
	got := s.fails(t, "POST", "/api/connections/gitlab/start", map[string]string{"instance_url": "http://127.0.0.1:1"}, http.StatusBadGateway)
	wantFailure(t, got, "unreachable", "Reef couldn't reach 127.0.0.1:1.")
}

func TestErrors_BusyThread(t *testing.T) {
	t.Parallel()
	_, s := start(t, "claude")
	th := s.newThread(t, s.newBot(t, "Busy", "claude"))
	events := s.events(t, th)
	run := s.send(t, th, _hangPrompt)
	until(t, events, run.ID, "tool_call")
	got := s.fails(t, "POST", "/api/threads/"+th.ID+"/close", nil, http.StatusConflict)
	wantFailure(t, got, "busy", "A run is going in this thread.")
}

// A cause Reef has no sentence for answers the generic one. The chain is the log's.
func TestErrors_UnknownCauseIsGenericAndTheChainIsLogged(t *testing.T) {
	t.Parallel()
	m := newMachine(t, "claude")
	const chain = "exit status 3"
	for _, picker := range []string{"zenity", "osascript"} {
		writeExec(t, filepath.Join(m.bin, picker), "#!/bin/sh\nexit 3\n")
	}
	s := m.serve(t)
	got := s.fails(t, "POST", "/api/repos/pick-folder", nil, http.StatusInternalServerError)
	wantFailure(t, got, "internal", "Something went wrong on our side.")
	if strings.Contains(got.Error, chain) {
		t.Errorf("error %q holds the chain", got.Error)
	}
	log := s.log.String()
	if !strings.Contains(log, "request failed") || !strings.Contains(log, "/api/repos/pick-folder") || !strings.Contains(log, chain) {
		t.Errorf("server log lacks the failed request with its chain:\n%s", log)
	}
}

// A device-flow sign-in that GitHub says expired is a sign-in problem, not a bad gateway.
func TestErrors_ExpiredSignInIsSignedOut(t *testing.T) {
	t.Parallel()
	gh := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/login/device/code" {
			_ = json.NewEncoder(w).Encode(map[string]any{"device_code": "d", "user_code": "ABCD-1234",
				"verification_uri": "https://github.com/login/device", "expires_in": 900, "interval": 1})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"error": "expired_token"})
	}))
	t.Cleanup(gh.Close)
	m := newMachine(t, "claude")
	m.env = []string{"ATOLL_GITHUB_URL=" + gh.URL}
	s := m.serve(t)
	var started struct {
		ID string `json:"id"`
	}
	s.do(t, "POST", "/api/connections/github/start", nil, &started, http.StatusCreated)
	time.Sleep(1100 * time.Millisecond)
	got := s.fails(t, "POST", "/api/connections/"+started.ID+"/poll", nil, http.StatusUnauthorized)
	wantFailure(t, got, "signed_out", "That sign-in expired or was refused.")
}

// A Bot on a provider that is not installed fails its run with provider_off and a plain
// sentence, not the provider's start error.
func TestErrors_ProviderOffFailsTheRunPlainly(t *testing.T) {
	t.Parallel()
	m := newMachine(t, "claude")
	m.signIn(t, "claude")
	s := m.serve(t)
	th := s.newThread(t, s.newBot(t, "Ada", "codex"))
	ev := s.events(t, th)
	done := s.finished(t, ev, s.send(t, th, _keepPrompt).ID)
	var run struct {
		Status    string `json:"status"`
		Error     string `json:"error"`
		ErrorKind string `json:"error_kind"`
	}
	s.do(t, "GET", "/api/runs/"+done.ID, nil, &run, http.StatusOK)
	if run.Status != "failed" || run.ErrorKind != "provider_off" || run.Error != "Codex isn't installed or isn't signed in." {
		t.Errorf("run %+v, want failed, error_kind provider_off, error %q", run, "Codex isn't installed or isn't signed in.")
	}
}
