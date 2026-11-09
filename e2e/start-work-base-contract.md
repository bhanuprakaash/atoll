# start_work base: test contract

Extends the worktree contract in [README.md](README.md) and [helper-work-contract.md](helper-work-contract.md).
Tests: `repo_start_work_branch_base_test.go`. They build the real server; no product package is imported.

## base

`start_work` takes an optional `base`: a plain branch name that exists on origin, such as `test/x`
(the server never fetches, so `origin/<base>` is already in the repo). The new branch `<type>/<slug>`
starts at `origin/<base>`. Reef sets the branch's upstream, as it does with no `base`
(`branch.<b>.remote` = `origin`, `branch.<b>.merge` = `refs/heads/<b>`); the Bot's `.git/config` stays
read-only. The reply's `base` names the base and its short commit. With no `base`, `start_work`
behaves as before.

A `base` that is not a branch on origin, or is not a plain branch name (contains `..`, starts with
`-`, a full commit id, `origin/x`, or has a space), is a tool error that names the valid form: a
branch on origin, for example `test/x`. It makes no worktree and no branch. The tool schema Bots
read declares `base`.

## Fakes

The `mcp-use` scenario calls `start_work`. Its call takes `"schema": true`: the fake then sends only
arguments the tool's schema declares and answers an `Error:` line for any other. The fake Codex
enforces no sandbox, so on Codex Auto the test covers Reef's side (the worktree on the base's
commit); the commit and push run unsandboxed and show the branch is pushable. Real Codex Auto proof
belongs to the fix PR.

## Acceptance mapping

| Acceptance                                                                                       | Tests                                                         |
| ------------------------------------------------------------------------------------------------ | ------------------------------------------------------------- |
| New branch starts at `origin/<base>`, upstream set by Reef, reply names the base and short commit | `TestRepo_StartWorkBranchesFromBase/{claude,codex,codex/auto}` |
| Unknown or malformed `base` refused with the valid form named, no worktree or branch made         | `TestRepo_StartWorkRefusesBadBase/{claude,codex,codex/auto}`   |
| The tool schema declares `base`                                                                  | `TestRepo_StartWorkDeclaresBase/{claude,codex}`               |

Not covered here: the Bot prompt's wording about `base`, and a real Codex on Auto.
