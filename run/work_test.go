package run

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"atoll/memory"
	"atoll/provider"
	"atoll/store"
)

type repoEnv struct {
	m      *Manager
	st     *store.Store
	bot    store.Bot
	repo   store.Repo
	thread store.Thread
	data   string
	root   string
	events <-chan Event
	next   *fakeSession
}

func newRepoEnv(t *testing.T) *repoEnv {
	t.Helper()
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init", "-q"},
		{"-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "--allow-empty", "-m", "init"},
	} {
		if out, err := exec.CommandContext(t.Context(), "git", append([]string{"-C", dir}, args...)...).
			CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	rp, err := st.CreateRepo(t.Context(), "proj", dir)
	if err != nil {
		t.Fatal(err)
	}
	bot, err := st.CreateBot(t.Context(), store.Bot{
		Name: "Ada", Provider: "claude", RepoAccess: store.RepoAccess{rp.ID},
	})
	if err != nil {
		t.Fatal(err)
	}
	thread, err := st.CreateThread(t.Context(), bot.ID, rp.ID)
	if err != nil {
		t.Fatal(err)
	}
	e := &repoEnv{st: st, bot: bot, repo: rp, thread: thread, data: t.TempDir(), root: t.TempDir()}
	var mu sync.Mutex
	e.m = New(Config{
		Store: st,
		Launch: func(context.Context, string) (provider.Session, provider.Config, error) {
			mu.Lock()
			defer mu.Unlock()
			if e.next == nil {
				return exited(), provider.Config{}, nil
			}
			sess := e.next
			e.next = nil
			return sess, provider.Config{}, nil
		},
		MCPURL:       "http://atoll/mcp/",
		DataDir:      e.data,
		WorktreeRoot: e.root,
		Log:          slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	t.Cleanup(func() { _ = e.m.Close() })
	events, cancel := e.m.Subscribe(thread.ID)
	t.Cleanup(cancel)
	e.events = events
	return e
}

// run sends one message and calls fn with the run's token while the prompt is being answered.
// It returns the provider session of the run once the run has finished.
func (e *repoEnv) run(t *testing.T, fn func(token string)) *fakeSession {
	t.Helper()
	sess := &fakeSession{events: make(chan provider.Event, 8), script: []provider.Event{
		{Kind: provider.KindTurnEnd},
	}}
	sess.onSend = func(n int) {
		if n != 1 {
			return
		}
		e.m.mu.Lock()
		token := slices.Collect(maps.Keys(e.m.tokens))[0]
		e.m.mu.Unlock()
		fn(token)
	}
	e.next = sess
	if _, _, err := e.m.Send(t.Context(), e.thread.ID, "hello", nil); err != nil {
		t.Fatal(err)
	}
	for ev := range e.events {
		if ev.Type == EventRunFinished {
			return sess
		}
	}
	t.Fatal("no run_finished event")
	return nil
}

func TestCloseThread_WorktreeRootChange(t *testing.T) {
	e := newRepoEnv(t)
	var path string
	e.run(t, func(token string) {
		var err error
		path, _, _, err = e.m.StartWork(t.Context(), token, "fix", "root", "")
		if err != nil {
			t.Error(err)
		}
	})
	if err := e.m.SetWorktreeRoot(t.Context(), t.TempDir()); err != nil {
		t.Fatal(err)
	}
	if err := e.m.CloseThread(t.Context(), e.thread.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("worktree remains: %v", err)
	}
}

func TestRun_RepoAccessRevoked(t *testing.T) {
	e := newRepoEnv(t)
	mem, err := memory.Open(e.st.DB(), nil)
	if err != nil {
		t.Fatal(err)
	}
	e.m.cfg.Memory = mem
	var worktree string
	e.run(t, func(token string) {
		var err error
		if worktree, _, _, err = e.m.StartWork(t.Context(), token, "feat", "x", ""); err != nil {
			t.Error(err)
		}
	})
	first := e.run(t, func(string) {})
	if !strings.Contains(first.prompts[0], "Repo: proj") || first.cfgs[0].Cwd != worktree {
		t.Fatalf("with access: cwd %q, prompt %q; want cwd %q and a repo block",
			first.cfgs[0].Cwd, first.prompts[0], worktree)
	}

	bot := e.bot
	bot.RepoAccess = store.RepoAccess{}
	if _, err := e.st.UpdateBot(t.Context(), bot); err != nil {
		t.Fatal(err)
	}
	var startErr error
	var shelf string
	second := e.run(t, func(token string) {
		_, _, _, startErr = e.m.StartWork(t.Context(), token, "feat", "y", "")
		sh, err := e.m.shelf(t.Context(), token)
		if err != nil {
			t.Error(err)
		}
		shelf = sh.RepoID
		tests := []struct {
			name    string
			scope   string
			wantErr string
		}{
			{name: "repo save refused", scope: "repo", wantErr: "invalid: thread has no accessible repo"},
			{name: "bot save allowed", scope: "bot"},
			{name: "user save allowed", scope: "user"},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				item, _, err := e.m.Remember(t.Context(), token, "fact", "Use Go.", "", tt.scope, "", nil)
				if tt.wantErr != "" {
					if !errors.Is(err, memory.ErrInvalid) || err.Error() != tt.wantErr {
						t.Errorf("Remember = %v, want %q", err, tt.wantErr)
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				if item.Body != "Use Go." || item.Scope != tt.scope {
					t.Errorf("Remember = %+v, want Use Go. in scope %q", item, tt.scope)
				}
			})
		}
		counts, err := mem.Count(t.Context(), memory.Shelf{RepoID: e.repo.ID})
		if err != nil {
			t.Error(err)
		}
		if counts.Repo != 0 {
			t.Errorf("repo memory count = %d, want 0", counts.Repo)
		}
	})
	wantCwd := filepath.Join(e.data, "threads", e.thread.ID+"-noaccess")
	if got := second.cfgs[0].Cwd; got != wantCwd {
		t.Errorf("cwd %q, want %q", got, wantCwd)
	}
	if _, err := os.Stat(filepath.Join(wantCwd, ".git")); err == nil {
		t.Error("the folder holds a git checkout")
	}
	if strings.Contains(second.prompts[0], "Repo:") || strings.Contains(second.prompts[0], worktree) {
		t.Errorf("prompt has a repo block: %q", second.prompts[0])
	}
	if startErr == nil || startErr.Error() != "thread has no repo" {
		t.Errorf("start_work error %v, want thread has no repo", startErr)
	}
	if shelf != "" {
		t.Errorf("memory shelf repo %q, want none", shelf)
	}
}

func TestRun_StartWorkConcurrent(t *testing.T) {
	e := newRepoEnv(t)
	paths := make([]string, 2)
	e.run(t, func(token string) {
		var wg sync.WaitGroup
		for i := range paths {
			wg.Go(func() {
				var err error
				if paths[i], _, _, err = e.m.StartWork(t.Context(), token, "feat", "x", ""); err != nil {
					t.Error(err)
				}
			})
		}
		wg.Wait()
	})
	made, err := os.ReadDir(filepath.Join(e.root, "proj"))
	if err != nil {
		t.Fatal(err)
	}
	if len(made) != 1 || paths[0] != paths[1] || paths[0] != filepath.Join(e.root, "proj", "feat-x") {
		t.Errorf("paths %v, worktree folders %d; want one worktree %s", paths, len(made),
			filepath.Join(e.root, "proj", "feat-x"))
	}
}

func TestRun_TrackMRKeepsStoredState(t *testing.T) {
	const url = "https://gitlab.example/g/p/-/merge_requests/1"
	e := newRepoEnv(t)
	if _, err := e.st.SetMR(t.Context(), e.thread.ID, url, "merged", "T"); err != nil {
		t.Fatal(err)
	}
	events, cancel := e.m.Subscribe(e.thread.ID)
	defer cancel()
	e.run(t, func(token string) {
		if err := e.m.TrackMR(t.Context(), token, url, "T"); err != nil {
			t.Error(err)
		}
	})
	for {
		select {
		case ev := <-events:
			if mr, ok := ev.Data.(MRUpdated); ok {
				if mr.State != "merged" {
					t.Errorf("MRUpdated state %q, want merged", mr.State)
				}
				return
			}
		default:
			t.Fatal("no mr_updated event")
		}
	}
}

func TestRun_TrackMR(t *testing.T) {
	tests := []struct {
		name    string
		give    string
		wantErr string
	}{
		{name: "https", give: "https://gitlab.example/g/p/-/merge_requests/1"},
		{name: "http", give: "http://gitlab.example/g/p/-/merge_requests/1"},
		{name: "other scheme", give: "javascript://x/%0Aalert(1)", wantErr: "http(s)"},
		{name: "option", give: "--web", wantErr: "http(s)"},
		{name: "relative", give: "/g/p/-/merge_requests/1", wantErr: "http(s)"},
		{name: "no host", give: "https:///x", wantErr: "http(s)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newRepoEnv(t)
			var err error
			e.run(t, func(token string) { err = e.m.TrackMR(t.Context(), token, tt.give, "T") })
			if (err == nil) != (tt.wantErr == "") || err != nil && !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("TrackMR(%q) = %v, want error containing %q", tt.give, err, tt.wantErr)
			}
			th, terr := e.st.Thread(t.Context(), e.thread.ID)
			if terr != nil {
				t.Fatal(terr)
			}
			if (th.MRURL == tt.give) != (tt.wantErr == "") {
				t.Errorf("stored MR URL %q for %q", th.MRURL, tt.give)
			}
		})
	}
}

// TestHiddenPaths_ClosedThreads proves the hidden paths of a repo the Bot lacks hold the folder of
// each open thread on it and none of a closed thread's, which closing removed.
func TestHiddenPaths_ClosedThreads(t *testing.T) {
	ctx := t.Context()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	rp, err := st.CreateRepo(ctx, "proj", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	owner, err := st.CreateBot(ctx, store.Bot{Name: "Ada", Provider: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	other, err := st.CreateBot(ctx, store.Bot{Name: "Bob", Provider: "claude", RepoAccess: store.RepoAccess{}})
	if err != nil {
		t.Fatal(err)
	}
	open, err := st.CreateThread(ctx, owner.ID, rp.ID)
	if err != nil {
		t.Fatal(err)
	}
	closed, err := st.CreateThread(ctx, owner.ID, rp.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetClosed(ctx, closed.ID); err != nil {
		t.Fatal(err)
	}
	m := New(Config{Store: st, DataDir: t.TempDir(), WorktreeRoot: t.TempDir()})
	t.Cleanup(func() { _ = m.Close() })

	paths, err := m.hiddenPaths(ctx, other)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(paths, m.threadDir(open.ID)) {
		t.Errorf("hidden paths %q lack the open thread's folder %s", paths, m.threadDir(open.ID))
	}
	if slices.Contains(paths, m.threadDir(closed.ID)) {
		t.Errorf("hidden paths %q hold the closed thread's folder %s, want none", paths, m.threadDir(closed.ID))
	}
}
