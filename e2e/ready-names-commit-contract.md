# "Ready" names a commit, and a stacked fix is a draft: test contract

Extends [pr-wake-contract.md](pr-wake-contract.md) for issue #216; the behaviour is in `docs/design.md`
(Threads and runs: `finish_run` and `ready`; Delegation; Repos and worktrees: the merge sentence and the
stacked fix; Prompt). Tests drive the real binary with the fake GitHub, the scripted fake providers and
real git. This test PR is stacked on the pull-request watcher (#247, branch `feat/pr-wake`).

## What the fakes add

- fake GitHub: `GET /pulls/{n}` also answers `draft`, `body` and `base.ref`; `PATCH /pulls/{n}` takes
  `base`; `POST /graphql` takes the one mutation `gh pr ready` sends. `setHead(n, sha)` pushes a head
  commit; `prCalls()` lists what the Bots' gh did, in order (`create #2 base=<b> draft=<bool>`,
  `edit #2 base=<b>`, `ready #2`); `isDraft(n)` is the pull request's state now.
- fake `gh`: `pr create --draft`, `pr ready <n>`, `pr edit <n> --base <b>`.
- scripted Bot: the prompt `Fix the seeded bug, open a PR and report it ready. READY=<json>` opens and
  tracks a pull request and calls `finish_run` with `ready` set to that JSON (`repo-github-ready`);
  `Stack a fix on <branch> [waiting for PR <n>]` builds on that branch and opens a pull request against
  it, a draft with the body `Waits for #<n>` when it waits (`repo-github-stack`); a wake `PR #<n> merged;
  PR #<m> was waiting for it` makes it run `gh pr edit <m> --base main` and `gh pr ready <m>` and answer
  `Ready noted.` (`pr-wake-base`).

## Acceptance mapping

| Acceptance (#216) | Tests |
| --- | --- |
| `finish_run` `ready` entries carry the PR head SHA; the row says "changed since ready" when the head moved | `TestReadyNamesCommit_ReportCarriesHeadSHA` (the API field `report.ready[].changed`; the row lands with #147), `TestReadyNamesCommit_BadReadyEntryIsRefused` |
| The persona and the delegate-brief template carry the freeze sentence | `TestReadyNamesCommit_CodePartCarriesFreezeAndDraftRules`, `TestReadyNamesCommit_DelegateBriefCarriesFreezeSentence`, and the golden `testdata/golden/system.txt` (`TestMemory_PromptGoldenAndBudget`) |
| A stacked fix PR is a draft whose body starts "Waits for #<n>"; the Bot marks it ready when the base merges | `TestReadyNamesCommit_StackedFixWaitsAsDraft` (the create call had `draft=true`; the ready call follows the base's merge) |
| Carry-over from #214: the merge wake names the head sha and base, compares with the ready sha, and is a quiet line for a non-default base | `TestReadyNamesCommit_MergeWakeNamesTheCommit`, `TestReadyNamesCommit_MergeIntoAnotherBranchIsQuiet` |

Not covered: merging the fix first being impossible and the merge retargeting the fix to main with a clean
diff are GitHub's behaviour, not Reef's; the PR lists them under "Only you can check".
