package e2e

import (
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
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
			fixed := []string{f.root, filepath.Join(common, "objects"), filepath.Join(common, "refs"), filepath.Join(common, "logs"), filepath.Join(common, "worktrees")}
			for i, c := range sessions {
				checkMode(t, "codex", mode, c)
				method, cwd, present, roots, network := codexWorkspaceWrite(t, c)
				if want := []string{"thread/start", "thread/resume"}[i]; method != want {
					t.Fatalf("session %d began with %q, want %s", i+1, method, want)
				}
				if mode == "auto" {
					wantRoots := resolved(t, append(fixed, gitIn(t, cwd, "rev-parse", "--absolute-git-dir"))...)
					if len(wantRoots) != 6 {
						t.Fatalf("%s ran in %q, want a linked worktree with its own git dir", method, cwd)
					}
					wantRoots = append(wantRoots, resolveLoose(t, filepath.Join(f.m.data, "cache", b.ID)))
					slices.Sort(wantRoots)
					if i == 1 && !strings.HasPrefix(resolved(t, cwd)[0], resolved(t, f.root)[0]+string(filepath.Separator)) {
						t.Fatalf("%s ran in %q, want the thread's worktree under %q", method, cwd, f.root)
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

// TestRepo_ReefGitRunsNoRepoCode plants, the way a Bot that can write in the repo would, each route
// by which git runs a program the repo names, then makes Atoll run its own git (the thread's clean
// checkout, start_work, a close that saves unsaved work) and expects none of the programs to run.
// The planted programs record every git but the fake provider's, which sets GIT_AUTHOR_NAME to
// "Fixture Bot" and runs the same programs. Atoll's git is one function, so the provider does not
// matter to it. Each route has a control that runs the same operations with plain git and expects
// the program to run, so that a route or a marker that stopped working cannot make a leg pass.
func TestRepo_ReefGitRunsNoRepoCode(t *testing.T) {
	routes := []struct {
		name string
		// early plants before the first run, late after the run that leaves unsaved work.
		early func(t *testing.T, p planter)
		late  func(t *testing.T, p planter, worktree string)
		// snapshotOnly routes run a program only where Atoll stages the Bot's tree, so nothing is
		// asserted before the close: the checkout's own filters are the user's.
		snapshotOnly bool
		// anyClose accepts a refused close: git will not remove a worktree whose .git file no
		// longer points at its registered git dir.
		anyClose bool
	}{
		{name: "hooks", early: func(t *testing.T, p planter) {
			for _, h := range []string{"post-checkout", "reference-transaction", "post-index-change"} {
				writeExec(t, filepath.Join(p.common, "hooks", h), p.record+"exit 0\n")
			}
		}},
		{name: "fsmonitor-in-config", early: func(t *testing.T, p planter) {
			gitIn(t, p.repo, "config", "core.fsmonitor", p.script(t, "fsmonitor", p.record+"printf 'token\\0'\n"))
		}},
		{name: "filter-driver", snapshotOnly: true, early: func(t *testing.T, p planter) {
			gitIn(t, p.repo, "config", "filter.x.clean", p.script(t, "clean", p.record+"cat\n"))
			gitIn(t, p.repo, "config", "filter.x.smudge", "cat")
			writeFile(t, filepath.Join(p.common, "info", "attributes"), "* filter=x\n")
		}},
		{name: "filter-name-with-equals", snapshotOnly: true, early: func(t *testing.T, p planter) {
			gitIn(t, p.repo, "config", "filter.x=y.clean", p.script(t, "clean", p.record+"cat\n"))
			gitIn(t, p.repo, "config", "filter.x=y.smudge", "cat")
			writeFile(t, filepath.Join(p.common, "info", "attributes"), "* filter=x=y\n")
		}},
		{name: "commondir-rewritten", late: func(t *testing.T, p planter, worktree string) {
			writeFile(t, filepath.Join(p.gitDir(t, worktree), "commondir"), p.evilCommon(t)+"\n")
		}},
		{name: "gitfile-rewritten", anyClose: true, late: func(t *testing.T, p planter, worktree string) {
			evil := filepath.Join(t.TempDir(), "gitdir")
			copyDir(t, p.gitDir(t, worktree), evil)
			writeFile(t, filepath.Join(evil, "commondir"), p.evilCommon(t)+"\n")
			writeFile(t, filepath.Join(worktree, ".git"), "gitdir: "+evil+"\n")
		}},
		{name: "worktree-config", late: func(t *testing.T, p planter, worktree string) {
			gitIn(t, p.repo, "config", "extensions.worktreeConfig", "true")
			gitIn(t, p.repo, "config", "-f", filepath.Join(p.gitDir(t, worktree), "config.worktree"), "core.fsmonitor", p.script(t, "fsmonitor", p.record+"printf 'token\\0'\n"))
		}},
	}
	for _, route := range routes {
		t.Run("control/"+route.name, func(t *testing.T) {
			repo := t.TempDir()
			gitIn(t, repo, "init", "-b", "main")
			writeFile(t, filepath.Join(repo, "answer.txt"), "41\n")
			gitIn(t, repo, "add", "answer.txt")
			gitIn(t, repo, "commit", "-m", "seed")
			p, marker := newPlanter(t, repo)
			if route.early != nil {
				route.early(t, p)
			}
			plain := func(dir string, env []string, args ...string) {
				t.Helper()
				cmd := exec.CommandContext(t.Context(), "git", append([]string{"-C", dir}, args...)...)
				cmd.Env = append(os.Environ(), env...)
				_ = cmd.Run() // a planted route may break the command; only the marker matters
			}
			root := t.TempDir()
			plain(repo, nil, "worktree", "add", "--detach", filepath.Join(root, "clean"), "HEAD")
			worktree := filepath.Join(root, "work")
			plain(repo, nil, "worktree", "add", "--no-track", "-b", "fix/x", worktree, "HEAD")
			writeFile(t, filepath.Join(worktree, "answer.txt"), "42\n")
			writeFile(t, filepath.Join(worktree, "unfinished.txt"), "keep\n")
			if route.late != nil {
				route.late(t, p, worktree)
			}
			index := []string{"GIT_INDEX_FILE=" + filepath.Join(t.TempDir(), "index"), "GIT_AUTHOR_NAME=Atoll", "GIT_AUTHOR_EMAIL=a@x", "GIT_COMMITTER_NAME=Atoll", "GIT_COMMITTER_EMAIL=a@x"}
			plain(worktree, nil, "status", "--porcelain")
			plain(worktree, index, "add", "-A")
			plain(worktree, index, "write-tree")
			plain(worktree, nil, "update-ref", "refs/atoll/control", "HEAD")
			plain(repo, nil, "worktree", "remove", "--force", worktree)
			if _, err := os.ReadFile(marker); err != nil {
				t.Errorf("plain git did not run the %s route: the legs of this route would pass without testing anything", route.name)
			}
		})
	}
	for _, c := range []struct{ provider, end string }{{"codex", "close"}, {"codex", "delete"}, {"claude", "close"}} {
		provider := c.provider
		for _, route := range routes {
			t.Run(c.provider+"/"+c.end+"/"+route.name, func(t *testing.T) {
				f := newRepoFixture(t, provider)
				p, marker := newPlanter(t, f.repo.Path)
				noRun := func(what string) {
					t.Helper()
					if ran, err := os.ReadFile(marker); err == nil {
						t.Errorf("%s ran a program planted by the Bot (%s):\n%s", what, route.name, ran)
						if err := os.Remove(marker); err != nil {
							t.Fatal(err)
						}
					}
				}
				if route.early != nil {
					route.early(t, p)
				}

				b := f.bot(t, provider, "all", nil)
				th := f.thread(t, b)
				ch := f.s.events(t, th)
				var worktree string
				for _, step := range []struct{ git, prompt string }{
					{"the thread's clean checkout", _questionRepoPrompt},
					{"start_work", _fixRepoPrompt},
					{"a run that leaves unsaved work", _unfinishedRepoPrompt},
				} {
					r := f.s.send(t, th, step.prompt)
					es := until(t, ch, r.ID, "run_finished")
					if done := f.s.run(t, r.ID); done.Status != "done" {
						t.Fatalf("run %q: %+v", step.prompt, done)
					}
					if step.prompt == _fixRepoPrompt {
						worktree, _ = repoEvent(t, es, "worktree_created").Data["path"].(string)
					}
					if route.snapshotOnly {
						_ = os.Remove(marker)
						continue
					}
					noRun(step.git)
				}
				if route.late != nil {
					route.late(t, p, worktree)
				}

				method, path, want := "POST", "/api/threads/"+th.ID+"/close", "closing a thread with unsaved work"
				if c.end == "delete" {
					method, path, want = "DELETE", "/api/bots/"+b.ID, "deleting the Bot"
				}
				defer noRun(want)
				req, err := http.NewRequestWithContext(t.Context(), method, f.s.url+path, nil)
				if err != nil {
					t.Fatal(err)
				}
				resp, err := http.DefaultClient.Do(req)
				if err != nil {
					t.Fatal(err)
				}
				_ = resp.Body.Close()
				if resp.StatusCode != 204 && !route.anyClose {
					t.Errorf("%s: status %d, want 204", want, resp.StatusCode)
				}
				ref := "refs/atoll/unfinished/" + th.ID
				if err := exec.CommandContext(t.Context(), "git", "-C", f.repo.Path, "rev-parse", "--verify", "--quiet", ref+"^{commit}").Run(); err != nil {
					t.Errorf("%s: the repo has no %s; Atoll's git did not work on the repo's own git dir", want, ref)
				}
			})
		}
	}
}

// newPlanter is the planter for repo and the marker file its programs write to.
func newPlanter(t *testing.T, repo string) (planter, string) {
	t.Helper()
	marker := filepath.Join(t.TempDir(), "ran")
	p := planter{repo: repo, common: filepath.Join(repo, ".git"), dir: t.TempDir()}
	p.record = "#!/bin/sh\n[ \"$GIT_AUTHOR_NAME\" != \"Fixture Bot\" ] && echo \"$0 $PWD\" >> " + marker + "\n"
	return p, marker
}

// planter holds what a route needs to plant: the repo, its git common dir, a scratch folder, and
// the start of a script that records a call, but not the fake provider's, in the marker file.
type planter struct{ repo, common, dir, record string }

// script writes an executable named name that runs body, and returns its path.
func (p planter) script(t *testing.T, name, body string) string {
	t.Helper()
	path := filepath.Join(p.dir, name)
	writeExec(t, path, body)
	return path
}

// gitDir is the git dir of the linked worktree.
func (p planter) gitDir(t *testing.T, worktree string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(worktree, ".git"))
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(strings.TrimPrefix(string(b), "gitdir: "))
}

// evilCommon is a copy of the git common dir whose config runs the script as core.fsmonitor.
func (p planter) evilCommon(t *testing.T) string {
	t.Helper()
	evil := filepath.Join(t.TempDir(), "common")
	copyDir(t, p.common, evil)
	gitIn(t, p.repo, "config", "-f", filepath.Join(evil, "config"), "core.fsmonitor", p.script(t, "fsmonitor", p.record+"printf 'token\\0'\n"))
	return evil
}

func writeExec(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
}

func copyDir(t *testing.T, from, to string) {
	t.Helper()
	if err := os.MkdirAll(to, 0o755); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.CommandContext(t.Context(), "cp", "-R", from+"/.", to).CombinedOutput(); err != nil {
		t.Fatalf("copy %s: %v: %s", from, err, out)
	}
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

// TestRepo_CloseKeepsOtherWorktrees plants what a Bot with the worktree root and <common>/worktrees
// writable can: its own worktree folder swapped for a symlink to another linked worktree of the
// repo, one the user made outside the root, and its own entry's gitdir file pointed at nothing, so
// that git no longer finds the entry pointing back. Git then resolves the symlink when asked to
// remove the folder and removes the user's worktree. The control runs that removal with plain git.
func TestRepo_CloseKeepsOtherWorktrees(t *testing.T) {
	plant := func(t *testing.T, repo, worktree, user string) {
		t.Helper()
		entries, err := filepath.Glob(filepath.Join(repo, ".git", "worktrees", "*", "gitdir"))
		if err != nil {
			t.Fatal(err)
		}
		for _, gitdir := range entries {
			if b, err := os.ReadFile(gitdir); err == nil && strings.TrimSpace(string(b)) == filepath.Join(worktree, ".git") {
				writeFile(t, gitdir, "/nonexistent/.git\n")
			}
		}
		if err := os.Rename(worktree, worktree+".moved"); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(user, worktree); err != nil {
			t.Fatal(err)
		}
	}
	userWorktree := func(t *testing.T, repo string) string {
		t.Helper()
		user := filepath.Join(t.TempDir(), "user-worktree")
		gitIn(t, repo, "worktree", "add", "-b", "user/work", user, "HEAD")
		writeFile(t, filepath.Join(user, "precious.txt"), "uncommitted\n")
		return user
	}

	t.Run("control", func(t *testing.T) {
		repo := t.TempDir()
		gitIn(t, repo, "init", "-b", "main")
		writeFile(t, filepath.Join(repo, "answer.txt"), "41\n")
		gitIn(t, repo, "add", "answer.txt")
		gitIn(t, repo, "commit", "-m", "seed")
		user := userWorktree(t, repo)
		worktree := filepath.Join(t.TempDir(), "thread-worktree")
		gitIn(t, repo, "worktree", "add", "--no-track", "-b", "fix/x", worktree, "HEAD")
		plant(t, repo, worktree, user)
		_ = exec.CommandContext(t.Context(), "git", "-C", repo, "worktree", "remove", "--force", worktree).Run()
		if _, err := os.Stat(filepath.Join(user, "precious.txt")); err == nil {
			t.Error("plain git kept the user's worktree: the plant does not work, so the close below would pass without testing anything")
		}
	})

	for _, provider := range []string{"codex", "claude"} {
		t.Run(provider+"/close", func(t *testing.T) {
			f := newRepoFixture(t, provider)
			user := userWorktree(t, f.repo.Path)
			b := f.bot(t, provider, "all", nil)
			th := f.thread(t, b)
			ch := f.s.events(t, th)
			r := f.s.send(t, th, _unfinishedRepoPrompt)
			es := until(t, ch, r.ID, "run_finished")
			if done := f.s.run(t, r.ID); done.Status != "done" {
				t.Fatalf("run: %+v", done)
			}
			worktree, _ := repoEvent(t, es, "worktree_created").Data["path"].(string)
			plant(t, f.repo.Path, worktree, user)

			// A refused close is fine; removing the user's worktree is not.
			req, err := http.NewRequestWithContext(t.Context(), "POST", f.s.url+"/api/threads/"+th.ID+"/close", nil)
			if err != nil {
				t.Fatal(err)
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			_ = resp.Body.Close()
			if _, err := os.Stat(filepath.Join(user, "precious.txt")); err != nil {
				t.Errorf("closing the thread removed the user's worktree: %v", err)
			}
		})
	}
}

// TestRepo_UserFilterThatMustRunStillWorks has a repo with a Git LFS-style filter: the user's own
// config defines it as required, the repo's .gitattributes applies it. Atoll's checkouts must still
// be made; the checkout's smudge may run, it is the user's own filter.
func TestRepo_UserFilterThatMustRunStillWorks(t *testing.T) {
	f := newRepoFixture(t, "codex")
	writeFile(t, filepath.Join(f.repo.Path, ".gitattributes"), "*.bin filter=lfs\n")
	writeFile(t, filepath.Join(f.repo.Path, "big.bin"), "content\n")
	gitIn(t, f.repo.Path, "add", ".gitattributes", "big.bin")
	gitIn(t, f.repo.Path, "commit", "-m", "add a binary")
	gitIn(t, f.repo.Path, "push", "origin", "main")
	writeFile(t, filepath.Join(f.m.home, ".gitconfig"), "[filter \"lfs\"]\n\tclean = cat\n\tsmudge = cat\n\trequired = true\n")

	b := f.bot(t, "codex", "all", nil)
	th := f.thread(t, b)
	ch := f.s.events(t, th)
	for _, prompt := range []string{_questionRepoPrompt, _unfinishedRepoPrompt} {
		r := f.s.send(t, th, prompt)
		es := until(t, ch, r.ID, "run_finished")
		if done := f.s.run(t, r.ID); done.Status != "done" {
			t.Fatalf("run %q: %+v", prompt, done)
		}
		if prompt == _unfinishedRepoPrompt {
			worktree, _ := repoEvent(t, es, "worktree_created").Data["path"].(string)
			if _, err := os.Stat(filepath.Join(worktree, "big.bin")); err != nil {
				t.Errorf("start_work made a worktree without big.bin: %v", err)
			}
		}
	}
}
