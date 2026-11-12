# The lead's ledger: test contract

Extends [helpers-ui-contract.md](helpers-ui-contract.md) and [wake-contract.md](wake-contract.md).
Design: `docs/design.md`, App and UI, "The lead's ledger". Tests: `ui/lead-ledger.spec.ts`, Chromium
at 1280×800, locale `en-US`, time zone UTC. Issue #180.

## The scripted lead thread

No provider runs. The spec makes Poteto (whale, delegates to Tester, Reviewer and Coder) and its
thread through the API, stops the server, writes the thread into `atoll.db` (`bots` are the API's;
`threads`, `runs` and `messages` are written) and starts the server again on the same data. What it
writes is what a round leaves behind:

- the person's message and the lead's answer, one run (`runs.report.done` `Answered.`);
- per helper report: the helper's thread (`parent_thread_id` the lead's thread, `mr_url` and
  `mr_state` when it has a PR) with one run and its `finish_run` report; a wake run of the lead
  whose report is `Noted.` or the reply's; the wake message (`role` user, `from_bot_id` the helper
  Bot) with the text `run/inbox.go` `WakeText` builds, on that wake run; the lead's reply, a `bot`
  message on the same run, for some;
- every helper thread begins before the first message, so no handoff line is drawn, except where
  the spec says (`created` just after a message).

Thread A: 30 helper reports (one failed, 8 with a PR chip, one with a 200-character `done`), 4 status
replies of the lead's (`blocked` empty or `none`), then the 30th wake's reply, a request for a
decision with a real `blocked`; then the person's answer and the lead's reply. Thread B: the
person's message and the lead's `On it.`; a report and a status reply; a report from a helper
thread that began after that reply, answered with a question that ends in `?` and an empty
`blocked`; a report and a status reply; a report with a real `blocked` and no lead reply.

## What the page shows

- A ledger is a `list` named `Updates` inside the chat (`log` named `Messages`). Each event is a
  `listitem`: one row of at most 40 px, the longest `done` cut, never wrapped. The row holds the
  time (the message's clock time, `8:21 AM`), the creature (`img`, `Shark, …`) and name of the
  helper (of the lead for its own reply), the line, the state (`Done`, `Failed`) and, when the
  helper thread has a PR, a link named after it (`#201`) to the PR. The row has a `button` that
  toggles it; the PR link is not that button.
- The line is `finish_run.done` of the helper's run (of the lead's wake run for its reply); for a
  failed helper, its error.
- A folded event is not a message: the chat's `message` test ids are the person's, the lead's
  answers to the person and the decision, and nothing says `Open thread` or holds a report's prose
  until a row is opened.
- Clicking the row's button opens the full report below the row, inside the ledger and before the
  next row: the helper's prose and a link `Open thread` to `/bots/{helper}/threads/{helper thread}`;
  for the lead's row, its reply. The URL does not change and the row does not move. Clicking again
  folds it.
- A lead reply with a real `blocked` (empty and `none` are not real) is a normal message after the
  ledger, with the existing `Blocked:` line, not a row. So is a reply to the person's own message,
  however short, and a lead reply whose last line ends with `?`. A helper's report with a real
  `blocked` stays the article named after the helper, with `Open thread`. Each ends the ledger;
  the next folded event starts a second `Updates` list below it.
- A handoff line (`Handed off to Reviewer`) between folded events is a row of the ledger and appears
  nowhere else.
- Thread A's chat (`log` `scrollHeight`) is at most 2400 px at 800 px of window height: 3 screens.

## Acceptance mapping

| Acceptance (#180)                                                                                 | Test                                                                                         |
| ------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------- |
| One ledger between two person messages, one line per event (time, creature, who, line, PR, state) | `one ledger between two messages of the person: a row per event, no message per report`      |
| Clicking a line opens the full report in place                                                    | `clicking a row opens the full report in place and clicking again folds it`                  |
| A real-`blocked` or question reply is a normal message after the ledger                           | `a lead reply with a real blocked is a normal message after the ledger`, `a lead question …` |
| Thread of 30 reports and 1 decision fits in 3 screens at 800 px                                   | `the thread fits in 3 screens at 800 px`                                                     |
| `docs/design.md` states the rule                                                                  | docs PR #297                                                                                 |

## Not covered here

- "and also as the #147 card": the card lands with #147, which adds its own spec.
- The ledger's look (hairline border, no fill, muted colour, the red dot on `Failed`): the user
  checks it by eye.
- Replaying round 2's real lead thread: only the user has it.
- A helper report that carries a question and no `blocked`: the design folds it (it is the helper's
  question to the lead); not pinned, as the issue's wording could read either way.
- Reef wake lines (`PR #7 merged`) as rows: they join the ledger by the same rule as the handoff
  line but have no fixture here.
- Pages of messages: the thread is 39 messages, under the first page of 50.
