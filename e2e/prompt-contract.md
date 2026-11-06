# Platform prompt split: test contract

Extends [memory-contract.md](memory-contract.md). Tests read the system
prompt from the fakes' `<executable>.log.prompts` (`kind: "system"`): Claude's
`--append-system-prompt`, Codex's `developerInstructions`. Tests: `e2e/prompt_test.go`.

## Parts and their markers

A part is found by exact lines of the system prompt, not by its wording.

| Part | Present when | Marker |
| --- | --- | --- |
| Shared core | every Bot | the lines `How you talk`, `Memory` and `Finish` |
| Code part | `repo_access` is `"all"` or a non-empty list | the line `How you work` and the text `branch <type>/<short-slug>` |
| Platform block | `can_manage_platform` is true | the line `Managing Reef` |

A Bot without the code part has neither the `How you work` line nor the branch rule. Only the
platform block's heading is fixed here; its text belongs to the next ticket.

A Bot with repo access and the switch off gets today's prompt byte for byte:
`testdata/golden/system.txt` is unchanged, so the code part stays between `How you talk` and
`Memory`.

## Chief and API

- A fresh data directory has Chief with `repo_access: []`.
- Every launch sets Chief's `repo_access` to `[]`. A Chief saved with `"all"` (through
  `PUT /api/bots/chief`, as an older install has it) reads `[]` after a restart.
- `POST /api/bots` accepts `can_manage_platform: true` and returns it.
- The `prompt-hey` recordings answer the message `hey` with `finish_run` and the reply `Hello.`
  on both fakes.

## Acceptance mapping

| Acceptance | Test |
| --- | --- |
| Repo access, switch off: core and code part, no platform block | `TestPrompt_RepoAccessGetsCodePart` (`"all"` and a one-repo list) |
| Only these, empty list: no code part | `TestPrompt_NoReposNoCodePart` |
| Chief fresh install: no repos; core and platform block, no code part | `TestPrompt_ChiefFreshInstall` |
| Chief with all repos is moved to none on the next launch | `TestPrompt_ChiefLosesRepoAccessOnLaunch` |
| Switch on and repo access: code part and platform block | `TestPrompt_PlatformBotWithRepoAccess` |
| Chief sent "hey" completes; thread has no branch or worktree | `TestPrompt_ChiefHeyNoBranchNoWorktree` |
