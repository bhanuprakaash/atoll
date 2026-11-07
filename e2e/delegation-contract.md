# Delegate and continue: test contract

Extends the thread, repo and delegate-list contracts ([README.md](README.md),
[delegates-contract.md](delegates-contract.md)). Tests build the real server; no product package is
imported. Waking the boss (#84), helper code work (#85) and helpers in the UI (#86) extend this
contract.

## Tools

Reef's MCP server (`atoll`) lists `delegate` and `continue` in `tools/list` only for a run of a Bot
whose `delegates` is non-empty, in a thread that is not a helper thread. A helper's runs never list
them, even when the helper Bot has delegates (depth 1).

`delegate` takes `bot` (the helper Bot's name), `brief` and optional `repo` (a registered repo's
name). It returns at once with text JSON holding `thread_id` (more fields may follow); the boss's run
then ends `done` without waiting for the helper. The helper thread (`GET /api/threads/{id}`) has
`parent_thread_id` (the boss's thread), `bot_id` (the helper Bot) and `repo_id`: the boss thread's
repo, else the `repo` argument's, else none. Its first user message is exactly
`{Boss} delegated this to you; your replies go to {Boss}.`, a blank line, then the brief. Its first
run starts a fresh provider session: no `--resume` for claude, `thread/start` for codex.

`continue` takes `thread` (a helper thread id) and `message`. Only a run in that helper's boss thread
may call it; it then gives the helper a run with the message (delivery is #84's).

Refusals are tool results with `isError`, so the text reaches the model:

| Case                                            | Text                                                                                 |
| ----------------------------------------------- | ------------------------------------------------------------------------------------ |
| `bot` not on the boss's list                    | names every Bot on the list                                                          |
| the helper's repo access does not allow `atoll` | `Tester has no access to atoll`                                                      |
| 3 helper runs of the boss going                 | `3 helpers running. Tell the user, and hand this off when one of them reports back.` |
| `continue` from any thread but the boss's       | any                                                                                  |

`POST /api/threads/{id}/messages` on a helper thread answers `409` with `{error}` exactly
`Poteto is handling this thread` (the boss Bot is Poteto).

## Fakes

The tests use the `mcp-use` scenario on server `atoll`. The fakes call a tool only when the server
lists it, as the real CLIs do; a tool it does not list answers `unavailable`, which is how the tests
see `tools/list`. A brief is `\u`-escaped in the boss's call, so the boss's prompt does not hold the
prompt of the recording the helper plays (`_keepPrompt`, or `_hangPrompt` for a helper that stays
running).

## Acceptance mapping

| Acceptance                                                                          | Tests                                            |
| ----------------------------------------------------------------------------------- | ------------------------------------------------ |
| `delegate`/`continue` only for a Bot with delegates, not in a helper thread         | `TestDelegate_ToolsOnlyForBosses/{claude,codex}` |
| `delegate` returns `{thread_id}` at once; parent, Bot, first message, fresh session | `TestDelegate_HandsOff/{claude,codex}`           |
| Bot not on the list; 3 helpers running                                              | `TestDelegate_Refused/{claude,codex}`            |
| Helper gets the boss thread's repo, or `repo`, or none; no repo access              | `TestDelegate_HelperRepo/{claude,codex}`         |
| `continue` only from the boss thread; `POST` to a helper thread refused             | `TestContinue_OnlyFromBoss/{claude,codex}`       |

Not covered here: calling `delegate` or `continue` when they are not listed (the fakes, like the real
CLIs, never call an unlisted tool), Chief as the boss (a repo-less boss thread of another Bot stands
in for it), and whether the helper limit counts per boss Bot or per boss thread (the tests start all
helpers from one boss thread).
