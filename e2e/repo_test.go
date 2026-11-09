package e2e

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"atoll/e2e/fakes/fake"
)

const (
	_fixRepoPrompt        = "Fix the seeded bug and open an MR."
	_questionRepoPrompt   = "What answer is in answer.txt? Do not edit anything."
	_unfinishedRepoPrompt = "Edit the seeded answer but leave the work uncommitted."
	_resumeRepoPrompt     = "Read the answer again from this thread workspace."
)

type registeredRepo struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Path string `json:"path"`
}

type repoFixture struct {
	m      *machine
	s      *server
	repo   registeredRepo
	origin string
	root   string
}

func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=Fixture", "GIT_AUTHOR_EMAIL=fixture@example.test", "GIT_COMMITTER_NAME=Fixture", "GIT_COMMITTER_EMAIL=fixture@example.test", "GIT_CONFIG_NOSYSTEM=1")
	b, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, b)
	}
	return strings.TrimSpace(string(b))
}

func newRepoFixture(t *testing.T, provider string) *repoFixture {
	t.Helper()
	m := newMachine(t, provider, "glab")
	m.signIn(t, provider)
	writeFile(t, filepath.Join(m.bin, "mr-state"), "opened")
	// Share only the private Secret Service session, never the host's credentials or PATH.
	for _, key := range []string{"DBUS_SESSION_BUS_ADDRESS", "XDG_RUNTIME_DIR", "XDG_DATA_HOME"} {
		if v := os.Getenv(key); v != "" {
			m.env = append(m.env, key+"="+v)
		}
	}
	m.env = append(m.env, "ATOLL_REPO_POLL_INTERVAL=100ms")
	f := &repoFixture{m: m, root: t.TempDir(), origin: filepath.Join(t.TempDir(), "origin.git")}
	repoPath := t.TempDir()
	gitIn(t, repoPath, "init", "--bare", f.origin)
	gitIn(t, repoPath, "init", "-b", "main")
	writeFile(t, filepath.Join(repoPath, "answer.txt"), "41\n")
	gitIn(t, repoPath, "add", "answer.txt")
	gitIn(t, repoPath, "commit", "-m", "seed bug")
	gitIn(t, repoPath, "remote", "add", "origin", f.origin)
	gitIn(t, repoPath, "push", "-u", "origin", "main")
	gitIn(t, repoPath, "--git-dir", f.origin, "symbolic-ref", "HEAD", "refs/heads/main")
	f.s = m.serve(t)
	f.s.do(t, "PUT", "/api/settings", map[string]string{"worktree_root": f.root}, nil, 204)
	f.s.do(t, "POST", "/api/repos", map[string]string{"name": "seeded", "path": repoPath}, &f.repo, 201)
	if f.repo.ID == "" || f.repo.Path != repoPath || f.repo.Name != "seeded" {
		t.Fatalf("registered repo: %+v", f.repo)
	}
	var repos []registeredRepo
	f.s.do(t, "GET", "/api/repos", nil, &repos, 200)
	if len(repos) != 1 || repos[0] != f.repo {
		t.Fatalf("repo list: %+v", repos)
	}
	return f
}

func (f *repoFixture) bot(t *testing.T, provider string, access any, connections []string) bot {
	t.Helper()
	var b bot
	f.s.do(t, "POST", "/api/bots", map[string]any{"name": "Repo Bot", "persona": "Fix carefully.", "provider": provider, "repo_access": access, "connections": connections}, &b, 201)
	if b.ID == "" {
		t.Fatal("bot has no id")
	}
	return b
}

func (f *repoFixture) thread(t *testing.T, b bot) thread {
	t.Helper()
	var th struct {
		ID     string `json:"id"`
		BotID  string `json:"bot_id"`
		RepoID string `json:"repo_id"`
	}
	f.s.do(t, "POST", "/api/bots/"+b.ID+"/threads", map[string]string{"repo_id": f.repo.ID}, &th, 201)
	if th.ID == "" || th.BotID != b.ID || th.RepoID != f.repo.ID {
		t.Fatalf("repo thread: %+v", th)
	}
	return thread{ID: th.ID, BotID: th.BotID}
}

func repoEvent(t *testing.T, es []event, typ string) event {
	t.Helper()
	for _, e := range es {
		if e.Type == typ {
			return e
		}
	}
	t.Fatalf("missing %s in %s", typ, types(es))
	return event{}
}

func (f *repoFixture) work(t *testing.T, b bot, prompt, branch string) (thread, string, <-chan event, string) {
	t.Helper()
	th := f.thread(t, b)
	ch := f.s.events(t, th)
	r := f.s.send(t, th, prompt)
	es := until(t, ch, r.ID, "run_finished")
	done := f.s.run(t, r.ID)
	if done.Status != "done" || done.Report == nil {
		t.Fatalf("repository run: %+v", done)
	}
	if prompt == _fixRepoPrompt {
		mr := repoEvent(t, es, "mr_updated")
		if mr.Data["url"] != fake.MRURL || mr.Data["state"] != "opened" {
			t.Fatalf("MR event: %v", mr.Data)
		}
	}
	e := repoEvent(t, es, "worktree_created")
	path, _ := e.Data["path"].(string)
	if e.Data["repo_id"] != f.repo.ID || e.Data["branch"] != branch {
		t.Fatalf("worktree event: %v", e.Data)
	}
	rel, err := filepath.Rel(f.root, path)
	if err != nil || !filepath.IsAbs(path) || rel == "." || strings.HasPrefix(rel, "..") {
		t.Fatalf("worktree %q outside root %q", path, f.root)
	}
	if gitIn(t, path, "branch", "--show-current") != branch {
		t.Fatal("wrong worktree branch")
	}
	return th, path, ch, r.ID
}

func assertRemoved(t *testing.T, ch <-chan event, runID, path, reason string) event {
	t.Helper()
	events := until(t, ch, runID, "worktree_removed")
	if reason == "merged" {
		mr := repoEvent(t, events, "mr_updated")
		if mr.Data["state"] != "merged" || mr.Data["url"] != fake.MRURL {
			t.Fatalf("merged MR event: %v", mr.Data)
		}
	}
	e := repoEvent(t, events, "worktree_removed")
	if e.Data["path"] != path || e.Data["reason"] != reason {
		t.Fatalf("removal event: %v", e.Data)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("worktree remains at %s: %v", path, err)
	}
	return e
}

func TestRepo_FixPushMRAndMerge(t *testing.T) {
	for _, provider := range []string{"claude", "codex"} {
		t.Run(provider, func(t *testing.T) {
			f := newRepoFixture(t, provider)
			connection, token := f.connect(t)
			b := f.bot(t, provider, "all", []string{connection})
			th, path, ch, _ := f.work(t, b, _fixRepoPrompt, "fix/seeded-bug")
			if got := gitIn(t, f.repo.Path, "--git-dir", f.origin, "show", "refs/heads/fix/seeded-bug:answer.txt"); got != "42" {
				t.Fatalf("pushed answer %q", got)
			}
			if gitIn(t, path, "status", "--porcelain") != "" {
				t.Fatal("fix left dirty worktree")
			}
			if gitIn(t, path, "rev-parse", "HEAD") == gitIn(t, path, "rev-parse", "main") {
				t.Fatal("fix has no commit")
			}
			var messages []message
			f.s.do(t, "GET", "/api/threads/"+th.ID+"/messages", nil, &messages, 200)
			linked := false
			for _, m := range messages {
				linked = linked || m.Role == "bot" && strings.Contains(m.Text, fake.MRURL)
			}
			if !linked {
				t.Fatal("MR URL absent from bot reply")
			}
			for _, c := range f.m.sessions(t, provider) {
				assertGitLabEnv(t, "provider", c.env, token)
			}
			create := false
			for _, c := range f.m.calls(t, "glab") {
				if len(c.argv) >= 2 && c.argv[0] == "mr" && c.argv[1] == "create" {
					create = true
					assertGitLabEnv(t, "glab create", c.env, token)
				}
			}
			if !create {
				t.Fatal("glab mr create never ran")
			}
			// A subsequent provider run must start in the thread's worktree and resume its session.
			r := f.s.send(t, th, _resumeRepoPrompt)
			done := f.s.finished(t, ch, r.ID)
			if done.Status != "done" || !strings.Contains(done.Reply, path) || !strings.Contains(done.Reply, "42") {
				t.Fatalf("resumed run: %+v", done)
			}
			sessions := f.m.sessions(t, provider)
			if len(sessions) != 2 {
				t.Fatalf("provider runs %d, want 2", len(sessions))
			}
			if provider == "claude" && flagValue(sessions[1].argv, "--resume") == "" {
				t.Fatal("second run did not resume Claude")
			}
			if provider == "codex" && request(sessions[1], "thread/resume") == nil {
				t.Fatal("second run did not resume Codex")
			}
			assertNoSecret(t, token, f.m.data, f.root)
			f.s.kill()
			if bytes.Contains(f.s.log.Bytes(), []byte(token)) {
				t.Fatal("server log leaked token")
			}
			// Polling must survive a server restart; no provider is needed to notice the merge.
			f.s = f.m.serve(t)
			ch = f.s.events(t, th)
			writeFile(t, filepath.Join(f.m.bin, "mr-state"), "merged")
			assertRemoved(t, ch, r.ID, path, "merged")
			viewed := false
			for _, c := range f.m.calls(t, "glab") {
				viewed = viewed || len(c.argv) >= 2 && c.argv[0] == "mr" && c.argv[1] == "view"
			}
			if !viewed {
				t.Fatal("merge was not checked via glab mr view")
			}
			if len(f.m.sessions(t, provider)) != 2 {
				t.Fatal("merge polling started a provider run")
			}
			f.s.kill()
			if bytes.Contains(f.s.log.Bytes(), []byte(token)) {
				t.Fatal("restarted server log leaked token")
			}
			assertNoSecret(t, token, f.m.data, f.root)
		})
	}
}

func TestRepo_QuestionCreatesNoWorktree(t *testing.T) {
	for _, provider := range []string{"claude", "codex"} {
		t.Run(provider, func(t *testing.T) {
			f := newRepoFixture(t, provider)
			// Dirty the registered checkout: read-only runs must see a clean checkout of HEAD.
			writeFile(t, filepath.Join(f.repo.Path, "answer.txt"), "local dirty edit\n")
			b := f.bot(t, provider, "all", nil)
			th := f.thread(t, b)
			ch := f.s.events(t, th)
			r := f.s.send(t, th, _questionRepoPrompt)
			es := until(t, ch, r.ID, "run_finished")
			done := f.s.run(t, r.ID)
			if done.Status != "done" || !strings.Contains(done.Reply, ": 41") {
				t.Fatalf("question: %+v", done)
			}
			for _, e := range es {
				if e.Type == "worktree_created" {
					t.Fatal("question created worktree")
				}
			}
			entries, err := os.ReadDir(f.root)
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != 0 {
				t.Fatalf("question populated worktree root: %v", entries)
			}
			if got, err := os.ReadFile(filepath.Join(f.repo.Path, "answer.txt")); err != nil || string(got) != "local dirty edit\n" {
				t.Fatal("registered checkout was modified")
			}
		})
	}
}

func TestRepo_CloseSavesUncommittedWork(t *testing.T) {
	for _, provider := range []string{"claude", "codex"} {
		t.Run(provider, func(t *testing.T) {
			f := newRepoFixture(t, provider)
			b := f.bot(t, provider, "all", nil)
			th, path, ch, runID := f.work(t, b, _unfinishedRepoPrompt, "fix/seeded-bug")
			if gitIn(t, path, "status", "--porcelain") == "" {
				t.Fatal("scenario did not leave dirty work")
			}
			f.s.do(t, "POST", "/api/threads/"+th.ID+"/close", nil, nil, 204)
			e := assertRemoved(t, ch, runID, path, "closed")
			ref := "refs/atoll/unfinished/" + th.ID
			if e.Data["snapshot_ref"] != ref {
				t.Fatalf("snapshot event: %v", e.Data)
			}
			for name, want := range map[string]string{"answer.txt": "42", "unfinished.txt": "keep this untracked work"} {
				if got := gitIn(t, f.repo.Path, "show", ref+":"+name); got != want {
					t.Fatalf("saved %s = %q, want %q", name, got, want)
				}
			}
			f.s.do(t, "POST", "/api/threads/"+th.ID+"/messages", map[string]string{"text": _questionRepoPrompt}, nil, 409)
		})
	}
}

func TestRepo_BranchCollision(t *testing.T) {
	f := newRepoFixture(t, "claude")
	b := f.bot(t, "claude", "all", nil)
	first, path1, ch1, run1 := f.work(t, b, _unfinishedRepoPrompt, "fix/seeded-bug")
	second, path2, ch2, run2 := f.work(t, b, _unfinishedRepoPrompt, "fix/seeded-bug-2")
	if path1 == path2 {
		t.Fatal("threads share a worktree")
	}
	for _, work := range []struct {
		th   thread
		path string
		ch   <-chan event
		run  string
	}{
		{first, path1, ch1, run1}, {second, path2, ch2, run2},
	} {
		f.s.do(t, "POST", "/api/threads/"+work.th.ID+"/close", nil, nil, 204)
		assertRemoved(t, work.ch, work.run, work.path, "closed")
	}
}

func TestRepo_OnlyTheseDeniesOtherRepo(t *testing.T) {
	f := newRepoFixture(t, "claude")
	b := f.bot(t, "claude", []string{f.repo.ID}, nil)
	f.thread(t, b)
	otherPath := t.TempDir()
	gitIn(t, otherPath, "init", "-b", "main")
	writeFile(t, filepath.Join(otherPath, "README"), "other")
	gitIn(t, otherPath, "add", "README")
	gitIn(t, otherPath, "commit", "-m", "seed")
	var other registeredRepo
	f.s.do(t, "POST", "/api/repos", map[string]string{"name": "other", "path": otherPath}, &other, 201)
	f.s.do(t, "POST", "/api/bots/"+b.ID+"/threads", map[string]string{"repo_id": other.ID}, nil, 403)
	empty := f.bot(t, "claude", []string{}, nil)
	f.s.do(t, "POST", "/api/bots/"+empty.ID+"/threads", map[string]string{"repo_id": f.repo.ID}, nil, 403)
}

// connect completes the actual server's device flow against an RFC 8628-shaped local endpoint.
func (f *repoFixture) connect(t *testing.T) (string, string) {
	t.Helper()
	token := "glpat-e2e-" + strings.ReplaceAll(t.Name(), "/", "-")
	var allowed atomic.Bool
	var deviceCalls, tokenCalls atomic.Int32
	oauth := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := r.ParseForm(); err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		if r.Method != "POST" || r.Form.Get("client_id") == "" {
			t.Error("OAuth needs POST and client_id")
			w.WriteHeader(400)
			return
		}
		switch r.URL.Path {
		case "/oauth/authorize_device":
			deviceCalls.Add(1)
			if !strings.Contains(r.Form.Get("scope"), "api") {
				t.Error("OAuth scope missing api")
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"device_code": "fixture-device", "user_code": "REEF-1234", "verification_uri": "https://gitlab.example/oauth/device", "expires_in": 600, "interval": 1})
		case "/oauth/token":
			tokenCalls.Add(1)
			if r.Form.Get("device_code") != "fixture-device" || r.Form.Get("grant_type") != "urn:ietf:params:oauth:grant-type:device_code" {
				t.Error("wrong device token grant")
				w.WriteHeader(400)
				return
			}
			if !allowed.Load() {
				w.WriteHeader(400)
				_, _ = fmt.Fprint(w, `{"error":"authorization_pending"}`)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": token, "refresh_token": "glrt-" + token, "token_type": "Bearer", "scope": "api", "expires_in": 7200, "created_at": time.Now().Unix()})
		default:
			t.Errorf("unexpected GitLab HTTP call: %s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(oauth.Close)
	var start struct {
		ID              string `json:"id"`
		UserCode        string `json:"user_code"`
		VerificationURI string `json:"verification_uri"`
	}
	f.s.do(t, "POST", "/api/connections/gitlab/start", map[string]string{"instance_url": oauth.URL}, &start, 201)
	if start.ID == "" || start.UserCode != "REEF-1234" || start.VerificationURI != "https://gitlab.example/oauth/device" {
		t.Fatalf("device start: %+v", start)
	}
	var status struct {
		Status string `json:"status"`
	}
	f.s.do(t, "POST", "/api/connections/"+start.ID+"/poll", nil, &status, 200)
	if status.Status != "pending" {
		t.Fatalf("pending status: %+v", status)
	}
	allowed.Store(true)
	time.Sleep(time.Second)
	f.s.do(t, "POST", "/api/connections/"+start.ID+"/poll", nil, &status, 200)
	if status.Status != "connected" {
		t.Fatalf("connected status: %+v", status)
	}
	f.s.do(t, "GET", "/api/connections/"+start.ID, nil, &status, 200)
	if status.Status != "connected" || deviceCalls.Load() != 1 || tokenCalls.Load() < 2 {
		t.Fatal("device flow/status incomplete")
	}
	if runtime.GOOS == "linux" && os.Getenv("DBUS_SESSION_BUS_ADDRESS") == "" {
		t.Fatal("run via make e2e: Linux needs the private Secret Service session")
	}
	// The public Secret Service boundary proves storage independently of Atoll's status response.
	cmd := exec.CommandContext(t.Context(), "secret-tool", "lookup", "service", "atoll", "username", "gitlab:"+start.ID)
	deleteSecret := exec.Command("secret-tool", "clear", "service", "atoll", "username", "gitlab:"+start.ID)
	if runtime.GOOS == "darwin" {
		cmd = exec.CommandContext(t.Context(), "security", "find-generic-password", "-s", "atoll", "-a", "gitlab:"+start.ID, "-w")
		deleteSecret = exec.Command("security", "delete-generic-password", "-s", "atoll", "-a", "gitlab:"+start.ID)
	}
	out, err := cmd.Output()
	var stored struct {
		AccessToken string `json:"access_token"`
	}
	if err != nil || json.Unmarshal(out, &stored) != nil || stored.AccessToken != token {
		t.Fatal("token not retrievable from OS secret store")
	}
	t.Cleanup(func() { _ = deleteSecret.Run() })
	return start.ID, token
}

// assertGitLabEnv checks the env a selected GitLab connection gives: glab sends GITLAB_TOKEN as a
// personal token, so the OAuth token must come as OAUTH_TOKEN with a config marking the host OAuth.
func assertGitLabEnv(t *testing.T, who string, env map[string]string, token string) {
	t.Helper()
	if env["OAUTH_TOKEN"] != token {
		t.Fatalf("%s missing OAUTH_TOKEN", who)
	}
	if _, ok := env["GITLAB_TOKEN"]; ok {
		t.Fatalf("%s has GITLAB_TOKEN", who)
	}
	dir := env["GLAB_CONFIG_DIR"]
	if dir == "" {
		t.Fatalf("%s missing GLAB_CONFIG_DIR", who)
	}
	b, err := os.ReadFile(filepath.Join(dir, "config.yml"))
	if err != nil {
		t.Fatalf("%s glab config: %v", who, err)
	}
	if !bytes.Contains(b, []byte("is_oauth2")) || bytes.Contains(b, []byte(token)) {
		t.Fatalf("%s glab config must set is_oauth2 and hold no token", who)
	}
}

func assertNoSecret(t *testing.T, token string, roots ...string) {
	t.Helper()
	for _, root := range roots {
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			b, err := os.ReadFile(path)
			if errors.Is(err, fs.ErrNotExist) {
				// The server renames its temp files (hosts.yml.<random>) away while it runs.
				return nil
			}
			if err != nil {
				return err
			}
			if bytes.Contains(b, []byte(token)) {
				t.Errorf("token leaked in %s", path)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}
