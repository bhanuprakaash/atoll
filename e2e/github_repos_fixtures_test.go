package e2e

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"slices"
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

// newGitHubRepoFixture serves Atoll with env after the machine's own, so ATOLL_REPO_POLL_INTERVAL
// can be set again.
func newGitHubRepoFixture(t *testing.T, provider string, env ...string) *githubRepoFixture {
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
	m.env = append(m.env, env...)
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
