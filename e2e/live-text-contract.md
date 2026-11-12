# Live text test contract

Extends [chat-polish-contract.md](chat-polish-contract.md) for issue #148. Playwright drives the
real binary with the scripted fake providers; both providers replay `live-text.jsonl` (prompt
"Narrate the build check as you go."). Design: `docs/design.md`, thread view and Threads and runs.

## Recording

One turn with pauses, same for Claude and Codex:

| At | Happens |
| --- | --- |
| 0 s | text "Looking at the build config" |
| 3 s | text " and the test setup first." (same stretch) |
| 6 s | tool call `make check` (command), 3 s |
| 9 s | `finish_run`, then text "The build is green" |
| 13 s | text " and all 4 checks pass.", the run ends |

The reply is "The build is green and all 4 checks pass." (only the text after the last tool call).

## UI contract

- While the run works, the log holds one `data-testid="live-text"` element after the `role="status"`
  line. It holds the Bot's text so far in order, Markdown rendered, and one `data-testid="step"`
  element per finished tool step between the stretches. The running step is only the status line.
  A step reads exactly as the status line did while it ran (verb and target). `finish_run` is no step.
- When the run ends `live-text` and every `step` leave the log, and the reply is the bot
  `data-testid="message"` with only the closing text. The closing text is never in the log twice
  at one time and never missing between its first appearance and the end.
- A reload mid-run draws the same `live-text` (text so far, steps), each text once. So does a
  reconnect of the event stream.
- Scrolled up (`Jump to latest` shown), `log.scrollTop` stays within 2 px while the text streams
  and the run ends.
- The sidebar row's second line and the header island show the status line's words and never the
  streamed text.
