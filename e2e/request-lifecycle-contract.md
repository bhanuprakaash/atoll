# Request lifecycle test contract

The design is in `docs/design.md` (Providers, Request lifecycle). Tests: `request_lifecycle_test.go`.
The real server runs scripted Claude and Codex sessions; no product package is imported.

## What the fakes can script

Claude (`testdata/claude/lifecycle-*.jsonl`, wire shapes from Claude 2.1.323 with Reef's flags):

- `{"type":"command_lifecycle","command_uuid":"$COMMAND_UUID","state":"queued|started|completed|cancelled",...}`.
  `$COMMAND_UUID` is the `uuid` of the user line being answered, or "" when the line has none. A
  recording that holds no such line sends no lifecycle events (the `lifecycle-none-*` recordings
  also leave `msg_lifecycle_v1` out of the init line; older recordings keep it). Position in the
  recording sets the order against the `result`.
- A `result` with `"origin":{"kind":"task-notification","producer":"session-task","runId":"..."}`,
  with its own assistant text and tool call before it, placed before Reef's `started`.
- `{"fake":"next-turn"}`: the lines after it answer the next user line of the process.
- `{"fake":"exit","code":1,"stderr":"..."}`: print the text on stderr and exit.
- The fake reads stdin while it plays, and writes the order of the user lines it read and the
  `result` and `command_lifecycle` lines it printed to `<provider dir>/claude.log.trace` (the
  `fake.Trace` type). The test reads whether a user line carried a `uuid` from the plain log.
- The sequence of `lifecycle-resumed` is built from the design's account (Claude 2.1.323: on resume
  an empty `result` with an origin came before Reef's message started); no capture holds it whole.
  Order: `queued`, init, other activity (text, a Bash call), `result` with origin, `started`, the
  answer, `result`, `completed`.

Codex (`testdata/codex/lifecycle-*.jsonl`): the recordings carry notifications and a server request
of another `turnId` of the same thread: its `turn/started`, a command item, text, a
`thread/tokenUsage/updated`, `turn/completed`. `turn/start` answers Reef's turn id.

## Acceptance mapping

| Acceptance line of the ticket | Tests |
| --- | --- |
| Every user line has a `uuid`; a `result` with an origin before the request does not end the run; the reply is the answer after it | `TestRequestLifecycle_UserLinesCarryUUID`, `TestRequestLifecycle_NotificationBeforeRequestDoesNotEndRun` |
| The request ends when the terminal state and the `result` have both arrived, in either order | `TestRequestLifecycle_RequestEndsOnBothSignals` (two cases) |
| `cancelled` with an error result fails with the error text; a terminal state with no result, or an exit, fails with the exit reason | `TestRequestLifecycle_FailedRequestKeepsItsReason` (three cases) |
| No lifecycle events: ends on the first `result` with no origin, one warning per process | `TestRequestLifecycle_NoLifecycleEventsWarnOncePerProcess` |
| Codex: ends on `turn/completed` of Reef's turn; text, tool calls and usage of another turn reach neither chat nor reply nor the run's token counts | `TestRequestLifecycle_CodexOtherTurnIsNotTheRun` |
| Codex: a server request naming another turn still reaches the person | `TestRequestLifecycle_CodexApprovalOfOtherTurnReachesPerson` |
| Nothing is sent after the request: the process reads one user line | `TestRequestLifecycle_NothingIsSentAfterTheRequest` |
| Other activity is in the event log; the cost is the session total less the previous run's | `TestRequestLifecycle_OtherActivityIsLoggedAndPaidFor`, `TestRequestLifecycle_CodexOtherTurnIsNotTheRun` (the log) |
| Real provider, once by hand | owner (PR, Only you can check) |

## Where the design leaves a choice

The tests assert the following; the implementation may not differ without the tests changing.

- The warning for a build with no lifecycle events is a `WARN` line of the server log whose `msg`
  contains `lifecycle` (any case). A process that holds two requests logs one.
- Other activity in the run's event log (`<data>/runs/<run id>.jsonl`): the other activity's text
  and tool call appear in a line of that file, not in a line of type `text` or `tool_call` (a
  client that reconnects replays those as chat), and in no event the thread's stream sent while the
  run went.
- The run's `usage` counts Reef's request only: tokens and `calls` (the assistant messages of the
  request; the notification turn's message is not one). The cost is the session's `total_cost_usd`
  at Reef's `result` less the previous run's, so what the other activity spent is in it (0.30 less
  0.10 is 0.20; the other activity's `result` says 0.18).
- A failed run's `error` holds the text of the error `result`, or the exit text of the process
  (`claude exited: ...` with its stderr).
