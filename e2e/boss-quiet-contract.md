# Held notices and the wake window: test contract

Extends [wake-contract.md](wake-contract.md). Design: `docs/design.md`, Delegation (One run for what
is held, The wake window, Background runs) and Threads and runs.
Tests: `boss_quiet_test.go`. They build the real server; no product package is imported. Builds on
the queued-message design (#150): held Bot messages join the person's queued ones in one run.

## Knob

`ATOLL_WAKE_WINDOW` is a Go duration, 20 s when unset, invalid or not positive. Every test machine
sets it to `200ms` (`_wakeWindow`), so the existing wake tests wait little; the window tests set
`12s` and `6s`.

## Fakes

No recording of its own: a helper plays `helper-report.jsonl` and a woken boss plays `wake.jsonl`
(`Thanks, Tester.`); the quiet and `tell_user` runs are in [no-report-contract.md](no-report-contract.md).

## What the tests pin

- Helper notices that arrive inside the window start no boss run until it is over; then one boss run
  gets them all: each is its own message (`from_bot_id` the helper Bot) with the same `run_id`, and
  the one prompt holds the headers `Reef: Tester finished thread {id}:` in arrival order.
- A person's message sent while a notice waits starts its run at once and takes the notice: same
  `run_id`, the person's text before the header; the window's end starts nothing more. The run is an
  ordinary one: no background line, no `tell_user`, and its final message is the reply.
- A notice held for a boss whose run the user stopped starts no run; the next message to the boss
  takes it (the person's text first).
- A restart that interrupted 3 helper runs of one boss thread: one message in the boss thread that
  names all 3 helper thread ids, from Reef (`from_bot_id` empty), first line exactly `A restart
  interrupted 3 helper runs.`, each helper's `Tester's run failed: {error}` in the order the runs
  began, and one boss run.

## Acceptance mapping

| Acceptance | Tests |
| --- | --- |
| Notices within the window start one boss run that sees all, headers in order | `TestBossQuiet_ReportsInWindowStartOneRun` |
| A person's message held with a notice goes in the same run, person first, as an ordinary run | `TestBossQuiet_PersonTakesHeldReports`, `TestBossQuiet_StoppedRunStartsNone` |
| A stopped run still starts none; held messages survive a restart | `TestBossQuiet_StoppedRunStartsNone`, `TestWake_SurvivesRestart` |
| A restart that interrupted N helper runs gives one wake naming all N | `TestBossQuiet_RestartWakesOnce/{claude,codex}` |

Not covered here: the lead's busy share under 25% over a 20-minute round with 3 helpers, and runs
that start within 10 s of the previous end over a 20-run window of a live boss. Both need a live
round; the PR lists the steps.
