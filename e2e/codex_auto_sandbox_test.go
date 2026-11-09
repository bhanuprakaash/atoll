package e2e

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestCodexAuto_SandboxCoversWorktreeAndNetwork(t *testing.T) {
	for _, mode := range _modes {
		t.Run(mode, func(t *testing.T) {
			f := newRepoFixture(t, "codex")
			var b bot
			f.s.do(t, "POST", "/api/bots", map[string]any{"name": "Codex " + mode, "persona": "Fix carefully.", "provider": "codex", "mode": mode, "repo_access": "all"}, &b, 201)
			th := f.thread(t, b)
			ch := f.s.events(t, th)
			for _, prompt := range []string{_fixRepoPrompt, _resumeRepoPrompt} {
				r := f.s.send(t, th, prompt)
				if done := f.s.finished(t, ch, r.ID); done.Status != "done" {
					t.Fatalf("codex %s run %q: %+v", mode, prompt, done)
				}
			}

			sessions := f.m.sessions(t, "codex")
			if len(sessions) != 2 {
				t.Fatalf("codex ran %d sessions, want 2", len(sessions))
			}
			common := gitIn(t, f.repo.Path, "rev-parse", "--path-format=absolute", "--git-common-dir")
			for i, c := range sessions {
				checkMode(t, "codex", mode, c)
				method, cwd, present, roots, network := codexWorkspaceWrite(t, c)
				if want := []string{"thread/start", "thread/resume"}[i]; method != want {
					t.Fatalf("session %d began with %q, want %s", i+1, method, want)
				}
				if mode == "auto" {
					wantRoots := resolved(t, f.root, filepath.Join(common, "objects"), filepath.Join(common, "refs"), filepath.Join(common, "logs"), gitIn(t, cwd, "rev-parse", "--absolute-git-dir"))
					if len(wantRoots) != 5 || i == 1 && !strings.HasPrefix(resolved(t, cwd)[0], resolved(t, f.root)[0]+string(filepath.Separator)) {
						t.Fatalf("%s ran in %q, want a linked worktree (with its own git dir), under %q when resumed", method, cwd, f.root)
					}
					if !network {
						t.Errorf("codex on auto, %s: config.sandbox_workspace_write.network_access is not true", method)
					}
					got := resolved(t, roots...)
					for _, bad := range resolved(t, common, filepath.Join(common, "config"), filepath.Join(common, "hooks")) {
						if slices.Contains(got, bad) {
							t.Errorf("codex on auto, %s: writable_roots grant %s, which would let the Bot write the repo's config or hooks", method, bad)
						}
					}
					if !slices.Equal(got, wantRoots) {
						t.Errorf("codex on auto, %s: config.sandbox_workspace_write.writable_roots %q, want %q in any order (cwd %s)", method, got, wantRoots, cwd)
					}
				} else if present {
					t.Errorf("codex on %s, %s: config.sandbox_workspace_write is set; want it absent", mode, method)
				}
			}
		})
	}
}

// resolved is paths cleaned, with symlinks resolved, sorted and without repeats: macOS temp folders sit behind a
// symlink that git resolves.
func resolved(t *testing.T, paths ...string) []string {
	t.Helper()
	out := make([]string, len(paths))
	for i, p := range paths {
		var err error
		if out[i], err = filepath.EvalSymlinks(filepath.Clean(p)); err != nil {
			t.Fatal(err)
		}
	}
	slices.Sort(out)
	return slices.Compact(out)
}

// codexWorkspaceWrite is the method and cwd of a codex session's thread/start or thread/resume, whether
// its config has sandbox_workspace_write, and the writable_roots and network_access under it, the
// only place Codex reads them from at that point.
func codexWorkspaceWrite(t *testing.T, c *call) (method, cwd string, present bool, roots []string, network bool) {
	t.Helper()
	for _, line := range c.stdin {
		var r struct {
			Method string `json:"method"`
			Params struct {
				Cwd    string `json:"cwd"`
				Config struct {
					SandboxWorkspaceWrite *struct {
						WritableRoots []string `json:"writable_roots"`
						NetworkAccess bool     `json:"network_access"`
					} `json:"sandbox_workspace_write"`
				} `json:"config"`
			} `json:"params"`
		}
		if json.Unmarshal([]byte(line), &r) == nil && (r.Method == "thread/start" || r.Method == "thread/resume") {
			w := r.Params.Config.SandboxWorkspaceWrite
			if w == nil {
				return r.Method, r.Params.Cwd, false, nil, false
			}
			return r.Method, r.Params.Cwd, true, w.WritableRoots, w.NetworkAccess
		}
	}
	t.Fatal("codex session has no thread/start or thread/resume")
	return "", "", false, nil, false
}

func TestRepo_ReefGitRunsNoRepoHooks(t *testing.T) {
	f := newRepoFixture(t, "codex")
	marker := filepath.Join(t.TempDir(), "hook-ran")
	// Only Atoll's own git sets GIT_TERMINAL_PROMPT=0; the fake provider's git commits and pushes
	// run the same hooks and must not count.
	hook := "#!/bin/sh\n[ \"$GIT_TERMINAL_PROMPT\" = 0 ] && echo \"$(basename \"$0\") $PWD\" >> " + marker + "\nexit 0\n"
	hooks := filepath.Join(f.repo.Path, ".git", "hooks")
	if err := os.MkdirAll(hooks, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"post-checkout", "reference-transaction", "post-index-change"} {
		if err := os.WriteFile(filepath.Join(hooks, name), []byte(hook), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	noHook := func(what string) {
		t.Helper()
		if ran, err := os.ReadFile(marker); err == nil {
			t.Errorf("%s ran repo hooks:\n%s", what, ran)
			if err := os.Remove(marker); err != nil {
				t.Fatal(err)
			}
		}
	}

	b := f.bot(t, "codex", "all", nil)
	th := f.thread(t, b)
	ch := f.s.events(t, th)
	var worktree, lastRun string
	for _, step := range []struct{ git, prompt string }{
		{"the thread's clean checkout (git worktree add --detach)", _questionRepoPrompt},
		{"start_work (git worktree add -b)", _fixRepoPrompt},
		{"a run that leaves unsaved work", _unfinishedRepoPrompt},
	} {
		r := f.s.send(t, th, step.prompt)
		lastRun = r.ID
		es := until(t, ch, r.ID, "run_finished")
		if done := f.s.run(t, r.ID); done.Status != "done" {
			t.Fatalf("run %q: %+v", step.prompt, done)
		}
		if step.prompt == _fixRepoPrompt {
			worktree, _ = repoEvent(t, es, "worktree_created").Data["path"].(string)
		}
		noHook(step.git)
	}

	f.s.do(t, "POST", "/api/threads/"+th.ID+"/close", nil, nil, 204)
	assertRemoved(t, ch, lastRun, worktree, "closed")
	noHook("closing a thread with unsaved work (save, remove)")
}

func TestRepo_StartWorkSetsUpstream(t *testing.T) {
	f := newRepoFixture(t, "codex")
	path, _ := f.startWorkReply(t, "codex", _unfinishedRepoPrompt)
	for key, want := range map[string]string{
		"branch.fix/seeded-bug.remote": "origin",
		"branch.fix/seeded-bug.merge":  "refs/heads/fix/seeded-bug",
	} {
		if got := gitIn(t, path, "config", "--default", "", "--get", key); got != want {
			t.Errorf("git config %s in the worktree = %q, want %q", key, got, want)
		}
	}
}
