# Island memory: test contract

Issue #181. Design: `docs/design.md` (the island remembers across a close of the app). Tests:
`ui/island-memory.spec.ts`, fake Claude only. A close is `page.close()` and a new page in the same
context, or `kill()` then `restart()` on the same data folder. The page opened is a Bot with no
thread (a Bot's page opens its latest thread, which is left out of the list).

- `Earlier` (`list` named `Earlier` in the `Notifications` dialog) holds one `listitem` per event
  newer than `island_seen_at`: `{Bot} finished {title}`, `{Bot} failed {title}`, `{Bot} opened !N`
  with the MR title, `Chief created Bot Lagoon`. Its count is the sidebar's `data-marker="new"`
  count when nothing was opened.
- Closing the list (the pill clicked again) sets the timestamp; a reload shows `Nothing new`.
- `Mark all read` (a `button` in the dialog, only while Earlier has rows) empties Earlier and
  leaves the dialog open.
- A restart fails a run that went or waited; both are under `Earlier` as `failed`, `Needs you` is
  empty.
- An approval open when the page was closed is under `Needs you` on the next open (passes on main
  today; it guards "as today"), never under `Earlier`.

Not covered: the 4 s widening after a reopen (not replayed), a platform change by a non-Chief Bot,
the first launch of the version with old runs (the timestamp starts at now), two windows open at
once.
