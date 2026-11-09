# Bot mode: test contract

Extends the Bot and Chief contracts ([mac-app-contract.md](mac-app-contract.md),
[chief-bot-contract.md](chief-bot-contract.md)). Tests: `bot_mode_test.go`, UI `ui/bot-mode.spec.ts`.
They build the real server; no product package is imported.

## API

A Bot has `mode`: `auto`, `ask` or `full`. `POST /api/bots`, `PUT /api/bots/{id}` (partial, as for
`look`), `GET /api/bots` and `GET /api/bots/{id}` carry it. A Bot created without `mode` gets
`auto`. Any other value is `400`; its `error` contains `auto`, `ask` and `full`, and nothing is
created or changed.

A launch moves every Bot of an older install to `auto`, once: a Bot set to `ask` after that launch
stays `ask` across a restart. `testdata/before-modes.sql` is the `atoll.db` the server left before
Bots had a mode (schema version 11, Chief and two Bots); the test loads it into a fresh data dir,
then launches.

## What reaches the provider

Claude on Auto runs Claude Code's auto mode: its classifier lets safe steps run and blocks risky
ones, and Claude reports a block back. Explicit ask rules still reach the permission prompt tool.

| Mode | Claude argv | Codex `thread/start` / `thread/resume` |
| --- | --- | --- |
| `auto` | `--permission-mode auto`; `--permission-prompt-tool mcp__atoll__approve` | `sandbox: workspace-write`, `approvalPolicy: on-request` |
| `ask` | no `--permission-mode`, or `--permission-mode default`; `--permission-prompt-tool mcp__atoll__approve` | `approvalPolicy: untrusted`; sandbox not `danger-full-access` |
| `full` | `--permission-mode bypassPermissions` | `sandbox: danger-full-access`, `approvalPolicy: never` |

Codex may carry them as params or in `config` (`sandbox_mode`, `approval_policy`). A mode changed
while a run goes applies from the next run; the going run keeps the mode it started with.

Codex on Auto in a repo thread also starts every run, `thread/start` and `thread/resume` alike, with
`config.sandbox_workspace_write.writable_roots` holding exactly these five folders, in any order:
the `worktree_root` folder, the repo's git common dir's `objects`, `refs` and `logs`, and the
absolute git dir of that session's `cwd` (`git -C <cwd> rev-parse --absolute-git-dir`), and
`config.sandbox_workspace_write.network_access` true. Both a first run, in the repo's clean
checkout, and a resumed run, in the thread's worktree, start in a linked worktree, so the last root
is `<repo>/.git/worktrees/<name>`, different per session. Codex mounts the git dir of a linked
worktree cwd read-only unless it is a root itself, and a root must be a folder. The common dir
itself, its `config` and its `hooks` are not roots, so the Bot cannot change the repo's config or
hooks. Codex reads those keys there and nothing else, so the same values anywhere else do not
count. Paths compare after symlinks resolve and a trailing slash is dropped. Ask and Full access
have no `sandbox_workspace_write` key. The spec runs a plain clone.

`git push -u` cannot save the upstream when `config` is read-only, so `start_work` sets it when it
makes the branch: `branch.<branch>.remote` is `origin` and `branch.<branch>.merge` is
`refs/heads/<branch>`, with no run pushing anything. A repo without an `origin` remote is not
covered.

Codex on Auto can write the repo's `.git/hooks`, because the git common dir is a writable root. None
of Atoll's own git calls in the repo, which run outside any sandbox, may therefore run a repo hook.
The spec plants `post-checkout`, `reference-transaction` and `post-index-change` hooks that record
a call made with `GIT_TERMINAL_PROMPT=0`, as Atoll's git does, and expects no record after the
thread's clean checkout, after `start_work`, after a run that leaves unsaved work, and after the
thread closes (save and remove).

## Chief

`create_bot` and `edit_bot` declare a string `mode` in their `inputSchema` (the fakes send only
declared arguments). `create_bot` without `mode` gives `auto`. An unknown mode fails as an unknown
look does: the text contains `auto`, `ask` and `full`, and nothing changes. An `edit_bot` of `mode`
records one `edit` change (`platform_changed`); its Undo restores the previous mode. `list_bots`
shows each Bot's `mode`.

## UI

Bot settings has a radio group `Mode` with `Auto`, `Ask` and `Full access`; a new Bot shows `Auto`.
With `Full access` picked, the panel shows exactly: "Full access: this Bot never asks, even to delete
files or push. Use it for Bots that run in a VM." With another mode the text is gone. The choice
saves as you change it and shows again after a reload.

## Acceptance mapping

| Acceptance | Tests |
| --- | --- |
| `GET/PUT /api/bots` round-trip `mode`; unknown → 400 naming the three; new Bot gets `auto` | `TestBotMode_APIRoundTrip` |
| A launch moves every existing Bot to `auto` once | `TestBotMode_LaunchMovesOldBotsToAutoOnce` |
| Each mode's flags or config reach Claude and Codex | `TestBotMode_ReachesProvider/{claude,codex}` |
| Bot settings Mode control; Full access text | UI `Bot settings pick a Mode; Full access warns and the choice persists` |
| Chief `create_bot`/`edit_bot` take `mode`; `list_bots` shows it; change with Undo | `TestBotMode_Chief/{claude,codex}` |
| A change of mode mid-run applies from the next run | `TestBotMode_ChangeMidRunAppliesFromNextRun/{claude,codex}` |
| `start_work` sets the new branch's upstream to `origin` | `TestRepo_StartWorkSetsUpstream` |
| None of Atoll's own git calls in the repo (clean checkout, `start_work`, close with unsaved work) runs a repo hook | `TestRepo_ReefGitRunsNoRepoHooks` |
| Codex Auto starts and resumes with the `worktree_root` folder, the git common dir's `objects`, `refs` and `logs` and the session cwd's git dir (not the common dir, `config` or `hooks`) as writable roots and network access; Ask and Full access carry neither | `TestCodexAuto_SandboxCoversWorktreeAndNetwork/{auto,ask,full}` |

Not covered here: the spike and the real-Claude line are implementer and user proof. Not pinned:
Codex's sandbox on Ask beyond not being `danger-full-access`; whether Claude on Full access still
gets `--permission-prompt-tool`; error wording beyond the three names.
