package e2e

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// legacySlashRepo is a fixture whose repo carries the name `org/proj`, as one registered before
// the API refused slashes. The old database is made by renaming the row while no server runs.
func legacySlashRepo(t *testing.T, provider string) *repoFixture {
	t.Helper()
	f := newRepoFixture(t, provider)
	f.s.kill()
	dbExec(t, f.m, `UPDATE repos SET name = 'org/proj' WHERE id = '`+f.repo.ID+`'`)
	f.s = f.m.serve(t)
	return f
}

func TestRepo_SlashNamedRepoCloses(t *testing.T) {
	for _, provider := range []string{"claude", "codex"} {
		t.Run(provider, func(t *testing.T) {
			f := legacySlashRepo(t, provider)
			b := f.bot(t, provider, "all", nil)
			th, path, ch, runID := f.work(t, b, _unfinishedRepoPrompt, "fix/seeded-bug")
			if want := filepath.Join(f.root, "org", "proj"); filepath.Dir(path) != want {
				t.Fatalf("worktree %q is not under %q: the repo name was not used as a path", path, want)
			}
			f.s.do(t, "POST", "/api/threads/"+th.ID+"/close", nil, nil, http.StatusNoContent)
			assertRemoved(t, ch, runID, path, "closed")
		})
	}
}

// TestRepo_SlashNamedRepoCloseKeepsOtherWorktrees plants what a Bot that can write the whole
// worktree root can: the folder <root>/org swapped for a symlink to the parent of a worktree the
// user made, laid out so that <root>/org/proj/<folder> is that worktree.
func TestRepo_SlashNamedRepoCloseKeepsOtherWorktrees(t *testing.T) {
	for _, provider := range []string{"claude", "codex"} {
		t.Run(provider, func(t *testing.T) {
			f := legacySlashRepo(t, provider)
			b := f.bot(t, provider, "all", nil)
			th, path, _, _ := f.work(t, b, _unfinishedRepoPrompt, "fix/seeded-bug")

			elsewhere := t.TempDir()
			user := filepath.Join(elsewhere, "proj", filepath.Base(path))
			gitIn(t, f.repo.Path, "worktree", "add", "-b", "user/work", user, "HEAD")
			precious := filepath.Join(user, "precious.txt")
			writeFile(t, precious, "uncommitted\n")

			// With the entry's gitdir file pointing at nothing, git finds the folder by path and
			// resolves the symlink when asked to remove it.
			entries, err := filepath.Glob(filepath.Join(f.repo.Path, ".git", "worktrees", "*", "gitdir"))
			if err != nil {
				t.Fatal(err)
			}
			for _, gitdir := range entries {
				if b, err := os.ReadFile(gitdir); err == nil && strings.TrimSpace(string(b)) == filepath.Join(path, ".git") {
					writeFile(t, gitdir, "/nonexistent/.git\n")
				}
			}
			org := filepath.Join(f.root, "org")
			if err := os.Rename(org, org+".moved"); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(elsewhere, org); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(path); err != nil {
				t.Fatalf("the worktree path %q does not reach the user's worktree: the plant does not work: %v", path, err)
			}

			req, err := http.NewRequestWithContext(t.Context(), "POST", f.s.url+"/api/threads/"+th.ID+"/close", nil)
			if err != nil {
				t.Fatal(err)
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusNoContent {
				t.Errorf("close answered %d, want it refused: <root>/org is a symlink", resp.StatusCode)
			}
			if _, err := os.Stat(precious); err != nil {
				t.Errorf("closing the thread removed the user's worktree: %v", err)
			}
		})
	}
}
