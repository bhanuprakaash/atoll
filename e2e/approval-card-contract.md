# Approval card: test contract

Extends the thread and approval contracts ([README.md](README.md),
[approvals-contract.md](approvals-contract.md),
[allow-in-thread-contract.md](allow-in-thread-contract.md)). Design: "Approval card", "Colour" and
"Replying to an approval" in `docs/design.md`. Tests build the real server; no product package is
imported. Stacked on the Allow in this thread test PR: its `command-use` scenario
(`Run commands. ["<command>", ...]`) gives both providers command approvals.

## The card

- It is a `role="group"` named `Approval`, inside the messages log, in a thread's own run and under
  a helper's line.
- The command, file or tool target is in one `<code>` element whose text is the command as is. An
  edit's file is there too. A one-line command keeps its `Wants to run` lead-in, so the
  allow-in-thread specs still find `Wants to run <cmd>`.
- Allow and Deny are buttons named `Allow` and `Deny` (exact), on the code line's row. At 1280x800 a
  one-line command's card is at most 72 px tall.
- A command wider than 2 lines is clamped to 2 inside the code line (card at most 120 px), with a
  button `Show all` in the card; it then reads `Show less` and the card is taller. A short command
  has neither. The code element's text is the whole command either way.
- An edit keeps `Show diff` / `Hide diff`; the diff is a `<pre>` in the card.
- Deny is the only red in the card, on hover and on keyboard focus (`:focus-visible`).

## "Red"

A spec calls a colour red when it has hue within 10 degrees of 0 (350 to 10) and the red channel
leads the others by at least 12, whether it is text, a background, a border, a fill or a stroke of
an icon, at 20 percent opacity or more. A creature is a Bot's own colour and is skipped, but its
needs-you pulse (`.creature-pulse`) counts. Brand colours (the Claude mark, hue 15) are not red.
The scan covers the whole page, so a new red at rest anywhere fails it.

## Replying

`POST /api/threads/{id}/messages` while approvals are open in the thread (a helper's included):

- `202 {message, run, answered}`. `run` is the thread's going run, `null` when only a helper's
  approval was answered; `answered` is the ids of the approvals it denied; `message` is the user's
  message, not queued, with `answered_approvals: [{id, tool, input}]`.
- Each answered approval closes (`approval_closed`); `POST /api/approvals/{id}` on it is `404`.
- The same run goes on and asks about the next command: no second run starts, the message is stored
  once. The reply is never read as Allow: "yes, go ahead" denies too.
- Claude is told through the permission prompt tool's deny message, which holds the text (Reef's
  stock `The user rejected this step.` is for a plain Deny). Codex gets `turn/steer` with the text,
  then `decline`.
- The chat shows one muted line `You replied instead` per answered approval, after a reload too.

The fake providers report what they were told: a command the user refused adds
`<command>: denied` to the reply, plus `, told: <reason>` when the reason is not the stock one.
Claude's reason is the deny message; Codex's is the text of the last `turn/steer` the fake read
before the decline (so Reef sends the steer first).

## Acceptance mapping

| Acceptance                                                                        | Tests                                                                                                                                       |
| --------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------- |
| One-line command: compact card, code line, Allow and Deny on its row, at most 72 px | UI `a one-line command is a compact card: ...`                                                                                              |
| No red at rest; Deny red on hover and focus; the thread's Needs you marker        | UI `the card is quiet: ...`                                                                                                                 |
| Red only for danger, app-wide                                                     | UI `while an approval waits nothing on screen is red`, `an approval in another thread: ...` (sidebar, island, count, list), `a folded group ...`, `a helper approval waiting ... is not red anywhere`, `a closed MR is not red`; guard `a failed run keeps its red icon ...` |
| Long command wraps with Show all; an edit keeps its diff toggle                   | UI `a long command wraps inside its code line ...`, `an edit shows its file ...`                                                            |
| A reply denies with the text, the run continues, the card collapses               | `TestApprovalReply_DeniesWithTheText/{claude,codex}`, `TestApprovalReply_YesIsNotAllow/{claude,codex}`; UI `writing in the chat answers the card ...` |
| A helper's approval answered from the boss thread                                 | `TestApprovalReply_HelperApprovalFromBossThread/{claude,codex}`; UI `a helper approval in the boss thread is answered by writing there`      |

Not covered here, for the owner: the before and after screenshots of every place that moved from
red (the card's own is attached to the first UI spec), a real Claude and a real Codex run, where a
real Codex must continue its turn after `turn/steer` plus `decline` (the fake only proves what Reef
sends), and several approvals open at once (the fakes ask one at a time).
