# Rich output test contract

Issue #185. Design: `docs/design.md`, "Rich output". The real binary is driven by Playwright with
the scripted fake claude (recordings `testdata/claude/rich-*.jsonl`, one reply each) and by a Go
test that reads the system prompt the provider got. No new fake code.

## Selectors

Every component's root carries `data-component="code|table|diff|checklist|metrics|kv"`. A reef-
fence that does not fit its form, an unknown reef- tag and ordinary fenced code are all `code`.
Inside a component:

| Component   | Contract                                                                                                                                                                                                                                                                                                                                                                                                                                                                                             |
| ----------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `code`      | a `pre` inside the root; the language as exact text (`go`) when the fence has one, none for a reef- fallback; a button `Copy` that puts the whole source (no trailing newline) on the clipboard; past 20 lines the root holds the first 20 and a button `Show all`, which becomes `Show less`. The root has a background other than the page's and at least 8px between its edge and the text; highlighted code has more than one text colour among its spans. Inline code is not a `code` component |
| `table`     | a `table` with `columnheader`s; each header holds a button named as the header text and `aria-sort` (`none`, `ascending`, `descending`), a click cycling ascending, descending, none (the order written); one column sorted at a time; headers `position: sticky`; a column whose cells all parse as numbers is `text-align: right` and sorts as numbers                                                                                                                                             |
| `diff`      | one `[data-kind="file"]` header per file with its path and both counts (`+N`, `-N`, zero included); `[data-kind="hunk"]` for `@@` lines; one `[data-kind="add"]`, `"del"` or `"ctx"` row per line, the `diff --git`, `---` and `+++` lines being none of these; the three kinds have three different backgrounds                                                                                                                                                                                     |
| `checklist` | rows with `role="checkbox"`, named by the item text, `aria-checked`, `aria-readonly="true"`; a click changes nothing. Only a list whose every item is a task item                                                                                                                                                                                                                                                                                                                                    |
| `metrics`   | a list with one `listitem` per metric: label, value, delta when given                                                                                                                                                                                                                                                                                                                                                                                                                                |
| `kv`        | `dt` keys and `dd` values in order                                                                                                                                                                                                                                                                                                                                                                                                                                                                   |

## Replies

| Prompt (contains)                        | Reply holds                                                                   |
| ---------------------------------------- | ----------------------------------------------------------------------------- |
| `Rich output: show code.`                | a 4-line `go` fence, a 30-line `yaml` fence, inline code                      |
| `Rich output: show a table.`             | a 3-row pipe table; `Lines` holds 9, 120, 15                                  |
| `Rich output: show a diff.`              | a two-file `diff` fence                                                       |
| `Rich output: show a checklist.`         | a 3-item task list, then a list mixing task and plain items                   |
| `Rich output: show metrics.`             | a `reef-metrics` fence of 3 items, one without delta, one with a number value |
| `Rich output: show details.`             | a `reef-kv` fence with a blank line and a value holding `:`                   |
| `Rich output: show broken blocks.`       | cut-off JSON, 5 metrics, a `kv` line with no `: `, a `reef-chart` fence       |
| `Rich output: show an unfinished block.` | a `reef-metrics` fence that never closes                                      |
| `Rich output: report the status table.`  | a helper's reply, a 2-row table, relayed into the boss thread                 |

## Acceptance mapping

| Acceptance                                                                           | Spec                                                                                         |
| ------------------------------------------------------------------------------------ | -------------------------------------------------------------------------------------------- |
| Ordinary fenced code: boxed, highlighted, Copy                                       | `rich-output.spec.ts` "fenced code is a boxed, highlighted block with its language and Copy" |
| Long code collapses with Show all                                                    | "code past 20 lines shows 20 with Show all; Copy takes all of it"                            |
| Inline code stays inline                                                             | "inline code stays inline"                                                                   |
| A table renders as Reef's table (sticky header, sort)                                | "a GFM table is a Reef table: sticky header, sort, numbers right-aligned"                    |
| Broken or unknown reef- fence is a code block, never an error                        | "a reef- fence that does not fit its form shows its body as a code block"                    |
| A fence that never closes is a code block at run end                                 | "a reef- fence still open when the run ends is a code block, not a placeholder"              |
| The boss thread's relay of a helper report renders the same components               | "a helper's report in the boss thread renders the same table"                                |
| diff, checklist, metrics, kv render, each with a screenshot                          | `rich-components.spec.ts`, one spec each; the screenshot is attached to the test             |
| Catalog line: the design's line, reef-metrics and reef-kv render only in Reef chat    | `TestPrompt_RichOutputCatalog/{claude,codex}`                                                |
| The prompt-size test still passes                                                    | `TestMemory_PromptGoldenAndBudget/{claude,codex}` (golden updated with the catalog line)     |

## Not specced

- Streaming placeholder while a reef- fence is open, and the sidebar preview, island line and
  notification text: they need live text (#148) and the reply previews, neither on `main`.
- A Bot's real choices (a table for "last 5 PRs", Markdown only in a PR body written with `gh`):
  a fake only replays a script. The owner checks them with a real provider.
- Light theme: the app has none yet.
