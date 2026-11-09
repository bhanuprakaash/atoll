package e2e

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

func TestKeychainFailure_Connections(t *testing.T) {
	requireSecretStore(t)
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
			m.env = []string{"ATOLL_SECRET_STORE_UNAVAILABLE=1", "ATOLL_GITHUB_URL=" + gh.URL}
			for _, key := range []string{"DBUS_SESSION_BUS_ADDRESS", "XDG_RUNTIME_DIR", "XDG_DATA_HOME"} {
				m.env = append(m.env, key+"="+os.Getenv(key))
			}
			s := m.serve(t)
			var start struct {
				ID string `json:"id"`
			}
			s.do(t, "POST", tc.path, tc.body, &start, http.StatusCreated)
			time.Sleep(1100 * time.Millisecond)
			var result struct {
				Error string `json:"error"`
			}
			keychainFailure(t, s, "/api/connections/"+start.ID+"/poll", nil, &result)
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
	requireSecretStore(t)
	m := newMachine(t)
	m.env = []string{"ATOLL_SECRET_STORE_UNAVAILABLE=1"}
	for _, key := range []string{"DBUS_SESSION_BUS_ADDRESS", "XDG_RUNTIME_DIR", "XDG_DATA_HOME"} {
		m.env = append(m.env, key+"="+os.Getenv(key))
	}
	s := m.serve(t)
	var result struct {
		Error string `json:"error"`
	}
	keychainFailure(t, s, "/api/mcp-servers", map[string]any{"name": "docs-local", "command": "echo", "env": map[string]string{"TOKEN": "secret"}}, &result)
	if !strings.Contains(strings.ToLower(result.Error), "keychain") {
		t.Errorf("error = %q, want a message naming the keychain", result.Error)
	}
}

func keychainFailure(t *testing.T, s *server, path string, body any, out any) {
	t.Helper()
	var data []byte
	if body != nil {
		var err error
		data, err = json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
	}
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, s.url+path, bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 500 {
		t.Fatalf("POST %s: status %d, want >= 500 (secret save did not fail)", path, resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		t.Fatal(err)
	}
}
