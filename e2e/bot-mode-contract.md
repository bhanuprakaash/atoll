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

Claude's auto mode never calls the permission prompt tool in headless `-p` (spike: 6 real runs, 0
calls), so a Claude Bot on Auto runs exactly as on Ask.

| Mode | Claude argv | Codex `thread/start` / `thread/resume` |
| --- | --- | --- |
| `auto` | no `--permission-mode`, or `--permission-mode default`; `--permission-prompt-tool mcp__atoll__approve` | `sandbox: workspace-write`, `approvalPolicy: on-request` |
| `ask` | same as `auto` | `approvalPolicy: untrusted`; sandbox not `danger-full-access` |
| `full` | `--permission-mode bypassPermissions` | `sandbox: danger-full-access`, `approvalPolicy: never` |

Codex may carry them as params or in `config` (`sandbox_mode`, `approval_policy`). A mode changed
while a run goes applies from the next run; the going run keeps the mode it started with.

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

Not covered here: the spike and the real-Claude line are implementer and user proof. Not pinned:
Codex's sandbox on Ask beyond not being `danger-full-access`; whether Claude on Full access still
gets `--permission-prompt-tool`; error wording beyond the three names.
