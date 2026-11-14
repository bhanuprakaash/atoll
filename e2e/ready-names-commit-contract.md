# "Ready" names a commit, and a stacked fix is a draft: test contract

Extends [pr-wake-contract.md](pr-wake-contract.md) for issue #216; the behaviour is in `docs/design.md`
(Delegation; Repos and worktrees: the merge sentence and the stacked fix; Prompt). Tests drive the real binary with the fake GitHub, the scripted fake providers and
real git. This test PR is stacked on the pull-request watcher (#247, branch `feat/pr-wake`).

## What the fakes add

- fake GitHub: `GET /pulls/{n}` also answers `draft`, `body` and `base.ref`; `PATCH /pulls/{n}` takes
  `base`; `POST /graphql` takes the one mutation `gh pr ready` sends. `setHead(n, sha)` pushes a head
  commit; `prCalls()` lists what the Bots' gh did, in order (`create #2 base=<b> draft=<bool>`,
  `edit #2 base=<b>`, `ready #2`); `isDraft(n)` is the pull request's state now.
- fake `gh`: `pr create --draft`, `pr ready <n>`, `pr edit <n> --base <b>`.
- scripted Bot: `Stack a fix on <branch> [waiting for PR <n>]` builds on that branch and opens a pull
  request against it, a draft with the body `Waits for #<n>` when it waits (`repo-github-stack`); a
  wake `PR #<n> merged; PR #<m> was waiting for it` makes it run `gh pr edit <m> --base main` and
  `gh pr ready <m>` and answer `Ready noted.` (`pr-wake-base`).

## Acceptance mapping

The report's `ready` entries are gone with `finish_run`, and so are the tests of them. What stays:

| Acceptance (#216) | Tests |
| --- | --- |
| The freeze sentence and the draft rule are not in Reef's prompt or `delegate`'s description (they are in the repo's own instructions) | `TestReadyNamesCommit_PromptHasNoFreezeOrDraftRules`, `TestReadyNamesCommit_ToolDescriptionsHaveNoFreezeSentence` |
| A stacked fix PR is a draft whose body starts "Waits for #<n>"; the Bot marks it ready when the base merges | `TestReadyNamesCommit_StackedFixWaitsAsDraft` (the create call had `draft=true`; the ready call follows the base's merge) |
| The merge wake names the head sha and base, and is a quiet line for a non-default base | `TestReadyNamesCommit_MergeWakeNamesTheCommit`, `TestReadyNamesCommit_MergeIntoAnotherBranchIsQuiet` |

Not covered: merging the fix first being impossible and the merge retargeting the fix to main with a clean
diff are GitHub's behaviour, not Reef's; the PR lists them under "Only you can check".
