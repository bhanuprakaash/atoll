package e2e

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"atoll/e2e/fakes/fake"
)

// Contract: helper-work-contract.md → Failed delegate.

// TestHelperWork_FailedDelegateKeepsOtherWorktrees: a delegate fails right after the helper's
// worktree is made, and what a Bot with the worktree root writable could plant is planted at that
// moment: the helper's path swapped for a symlink to a worktree the user made outside the root
// (leaf), or the folder holding it swapped for a symlink to a folder holding that worktree at the
// same name (parent). Reef must refuse to remove it, as it does for a thread's worktree.
//
// A git on the server's PATH does the swap when `git worktree add` for the helper's branch has
// finished, and a trigger on the database makes the helper's title update fail: the first
// failure startHelper can hit after the worktree exists, with no place for the test to race.
func TestHelperWork_FailedDelegateKeepsOtherWorktrees(t *testing.T) {
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range _providers {
		for _, kind := range []string{"leaf", "parent"} {
			t.Run(p+"/"+kind, func(t *testing.T) {
				f := newRepoFixture(t, p)
				s := f.s
				poteto, tester := s.newBot(t, "Poteto", p), s.newBot(t, "Tester", p)
				setDelegates(t, s, poteto, tester)
				boss := f.thread(t, poteto)
				ev := s.events(t, boss)
				own := bossWorktree(t, s, boss, ev)

				helperBranch := own.Branch + "--tester"
				name := strings.ReplaceAll(helperBranch, "/", "-")
				helperPath := filepath.Join(f.root, f.repo.Name, name)
				outside, user := t.TempDir(), ""
				// The helper's git entry is pointed at nothing, as in TestRepo_CloseKeepsOtherWorktrees,
				// so that git no longer finds the entry pointing back and removes the folder it resolves to.
				swap := fmt.Sprintf("for g in %q/worktrees/*/gitdir; do [ \"$(cat \"$g\")\" = %q/.git ] && echo /nonexistent/.git >\"$g\"; done\n",
					filepath.Join(f.repo.Path, ".git"), helperPath)
				switch kind {
				case "leaf":
					user = filepath.Join(outside, "user-worktree")
					swap += fmt.Sprintf("mv %[1]q %[1]q.moved\nln -s %[2]q %[1]q\n", helperPath, user)
				case "parent":
					user = filepath.Join(outside, name)
					folder := filepath.Join(f.root, f.repo.Name)
					swap += fmt.Sprintf("mv %[1]q %[1]q.moved\nln -s %[2]q %[1]q\n"+
						"for d in %[1]q.moved/*; do [ \"${d##*/}\" = %[3]q ] || ln -s \"$d\" %[2]q/\"${d##*/}\"; done\n",
						folder, outside, name)
				}
				gitIn(t, f.repo.Path, "worktree", "add", "-b", "user/work", user, "HEAD")
				precious := filepath.Join(user, "precious.txt")
				writeFile(t, precious, "uncommitted\n")

				swapped := filepath.Join(t.TempDir(), "swapped")
				writeFile(t, filepath.Join(f.m.bin, "git"), fmt.Sprintf(`#!/bin/bash
%[1]q "$@" || exit $?
args=("$@")
n=${#args[@]}
for ((i = 0; i + 1 < n; i++)); do
	if [ "${args[i]}" = worktree ] && [ "${args[i+1]}" = add ] && [ "${args[n-2]}" = %[2]q ]; then
		%[3]s
		: >%[4]q
	fi
done
`, realGit, helperPath, swap, swapped))
				if err := os.Chmod(filepath.Join(f.m.bin, "git"), 0o755); err != nil {
					t.Fatal(err)
				}
				dbExec(t, f.m, "CREATE TRIGGER fail_helper_title BEFORE UPDATE OF title ON threads "+
					"WHEN OLD.parent_thread_id != '' BEGIN SELECT RAISE(ABORT, 'no title'); END")

				calls, _ := json.Marshal([]fake.MCPCall{delegateCall("Tester", _unfinishedRepoPrompt, "")})
				r := s.finished(t, ev, s.send(t, boss, _mcpPromptHead+string(calls)).ID)

				if _, err := os.Stat(swapped); err != nil {
					t.Fatalf("the helper's path was never swapped, so the test planted nothing: %v", err)
				}
				if r.Status != "failed" || !strings.Contains(r.Error, "no title") {
					t.Fatalf("run %s: status %q, error %q; want the delegate to fail on the helper's title, so the helper is abandoned", r.ID, r.Status, r.Error)
				}
				if b, err := os.ReadFile(precious); err != nil || string(b) != "uncommitted\n" {
					t.Errorf("after the failed delegate, the user's worktree file reads %q (error %v), want it intact", b, err)
				}
				if got := worktreeOf(t, f.repo.Path, "user/work"); got != user {
					t.Errorf("after the failed delegate, user/work is checked out at %q, want the user's worktree %q", got, user)
				}
			})
		}
	}
}
