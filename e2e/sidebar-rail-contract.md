# Sidebar rail and thread pane: test contract

Issues #469–#472 and #246, slice #473. Design: `docs/design.md`, App and UI ("Sidebar", "Thread pane",
"Now", "Selected row", "Narrow windows", "Bot groups", "New thread"). Tests:
`ui/sidebar-rail.spec.ts`, plus the specs retargeted to the rail and pane (`sidebar`,
`sidebar-tree`, `sidebar-selection`, `sidebar-resize`, `bot-groups`, `helpers-sidebar`,
`merged-threads`, `narrow-drawer`, `keyboard-nav`, `thread-labels`, `threads`, `chat-polish`,
`chat-consistency`, `island-earlier`, `island-memory`, `helpers`). Real binary, scripted fakes, no
new fake code or recording. This replaces the thread tree, `N more`, threads popup, group folding,
drawer and sidebar search parts of `sidebar-tree-contract.md`, `helpers-sidebar-contract.md` and
`keyboard-nav-contract.md`.

## Selectors

The tests find these by role and name; the UI must expose them.

Rail

- The rail is the `navigation` named `Bots`, 76 px wide, the same at every window width.
- Now is the rail's first item: a `button` whose name starts with `Now`. Its badge is an element
  with `data-badge` whose text is the Needs you count; with nothing waiting there is no
  `[data-badge]`.
- A Bot face is a `link` with `href="/bots/{id}"` whose accessible name holds the full Bot name and
  the creature `img` (named as today: `Shark, working`, `Jellyfish, idle`, `Shark, needs you`). The
  name shows under the creature as text holding the full name; the cut to `…` is CSS
  (`text-overflow: ellipsis`, at most 64 px wide).
- The selected Bot's face (the bar) has `data-current="true"`; no other face has it. A face never
  has `aria-current`.
- A face of a Bot that needs the person has `data-marker="needs-you"` (the light dot).
- Hover or keyboard focus on a face or a group line shows a `tooltip` with the full name (and the
  Bot's state) or the group name and its Bot count.
- Each user group and Ungrouped is a `group` named by its name (`aria-label`), holding its faces,
  in rail order. Pinned Bots are not in a named `group`. A group's line is a focusable `button`
  named exactly the group's name; right-click on it opens the group `menu` (Rename, New Bot in this
  group, Delete group). Rename and New group use a `textbox` named `Group name`.
- Drag to reorder (#246, `ui/sidebar-drag.spec.ts`, `sidebar_place_test.go`): native HTML5 drag
  and drop. Faces and group lines other than Chief's and Ungrouped's have `draggable="true"`; Chief's
  face and every thread row link have `draggable="false"`; Ungrouped's line is not draggable. A face
  dropped on the top half of another face lands before it, on the bottom half after it (after
  Chief: first in Pinned); on a group's line, last in that group. A group's line dropped on another
  group's line lands before that group. API: `PUT /api/bots/{id}/place {pinned, group_id, before}`
  answers `204` (`409` for Chief); `POST /api/groups/{id}/move {before}` answers `204`.
- Bottom: `button` `New` (menu: New Bot, New group) and `link` `Settings`.
- Right-click on a face opens the Bot `menu`: New thread, Pin to sidebar (or Unpin), Move to group,
  Bot settings.

Pane

- The pane is a `region` named exactly the selected Bot's name, or `Now`. It sits outside the
  rail's `navigation`.
- Header: `button` `New thread` (the `+`).
- Search: `textbox` named `Search threads`.
- Sections are `list`s named `Needs you`, `Running`, `Pinned`, `Recent` in that order (Now:
  `Needs you`, `Running`, `Replied`); an empty section is left out.
- A row is a `listitem` with `data-marker` (`needs-you`, `running`, `failed`, `limited`, `new`,
  `pinned`, or none) holding a `link` to `/bots/{bot}/threads/{id}` named by its title. In Now a
  row also holds its Bot's creature `img` and the text `{Bot} · {note}`.
- Hover or focus inside a row shows `button`s `Pin` (or `Unpin`) and `Close thread`, and hides its
  marker. Pinning a 4th thread opens the `dialog` `Replace which pin?`. Right-click on a row: `menu`
  with Rename and Close; Rename uses the `textbox` `Thread title`.
- The helper line under a boss thread has `data-line="helpers"` and is not a `listitem`.
- Closed: a `button` named `Closed · N` with `aria-expanded`; open, a `list` named `Closed` and a
  `textbox` named `Search closed threads`.
- The open thread's row link has `aria-current="page"`; it is the only `[aria-current]` in the rail
  and the pane. On the New thread screen nothing there has `aria-current`.
- Now with nothing waiting, running or replied reads `Nothing needs you`.
- Resize: a `separator` named `Resize thread pane`, `aria-orientation="vertical"`,
  `aria-valuenow` the width, `aria-valuemin="240"`, `aria-valuemax="420"`. Default 284.
- Below 900 px the pane is hidden until a face, Now or `⌘B` opens it; it then overlays `main`
  over a scrim, and has no separator.

## Not covered

Colours, the bar's look, exact sizes beyond 76 px rail and pane widths, the "latest few" closed
count, the note's text, the group count's wording, motion curves.
