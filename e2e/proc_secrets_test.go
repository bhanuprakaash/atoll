package e2e

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Contract: proc-secrets-contract.md.

const (
	_holdPrompt = "Hold the secrets watch."
	_scanPrompt = "Scan the process list for secrets of other runs."
)

// procfs reports whether the test can read other processes' env and command line. Only Linux
// can, through /proc; the contract says what macOS leaves open.
func procfs(t *testing.T) bool {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Logf("%s has no /proc: the same-user read it checks is not run here", runtime.GOOS)
		return false
	}
	return true
}

// environ is the NAME=value lines of the process's environment as the kernel holds them.
func environ(t *testing.T, pid int) string {
	t.Helper()
	b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/environ")
	if err != nil {
		t.Fatalf("read the env of pid %d: %v", pid, err)
	}
	return string(b)
}

func hasVar(env, name string) bool {
	for _, kv := range strings.Split(env, "\x00") {
		if strings.HasPrefix(kv, name+"=") {
			return true
		}
	}
	return false
}

type procFinding struct {
	PID  int    `json:"pid"`
	Kind string `json:"kind"`
	Name string `json:"name"`
}

// scan runs the Bot's shell scan of every readable /proc/*/environ and cmdline for the needles,
// in a new thread, and returns what it found. The needles go in a file so no process of the scan
// holds one.
func scan(t *testing.T, s *server, b bot, needles ...string) []procFinding {
	t.Helper()
	file := filepath.Join(t.TempDir(), "needles")
	writeFile(t, file, strings.Join(needles, "\n")+"\n")
	th := s.newThread(t, b)
	ch := s.events(t, th)
	done := s.finished(t, ch, s.send(t, th, _scanPrompt+" needles="+file).ID)
	if done.Status != "done" {
		t.Fatalf("scan run: %+v", done)
	}
	var found []procFinding
	if err := json.Unmarshal([]byte(done.Reply), &found); err != nil {
		t.Fatalf("scan reply %q: %v", done.Reply, err)
	}
	return found
}

// procFixture is a GitHub-connected Bot with a remote MCP server (the holder) and a Bot with
// neither (the scanner), on one server.
type procFixture struct {
	*githubFixture
	token          string // the holder's GitHub token
	holder, viewer bot
}

func newProcFixture(t *testing.T, p string) *procFixture {
	t.Helper()
	f := &procFixture{githubFixture: newGitHubFixture(t, p)}
	conn := f.connect(t)
	f.token = f.gh.currentToken()
	var remote mcpServer
	f.s.do(t, "POST", "/api/mcp-servers", map[string]any{
		"name": "docs-remote", "url": newHTTPMCP(t).url(), "headers": map[string]string{_mcpHeaderName: _mcpHeaderSecret},
	}, &remote, http.StatusCreated)
	t.Cleanup(func() { clearSecretEntry("mcp:" + remote.ID + ":header:" + _mcpHeaderName) })
	f.s.do(t, "POST", "/api/bots", map[string]any{
		"name": "Holder", "persona": "Works on GitHub and reads docs.", "provider": p,
		"connections": []string{conn}, "mcp_servers": []string{remote.ID},
	}, &f.holder, http.StatusCreated)
	f.viewer = f.s.newBot(t, "Scanner", p)
	return f
}

// held is a run of the holder that is live and waits.
type held struct {
	pid      int
	runToken string
	release  string
	ch       <-chan event
	runID    string
}

// hold starts the holder's run and returns once it has used its servers and waits.
func (f *procFixture) hold(t *testing.T, p string) *held {
	t.Helper()
	th := f.s.newThread(t, f.holder)
	h := &held{ch: f.s.events(t, th)}
	h.runID = f.s.send(t, th, _holdPrompt).ID
	deadline := time.Now().Add(30 * time.Second)
	for h.pid == 0 {
		if time.Now().After(deadline) {
			t.Fatalf("the holder's %s did not reach the wait in 30s", p)
		}
		files, _ := filepath.Glob(filepath.Join(f.m.bin, p+".procs.[0-9]*"))
		for _, file := range files {
			if strings.HasSuffix(file, ".release") {
				continue
			}
			b, err := os.ReadFile(file)
			var url struct {
				AtollURL string `json:"atoll_url"`
			}
			if err != nil || json.Unmarshal(b, &url) != nil {
				continue
			}
			h.pid, _ = strconv.Atoi(strings.TrimPrefix(filepath.Base(file), p+".procs."))
			h.release = file + ".release"
			h.runToken = url.AtollURL[strings.LastIndex(url.AtollURL, "/")+1:]
		}
		time.Sleep(50 * time.Millisecond)
	}
	if len(h.runToken) < 16 || strings.Contains(h.runToken, ":") {
		t.Fatalf("the holder's run token %q does not look like one", h.runToken)
	}
	return h
}

func TestProcSecrets_OtherRunReadsNoSecretOfAHeldRun(t *testing.T) {
	if !procfs(t) {
		return
	}
	for _, p := range _providers {
		t.Run(p, func(t *testing.T) {
			f := newProcFixture(t, p)
			h := f.hold(t, p)
			found := scan(t, f.s, f.viewer, h.runToken, f.token, _mcpHeaderSecret)

			// The scan must be able to read the holder's env at all, or it proves nothing. The
			// holder's own login tool env (GH_TOKEN) is what its shell tools run with; the
			// contract lists it as the one secret that stays.
			var sawToolToken bool
			for _, x := range found {
				switch {
				case x.PID == h.pid && x.Kind == "environ" && x.Name == "GH_TOKEN":
					sawToolToken = true
				default:
					t.Errorf("a shell of another run found a secret of the holder in %s of pid %d (%s), variable %q", x.Kind, x.PID, procName(x.PID), x.Name)
				}
			}
			if !sawToolToken {
				t.Errorf("the scan did not find the holder's GH_TOKEN in pid %d: it cannot read other processes' env, so it proves nothing; found %+v", h.pid, found)
			}

			writeFile(t, h.release, "")
			done := f.s.finished(t, h.ch, h.runID)
			if done.Status != "done" {
				t.Fatalf("holder run: %+v", done)
			}
			var reply struct {
				GitHub githubProbe `json:"github"`
				Docs   string      `json:"docs"`
			}
			if err := json.Unmarshal([]byte(done.Reply), &reply); err != nil {
				t.Fatalf("holder reply %q: %v", done.Reply, err)
			}
			if reply.GitHub.GH.Exit != 0 || !strings.Contains(reply.GitHub.GH.Output, "reef-fixture") {
				t.Errorf("gh api user in the holder's run: %+v, want it to reach GitHub", reply.GitHub.GH)
			}
			if !reply.GitHub.MCP.Connected || !strings.Contains(reply.GitHub.MCP.Text, "reef-fixture") {
				t.Errorf("GitHub MCP in the holder's run: %+v, want connected and answering", reply.GitHub.MCP)
			}
			if reply.Docs != "http-answer-42" {
				t.Errorf("docs-remote answered %q, want http-answer-42", reply.Docs)
			}
			for _, r := range f.gh.mcpRequests() {
				if r.Auth != "Bearer "+f.token {
					t.Errorf("GitHub MCP request carried %q, want the holder's connection token", r.Auth)
				}
			}
		})
	}
}

// procName is the command line of pid, for a message; empty when it has gone.
func procName(pid int) string {
	b, _ := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/cmdline")
	argv := strings.Split(strings.TrimRight(string(b), "\x00"), "\x00")
	return fmt.Sprintf("%q", argv[:min(len(argv), 2)])
}

// _reefLogins are the variables Reef drops from its own env at start: the login tokens and the
// provider families provider.Env drops from a Bot. Reef keeps what it needs itself: the three
// secret-store variables and SSH_AUTH_SOCK (its own git fetches over SSH).
var _reefLogins = []string{
	"GH_TOKEN", "GITHUB_TOKEN", "GH_ENTERPRISE_TOKEN", "GITHUB_ENTERPRISE_TOKEN",
	"GITLAB_TOKEN", "GITLAB_ACCESS_TOKEN", "OAUTH_TOKEN",
	"ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN", "CLAUDE_CODE_OAUTH_TOKEN", "OPENAI_API_KEY", "CODEX_API_KEY",
}

func TestProcSecrets_ReefEnvironHoldsNoLoginToken(t *testing.T) {
	if !procfs(t) {
		return
	}
	m := newMachine(t, "claude")
	m.signIn(t, "claude")
	m.env = append(m.env, _sentinelEnv...)
	m.env = append(m.env, "SSH_AUTH_SOCK=/nonexistent/agent")
	for _, name := range _reefLogins {
		m.env = append(m.env, name+"=reef-login-"+name)
	}
	s := m.serve(t)
	pid := s.cmd.Process.Pid

	env := environ(t, pid)
	for _, name := range _reefLogins {
		if hasVar(env, name) {
			t.Errorf("Reef's /proc/%d/environ holds %s, want it dropped", pid, name)
		}
	}
	if strings.Contains(env, "reef-login-") {
		t.Errorf("Reef's /proc/%d/environ still holds a login value", pid)
	}
	for _, name := range append([]string{"HOME", "PATH", "ATOLL_DATA_DIR", "ATOLL_PROVIDER_PATH", "SSH_AUTH_SOCK"}, _secretServiceVars...) {
		if !hasVar(env, name) {
			t.Errorf("Reef's /proc/%d/environ lost %s, which Reef needs", pid, name)
		}
	}

	if found := scan(t, s, s.newBot(t, "Scanner", "claude"), "reef-login-"); len(found) > 0 {
		t.Errorf("a Bot's shell found login values in %+v, want none", found)
	}
}

func TestProcSecrets_ReefKeepsItsAPITokenOutOfEnviron(t *testing.T) {
	if !procfs(t) {
		return
	}
	const token = "reef-api-token-0123456789abcdef"
	m := newMachine(t, "claude")
	m.env = append(m.env, "ATOLL_TOKEN="+token)
	s := m.serve(t)
	pid := s.cmd.Process.Pid

	get := func(cookie bool) int {
		req, err := http.NewRequestWithContext(t.Context(), "GET", s.url+"/api/bots", nil)
		if err != nil {
			t.Fatal(err)
		}
		if cookie {
			req.AddCookie(&http.Cookie{Name: "atoll_token", Value: token})
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		return resp.StatusCode
	}
	if got := get(true); got != http.StatusOK {
		t.Errorf("GET /api/bots with the token: %d, want 200", got)
	}
	if got := get(false); got != http.StatusUnauthorized {
		t.Errorf("GET /api/bots without the token: %d, want 401", got)
	}
	env := environ(t, pid)
	if hasVar(env, "ATOLL_TOKEN") || strings.Contains(env, token) {
		t.Errorf("Reef's /proc/%d/environ holds the API token, which lets a Bot call the whole API", pid)
	}
}
