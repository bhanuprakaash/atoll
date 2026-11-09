package e2e

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestKeychainFailure_Connections(t *testing.T) {
	gh := newFakeGitHub(t)
	gh.set(func() { gh.approved = true })
	gl := newGitLabOAuth(t, 7200)
	gl.set(func() { gl.allowed = true })
	for _, tc := range []struct {
		name, path, prefix string
		body               map[string]string
	}{
		{"GitHub", "/api/connections/github/start", "Reef couldn't save the GitHub sign-in to the system keychain: ", nil},
		{"GitLab", "/api/connections/gitlab/start", "Reef couldn't save the GitLab sign-in to the system keychain: ", map[string]string{"instance_url": gl.URL}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := newMachine(t)
			m.env = []string{"DBUS_SESSION_BUS_ADDRESS=unix:path=/nonexistent/atoll-e2e", "ATOLL_GITHUB_URL=" + gh.URL}
			s := m.serve(t)
			var start struct {
				ID string `json:"id"`
			}
			s.do(t, "POST", tc.path, tc.body, &start, http.StatusCreated)
			time.Sleep(1100 * time.Millisecond)
			var result struct {
				Error string `json:"error"`
			}
			s.do(t, "POST", "/api/connections/"+start.ID+"/poll", nil, &result, http.StatusInternalServerError)
			if !strings.HasPrefix(result.Error, tc.prefix) || len(result.Error) <= len(tc.prefix) {
				t.Errorf("error = %q, want %q followed by the cause", result.Error, tc.prefix)
			}
			var connection connectionInfo
			s.do(t, "GET", "/api/connections/"+start.ID, nil, &connection, http.StatusOK)
			if connection.Status != "needs_sign_in" {
				t.Errorf("status = %q, want needs_sign_in", connection.Status)
			}
		})
	}
}

func TestKeychainFailure_MCPSecret(t *testing.T) {
	m := newMachine(t)
	m.env = []string{"DBUS_SESSION_BUS_ADDRESS=unix:path=/nonexistent/atoll-e2e"}
	s := m.serve(t)
	var result struct {
		Error string `json:"error"`
	}
	s.do(t, "POST", "/api/mcp-servers", map[string]any{"name": "docs-local", "command": "echo", "env": map[string]string{"TOKEN": "secret"}}, &result, http.StatusInternalServerError)
	if !strings.Contains(strings.ToLower(result.Error), "keychain") {
		t.Errorf("error = %q, want a message naming the keychain", result.Error)
	}
}
