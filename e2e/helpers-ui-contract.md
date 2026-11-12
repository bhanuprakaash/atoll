# Helpers in the UI: test contract

Extends [delegation-contract.md](delegation-contract.md) and [wake-contract.md](wake-contract.md).
Tests: `ui/helpers.spec.ts` (hand-off, island, helper thread), `ui/helpers-tray.spec.ts` (the tray,
#175) and `ui/helpers-pr.spec.ts` (the tray's PR and CI, #175 comment, needs the watcher of #163),
fake Claude only (no line differs by provider). Shared set-up: `ui/helpers-fixture.ts`. Poteto is
the boss Bot, Tester and Reviewer its helpers; Tester's look is the shark.

## What the page shows

- Boss thread: while a helper runs or is unread, a region named `Helpers` (the tray) is attached to
  the top of the composer, one line of at most 36px: each helper task's creature (`img` named
  `Tester: working`, `Tester: needs you`; two helpers of one Bot are two), `N working` and
  `N reported` (a zero part is left out) and a button `Show`. Nothing above the composer when no
  helper is running or unread. The resting line has no link, no Allow, no `Open` and no PR text.
- A helper's open approval shows inside the region under that line: its Bot name, `make check`,
  `Allow` and `Deny`; two waiting show one and the text `1 more`. Allow, Deny (or a reply, see
  [approvals-contract.md](approvals-contract.md)) answer it in both threads.
- `Show` opens a `dialog` named `Helper tasks` over the chat (the chat's box and scroll do not
  change), one `listitem` per helper task: creature, Bot name, the helper thread's title, the step
  (`Running make build`; a loop shows its first command, `Running gh pr`, never `Running for n`),
  the time (`3s`, `4m`, `1h 5m`), a link `Open` to `/bots/{helper bot id}/threads/{helper thread id}`
  and a button `Stop` while its run goes, shown on hover. Esc, a click outside or `Show` again close
  it and Esc returns focus to `Show`; `Ctrl+.` or `Cmd+.` toggles it, the arrow keys move between
  rows and Enter opens the focused row's thread.
- A row whose helper has a PR shows a link `#<n>` to it and, while the PR is open, `running`,
  `passed` or `failed`; a merged or closed PR shows `merged` or `closed` and no CI word; a helper
  with no PR shows none of these.
- A helper whose run ended is a row saying `Reported` and counts under `N reported`, until its
  report (the helper Bot's message in the chat) has been in the visible part of the chat; then the
  row and, with the last one, the tray go, also after a reload.
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

No new fake code. The user sends the `mcp-use` prompt with `delegate` calls on server `atoll`,
briefs `\u`-escaped (as in `delegation_test.go`); a brief may start with the task's words and end
with the prompt of the recording the helper plays (the thread's title is the brief's first six
words), and a `repo` names a registered repo for a boss thread with no repo. A working helper plays
`run-hang` (`Wait for the build to finish.`: runs `make build` and stays going); one with an
approval plays `helper-report` (asks to run `make check`, then reports); the woken boss plays
`wake`. New recordings: `helper-loop` (`Check each open pull request.`: a `for` loop over
`gh pr view`, never ends) and `helper-pr-hang` (`Fix the seeded bug, open a PR and wait for CI.`:
the `repo-github-fix` scenario, then `gh pr checks 1 --watch` and a hang). The PR spec serves
GitHub locally with the routes of the watcher (`pulls/{n}`, `commits/{sha}/check-runs`,
`commits/{sha}/status`) and `ATOLL_REPO_POLL_INTERVAL=200ms`.

## Acceptance mapping

| Acceptance (#175)                                                                          | Tests                                                                                                                         |
| ------------------------------------------------------------------------------------------ | ----------------------------------------------------------------------------------------------------------------------------- |
| 3 helpers running, none waiting: one line of 36px at most; the chat is no shorter          | `three helpers running: the tray is one line of 36px at most, …`                                                              |
| Creatures and counts; two helpers of one Bot: two creatures, two rows, different titles    | `three helpers running: …`, `two helpers of one Bot are two creatures and, in the popover, …`                                 |
| A helper approval is one line under the summary; Allow and Deny answer it, both threads    | `a helper approval is one line under the summary within 2s; …`, `a helper approval shows under its line, Allow works there …`   |
| `Show` opens a popover over the chat with a row each; Esc and a click outside close it     | `Show opens a popover over the chat that does not move it; …`, `the popover has a shortcut, arrow keys and Enter, and Stop …` |
| A reported helper stays until its report was on screen; the count follows                  | `a helper that reported stays in the tray until its report was on screen, then leaves`                                        |
| No raw shell fragment in a step                                                            | `a step never shows a raw shell fragment: a loop shows its first command`                                                     |
| Nothing above the composer with no helper running or unread                                | `with no helper running or unread nothing shows above the composer`, the first test (before the hand-off)                     |
| PR and CI in the row; none for a helper with no PR; the resting line unchanged (comment)   | `helpers-pr.spec.ts`: `a helper row shows its PR and CI as the watcher read them; …`                                          |
| Helper thread in its Bot's tree, read-only: the text, Open Poteto's thread, Stop           | `two helpers: …; Stop in the helper thread`, `the helper thread is under its Bot in the sidebar and …`                        |
| One quiet handoff line; the report is a message from the helper Bot with its creature      | `two helpers: …`, `a helper approval shows under its line, …`                                                                 |
| Island creature per helper after the boss's name; the count; boss's sidebar row Needs you  | `two helpers: …`, `a helper approval shows under its line, …`                                                                 |

Not covered here: the island's `done` and `failed` states and a row's `Failed` and `Stopped`
(when a stopped or failed helper's row leaves is not pinned), a reply in the boss's composer
answering a helper's approval (`approvals-contract.md`), the colours (Needs you, red only for a failed
CI), the elapsed time's exact value, a helper with a PR whose CI
changes on the same commit (the watcher's known gap), Codex, the Waiting on you card above the tray
(#147), and the real-provider run with an approval answered after more than 60s (only the user
checks it). `docs/design.md` matching its Helpers paragraph is checked in review.
