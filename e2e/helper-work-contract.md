# Helper code work: test contract

Extends [delegation-contract.md](delegation-contract.md), [wake-contract.md](wake-contract.md) and
the worktree contract in [README.md](README.md). Tests: `helper_work_test.go`. They build the real
server; no product package is imported.

## Boss with a worktree

A boss thread has a worktree once it called `start_work`. Its `delegate` makes the helper's worktree
at once, under the worktree root, on a new branch `<boss-branch>--<helper Bot name>` (the name
lowercased and slugged as `start_work` slugs: `fix/seeded-bug--tester`) at the boss branch's last
commit. The boss's uncommitted changes are not in it. The delegate result holds `thread_id`,
`branch` (that branch) and `note`, exactly `Uncommitted changes in your worktree are not included.`
A `worktree_created` event on the helper thread is allowed, not required.

The helper's first message has, after the handoff line and before the brief, the line
`Work on branch {branch} and commit there. Do not push or open a PR: {Boss} merges it.` Its
`start_work` (any type and slug) returns that worktree's `path` and `branch`, and needs no GitHub
connection on a GitHub repo. The branch lives in the registered repo, so `git merge {branch}` in
the boss's worktree brings in what the helper committed. The message that wakes the boss when the
helper ends `done` also holds `Merge {branch} into your branch.`

## Boss without a worktree

A repo thread that never called `start_work`, or a thread with no repo that names `repo` (Chief),
gets no helper worktree at `delegate`, and the result has no `branch` or `note` (absent or empty).
The helper's `start_work` and `track_mr` work as today: a `<type>/<slug>` branch from origin/HEAD
under the root, and on a GitHub repo the tool error `… give this Bot the GitHub connection` without
one.

## Removal

Closing the boss's thread removes its helpers' worktrees; the boss's MR turning `merged` (GitLab,
via the fake `glab`'s `mr-state`) removes the boss's worktree and its helpers'. Each removal saves
first: `worktree_removed` on the helper thread's stream with `reason` `closed` or `merged` and
`snapshot_ref` `refs/atoll/unfinished/<helper thread id>`, holding its tracked and untracked edits.

## Failed delegate

When delegate fails after the helper's worktree is made, Reef removes the worktree and its branch,
but not one that has been swapped for a symlink to another worktree, or whose folder is under a
symlink: it refuses, as for a thread's worktree, and the other worktree and its files survive. The
test swaps the path from a `git` on the server's PATH as `git worktree add` ends, and makes the
helper's title update fail with a database trigger.

## Fakes

No fake code. The boss calls `start_work` (and `track_mr` with the fake MR URL) through the `mcp-use`
scenario; the test commits and edits in the boss's worktree itself. The helper's brief is a repo
scenario's prompt, `\u`-escaped as in the delegation tests: `repo-uncommitted` (start_work, edits
`answer.txt` to `42` and adds `unfinished.txt`, no commit), `repo-fix` (start_work, commit, push,
`glab mr create`, `track_mr`) or `repo-github-fix` (replies with start_work's error). No scenario
commits without pushing, so the test makes the helper's commit in the helper's worktree before the
boss's merge.

## Acceptance mapping

| Acceptance                                                                                            | Tests                                                                                                             |
| ----------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------- |
| Boss with a worktree: helper worktree at once on `<boss-branch>--<bot>`, start_work returns it, note  | `TestHelperWork_BranchesOffTheBoss/{claude,codex}`                                                                |
| No GitHub connection on the boss's branch; `git merge <helper-branch>` brings its work in             | `TestHelperWork_GitHubConnectionOnlyOwnBranch/{claude,codex}`, `TestHelperWork_BranchesOffTheBoss/{claude,codex}` |
| Helper told not to push or open a PR; the wake names the branch to merge                              | `TestHelperWork_BranchesOffTheBoss/{claude,codex}`                                                                |
| Boss without a worktree, or Chief: start_work from origin/HEAD, track_mr; GitHub needs the connection | `TestHelperWork_NoBossBranch/{claude,codex}`, `TestHelperWork_GitHubConnectionOnlyOwnBranch/{claude,codex}`       |
| Closing the boss's thread or its MR merging removes helpers' worktrees, saving first                  | `TestHelperWork_CloseOrMergeRemovesHelpers/{claude,codex}`                                                        |

`TestHelperWork_FailedDelegateKeepsOtherWorktrees/{claude,codex}/{leaf,parent}` covers a failed
delegate (leaf symlink; symlink in a parent folder).

Not covered here: a helper's own commit (the test commits for it), merge conflicts, the helper's
worktree when the boss's MR merges on GitHub (not polled), and the wake text when the helper fails.
