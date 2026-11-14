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

`claude/fast-stream.jsonl` (prompt "Stream fast: review the server package."), Claude only, pauses add up to 9 s
with no pause over 150 ms:

| At | Happens |
| --- | --- |
| 0 s | text in 6-character deltas 18 ms apart: three lines, a long sentence, "The plan, in order:" ended by a newline, a long sentence |
| after each text | 3 to 5 tool calls (Bash with a description, Read, Grep) 60 to 150 ms apart |
| 5 more times | a text of three long sentences, then a burst of tool calls |
| end | `finish_run`, then the one-line closing text "The server package is consistent: …", the run ends |

The reply is the closing text.

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
- The status line is calm on a fast stream (`fast-stream.jsonl`):
  - It shows a sentence only once it is complete: the text ends in `.`, `!` or `?`, or a newline
    ended its line. An unfinished sentence shows only after its text stopped growing for about 1 s.
    Over the whole run the line reads only `Thinking`, a step, or a complete sentence (cut with `…`
    when too long), never a fragment of the streamed text.
  - It changes at most once per about 700 ms; the newest text or step wins, so some never show.
    Two changes are never less than 600 ms apart (frame timing), and the run still ends with the
    reply as one message and no status line.
  - The elapsed timer does not move horizontally when the status text changes length: its left
    edge stays within 1 px over the run.
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
