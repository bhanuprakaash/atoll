# Sidebar thread tree, groups and island notifications: test contract

Issue #99. Tests: `ui/sidebar-tree.spec.ts`, `ui/threads-popup.spec.ts`, `ui/bot-groups.spec.ts`,
`ui/island-notifications.spec.ts`, fake Claude only. State is set up with the existing API and
fake prompts; everything new is checked in the page. A restart is `kill()` then `restart()` on
the same data folder, then a reload. No new fake code or recording.

## Hooks

Accessible roles and names first. Where the eye sees a dot or a pin, the row carries
`data-marker`, one value: `needs-you`, `new`, `running` or `pinned` (absent when none).

## Sidebar (navigation `Bots`)

- A Bot row stays a `link` named with the Bot's name. Its thread tree is a `list` named
  `{Bot} threads`, beside the Bot's link, not inside it. Each thread row is a `listitem` holding a
  `link` to `/bots/{bot}/threads/{thread}`; the title is an element of its own.
- Row copy: `Needs you` (`data-marker="needs-you"`), `New` with a bold title (`new`, weight 600 or
  more), wave dots (`running`), a pin (`pinned`).
- The overflow line is the tree's last item, a `button` named exactly `N more` or
  `N more running`, where N counts the hidden threads it stands for (running ones for
  `N more running`). Unfolded, it becomes `Show less`. `N more` opens the threads popup.
- No spinner: no element runs an animation whose keyframes turn it a full circle
  (`rotate(360deg)` or `1turn`).
- Bot row hover: a `button` named `New thread with {Bot}`. Right-click a Bot row: a `menu` whose
  `menuitem`s are, in order, `New thread`, `Pin to sidebar`, `Move to group`, `Bot settings`.
  `Move to group` opens `menuitem`s for each group, `No group` and `New group…`. `Meta+N` starts
  a new thread with the open Bot. A new thread shows the heading `What should {Bot} work on?`.
- Thread row hover: a `button` named `Close thread` (none on a helper thread, as today). Close
  saves unfinished work at `refs/atoll/unfinished/{thread id}` as today. Right-click a thread
  row: a `menu` with `Rename` and `Close`; Rename shows a `textbox` named `Thread title`, Enter
  saves.
- The sidebar `+` is a `button` named `New`; it opens a `menu` with `New Bot` and `New group`.
  New group shows a `textbox` named `Group name`: Enter saves, Escape cancels.

## Groups

- Each group is an element with role `group` named exactly the group's name, in the user's order,
  holding its heading and its Bot rows. Bots in no group sit in a `group` named `Ungrouped`, last.
- The heading holds a `button` named the group's name with `aria-expanded` (the fold arrow) and a
  `button` named `Group actions` (the ⋯), whose `menu` has `Rename` (the `Group name` textbox),
  `New Bot in this group`, `Move up`, `Move down`, `Delete group`. Delete acts at once; its Bots
  move to `Ungrouped`.
- A folded group carries `data-marker="needs-you"` while a Bot in it needs the user, no
  `data-marker` otherwise. Dragging a Bot row onto a group's heading moves the Bot there.

## Threads popup

A `dialog` named `Threads with {Bot}` holding `New thread`, a `button` named `Close`, the text
`N open threads · N closed`, a `searchbox` named `Search threads` (title, branch, MR reference such
as `!7`), `list`s named `In the sidebar` and `Other open threads`, and a `Closed · N` button.
Escape closes it. Row hover: `Pin to sidebar` or `Unpin`, and `Close thread`. Pinning a fourth
thread opens a `dialog` named `Replace which pin?` with one `button` per pinned thread, named by
its title.

## Header island

- The pill's click target is a `button` named `Notifications`, or `Notifications, N waiting` while
  N approvals in other threads wait (the red count, also shown as text). The header has no
  `Threads` button, `Search threads`, `New thread` or `Close thread`.
- An approval in a thread other than the one on screen drops the island open inside `header`:
  the Bot's name, the thread title, the command or path, a `link` named `Open thread` to that
  thread, and `button`s `Deny` and `Allow`. Escape shrinks it to the red count.
- A run that ends in another thread shows `{Bot} finished {thread title}` and `View` in `header`
  for about 4 s, then the pill is back at rest.
- Clicking the island opens a `dialog` named `Notifications` with a `list` named `Needs you`
  (each item: the thread title, what it waits for, `Allow` and `Deny`) and a `list` named
  `Earlier` (finished runs, by thread title).

## Acceptance mapping

| Acceptance                                                           | Tests                                                                                           |
| -------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------- |
| Needs-you row with red dot; answering removes it                     | `sidebar-tree`: `a thread waiting on an approval shows under its Bot as Needs you; …`           |
| New after a run ends unseen; opening clears it, also after a restart | `sidebar-tree`: `a run ending in an unseen thread shows it as New; opening it clears New, …`    |
| Wave dots on a running row; no spinner ring                          | `sidebar-tree`: `a running thread shows wave dots; no spinner ring on any creature or thread`   |
| 9 idle threads: at most 3 rows and `N more`, which opens the popup   | `sidebar-tree`: `nine idle threads show at most 3 rows and N more, which opens the …`           |
| 6 running: `N more running`, unfold, rows leave, switching Bot folds | `sidebar-tree`: `six running threads show N more running; it unfolds only running …`            |
| Two needs-you rows plus 3 others: both show                          | `sidebar-tree`: `two threads needing approval both show beside three others`                    |
| Clicking a Bot opens its latest thread                               | `sidebar-tree`: `clicking a Bot opens its latest thread`                                        |
| `+` on hover, right-click › New thread, `⌘N`                         | `sidebar-tree`: three `… starts a new thread with that Bot` tests                               |
| Hover × closes and saves unfinished work; right-click › Rename       | `sidebar-tree`: `thread hover × closes the thread and saves …`, `thread right-click › Rename …` |
| Popup search; Pin to sidebar; 4th pin asks; pins survive restart     | `threads-popup`: both tests                                                                     |
| Groups: create, move, drag, rename, order, fold persist; delete      | `bot-groups`: first two tests                                                                   |
| Folded group red dot only when a Bot inside needs the user           | `bot-groups`: `a folded group shows a red dot only while a Bot inside needs the user`           |
| No thread list in the island; a click opens the notification list    | `island-notifications`: `the island has no thread list; clicking it opens …`                    |
| Approval in B drops the island open; Allow; Esc leaves a red count   | `island-notifications`: two `an approval in another thread …` tests                             |
| A run ending in B widens the island about 4 s                        | `island-notifications`: `a run ending in another thread widens the island …`                    |

Not covered: the tree's line and ticks, colours, the slot order beyond needs-you first, pinning
moving the least urgent row behind `more`, `Show less` itself, the popup's `Closed · N` fold,
`New Bot in this group`, click-away shrinking the island, Chief's changes and MRs under `Earlier`,
and the `docs/design.md` line.
