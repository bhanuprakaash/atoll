package e2e

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// Contract: prompt-contract.md → Repo access at every run.

// repoBlock reports whether a first message carries the thread's repo block.
func repoBlock(message, repoID string) bool {
	return slices.ContainsFunc(strings.Split(message, "\n"), func(l string) bool {
		return strings.HasPrefix(l, "Repo: ")
	}) || strings.Contains(message, repoID)
}

func TestRepo_LostAccessRunsWithoutRepo(t *testing.T) {
	for _, p := range _providers {
		t.Run(p, func(t *testing.T) {
			f := newRepoFixture(t, p)
			lost := f.bot(t, p, []string{f.repo.ID}, nil)
			kept := f.bot(t, p, "all", nil)
			lostThread, keptThread := f.thread(t, lost), f.thread(t, kept)
			f.s.do(t, "PUT", "/api/bots/"+lost.ID, map[string]any{"repo_access": []string{}}, nil, 200)

			for _, c := range []struct {
				th   thread
				repo bool
			}{{lostThread, false}, {keptThread, true}} {
				ch := f.s.events(t, c.th)
				r := f.s.send(t, c.th, _hey)
				es := until(t, ch, r.ID, "run_finished")
				if got := f.s.run(t, r.ID); got.Status != "done" || got.Reply != "Hello." {
					t.Fatalf("hey in a repo thread (access %v): %+v", c.repo, got)
				}
				first := lastPrompt(t, f.m, p, "user")
				if got := repoBlock(first, f.repo.ID); got != c.repo {
					t.Errorf("repo block in the first message %v, want %v (access %v):\n%s", got, c.repo, c.repo, first)
				}
				// The thread folder holds a checkout of the repo only while the Bot has access.
				_, err := os.Stat(filepath.Join(f.m.data, "threads", c.th.ID, "answer.txt"))
				if got := err == nil; got != c.repo {
					t.Errorf("thread folder has the repo's answer.txt %v, want %v (access %v)", got, c.repo, c.repo)
				}
				for _, e := range es {
					if e.Type == "worktree_created" {
						t.Errorf("run created a worktree (access %v): %v", c.repo, e.Data)
					}
				}
			}
			if entries, _ := os.ReadDir(f.root); len(entries) != 0 {
				t.Errorf("worktree root not empty: %v", entries)
			}
		})
	}
}
