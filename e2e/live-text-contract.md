# Live text test contract

Extends [chat-polish-contract.md](chat-polish-contract.md) for issue #148 and, since #399, shows
the status line instead of streamed text. Playwright drives the real binary with the scripted fake
providers. Design: `docs/design.md`, thread view and Threads and runs.

## Recordings

`live-text.jsonl`, same for Claude and Codex (prompt "Narrate the build check as you go."): one
turn with pauses.

| At | Happens |
| --- | --- |
| 0 s | text "Looking at the build config" |
| 3 s | text " and the test setup first." (same stretch) |
| 6 s | tool call `make check` (command), 3 s |
| 9 s | text "The build is green" |
| 13 s | text " and all 4 checks pass.", the run ends |

Claude's `make check` is a Bash call with the description "Run the checks"; Codex's command has no
description. The reply is "The build is green and all 4 checks pass." (only the text after the last
tool call).

`claude/live-status.jsonl` (prompt "Check CI on PR 398 and tell me what failed."), Claude only:

| At | Happens |
| --- | --- |
| 0 s | text "Looking at PR #398.\nChecking its CI" (two lines) |
| 3 s | text " runs first." |
| 6 s | Bash `gh pr checks 398` with the description "Check CI on #398", 3 s |
| 9 s | text, one line of 297 characters ("The lint job on #398 failed because …"), its tail at 12 s |
| 15 s | Bash `curl --silent https://ci.example.test/398/lint.log`, no description, 3 s |
| 18 s | text "The lint job failed because the generated client is out of date." |

## UI contract

- While the run works the log holds one `role="status"` line (creature, text, timer) and no
  `data-testid="step"` and no `data-testid="live-text"` element, at any moment, not only at the end.
  The status line's text (the `role="status"` element) is the newest of:
  - the Bot's latest line of streamed text: with a text of several lines, only the last line;
  - a tool step: a Bash call with a `description` reads just the description ("Run the checks",
    "Check CI on #398", never "Running Run the checks"); any other command reads `Running` and its
    program (Codex `Running make`, Claude `Running curl`);
  - `Thinking` before anything has arrived (not asserted here).
  Each one replaces the last: a text replaces a step, a call replaces a text. What the line reads
  from the last tool call to the end of the run, with the closing text streaming, is not asserted.
- A line too long for the status line is cut to one line with `…`: the element is one line high, and
  the text either ends in `…` or the box clips it with `text-overflow: ellipsis`.
- When the run ends the status line leaves and the reply is the bot `data-testid="message"` with
  only the closing text, once. No message holds the reply before the run ends, the closing text is
  never in the log twice at one time (status line and message together), and no working text (the
  opening) is in the log afterwards.
- A reload mid-run shows the status line with the latest text or step. So does a reconnect of the
  event stream: one status line, never a repeated or merged text.
- Scrolled up (`Jump to latest` shown), `log.scrollTop` stays within 2 px while the status line
  changes and the run ends.
- The sidebar row's second line and the header island keep the short verb (`Thinking`, then
  `Running …`, as today) and never show the streamed text at any moment of the run.
- A `reef-metrics` fence streaming mid-run draws no streamed text block; the reply message holds the
  one metrics component.

The tests find the status line by `role="status"` inside the `Messages` log; no new test id is
needed.
