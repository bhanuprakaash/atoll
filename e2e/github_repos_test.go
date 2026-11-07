package e2e

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// Contract: github-repos-contract.md.

func TestGitHubRepos_RepoShowsItsHost(t *testing.T) {
	f := newGitHubRepoFixture(t, "claude")
	hello := f.register(t, "octo-hello", f.hello(t))
	gitlab := f.register(t, "gitlab-repo", seedRepo(t, "https://gitlab.example/fixtures/seeded.git"))
	local := f.register(t, "local-only", seedRepo(t, ""))
	var repos []githubRepo
	f.s.do(t, "GET", "/api/repos", nil, &repos, 200)
	for _, c := range []struct {
		repo githubRepo
		host string
	}{{hello, "github"}, {gitlab, "gitlab"}, {local, "gitlab"}} {
		if c.repo.Host != c.host {
			t.Errorf("POST /api/repos %s: host %q, want %q", c.repo.Name, c.repo.Host, c.host)
		}
		if !slices.Contains(repos, c.repo) {
			t.Errorf("GET /api/repos: %+v lacks %+v", repos, c.repo)
		}
	}
}

func TestGitHubRepos_BotPushesAndOpensAPullRequest(t *testing.T) {
	for _, p := range _providers {
		t.Run(p, func(t *testing.T) {
			f := newGitHubRepoFixture(t, p)
			repo := f.register(t, "octo-hello", f.hello(t))
			id := f.connect(t)
			token := f.gh.currentToken()
			b := f.bot(t, p, []string{id})
			th := f.thread(t, b, repo)
			ch := f.s.events(t, th)
			r := f.s.send(t, th, _githubFixPrompt)
			es := until(t, ch, r.ID, "run_finished")
			done := f.s.run(t, r.ID)
			if done.Status != "done" || done.Report == nil {
				t.Fatalf("run: %+v", done)
			}
			if wt := repoEvent(t, es, "worktree_created"); wt.Data["repo_id"] != repo.ID || wt.Data["branch"] != "fix/seeded-bug" {
				t.Fatalf("worktree event: %v", wt.Data)
			}

			url := f.gh.URL + "/octo/hello/pull/1"
			if got, want := f.gh.pullRequests(), []githubPull{{"octo", "hello", "fix/seeded-bug", "main", _githubPRTitle, "Correct the fixture answer."}}; !slices.Equal(got, want) {
				t.Errorf("the fake GitHub's POST /repos/octo/hello/pulls: %+v, want %+v (the thread's branch as head, and base)", got, want)
			}
			if got := gitIn(t, f.gh.root, "--git-dir", filepath.Join(f.gh.root, "octo", "hello.git"), "show", "refs/heads/fix/seeded-bug:answer.txt"); got != "42" {
				t.Errorf("the remote's fix/seeded-bug has answer.txt %q, want the pushed 42", got)
			}
			if auth := f.gh.pushes(); len(auth) == 0 {
				t.Error("the fake GitHub accepted no git request")
			} else {
				for _, a := range auth {
					if a != "x-access-token:"+token {
						t.Errorf("git request authenticated as %q, want x-access-token and the token", a)
					}
				}
			}
			if mr := repoEvent(t, es, "mr_updated"); mr.Data["url"] != url || mr.Data["state"] != "opened" || mr.Data["title"] != _githubPRTitle {
				t.Errorf("mr_updated event: %v, want url %s, state opened, title %q", mr.Data, url, _githubPRTitle)
			}

			var got struct {
				Branch  string `json:"branch"`
				MRURL   string `json:"mr_url"`
				MRState string `json:"mr_state"`
				MRTitle string `json:"mr_title"`
			}
			f.s.do(t, "GET", "/api/threads/"+th.ID, nil, &got, 200)
			if got.Branch != "fix/seeded-bug" || got.MRURL != url || got.MRState != "opened" || got.MRTitle != _githubPRTitle {
				t.Errorf("GET /api/threads/%s: %+v, want the branch and the pull request %s opened", th.ID, got, url)
			}
			var messages []message
			f.s.do(t, "GET", "/api/threads/"+th.ID+"/messages", nil, &messages, 200)
			if !slices.ContainsFunc(messages, func(m message) bool { return m.Role == "bot" && strings.Contains(m.Text, url) }) {
				t.Errorf("the PR URL %s is in no Bot message: %+v", url, messages)
			}

			sessions := f.m.sessions(t, p)
			if len(sessions) != 1 {
				t.Fatalf("%s ran %d times, want 1", p, len(sessions))
			}
			assertGitHubEnv(t, p, sessions[0].env, token)
			created := false
			for _, c := range f.m.calls(t, "gh") {
				if len(c.argv) >= 2 && c.argv[0] == "pr" && c.argv[1] == "create" {
					created = true
					assertGitHubEnv(t, "gh pr create", c.env, token)
				}
			}
			if !created {
				t.Error("gh pr create never ran")
			}

			// A GitHub repo's PR is not an MR: the server never asks glab about it.
			time.Sleep(time.Second)
			for _, c := range loggedCalls(t, f.m, "glab") {
				if len(c.argv) > 0 && c.argv[0] == "mr" {
					t.Errorf("glab ran %v for a GitHub repo's thread", c.argv)
				}
			}

			assertNoSecret(t, token, f.m.data, f.root)
			f.s.kill()
			if bytes.Contains(f.s.log.Bytes(), []byte(token)) {
				t.Fatalf("server log leaked the token:\n%s", f.s.log)
			}
		})
	}
}

func TestGitHubRepos_WithoutTheConnectionStartWorkSaysWhatToDo(t *testing.T) {
	for _, p := range _providers {
		t.Run(p, func(t *testing.T) {
			f := newGitHubRepoFixture(t, p)
			repo := f.register(t, "octo-hello", f.hello(t))
			f.connect(t)
			b := f.bot(t, p, []string{})
			th := f.thread(t, b, repo)
			ch := f.s.events(t, th)
			r := f.s.send(t, th, _githubFixPrompt)
			es := until(t, ch, r.ID, "run_finished")
			done := f.s.run(t, r.ID)
			if done.Status != "done" || !strings.Contains(done.Reply, "give this Bot the GitHub connection") {
				t.Fatalf("run: %+v, want done with a reply telling the user to give this Bot the GitHub connection", done)
			}
			for _, bad := range []string{"Authentication failed", "could not read Username", "Permission denied"} {
				if strings.Contains(done.Reply, bad) {
					t.Errorf("reply shows a git error %q: %s", bad, done.Reply)
				}
			}
			for _, e := range es {
				if e.Type == "worktree_created" {
					t.Errorf("start_work made a worktree for a Bot without the connection: %v", e.Data)
				}
			}
			if entries, _ := os.ReadDir(f.root); len(entries) != 0 {
				t.Errorf("worktree root not empty: %v", entries)
			}
			if refs := gitIn(t, f.gh.root, "--git-dir", filepath.Join(f.gh.root, "octo", "hello.git"), "for-each-ref", "refs/heads/fix"); refs != "" {
				t.Errorf("the remote has fix branches: %s", refs)
			}
			if len(f.gh.pushes()) != 0 || len(f.gh.pullRequests()) != 0 {
				t.Errorf("the fake GitHub got git requests %v and pull requests %+v", f.gh.pushes(), f.gh.pullRequests())
			}
			for _, c := range loggedCalls(t, f.m, "gh") {
				if len(c.argv) > 0 && c.argv[0] == "pr" {
					t.Errorf("gh ran %v", c.argv)
				}
			}

			// Only editing needs the connection: a question in the same repo is answered.
			th = f.thread(t, b, repo)
			ch = f.s.events(t, th)
			done = f.s.finished(t, ch, f.s.send(t, th, _questionRepoPrompt).ID)
			if done.Status != "done" || !strings.Contains(done.Reply, ": 41") {
				t.Errorf("question in a GitHub repo without the connection: %+v", done)
			}
		})
	}
}
