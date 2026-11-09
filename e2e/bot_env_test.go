package e2e

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Contract: bot-env-contract.md.

var _secretServiceVars = []string{"DBUS_SESSION_BUS_ADDRESS", "XDG_RUNTIME_DIR", "DISPLAY"}

// _sentinelEnv is a server env that reaches a Linux secret store by no real path.
var _sentinelEnv = []string{
	"DBUS_SESSION_BUS_ADDRESS=unix:path=/nonexistent/reef-bus",
	"XDG_RUNTIME_DIR=/nonexistent/reef-run",
	"DISPLAY=:99",
}

// secretServiceEnv is the server env of a test that stores a connection: the private Secret
// Service session when there is one, a sentinel for each variable when there is none (macOS), and
// a DISPLAY.
func secretServiceEnv() []string {
	var env []string
	for _, kv := range _sentinelEnv {
		key, _, _ := strings.Cut(kv, "=")
		if v := os.Getenv(key); v != "" {
			kv = key + "=" + v
		}
		env = append(env, kv)
	}
	if v := os.Getenv("XDG_DATA_HOME"); v != "" {
		env = append(env, "XDG_DATA_HOME="+v)
	}
	return env
}

// assertNoSecretService fails when any process the machine ran of provider, or of gh and glab (the
// tools a Bot's shell runs), holds a secret-store variable, even empty. It fails too when fewer
// than sessions runs of provider held a session: a run that never happened proves nothing.
func assertNoSecretService(t *testing.T, m *machine, provider string, sessions int) {
	t.Helper()
	if got := len(m.sessions(t, provider)); got < sessions {
		t.Fatalf("%s held %d sessions, want at least %d", provider, got, sessions)
	}
	for _, who := range []string{provider, "gh", "glab"} {
		if _, err := os.Stat(filepath.Join(m.bin, who+".log")); err != nil {
			continue
		}
		for _, c := range m.runs(t, who) {
			if len(c.argv) == 1 && c.argv[0] == "--version" {
				continue // the installed-version probe runs no Bot and takes the server's env as it is
			}
			for _, name := range _secretServiceVars {
				if v, ok := c.env[name]; ok {
					t.Errorf("%s %q (pid %d) env has %s=%q, want it absent", who, c.argv[:min(len(c.argv), 2)], c.pid, name, v)
				}
			}
		}
	}
}

// botIn makes a Bot of the provider in the mode.
func (s *server) botIn(t *testing.T, provider, mode string) bot {
	t.Helper()
	var b bot
	s.do(t, "POST", "/api/bots", map[string]any{"name": "Env " + mode, "persona": "x", "provider": provider, "mode": mode, "repo_access": "all"}, &b, http.StatusCreated)
	return b
}

func TestBotEnv_NoSecretServiceInAnyMode(t *testing.T) {
	for _, p := range _providers {
		for _, mode := range _modes {
			t.Run(p+"/"+mode, func(t *testing.T) {
				m := newMachine(t, p)
				m.signIn(t, p)
				xdg := map[string]string{
					"XDG_DATA_HOME":   t.TempDir(),
					"XDG_CONFIG_HOME": t.TempDir(),
					"XDG_CACHE_HOME":  t.TempDir(),
				}
				m.env = append(m.env, _sentinelEnv...)
				for k, v := range xdg {
					m.env = append(m.env, k+"="+v)
				}
				s := m.serve(t)
				th := s.newThread(t, s.botIn(t, p, mode))
				ev := s.events(t, th)
				for _, prompt := range []string{_keepPrompt, _askPrompt} {
					if r := s.finished(t, ev, s.send(t, th, prompt).ID); r.Status != "done" {
						t.Fatalf("%s run %q: %+v", mode, prompt, r)
					}
				}
				assertNoSecretService(t, m, p, 2)
				for _, c := range m.sessions(t, p) {
					for k, want := range xdg {
						if got := c.env[k]; got != want {
							t.Errorf("%s env %s=%q, want %q as the server had it", p, k, got, want)
						}
					}
				}
			})
		}
	}
}

func TestBotEnv_NoSecretServiceOnARepoThread(t *testing.T) {
	for _, p := range _providers {
		for _, mode := range _modes {
			t.Run(p+"/"+mode, func(t *testing.T) {
				f := newRepoFixture(t, p)
				th := f.thread(t, f.s.botIn(t, p, mode))
				ev := f.s.events(t, th)
				for _, prompt := range []string{_keepPrompt, _askPrompt} {
					if r := f.s.finished(t, ev, f.s.send(t, th, prompt).ID); r.Status != "done" {
						t.Fatalf("%s run %q: %+v", mode, prompt, r)
					}
				}
				assertNoSecretService(t, f.m, p, 2)
			})
		}
	}
}

func TestBotEnv_NoSecretServiceForChiefAndHelpers(t *testing.T) {
	for _, p := range _providers {
		t.Run(p, func(t *testing.T) {
			m := newMachine(t, p)
			m.signIn(t, p)
			m.env = append(m.env, _sentinelEnv...)
			s := m.serve(t)
			chiefCall(t, s, "list_bots", map[string]any{})

			boss, helper := s.newBot(t, "Boss", p), s.newBot(t, "Tester", p)
			setDelegates(t, s, boss, helper)
			th := s.newThread(t, boss)
			h := handedOff(t, useAtoll(t, s, th, s.events(t, th), delegateCall("Tester", _keepPrompt, ""))[0])
			if r := settled(t, s, h.ThreadID); r.Status != "done" {
				t.Fatalf("helper run: %+v", r)
			}
			assertNoSecretService(t, m, p, 3)
		})
	}
}

func TestBotEnv_ToolsRunWithoutSecretService(t *testing.T) {
	for _, p := range _providers {
		t.Run(p, func(t *testing.T) {
			m := newLoginsMachine(t, p, false)
			m.env = append(m.env, _sentinelEnv...)
			s := m.serve(t)
			th := s.newThread(t, s.newBot(t, "Login Bot", p))
			if r := s.finished(t, s.events(t, th), s.send(t, th, _loginsPrompt).ID); r.Status != "done" {
				t.Fatalf("run: %+v", r)
			}
			if len(m.calls(t, "gh")) == 0 || len(m.calls(t, "glab")) == 0 {
				t.Fatal("gh and glab never ran in the run")
			}
			assertNoSecretService(t, m, p, 1)
		})
	}
}

func TestBotEnv_ReefStillReadsItsGitHubToken(t *testing.T) {
	for _, p := range _providers {
		t.Run(p, func(t *testing.T) {
			f := newGitHubFixture(t, p)
			b := f.bot(t, p, []string{f.connect(t)})
			token := f.gh.currentToken()
			done, probe := f.probe(t, b)
			if done.Status != "done" || probe.GH.Exit != 0 {
				t.Fatalf("run %+v, gh api user %+v; want done and exit 0", done, probe.GH)
			}
			sessions := f.m.sessions(t, p)
			if len(sessions) != 1 {
				t.Fatalf("%s ran %d times, want 1", p, len(sessions))
			}
			assertGitHubEnv(t, p, sessions[0].env, token)
			assertNoSecretService(t, f.m, p, 1)
		})
	}
}

func TestBotEnv_ReefStillReadsItsGitLabToken(t *testing.T) {
	for _, p := range _providers {
		t.Run(p, func(t *testing.T) {
			f := newRepoFixture(t, p)
			connection, token := f.connect(t)
			f.work(t, f.bot(t, p, "all", []string{connection}), _fixRepoPrompt, "fix/seeded-bug")
			sessions := f.m.sessions(t, p)
			if len(sessions) != 1 {
				t.Fatalf("%s ran %d times, want 1", p, len(sessions))
			}
			assertGitLabEnv(t, p, sessions[0].env, token)
			creates := 0
			for _, c := range f.m.calls(t, "glab") {
				if len(c.argv) > 1 && c.argv[0] == "mr" && c.argv[1] == "create" {
					creates++
					assertGitLabEnv(t, "glab create", c.env, token)
				}
			}
			if creates != 1 {
				t.Errorf("glab mr create ran %d times, want 1", creates)
			}
			assertNoSecretService(t, f.m, p, 1)
		})
	}
}
