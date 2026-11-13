# Delegated work in the sidebar and merged threads: test contract

Tests: `ui/helpers-sidebar.spec.ts`, `ui/merged-threads.spec.ts` (fixture: `ui/github-fixture.ts`),
and the sidebar part of `ui/helpers.spec.ts`. Fake Claude and the scripted GitHub the PR watcher
reads. No new Go fake; two recordings (`repo-github-fix-ask`, `repo-github-fix-blocked`) are
`repo-github-fix` ending its run with a decision or a blocker.

## Hooks

- The line under a boss thread is an element with `data-line="helpers"` inside the boss Bot's
  `{Bot} threads` list. At most one per boss thread. It holds the helper Bots' names, the step
  of the one that acted last (e.g. `Running make build`) or `Needs you`, and never the word
  "helper". It is not a row of the thread tree: it does not count toward the 3 rows. Clicking it
  opens the boss thread.
- No `a[href$="/threads/{helper thread}"]` in the sidebar for a helper thread, running or finished.
- The boss thread's `Helpers` region (the tray) reads `Working with {boss}` and is scrolled into
  view by the click.
- A folded boss thread: the tree's `N more` / `N more running` button also says `{Bot} working`.
- The helper thread's header holds a `navigation` named `Breadcrumb`: the boss creature `img`, a
  `link` named with the boss thread's title, `›`, the helper Bot's name.
- `GET /api/bots`: each thread has `parent_thread_id` (the boss thread id for a helper thread).
- A merged thread that leaves the tree is the same thread in the threads popup's `Other open
threads` list; nothing in the tree links to it.
- A Bot whose open threads are all hidden from the tree (merged, or helper threads) still shows
  its `1 more` button in the tree, which opens the `Threads with {Bot}` popup.
- A message from the person after the merge keeps the thread in the tree after that run ends,
  also after a reload. The merge wake's run does not: the thread shows while it goes and leaves
  when it ends.

## Acceptance mapping

| Acceptance                                                  | Test                                                                            |
| ----------------------------------------------------------- | ------------------------------------------------------------------------------- |
| One line with name and step; no row under the helper Bot    | `helpers-sidebar`: `one helper: one line …`                                     |
| Two helpers: one line naming both                           | `helpers-sidebar`: `two helpers: still one line, naming both`                   |
| `Needs you` once, on the line; Allow clears it              | `helpers-sidebar`: `a helper waiting on approval: …`                            |
| Click opens the boss thread, helpers panel in view          | `helpers-sidebar`: `clicking the line opens the boss thread …`                  |
| Helper thread header shows the boss title and links back    | `helpers-sidebar`: `the helper thread header is a breadcrumb …`                 |
| After the report: line gone, no row, `1 more`, in the popup | `helpers-sidebar`: `after the helper reports …` (also reload, restart)          |
| Folded boss thread says a helper works                      | `helpers-sidebar`: `with the boss thread folded …`                              |
| Helper thread not under its Bot (was: under it)             | `helpers`: `the helper thread is not in the sidebar and read-only …`            |
| Merged PR leaves the tree; stays open; popup; history       | `merged-threads`: `a thread whose PR merged leaves the sidebar tree …`          |
| Waiting approval, ask or blocker keeps it until answered    | `merged-threads`: three `a merged thread with … waiting stays …`                |
| Pinned merged thread stays                                  | `merged-threads`: `a pinned thread whose PR merged stays …`                     |
| A message brings it back to the tree                        | `merged-threads`: `a message sent to a merged thread from the popup …`          |
| It stays in the tree after that run ends                    | `merged-threads`: `a message sent to a merged thread brings it back for good …` |
| Only open thread merged: `1 more` opens the popup           | `merged-threads`: `a Bot whose only open thread merged shows 1 more …`          |

Not covered: the composer text of a helper thread, the brief highlight of the panel, the line
not counting toward the 3 rows, `docs/design.md` and `make check`.
