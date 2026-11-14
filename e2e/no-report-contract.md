# Runs without a report: test contract

Extends [request-lifecycle-contract.md](request-lifecycle-contract.md). Design: `docs/design.md`,
Threads and runs (the reply, "A run has no report"), Delegation (waking the boss, background runs,
the guard), Prompt, App and UI (thread labels, the status line). Tests build the real server with
the scripted fake providers; the real-provider lines and the before/after timings of the ticket are
checked by hand.

## What the fakes add

- `<provider dir>/<provider>.log.prompts` gets a record of kind `tools` per provider process:
  `{server: [tool, ...]}` with the names each connected MCP server listed to that process, sorted.
  The record has the pid of the `user` record of the same process, which is how a test finds the
  tools of a run kind.
- Recordings: no recording calls `finish_run`; the final message is the last text. `final-answer`
  answers, calls `remember`, then writes its final message. `helper-blocker` (prompt `Run the checks,
  then report the blocker.`) answers `Tester here: blocked on the release notes.` at once;
  `wake-tell` (a resumed boss session, prompt `blocked on the release notes`) calls `tell_user`
  twice, then writes `Told the user about the blocker.`; `wake` (prompt `Tester`) writes `Thanks,
  Tester.`. `lifecycle-answer` is an answer with nothing after it.
- `testdata/golden/system.txt` is the code Bot's prompt of the design for Ada, with the user Bhanu
  and the persona `A careful Go engineer.`

## Reply and report

- The reply is the final message: text before a `remember` call is progress, in the events and the
  event log only; the stored reply and the one Bot message are the text after it.
- `finish_run` is in no run's tool list (ordinary, boss, helper, background, platform), no nudge is
  sent (the provider reads one user line per run), and `GET /api/runs/{id}`, `GET /api/threads/{id}`,
  its messages and the Bots list hold none of `report`, `waiting`, `done`, `tried`, `blocked`, `next`,
  `ask`, `ready`, at any depth. A run stored before keeps its `report` column, byte for byte, across
  launches (seeded into `testdata/before-modes.sql`'s schema, the way `TestBotMode_LaunchMovesOldBotsToAutoOnce` does).

## Thread labels

`state` of a thread in `GET /api/bots` is `needs_you`, `running`, `failed`, `limited` or `new`
(Replied), or empty; priority in that order, with `limited` below `failed`. Replied and Failed clear
at `POST /api/threads/{id}/opened`. A helper thread is never `new`. The sidebar row carries the
words `Needs you`, `Replied` (with `data-marker="new"`), `Failed` and `Out until <time>`; wave dots,
`data-marker="running"`, for Working. Where the design leaves it open the tests do not pin it:
whether a rate-limited run reads Failed or Out until before the person opens it.

## Background runs

A run whose messages are all Reef's (a helper's notice, a PR wake) is a background run. It adds no
Bot message and does not make the thread Replied (the person opened it first); the Bot's final
message is the status line above the composer, muted (`#9B9B9B`), newest only. The prompt starts with
the line `Reef: background run. {user} does not see your final message, so end with one line of
status. If {user} must act (a PR to merge, a decision, a failure you cannot fix), tell them with
tell_user.` and the run lists `tell_user`; each call adds one Bot message and makes the thread
Replied. A run with a message of the person lists no `tell_user`, has no such line and its final
message is the reply. Guard: a notice that holds a failure Reef knows of (a helper failed, was
stopped or hit its limit; CI failed) with no `tell_user` posts the final message as a reply and the
thread is Replied. A deleted helper is not in the design's list, so no test pins its wake either way.

The notice is `Reef: {Helper} finished thread {id}:` on the first line, then the helper's reply,
then the helpers line; no `Done:`, `Tried:`, `Blocked:` or `Next:`. Every notice starts `Reef: `.
The notice is a stored message of the boss thread (`from_bot_id`, listed by the messages API) but
never a message of its chat: the UI shows neither it nor the helper's reply in the boss thread.

## Prompt

`prompt_text_test.go` holds the design's platform prompt, one constant per section, copied word for
word (`{bot}`, `{user}`, `{boss}` filled), and assembles it in the design's order: core, code part
(the helper variant on its boss's branch), boss part, helper part, platform block, then
`\n\nPersona:\n` and the persona. Pins where the design is not literal:

- the first paragraph is two lines, as the design wraps it;
- the design's headings `Leading helpers (boss part)` and `Working for {boss} (helper part)` are
  `Leading helpers` and `Working for {boss}`; the design's `(helper on its boss's branch, instead)` is
  not text;
- in a helper thread the core's first line reads `{boss} talks to you in this thread.` (the design:
  the helper part's "first line names its boss instead of {user}");
- the budgets are the design's: a code Bot's system prompt at most 500 tokens (2000 bytes), a boss's
  addition at most 150 tokens, the platform block at most 2000 bytes.

Existing Bots with repo access (`repo_access` NULL, `"all"` or a non-empty list) get
`Never merge: {user} merges.` as the end of their persona at the launch that upgrades them (`{user}`
read from the `user_name` setting), once: not again at the next launches, not to a Bot with no repo
access, Chief, or a Bot made later. The separator between persona and line is not pinned.

`delegate`'s description holds no freeze sentence or `finish_run`; the design gives `track_mr` no
wording, so its description is only checked for the same two. Both pass on the base.

## Acceptance mapping

| Acceptance line of #434 | Tests |
| --- | --- |
| Final message is the reply; earlier text is progress | `TestFinalAnswer_ReplyIsFinalMessage`, `TestFinalAnswer_EarlierTextStillStreams`, `TestFinalAnswer_NoToolCallKeepsWholeText` |
| `finish_run` in no tool list; no nudge; no `report`/`waiting`; stored rows kept | `TestNoReport_FinishRunIsInNoToolList` (plus `TestBackgroundRun_TellUserReachesThePerson` for the background run), `TestNoReport_NoNudgeIsSent`, `TestRequestLifecycle_NothingIsSentAfterTheRequest`, `TestNoReport_APIHasNoReportOrWaiting`, `TestNoReport_StoredReportKeepsItsRow` |
| One label per thread, in priority; clears when opened; helper never Replied | `TestThreadLabels_*`, `ui/thread-labels.spec.ts`, `ui/sidebar-tree.spec.ts` |
| Background run adds no message, not Replied, status line | `TestBackgroundRun_HelperNoticeAddsNoMessage`, `TestPRWake_MergedWakesTheBot`, `TestPRWake_ClosedUnmergedWakesTheBot`, `ui/thread-labels.spec.ts` |
| Background run lists `tell_user` and starts with the line; `tell_user` adds a message; person's run lists none | `TestBackgroundRun_TellUserReachesThePerson`, `TestBossQuiet_PersonTakesHeldReports`, `ui/thread-labels.spec.ts` |
| Guard | `TestWake_HelperFailedStoppedDeleted`, `TestUsageLimit_WakeSaysHelperIsOut`, `TestPRWake_CIFinishedWakesOnce` |
| Helper's report not in the boss chat; the notice text | `TestWake_ReportWakesBoss`, `TestFinalAnswer_HelperReplyRelaysFinalMessage`, `ui/helpers.spec.ts` |
| Prompt word for word; budgets | `TestPromptText_*`, `TestMemory_PromptGoldenAndBudget`, `TestPrompt_ChiefPlatformBlock` |
| `Never merge` appended once | `TestNeverMerge_AppendedOnceToBotsWithRepoAccess` |
| `delegate` and `track_mr` descriptions | `TestReadyNamesCommit_ToolDescriptionsHaveNoFreezeSentence` |
| No Waiting card, Blocked line, ready card, ledger; status line as #399 | `ui/thread-labels.spec.ts`, `ui/chat.spec.ts` |

Deleted with the subjects they tested (test-guard flags them): the `finish_run` report, refusal and
nudge tests, the Waiting on you card, the ready-PR rows and the lead's ledger.
