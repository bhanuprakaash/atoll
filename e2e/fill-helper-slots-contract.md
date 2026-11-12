# Free helper slots: test contract

Extends [delegation-contract.md](delegation-contract.md) and [wake-contract.md](wake-contract.md).
Tests: `fill_helper_slots_test.go`. They build the real server; no product package is imported.
Design: `docs/design.md`, Delegation (the helpers line) and Prompt.

## The Helpers line

Built by Reef from the helper runs of one boss thread that are going, against the limit of 3
helper runs (the one `delegate` refuses a fourth at):

| Going | Line |
| --- | --- |
| none | `Helpers: 0 of 3 running. 3 free.` |
| n of 3 | `Helpers: n of 3 running ({Bot} on {title}; {Bot} on {title}). {3-n} free.` |

`{Bot}` is the helper Bot's name; `{title}` the helper thread's title as `GET /api/threads/{id}`
shows it (the brief's first words until the thread is named). Helpers are listed oldest thread first.

## Where it appears

| Where | How |
| --- | --- |
| A boss wake (done, stopped, failed, Bot deleted) | the message's last line, after a blank line; the helper whose run just ended is not counted |
| `delegate` result | the `helpers` field of the JSON; the new helper counts; a refusal keeps its text |
| `continue` result | `Sent.`, then the line on a new line |
| `finish_run` result | `Report saved.`, then the line on a new line; only in a run that lists `delegate`, else `Report saved.` alone |

## System prompt

A Bot with a non-empty delegate list, in a thread that is not a helper thread, has the sentence
`Before finish_run, if a slot is free and a ticket is ready, delegate it first.` in its system
prompt. A Bot with no delegates, and a helper's run (even of a Bot that has delegates), do not.

## Acceptance mapping

| Acceptance (#205) | Tests |
| --- | --- |
| A boss wake ends with the line, built from the helper runs going | `TestSlots_WakeEndsWithHelpersLine/{claude,codex}` |
| `delegate`, `continue` and `finish_run` results carry the same line | `TestSlots_ToolResultsCarryHelpersLine/{claude,codex}`, `TestSlots_FinishRunLineOnlyForBosses/{claude,codex}` |
| The boss persona says: before `finish_run`, delegate a ready ticket into a free slot | `TestPrompt_BossGetsHelpersRule/{claude,codex}` |
| Over a 60-minute live window, idle share with queued tickets drops | not specced: measured by the owner on a live round (see the PR) |

Not covered: the optional Reef-side 60 s wake (skipped, see the PR), a wake held while the boss's
run goes (it keeps the line it was posted with), and the owner-only live measurement.
