# Helpers in the UI: test contract

Extends [delegation-contract.md](delegation-contract.md) and [wake-contract.md](wake-contract.md).
Tests: `ui/helpers.spec.ts`, fake Claude only (no line differs by provider). Poteto is the boss Bot,
Tester and Reviewer its helpers; Tester's look is the shark.

## What the page shows

- Boss thread: while its helpers run, a region named `Helpers` sits between the chat and the
  composer. Each helper's line holds its Bot name, its state (the live verb, e.g.
  `Running make build`, else `Working`; `Needs you` while it has an open approval) and a link
  named `Open` to `/bots/{helper bot id}/threads/{helper thread id}`. A helper's open approval
  shows inside the region in the usual inline style, `Allow` and `Deny`; Allow there lets the
  helper's run go on.
- The chat has one line, exactly `Handed off to Tester` (two helpers in one run:
  `Handed off to Tester and Reviewer`).
- The helper's report (#84's message from the helper Bot) is an `article` named after the helper Bot
  (`Tester`, as the user's own message is an `article` named `You`) holding its reply and the
  helper's creature (`img` named `Shark, …`).
- Header island: one element per running helper labelled `Tester: working`, `Tester: needs you`
  (also `done`, `failed`), right of the boss's name (`h1`). The island's waiting count
  includes helper approvals (`Notifications, 1 waiting`, see
  [sidebar-tree-contract.md](sidebar-tree-contract.md)). The boss's sidebar row shows `Needs you`.
- Helper thread: a row in the helper Bot's sidebar tree, with no `Close thread`. No
  textbox named `Message…`; instead the text
  `Poteto is handling this thread. Talk to Poteto to change it.`, a link or button
  `Open Poteto's thread` (to the boss thread) and a button `Stop` while its run goes.
- An approval answered in the boss thread leaves the helper thread, and the reverse.

## Fakes

No new fake code or recording. The user sends the `mcp-use` prompt with `delegate` calls on server
`atoll`, briefs `\u`-escaped (as in `delegation_test.go`). A working helper plays `run-hang`
(`Wait for the build to finish.`: runs `make build` and stays going); one with an approval plays
`helper-report` (asks to run `make check`, then reports); the woken boss plays `wake`.

## Acceptance mapping

| Acceptance                                                                                | Tests                                                                                                              |
| ----------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------ |
| Running helpers above the composer, one line each (name, live verb, Open)                 | `two helpers: one handoff line, a line each above the composer and in the island; Stop …`                          |
| A helper's approval under its line; Allow works there                                     | `a helper approval shows under its line, Allow works there, and the report comes from …`                           |
| One quiet handoff line; the report is a message from the helper Bot with its creature     | both tests above                                                                                                   |
| Island creature per helper after the boss's name; red count; boss's sidebar row Needs you | both tests above                                                                                                   |
| Helper thread in its Bot's tree, read-only: the text, Open Poteto's thread, Stop          | `two helpers: …; Stop in the helper thread`, `the helper thread is under its Bot in the sidebar and …`             |
| An approval answered in one place leaves the other                                        | `a helper approval shows under its line …` (boss → helper), `the helper thread is under its Bot …` (helper → boss) |

Not covered here: the island's `done` and `failed` states and the line's `Done` and `Failed` (when a
finished helper's line leaves is not pinned), Deny, the creature's look in the island (red ring,
faded, grey), Codex, and the real-provider run with an approval answered after more than 60s (only
the user checks it).
