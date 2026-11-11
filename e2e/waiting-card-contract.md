# The "Waiting on you" card: test contract

Extends [helpers-ui-contract.md](helpers-ui-contract.md), [sidebar-tree-contract.md](sidebar-tree-contract.md)
and [ready-names-commit-contract.md](ready-names-commit-contract.md). Design: `docs/design.md`,
Threads and runs ("What waits on the person") and App and UI ("The Waiting on you card"). Tests:
`ui/waiting-card.spec.ts`, Chromium at 1280×800. Issue #147 and its extension (merge-ready PRs).

## What the scripted Bots do

No provider runs: the fake `claude` plays recordings (`testdata/claude/waiting-*.jsonl`) that call
Reef's real MCP tools (`track_mr`, `delegate`, `finish_run`) with fixed arguments.

| Prompt | The Bot's `finish_run` |
| --- | --- |
| `Pick the deploy path.` | `ask` of 3 options (`Behind a flag`, `All at once`, `Wait a week`), `blocked` empty |
| `Open the dashboard export.` | `blocked`: `GitHub sign-in expired: reconnect it in Settings.` |
| `Open the PR and report it ready.` | `track_mr` PR 201, then `ready` `[{url: pull/201, sha: a×40}]` |
| `Track the fix PR.` | `track_mr` PR 302 (`Fix the export`), nothing else |
| `Report both PRs ready and ask which goes first.` | `track_mr` PR 301 (`Test the export`); `ready` 301 and 302; `ask` of 2 options |
| `Report the test PR ready.` | PR 311, `ready` with `note` `fails on main for the right reason` |
| `Report the other PR ready.` | PR 312, `ready` with no note |
| `Hand the changelog to Tester and ask me which format.` | `delegate` Tester; `ask` `Which changelog format?` (2 options) |
| helper `Check the changelog.` | waits 4 s, reports, replies `changelog-checked: …` |
| the wake that report causes | `ask` `Which changelog format, or neither?` (3 options) |

Answers (`Behind a flag`, `Canary first, please.`, `I reconnected it.`, `The test PR`) have a plain
recording with no `ask` and `blocked` empty.

What only the PR watcher writes is set in `atoll.db` while the server runs, as the watcher would:
`threads.mr_ci` (`{head, passed, failed, pending, names}`), `threads.mr_head`, `threads.mr_state`,
and `runs.finished_at` (to make a ready PR 2 hours old). Everything else comes from the real flow.

## What the page shows

- A region named `Waiting on you` sits between the chat and the composer. It is not in the `Messages`
  log; the reply that raised the decision stays a message and the log has no Blocked line for a
  question.
- A decision: the question, one `button` per option (named by the option, nothing else is a
  button), the text `or reply below`; the composer stays enabled.
- A blocker: its text and no button. The reply keeps `Blocked: <text>` in the log.
- Ready PRs: a `list` named `Ready to merge` in the region, a `listitem` per PR holding `#<n>`, the
  title of the thread tracking the PR, `green` / `red` / `red on purpose` from that thread's checks,
  the age (`2h`), the short sha of the entry (`aaaaaaa`), `changed since ready` when the tracking
  thread's `mr_head` is another commit, and a link `Open` to the PR.
- The header pill of the open thread reads `N waiting on you` (decision, blocker and ready PRs
  counted); the sidebar thread row has `data-marker="waiting"` and `Waiting on you`; the Bot row
  says `Waiting on you`; a folded group has `data-marker="needs-you"`; from another page the island
  button is named `Notifications, N waiting` and its list `Waiting on you` has the item with
  `Open thread`. An item never drops the island open.
- An option button posts its text as the person's message (`article` `You`), the same as the
  composer. The card and every marker go when the message is stored.
- A ready PR leaves when `mr_state` is `merged` or `closed`, with no message and no run.

## Acceptance mapping

| Acceptance (#147 and the extension) | Test |
| --- | --- |
| A Bot asks a decision through Reef; a "Waiting on you" card with options as buttons and free text | `a decision from finish_run is a Waiting on you card…`, `finish_run refuses an ask with one option, five options or no question` |
| Sidebar row and island show Waiting on you until answered; a folded group shows the dot | `the sidebar row, the Bot row, a folded group and the island show Waiting on you until it is answered` |
| Answering by button or composer posts the answer as the person's message and closes the card | `an option button posts its text…`, `answering in the composer posts the text…` |
| The question is not repeated in later runs while open (one card, updated) | `a wake while the decision is open leaves one card, updated; the Bot is told it is open` |
| Blocked stays for what only the user can fix | `a real blocked keeps its Blocked line and is a row of the card; a reply closes it` (and the first test: a question has no Blocked line) |
| `docs/design.md` updated, with the prompt line | docs PR #320; the prompt line is in `testdata/golden/system.txt` (`TestMemory_PromptGoldenAndBudget`, which also holds the 500-token budget) |
| A ready PR is a row `#n title · green · age` with Open; the sidebar and island count it | `a ready PR is a row…` |
| The row clears on `merged` or `closed` without a message from the Bot | `a ready PR leaves when it is merged`, `… closed` |
| A test PR reported ready with the line shows `red on purpose` | `a test PR reported ready as failing on main for the right reason says red on purpose…` |
| Two ready PRs and one decision: 3 waiting on you; answering leaves 2 | `two ready PRs and one decision make 3 waiting on you…` |
| #295: the row names the sha and says `changed since ready` | `a ready PR says changed since ready…` |

## Not covered here

- The look: red dot for Needs you and Waiting on you, amber for a blocker, a quiet hairline card.
- A reply that only ends in `?` (no `ask`) shows no card: nothing to assert before the feature.
- The lead's ledger (#180): a decision reply stays a message there; that rule has its own spec.
- The real PR watcher feeding checks and state (#163): its own tests; here its output is written
  to the database.
- A live round: the prompt line makes the Bot ask once with `ask`; only a real round shows it does.
