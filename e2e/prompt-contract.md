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

A Bot without the code part has neither the `How you work` line nor the branch rule. The platform
block's text is fixed below only by its marker lines and its size.

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

## Platform block text

Tests: `e2e/platform_block_test.go`. The platform block runs from the line `Managing Reef` up to
the blank line before `Persona:` (or the end of the system prompt). Inside it, a section is an
exact heading line and the non-empty lines after it, up to the next blank line.

| Section | Heading line | Fixed |
| --- | --- | --- |
| Reef guide | `Reef guide` | names connections (any case) and `MCP` |
| Ask-first rule | `Ask first` | at least one line |
| Voice | `Voice` | 1 to 3 lines |

- The whole block, headings included, is at most 2000 bytes (the design's ~500 token budget).
- Chief's persona (`GET /api/bots`, and the `Persona:` part of its system prompt) contains no voice
  line.
- A Bot with `can_manage_platform` off, with repo access `"all"` or `[]`, has none of the lines
  `Managing Reef`, `Reef guide`, `Ask first`, `Voice`, and none of Chief's voice lines.

| Acceptance | Test |
| --- | --- |
| Voice at most 3 lines, in the platform block, not the persona | `TestPrompt_ChiefPlatformBlock` |
| Platform block, guide included, under about 500 tokens | `TestPrompt_ChiefPlatformBlock` |
| Only Chief has a voice (via the platform block) | `TestPrompt_NoSwitchNoPlatformTextNoVoice` |
| "ok" creates the Bot; the thread shows the change card with Undo | existing `e2e/ui/skills.spec.ts` "Chief creates a skill and Bot in chat, and Undo removes the Bot" |

Real-provider only (a fake can only replay a scripted answer): connection vs MCP answered with no
repo and no file read; one message asking for model, repo access, skills and connections with a
default each, creating nothing; rename without asking; sign-in is the user's; refusing code work;
the voice's tone and plain questions, errors and undo. `TestPrompt_ChiefPlatformBlock` covers the
deterministic part behind them: the guide and the ask-first rule are in the prompt.
