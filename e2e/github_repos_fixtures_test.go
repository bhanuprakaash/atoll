package e2e

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
)

const (
	_githubFixPrompt = "Fix the seeded bug and open a PR."
	_githubPRTitle   = "fix: correct seeded answer"
)

// githubPull is a pull request the fake GitHub accepted.
type githubPull struct{ Owner, Repo, Head, Base, Title, Body string }

func (g *fakeGitHub) pullRequests() []githubPull {
	g.mu.Lock()
	defer g.mu.Unlock()
	return slices.Clone(g.pulls)
}

// createPull is POST /repos/{owner}/{repo}/pulls as github.com has it: a valid token and a head and
// a title, else 401 or 422. It answers 201 with the pull request's number and html_url, which is
// <fake GitHub URL>/<owner>/<repo>/pull/<number>, numbered from 1.
func (g *fakeGitHub) createPull(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if !g.authorised(r.Header.Get("Authorization")) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = fmt.Fprint(w, `{"message":"Bad credentials","status":"401"}`)
		return
	}
	var body struct {
		Title, Body, Head, Base string
		Draft                   bool
	}
	if json.NewDecoder(r.Body).Decode(&body) != nil || body.Head == "" || body.Title == "" {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = fmt.Fprint(w, `{"message":"Validation Failed"}`)
		return
	}
	in := githubPull{r.PathValue("owner"), r.PathValue("repo"), body.Head, body.Base, body.Title, body.Body}
	g.mu.Lock()
	g.pulls = append(g.pulls, in)
	n := len(g.pulls)
	if g.watch.drafts == nil {
		g.watch.drafts = map[int]bool{}
	}
	g.watch.drafts[n] = body.Draft
	g.watch.calls = append(g.watch.calls, fmt.Sprintf("create #%d base=%s draft=%v", n, in.Base, body.Draft))
	g.mu.Unlock()
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]any{"number": n, "state": "open", "title": in.Title,
		"html_url": fmt.Sprintf("%s/%s/%s/pull/%d", g.URL, in.Owner, in.Repo, n)})
}

// githubRepo is a registered repo as the API has it.
type githubRepo struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Path string `json:"path"`
	Host string `json:"host"`
}

// githubRepoFixture is a githubFixture with a worktree root and a short MR poll interval.
type githubRepoFixture struct {
	*githubFixture
	root string
}

func newGitHubRepoFixture(t *testing.T, provider string) *githubRepoFixture {
	t.Helper()
	requireSecretStore(t)
	m := newLoginsMachine(t, provider, false)
	for _, key := range []string{"DBUS_SESSION_BUS_ADDRESS", "XDG_RUNTIME_DIR", "XDG_DATA_HOME"} {
		if v := os.Getenv(key); v != "" {
			m.env = append(m.env, key+"="+v)
		}
	}
	gh := newFakeGitHub(t)
	m.env = append(m.env, "ATOLL_GITHUB_URL="+gh.URL, "ATOLL_GITHUB_MCP_URL="+gh.URL+"/mcp/", "ATOLL_REPO_POLL_INTERVAL=100ms")
	writeFile(t, filepath.Join(m.bin, "github-url"), gh.URL)
	f := &githubRepoFixture{githubFixture: &githubFixture{m: m, s: m.serve(t), gh: gh}, root: t.TempDir()}
	f.s.do(t, "PUT", "/api/settings", map[string]string{"worktree_root": f.root}, nil, 204)
	return f
}

// seedRepo makes a local repo with answer.txt "41" on main, and the remote origin when it is not empty.
func seedRepo(t *testing.T, origin string) string {
	t.Helper()
	path := t.TempDir()
	gitIn(t, path, "init", "-b", "main")
	writeFile(t, filepath.Join(path, "answer.txt"), "41\n")
	gitIn(t, path, "add", "answer.txt")
	gitIn(t, path, "commit", "-m", "seed bug")
	if origin != "" {
		gitIn(t, path, "remote", "add", "origin", origin)
	}
	return path
}

// hello is the repo of the fake GitHub's octo/hello: origin is its URL, and the fake already has
// main, as github.com has the repo's history.
func (f *githubRepoFixture) hello(t *testing.T) string {
	t.Helper()
	path := seedRepo(t, f.gh.URL+"/octo/hello.git")
	gitIn(t, path, "push", filepath.Join(f.gh.root, "octo", "hello.git"), "main")
	return path
}

func (f *githubRepoFixture) register(t *testing.T, name, path string) githubRepo {
	t.Helper()
	var repo githubRepo
	f.s.do(t, "POST", "/api/repos", map[string]string{"name": name, "path": path}, &repo, 201)
	if repo.ID == "" || repo.Name != name || repo.Path != path {
		t.Fatalf("registered repo: %+v", repo)
	}
	return repo
}

func (f *githubRepoFixture) thread(t *testing.T, b bot, repo githubRepo) thread {
	t.Helper()
	var th thread
	f.s.do(t, "POST", "/api/bots/"+b.ID+"/threads", map[string]string{"repo_id": repo.ID}, &th, 201)
	return th
}

// loggedCalls is calls of a fake CLI, none when it never ran.
func loggedCalls(t *testing.T, m *machine, name string) []*call {
	t.Helper()
	if _, err := os.Stat(filepath.Join(m.bin, name+".log")); err != nil {
		return nil
	}
	return m.calls(t, name)
}

// TestGitHubReposFixtures_ProviderScenario proves the repo-github-fix scenario, the fake gh and the
// fake GitHub's pull request route work without the product: the fake provider is started by hand
// with the env of a run of a Bot with GitHub, against a stub of Reef's MCP tools.
func TestGitHubReposFixtures_ProviderScenario(t *testing.T) {
	for _, provider := range _providers {
		t.Run(provider, func(t *testing.T) {
			m := newMachine(t, provider, "gh")
			g := newFakeGitHub(t)
			g.set(func() { g.token = "fake-gho-direct" })
			writeFile(t, filepath.Join(m.bin, "github-url"), g.URL)
			repoPath := seedRepo(t, g.URL+"/octo/hello.git")
			gitIn(t, repoPath, "push", filepath.Join(g.root, "octo", "hello.git"), "main")
			work := filepath.Join(t.TempDir(), "work")
			gitIn(t, repoPath, "worktree", "add", "-b", "fix/seeded-bug", work)
			empty := t.TempDir()
			writeFile(t, filepath.Join(empty, "hosts.yml"), "")

			var mu sync.Mutex
			calls := map[string]json.RawMessage{}
			stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var req struct {
					ID     json.RawMessage `json:"id"`
					Method string          `json:"method"`
					Params struct {
						Name      string          `json:"name"`
						Arguments json.RawMessage `json:"arguments"`
					} `json:"params"`
				}
				_ = json.NewDecoder(r.Body).Decode(&req)
				if len(req.ID) == 0 {
					w.WriteHeader(202)
					return
				}
				var result any = map[string]any{}
				switch req.Method {
				case "initialize":
					result = map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{}, "serverInfo": map[string]string{"name": "fixture", "version": "0"}}
				case "tools/list":
					result = map[string]any{"tools": []any{}}
				case "tools/call":
					mu.Lock()
					calls[req.Params.Name] = req.Params.Arguments
					mu.Unlock()
					text := "saved"
					if req.Params.Name == "start_work" {
						text = fmt.Sprintf(`{"path":%q,"branch":"fix/seeded-bug"}`, work)
					}
					result = map[string]any{"content": []map[string]string{{"type": "text", "text": text}}}
				}
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result})
			}))
			defer stub.Close()

			var cmd *exec.Cmd
			if provider == "claude" {
				config, _ := json.Marshal(map[string]any{"mcpServers": map[string]any{"atoll": map[string]string{"type": "http", "url": stub.URL}}})
				cmd = exec.CommandContext(t.Context(), filepath.Join(m.bin, provider), "-p", "--input-format", "stream-json", "--output-format", "stream-json", "--mcp-config", string(config), "--allowedTools", "mcp__atoll__*")
				input, _ := json.Marshal(map[string]any{"type": "user", "message": map[string]string{"role": "user", "content": _githubFixPrompt}})
				cmd.Stdin = strings.NewReader(string(input) + "\n")
			} else {
				cmd = exec.CommandContext(t.Context(), filepath.Join(m.bin, provider), "app-server")
				var input strings.Builder
				for i, req := range []map[string]any{
					{"method": "initialize", "params": map[string]any{}},
					{"method": "thread/start", "params": map[string]any{"cwd": repoPath, "config": map[string]any{"mcp_servers": map[string]any{"atoll": map[string]string{"url": stub.URL}}}}},
					{"method": "turn/start", "params": map[string]any{"input": []map[string]string{{"type": "text", "text": _githubFixPrompt}}}},
				} {
					req["id"] = i + 1
					b, _ := json.Marshal(req)
					input.Write(b)
					input.WriteByte('\n')
				}
				cmd.Stdin = strings.NewReader(input.String())
			}
			cmd.Dir = repoPath
			cmd.Env = []string{
				"HOME=" + m.home, "PATH=" + m.path, "GH_TOKEN=fake-gho-direct", "GH_CONFIG_DIR=" + empty,
				"GIT_CONFIG_COUNT=2", "GIT_CONFIG_KEY_0=credential.helper", "GIT_CONFIG_VALUE_0=",
				"GIT_CONFIG_KEY_1=credential." + g.URL + ".helper",
				"GIT_CONFIG_VALUE_1=" + `!f() { test "$1" = get && echo username=x-access-token && echo "password=$GH_TOKEN"; }; f`,
			}
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("scenario: %v: %s", err, out)
			}
			url := g.URL + "/octo/hello/pull/1"
			if got := gitIn(t, g.root, "--git-dir", filepath.Join(g.root, "octo", "hello.git"), "show", "refs/heads/fix/seeded-bug:answer.txt"); got != "42" {
				t.Errorf("fake did not push the fix: %q", got)
			}
			if got, want := g.pullRequests(), []githubPull{{"octo", "hello", "fix/seeded-bug", "main", _githubPRTitle, "Correct the fixture answer."}}; !slices.Equal(got, want) {
				t.Errorf("pull requests %+v, want %+v", got, want)
			}
			mu.Lock()
			defer mu.Unlock()
			if got, want := string(calls["track_mr"]), fmt.Sprintf(`{"title":%q,"url":%q}`, _githubPRTitle, url); got != want {
				t.Errorf("track_mr arguments %s, want %s", got, want)
			}
			if _, ok := calls["finish_run"]; !ok {
				t.Errorf("finish_run never called: %v", calls)
			}
		})
	}
}
