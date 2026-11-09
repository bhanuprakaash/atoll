# start_work base: test contract

Extends the worktree contract in [README.md](README.md) and [helper-work-contract.md](helper-work-contract.md).
Tests: `repo_start_work_branch_base_test.go`. They build the real server; no product package is imported.

## base

`start_work` takes an optional `base`: a plain branch name that exists on origin, such as `test/x`
(the server never fetches, so `origin/<base>` is already in the repo). The new branch `<type>/<slug>`
starts at `origin/<base>`, also when a tag has the same name. Reef sets the branch's upstream, as it
does with no `base` (`branch.<b>.remote` = `origin`, `branch.<b>.merge` = `refs/heads/<b>`, in the
repo's own config); the Bot's `.git/config` stays read-only. The reply's `base` names the base and
its short commit. With no `base`, or `"base":""`, `start_work` behaves as before: the branch starts
at `origin/HEAD`.

A `base` that is not a branch on origin, or is not a plain branch name, is refused, as a tool error
or a failed call, with text that says a base is a branch on origin and gives a `<type>/<slug>`-style
example (no particular example is pinned). It makes no worktree, branch or folder. Refused: an
unknown name, a branch only the local repo has, a tag, `origin/x`, a full commit id, `refs/heads/x`,
`HEAD`, `@`, `..`, a leading `-`, a space, `^`, `~`, `:`, `@{`, a `.lock` suffix, and the new
branch's own name. The tool schema Bots read declares `base`.

## type

`start_work` accepts the type `test` (branch `test/<slug>`) beside feat, fix, chore and docs. Any
other type is refused naming all five, and makes nothing.

## Fakes

The `mcp-use` scenario calls `start_work`. Its call takes `"schema": true`: the fake then sends only
arguments the tool's schema declares and answers an `Error:` line for any other. The fake Codex
enforces no sandbox, so on Codex Auto the test covers Reef's side (the worktree on the base's
commit); the commit and push run unsandboxed and show the branch is pushable. Real Codex Auto proof
belongs to the fix PR.

## Acceptance mapping

| Acceptance                                                                            | Tests                                                          |
| ------------------------------------------------------------------------------------- | -------------------------------------------------------------- |
| New branch starts at `origin/<base>`, repo-config upstream set by Reef, reply names it | `TestRepo_StartWorkBranchesFromBase/{claude,codex,codex/auto}` |
| A tag named like the base does not stand in for it                                    | `TestRepo_StartWorkBaseBeatsTag`                               |
| Bad `base` refused with the valid form named, nothing made (one subtest per input)    | `TestRepo_StartWorkRefusesBadBase/{claude,codex}/<input>`      |
| `"base":""` is no base                                                                | `TestRepo_StartWorkEmptyBaseIsNoBase/{claude,codex}`           |
| The tool schema declares `base`                                                       | `TestRepo_StartWorkDeclaresBase/{claude,codex}`                |
| Type `test` makes `test/<slug>`; an unknown type is refused naming the valid types    | `TestRepo_StartWorkTypes/{claude,codex}/{test,unknown}`        |

Not covered here: the Bot prompt's wording about `base`, and a real Codex on Auto.
