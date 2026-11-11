package e2e

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const (
	_loginsPrompt = "Check which logins this run has."
	_gitPassword  = "fake-git-password-1"
)

var _tokenVars = []string{"GH_TOKEN", "GITHUB_TOKEN", "GH_ENTERPRISE_TOKEN", "GITHUB_ENTERPRISE_TOKEN"}

// loginProbe is what a CLI said in the run: its exit code and output.
type loginProbe struct {
	Exit   int    `json:"exit"`
	Output string `json:"output"`
}

// newLoginsMachine is a user's machine with a glab login, a gh login and a git credential helper
// of its own, and gh tokens in the env atoll starts with. The logins sit in ~/.config, or in
// $XDG_CONFIG_HOME when xdg is set.
func newLoginsMachine(t *testing.T, provider string, xdg bool) *machine {
	t.Helper()
	m := newMachine(t, provider, "glab", "gh")
	m.signIn(t, provider)
	config := filepath.Join(m.home, ".config")
	if xdg {
		config = filepath.Join(m.home, "xdg")
		m.env = append(m.env, "XDG_CONFIG_HOME="+config)
	}
	writeFile(t, filepath.Join(config, "glab-cli", "config.yml"), "hosts:\n  gitlab.com:\n    token: fake-glab-token-1\n    api_host: gitlab.com\n")
	writeFile(t, filepath.Join(config, "gh", "hosts.yml"), "github.com:\n    user: fake-gh-user\n    oauth_token: fake-gh-oauth-1\n    git_protocol: https\n")
	helper := filepath.Join(t.TempDir(), "git-credential-fake")
	writeFile(t, helper, "#!/bin/sh\n[ \"$1\" = get ] || exit 0\ngrep -q '^host=git.example$' || exit 0\necho username=fake-user\necho password="+_gitPassword+"\n")
	if err := os.Chmod(helper, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(m.home, ".gitconfig"), "[credential]\n\thelper = "+helper+"\n")
	for i, name := range _tokenVars {
		m.env = append(m.env, name+"=fake-gh-token-"+string(rune('1'+i)))
	}
	return m
}

// assertLoginsLive runs the three CLIs with the machine's own env, as a user would, and fails
// unless each one finds the login the test planted: without it a run that sees none proves nothing.
func assertLoginsLive(t *testing.T, m *machine) {
	t.Helper()
	bin := t.TempDir()
	installFake(t, "glab", bin)
	installFake(t, "gh", bin)
	env := []string{"HOME=" + m.home, "PATH=/usr/bin:/bin", "GIT_TERMINAL_PROMPT=0"}
	for _, kv := range m.env {
		if strings.HasPrefix(kv, "XDG_CONFIG_HOME=") {
			env = append(env, kv)
		}
	}
	for _, c := range []struct {
		cmd  *exec.Cmd
		want string
	}{
		{exec.CommandContext(t.Context(), filepath.Join(bin, "glab"), "auth", "status"), "Logged in"},
		{exec.CommandContext(t.Context(), filepath.Join(bin, "gh"), "auth", "status"), "Logged in"},
		{exec.CommandContext(t.Context(), "git", "credential", "fill"), "password=" + _gitPassword},
	} {
		c.cmd.Env = env
		c.cmd.Dir = t.TempDir()
		if c.cmd.Args[0] == "git" {
			c.cmd.Stdin = strings.NewReader("protocol=https\nhost=git.example\n\n")
		}
		if out, err := c.cmd.CombinedOutput(); err != nil || !strings.Contains(string(out), c.want) {
			t.Fatalf("%v on the user's machine: %v, output %q, want it to contain %q", c.cmd.Args, err, out, c.want)
		}
	}
}

// assertNoTokens fails when env holds any of the gh token variables, even empty.
func assertNoTokens(t *testing.T, who string, env map[string]string) {
	t.Helper()
	for _, name := range _tokenVars {
		if _, ok := env[name]; ok {
			t.Errorf("%s env has %s, want it absent", who, name)
		}
	}
}

func TestRun_UsesOnlyLoginsReefGives(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name, provider string
		xdg            bool
	}{
		{"claude", "claude", false},
		{"codex", "codex", false},
		{"claude-xdg", "claude", true},
	} {
		t.Run(c.name, func(t *testing.T) {
			m := newLoginsMachine(t, c.provider, c.xdg)
			assertLoginsLive(t, m)
			s := m.serve(t)
			b := s.newBot(t, "Login Bot", c.provider)
			th := s.newThread(t, b)
			ch := s.events(t, th)
			r := s.send(t, th, _loginsPrompt)
			done := s.finished(t, ch, r.ID)
			if done.Status != "done" {
				t.Fatalf("run: %+v", done)
			}
			var probes map[string]loginProbe
			if err := json.Unmarshal([]byte(done.Reply), &probes); err != nil {
				t.Fatalf("reply %q: %v", done.Reply, err)
			}
			for _, cli := range []string{"glab", "gh"} {
				p := probes[cli]
				if p.Exit == 0 || strings.Contains(p.Output, "Logged in") {
					t.Errorf("%s auth status in a run with no connection: exit %d, output %q; want no login", cli, p.Exit, p.Output)
				}
			}
			if p := probes["git"]; strings.Contains(p.Output, "password=") || strings.Contains(p.Output, _gitPassword) {
				t.Errorf("git credential fill in a run with no connection returned a password: %q", p.Output)
			}
			sessions := m.sessions(t, c.provider)
			if len(sessions) != 1 {
				t.Fatalf("%s ran %d times, want 1", c.provider, len(sessions))
			}
			assertNoTokens(t, c.provider, sessions[0].env)
			for _, cli := range []string{"glab", "gh"} {
				calls := m.calls(t, cli)
				if len(calls) == 0 {
					t.Fatalf("%s never ran in the run", cli)
				}
				for _, call := range calls {
					assertNoTokens(t, cli, call.env)
				}
			}
		})
	}
}
