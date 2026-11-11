package e2e

import (
	"net/http"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// Contract: prompt-contract.md.

const _hey = "hey"

// promptParts names the parts of a system prompt that are present, in the order core, code,
// platform. A part that is only half there fails the test.
func promptParts(t *testing.T, system string) []string {
	t.Helper()
	lines := strings.Split(system, "\n")
	has := func(line string) bool { return slices.Contains(lines, line) }
	var parts []string
	core := []bool{has("How you talk"), has("Memory"), has("Finish")}
	if slices.Contains(core, false) {
		t.Fatalf("shared core incomplete (How you talk, Memory, Finish: %v):\n%s", core, system)
	}
	parts = append(parts, "core")
	work, branch := has("How you work"), strings.Contains(system, "branch <type>/<short-slug>")
	if work != branch {
		t.Fatalf("half a code part (How you work %v, branch rule %v):\n%s", work, branch, system)
	}
	if work {
		parts = append(parts, "code")
	}
	if has("Managing Reef") {
		parts = append(parts, "platform")
	}
	return parts
}

// heySystem sends "hey" in a new thread of b and returns the system prompt the provider got.
func heySystem(t *testing.T, s *server, p string, b bot) string {
	t.Helper()
	th := s.newThread(t, b)
	ch := s.events(t, th)
	r := s.send(t, th, _hey)
	if got := s.finished(t, ch, r.ID); got.Status != "done" || got.Reply != "Hello." {
		t.Fatalf("hey run: %+v", got)
	}
	return lastPrompt(t, s.m, p, "system")
}

func assertParts(t *testing.T, system string, want ...string) {
	t.Helper()
	if got := promptParts(t, system); !slices.Equal(got, want) {
		t.Fatalf("prompt parts %q, want %q:\n%s", got, want, system)
	}
}

func TestPrompt_RepoAccessGetsCodePart(t *testing.T) {
	t.Parallel()
	for _, p := range _providers {
		t.Run(p, func(t *testing.T) {
			_, s := start(t, p)
			repoPath := t.TempDir()
			gitIn(t, repoPath, "init", "-b", "main")
			writeFile(t, filepath.Join(repoPath, "answer.txt"), "41\n")
			gitIn(t, repoPath, "add", "answer.txt")
			gitIn(t, repoPath, "commit", "-m", "seed")
			var repo registeredRepo
			s.do(t, "POST", "/api/repos", map[string]string{"name": "seeded", "path": repoPath}, &repo, http.StatusCreated)
			for _, access := range []any{"all", []string{repo.ID}} {
				var b bot
				s.do(t, "POST", "/api/bots", map[string]any{"name": "Coder", "persona": "Fixes things.", "provider": p, "repo_access": access}, &b, http.StatusCreated)
				assertParts(t, heySystem(t, s, p, b), "core", "code")
			}
		})
	}
}

func TestPrompt_NoReposNoCodePart(t *testing.T) {
	t.Parallel()
	for _, p := range _providers {
		t.Run(p, func(t *testing.T) {
			_, s := start(t, p)
			var b bot
			s.do(t, "POST", "/api/bots", map[string]any{"name": "Talker", "persona": "Answers questions.", "provider": p, "repo_access": []string{}}, &b, http.StatusCreated)
			assertParts(t, heySystem(t, s, p, b), "core")
		})
	}
}

func TestPrompt_ChiefFreshInstall(t *testing.T) {
	t.Parallel()
	for _, p := range _providers {
		t.Run(p, func(t *testing.T) {
			_, s := start(t, p)
			if got := string(s.bot(t, "chief").RepoAccess); got != "[]" {
				t.Fatalf("Chief repo_access %s, want []", got)
			}
			assertParts(t, heySystem(t, s, p, chief(t, s).bot), "core", "platform")
		})
	}
}

func TestPrompt_ChiefLosesRepoAccessOnLaunch(t *testing.T) {
	t.Parallel()
	m, s := start(t, "claude")
	s.kill()
	// The API no longer gives Chief repos, so write the row an older install left: NULL is "all".
	if n := dbExec(t, m, `UPDATE bots SET repo_access = NULL WHERE id = 'chief'`); n != 1 {
		t.Fatalf("set Chief to all repos: %d rows, want 1", n)
	}
	s = m.serve(t)
	if got := string(s.bot(t, "chief").RepoAccess); got != "[]" {
		t.Fatalf("Chief repo_access after restart %s, want []", got)
	}
}

func TestPrompt_PlatformBotWithRepoAccess(t *testing.T) {
	t.Parallel()
	for _, p := range _providers {
		t.Run(p, func(t *testing.T) {
			_, s := start(t, p)
			var b platformBot
			s.do(t, "POST", "/api/bots", map[string]any{"name": "Keeper", "persona": "Keeps the reef.", "provider": p, "repo_access": "all", "can_manage_platform": true}, &b, http.StatusCreated)
			if !b.CanManagePlatform {
				t.Fatalf("POST /api/bots dropped can_manage_platform: %+v", b)
			}
			assertParts(t, heySystem(t, s, p, b.bot), "core", "code", "platform")
		})
	}
}

func TestPrompt_ChiefHeyNoBranchNoWorktree(t *testing.T) {
	t.Parallel()
	for _, p := range _providers {
		t.Run(p, func(t *testing.T) {
			_, s := start(t, p)
			th := s.newThread(t, chief(t, s).bot)
			ch := s.events(t, th)
			r := s.send(t, th, _hey)
			events := until(t, ch, r.ID, "run_finished")
			if got := s.run(t, r.ID); got.Status != "done" || got.Reply != "Hello." || got.Report == nil {
				t.Fatalf("Chief hey run: %+v", got)
			}
			for _, e := range events {
				if e.Type == "worktree_created" {
					t.Fatalf("Chief run created a worktree: %v", e.Data)
				}
			}
			var got struct {
				RepoID   string `json:"repo_id"`
				Branch   string `json:"branch"`
				Worktree string `json:"worktree"`
			}
			s.do(t, "GET", "/api/threads/"+th.ID, nil, &got, http.StatusOK)
			if got.RepoID != "" || got.Branch != "" || got.Worktree != "" {
				t.Fatalf("Chief thread after hey: %+v, want no repo, branch or worktree", got)
			}
		})
	}
}
