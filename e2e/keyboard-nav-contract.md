# Keyboard: composer focus, the Cmd+K switcher, sidebar search: test contract

Issue #182, design in `docs/design.md` (Keyboard). Tests: `ui/keyboard-nav.spec.ts`, fake Claude
only. Threads are made through the API and titled with `PATCH /api/threads/{id}`; a branch comes
from the fake provider's `fix/seeded-bug` recording, an MR or PR from `track_mr`.

## Composer

- Each composer is the `textbox` named `Message {Bot}` (`, run in progress` while a run goes). It
  has focus after a thread opens by a sidebar link, a direct address, the switcher or `Meta+N`,
  and after the New thread screen opens.
- With nothing focused (`document.activeElement` is `body`), a printable key goes into the
  composer and focuses it. `Control` or `Alt` with a key types nothing. Typing in the sidebar
  search field leaves the composer as it was.

## Switcher

- `ControlOrMeta+K` (Ctrl on Linux, ⌘ on macOS) opens it from anywhere, the composer and a page
  with no composer included. It is a `dialog` named exactly `Go to` with a `combobox` named
  exactly `Go to` (focused on open), and `option`s.
- Each `option` has `data-kind` `bot` or `thread` and `aria-selected` `true` on the selected one
  (the first on open and after each change of query), `false` on the rest. A thread option's text
  holds its title, its Bot's name and, when it has them, its branch and MR reference. A Bot option's
  text holds the Bot's name.
- Ranking is as the design says: no query, needs you, then last activity, then Bots with Chief
  first; with a query, Bots by name, then titles, then branch or MR, then the Bot's name.
- `ArrowDown` and `ArrowUp` move the selection, `Enter` opens the selected row and closes the
  dialog, `Escape` closes it; focus returns to where it was, the draft in the composer kept.
- No match: the dialog contains `No threads or Bots match` and no `option`.

## Sidebar search

- The search `textbox` in navigation `Bots` has a name starting with `Search`.
- A title match shows its Bot with only the matching threads in its `{Bot} threads` list, and no
  `N more` button; a Bot matched by name alone keeps its usual list.
