package e2e

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"atoll/e2e/fakes/fake"
)

// gitLegRoutes are the routes by which git runs a program the repo names that the git calls below
// can reach: hooks, and core.fsmonitor in the repo's config.
var gitLegRoutes = []struct {
	name  string
	plant func(t *testing.T, p planter)
}{
	{"hooks", func(t *testing.T, p planter) {
		for _, h := range []string{"post-checkout", "reference-transaction", "post-index-change"} {
			writeExec(t, filepath.Join(p.common, "hooks", h), p.record+"exit 0\n")
		}
	}},
	{"fsmonitor-in-config", func(t *testing.T, p planter) {
		gitIn(t, p.repo, "config", "core.fsmonitor", p.script(t, "fsmonitor", p.record+"printf 'token\\0'\n"))
	}},
}

// failSetWorktree makes the database refuse the update that records a thread's worktree, which
// Atoll does right after it made the worktree, so that the call fails with the worktree and its
// branch made.
func failSetWorktree(t *testing.T, m *machine) {
	t.Helper()
	dbExec(t, m, `CREATE TRIGGER planted_no_worktree BEFORE UPDATE OF worktree ON threads WHEN NEW.worktree <> ''
		BEGIN SELECT RAISE(ABORT, 'planted'); END`)
}

func hasBranch(t *testing.T, repo, branch string) bool {
	t.Helper()
	return gitIn(t, repo, "branch", "--list", branch) != ""
}

// failedRun has the thread's Bot make the calls in one run, which must end failed because a call
// errored, and returns the run's error.
func failedRun(t *testing.T, s *server, th thread, ev <-chan event, calls ...fake.MCPCall) string {
	t.Helper()
	payload, _ := json.Marshal(calls)
	r := s.finished(t, ev, s.send(t, th, _mcpPromptHead+string(payload)).ID)
	if r.Status != "failed" {
		t.Fatalf("run %s: status %q, reply %q; want failed", r.ID, r.Status, r.Reply)
	}
	return r.Error
}

// TestRepo_ReefGitCleanupRunsNoRepoCode makes Atoll run DeleteBranch and CreateFrom (and Create's
// worktree add) with the Bot's planted hooks and fsmonitor in the repo, and expects none of the
// programs to run: start_work that fails after it made the worktree, and a delegate whose helper
// is abandoned after its worktree was made. The planted programs record every git but the fake
// provider's. Each git command has a control that runs it with plain git and expects the program
// to run, so that a route that stopped working cannot make a leg pass. Git runs a hook for
// `branch -D` (reference-transaction) but no fsmonitor, and neither a hook nor an fsmonitor for
// `worktree remove`: the hooks legs reach DeleteBranch, the fsmonitor legs the worktree add of
// Create and CreateFrom.
func TestRepo_ReefGitCleanupRunsNoRepoCode(t *testing.T) {
	for _, c := range []struct {
		name, route string
		setup, run  func(t *testing.T, repo, root string)
	}{
		{"branch-D", "hooks", func(t *testing.T, repo, _ string) { gitIn(t, repo, "branch", "fix/x") },
			func(t *testing.T, repo, _ string) { runPlain(t, repo, "branch", "-D", "fix/x") }},
		{"worktree-add", "hooks", nil,
			func(t *testing.T, repo, root string) {
				runPlain(t, repo, "worktree", "add", "--no-track", "-b", "fix/x", filepath.Join(root, "w"), "refs/heads/main")
			}},
		{"worktree-add", "fsmonitor-in-config", nil,
			func(t *testing.T, repo, root string) {
				runPlain(t, repo, "worktree", "add", "--no-track", "-b", "fix/x", filepath.Join(root, "w"), "refs/heads/main")
			}},
	} {
		t.Run("control/"+c.name+"/"+c.route, func(t *testing.T) {
			repo := t.TempDir()
			gitIn(t, repo, "init", "-b", "main")
			writeFile(t, filepath.Join(repo, "answer.txt"), "41\n")
			gitIn(t, repo, "add", "answer.txt")
			gitIn(t, repo, "commit", "-m", "seed")
			root := t.TempDir()
			if c.setup != nil {
				c.setup(t, repo, root)
			}
			p, marker := newPlanter(t, repo)
			for _, r := range gitLegRoutes {
				if r.name == c.route {
					r.plant(t, p)
				}
			}
			c.run(t, repo, root)
			if _, err := os.ReadFile(marker); err != nil {
				t.Errorf("plain git %s did not run the %s route: the legs of this route would pass without testing it", c.name, c.route)
			}
		})
	}

	for _, provider := range _providers {
		for _, route := range gitLegRoutes {
			t.Run(provider+"/start_work-fails/"+route.name, func(t *testing.T) {
				f := newRepoFixture(t, provider)
				p, marker := newPlanter(t, f.repo.Path)
				th := f.thread(t, f.bot(t, provider, "all", nil))
				ev := f.s.events(t, th)
				route.plant(t, p)
				failSetWorktree(t, f.m)

				if got := failedRun(t, f.s, th, ev, startWorkCall(`{"type":"fix","slug":"Seeded BUG"}`, false)); !strings.Contains(got, "start_work: set worktree: constraint failed: planted") {
					t.Fatalf("run error %q, want start_work to fail in set worktree, after the worktree was made", got)
				}
				if ran, err := os.ReadFile(marker); err == nil {
					t.Errorf("start_work's cleanup ran a program planted by the Bot (%s):\n%s", route.name, ran)
				}
				if hasBranch(t, f.repo.Path, "fix/seeded-bug") || worktreeOf(t, f.repo.Path, "fix/seeded-bug") != "" {
					t.Errorf("after the failed start_work the repo still has fix/seeded-bug or its worktree:\n%s", gitIn(t, f.repo.Path, "worktree", "list"))
				}
			})

			t.Run(provider+"/helper-abandoned/"+route.name, func(t *testing.T) {
				f := newRepoFixture(t, provider)
				s := f.s
				poteto, tester := s.newBot(t, "Poteto", provider), s.newBot(t, "Tester", provider)
				setDelegates(t, s, poteto, tester)
				boss := f.thread(t, poteto)
				ev := s.events(t, boss)
				own := bossWorktree(t, s, boss, ev)
				p, marker := newPlanter(t, f.repo.Path)
				route.plant(t, p)
				failSetWorktree(t, f.m)

				if got := failedRun(t, s, boss, ev, delegateCall("Tester", _unfinishedRepoPrompt, "")); !strings.Contains(got, "delegate: set worktree: constraint failed: planted") {
					t.Fatalf("run error %q, want delegate to fail in set worktree, after the helper's worktree was made", got)
				}
				if n := dbCount(t, f.m, "SELECT count(*) FROM threads WHERE parent_thread_id <> ''"); n != 1 {
					t.Errorf("%d helper threads, want 1: delegate failed before it made the helper's worktree", n)
				}
				if ran, err := os.ReadFile(marker); err == nil {
					t.Errorf("abandoning the helper ran a program planted by the Bot (%s):\n%s", route.name, ran)
				}
				if branch := own.Branch + "--tester"; hasBranch(t, f.repo.Path, branch) || worktreeOf(t, f.repo.Path, branch) != "" {
					t.Errorf("after the abandoned delegate the repo still has %s or its worktree:\n%s", branch, gitIn(t, f.repo.Path, "worktree", "list"))
				}
			})
		}
	}
}

// TestRepo_ReefReadOnlyGitRunsNoRepoCode: IsRepo (registering a repo), HasCommit (a thread in it)
// and Origin (start_work's host check) run `rev-parse --show-toplevel`, `rev-parse --verify HEAD`
// and `remote get-url origin`. None runs a hook or reads the index, so no planted route runs a
// program: the control runs them with plain git and expects no program to run. When a git version
// starts to run one, the control fails and these calls need the guard's own leg. The legs run them
// through the API with the routes planted.
func TestRepo_ReefReadOnlyGitRunsNoRepoCode(t *testing.T) {
	t.Run("control", func(t *testing.T) {
		for _, route := range gitLegRoutes {
			t.Run(route.name, func(t *testing.T) {
				repo := t.TempDir()
				gitIn(t, repo, "init", "-b", "main")
				writeFile(t, filepath.Join(repo, "answer.txt"), "41\n")
				gitIn(t, repo, "add", "answer.txt")
				gitIn(t, repo, "commit", "-m", "seed")
				gitIn(t, repo, "remote", "add", "origin", "https://example.test/seeded.git")
				p, marker := newPlanter(t, repo)
				route.plant(t, p)
				runPlain(t, repo, "rev-parse", "--show-toplevel")
				runPlain(t, repo, "rev-parse", "--verify", "--quiet", "HEAD")
				runPlain(t, repo, "remote", "get-url", "origin")
				if ran, err := os.ReadFile(marker); err == nil {
					t.Errorf("plain git ran a program planted by the %s route in a read-only call; IsRepo, HasCommit or Origin now need a leg:\n%s", route.name, ran)
				}
			})
		}
	})

	for _, route := range gitLegRoutes {
		t.Run("codex/"+route.name, func(t *testing.T) {
			f := newRepoFixture(t, "codex")
			p, marker := newPlanter(t, f.repo.Path)
			route.plant(t, p)

			var again registeredRepo
			f.s.do(t, "POST", "/api/repos", map[string]string{"name": "again", "path": f.repo.Path}, &again, 201)
			th := f.thread(t, f.bot(t, "codex", "all", nil))
			ev := f.s.events(t, th)
			if line := useAtoll(t, f.s, th, ev, startWorkCall(`{"type":"fix","slug":"Seeded BUG"}`, false))[0]; !strings.Contains(line, `"path"`) {
				t.Fatalf("start_work %q, want a worktree", line)
			}
			if ran, err := os.ReadFile(marker); err == nil {
				t.Errorf("registering the repo, starting a thread in it and start_work ran a program planted by the Bot (%s):\n%s", route.name, ran)
			}
		})
	}
}

// runPlain runs git in dir with the repo's own configuration and hooks. A planted route may break
// the command; only the marker matters.
func runPlain(t *testing.T, dir string, args ...string) {
	t.Helper()
	_ = exec.CommandContext(t.Context(), "git", append([]string{"-C", dir}, args...)...).Run()
}
