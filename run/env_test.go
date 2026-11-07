package run

import (
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/zalando/go-keyring"

	"atoll/connection"
	"atoll/store"
)

// githubBot is a Bot with a connected GitHub connection whose GET /user succeeds, and the number
// of GET /user requests made so far.
func githubBot(t *testing.T) (*Manager, store.Bot, *atomic.Int32) {
	t.Helper()
	keyring.MockInit()
	var users atomic.Int32
	gh := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/user" {
			users.Add(1)
		}
	}))
	t.Cleanup(gh.Close)
	t.Setenv("ATOLL_GITHUB_URL", gh.URL)
	e := newEnv(t, &fakeSession{})
	c, err := e.st.CreateConnection(t.Context(), store.KindGitHub, connection.GitHubInstance)
	if err != nil {
		t.Fatal(err)
	}
	if err := connection.SaveGitHubToken(c.ID, "fake-gho"); err != nil {
		t.Fatal(err)
	}
	if err := e.st.SetConnectionStatus(t.Context(), c.ID, store.ConnectionConnected); err != nil {
		t.Fatal(err)
	}
	bot, err := e.st.CreateBot(t.Context(), store.Bot{Name: "Gh", Provider: "claude", Connections: []string{c.ID}})
	if err != nil {
		t.Fatal(err)
	}
	return e.m, bot, &users
}

func TestRunEnv_GitHubSSHRemotes(t *testing.T) {
	m, bot, _ := githubBot(t)
	env, _, err := m.runEnv(t.Context(), bot, true)
	if err != nil {
		t.Fatal(err)
	}
	host := strings.TrimPrefix(connection.GitHubURL(), "http://")
	hostname, _, _ := strings.Cut(host, ":")
	tests := []struct{ give, want string }{
		{"git@" + hostname + ":o/r.git", connection.GitHubURL() + "/o/r.git"},
		{"ssh://git@" + hostname + "/o/r.git", connection.GitHubURL() + "/o/r.git"},
		{"git@elsewhere.test:o/r.git", "git@elsewhere.test:o/r.git"},
	}
	for _, tt := range tests {
		t.Run(tt.give, func(t *testing.T) {
			cmd := exec.CommandContext(t.Context(), "git", "ls-remote", "--get-url", tt.give)
			cmd.Env = append([]string{"HOME=" + t.TempDir(), "GIT_CONFIG_NOSYSTEM=1"}, env...)
			out, err := cmd.Output()
			if err != nil {
				t.Fatal(err)
			}
			if got := strings.TrimSpace(string(out)); got != tt.want {
				t.Errorf("git resolves %s to %s, want %s", tt.give, got, tt.want)
			}
		})
	}
}

func TestRunEnv_GitHubCheck(t *testing.T) {
	m, bot, users := githubBot(t)
	tests := []struct {
		name   string
		github bool
		want   int32
	}{
		{"run", true, 1},
		{"MR watcher", false, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			users.Store(0)
			if _, _, err := m.runEnv(t.Context(), bot, tt.github); err != nil {
				t.Fatal(err)
			}
			if got := users.Load(); got != tt.want {
				t.Errorf("GET /user requests = %d, want %d", got, tt.want)
			}
		})
	}
}
