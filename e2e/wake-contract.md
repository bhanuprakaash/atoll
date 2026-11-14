# Wake the boss: test contract

Extends [delegation-contract.md](delegation-contract.md). Tests: `wake_test.go`. They build the real
server; no product package is imported.

## Messages from a helper

When a helper run ends, Reef delivers one message into the boss thread from the helper Bot. In
`GET /api/threads/{boss}/messages` it has `from_bot_id` (the helper Bot's id; the user's own
messages have none or an empty one) and `text`. Delivering it starts a boss run that resumes the
boss's session with a prompt that holds the text. The text always holds the helper thread id, and the
prompt starts it with `Reef: `:

| Helper run                                        | Text also holds                                                                                                             |
| ------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------- |
| ends `done`                                       | first line exactly `Tester finished thread {helper thread id}:` (the prompt has it as `Reef: Tester finished thread …`), then its reply; no `Done:`, `Tried:`, `Blocked:` or `Next:` line |
| ends `failed`                                     | `Tester's run failed: <the run's error>`                                                                                    |
| stopped by the user (`POST /api/runs/{id}/stop`)  | `The user stopped Tester`                                                                                                   |
| its Bot deleted mid-run (`DELETE /api/bots/{id}`) | `Tester was deleted`                                                                                                        |

The boss's `continue(thread, message)` gives the helper a run that resumes the helper's session
(claude `--resume <session>`, codex `thread/resume`) with a prompt that holds the message.

A message for a thread whose run is going (a helper's to a busy boss, a `continue` to a busy
helper) is held and delivered when that run ends; the `continue` call still succeeds. A held
message survives a restart: the going run is marked `failed` (the existing rule) and the message is
delivered on start. A helper run going at a restart is marked `failed` and wakes the boss on start.

Closing the boss's thread (`POST /api/threads/{boss}/close`) stops its helpers' runs (`stopped`)
and closes their threads (`closed` in `GET /api/threads/{id}`). Stopping the boss's run leaves its
helpers going.

## Fakes

Two recordings per provider, no fake code:

- `helper-report.jsonl`, prompt `Run the checks, then report back.`: asks to run `make check`
  (the tests approve it, which is how they choose when a helper ends), then replies with
  `Tester here: …` (`_reportReply`). A `continue` whose
  message holds the prompt replays it in the resumed session (claude session `3c9e4b17-…`).
- `wake.jsonl`, prompt `Tester`, only on a resumed boss session (claude: the `mcp-use` session,
  marked `{"fake":"resumed"}`; codex: a `thread/resume` recording): writes `Thanks, Tester.`, so a woken boss run ends `done`. Every wake text holds `Tester`: the pinned
  texts all name the helper Bot. A boss's own `mcp-use`
  prompt naming Tester still plays `mcp-use`, the longer match.

The boss's answer to a helper is a `continue` in the boss's next run (an `mcp-use` prompt), not in
the woken run: a recording cannot know the helper thread id. A helper that fails plays a prompt no
recording has; one that stays going plays `_hangPrompt`. A busy boss waits on an approval
(`_writePrompt`).

## Acceptance mapping

| Acceptance                                                                                        | Tests                                                |
| ------------------------------------------------------------------------------------------------- | ---------------------------------------------------- |
| Helper ends `done` → message from Tester with its reply, boss run; `continue` resumes             | `TestWake_ReportWakesBoss/{claude,codex}`            |
| Busy boss and busy helper: delivered when the run ends, none lost, no 409                         | `TestWake_BusyThreadsDeliverLater/{claude,codex}`    |
| Helper failed, stopped by the user, Bot deleted                                                   | `TestWake_HelperFailedStoppedDeleted/{claude,codex}` |
| Held message survives a restart; helper going at a restart fails and wakes the boss               | `TestWake_SurvivesRestart/{claude,codex}`            |
| Closing the boss's thread stops helpers and closes their threads; stopping the boss's run doesn't | `TestWake_CloseBossStopsHelpers/{claude,codex}`      |

Not covered here: the message's `role` and wording beyond the pinned phrases, the woken boss run
calling `continue` itself, a helper's worktree on close (helper code work extends this), and what
happens to a helper's message when the boss's thread is already closed.
