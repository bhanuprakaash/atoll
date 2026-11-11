# The boss speaks only when needed: test contract

Extends [wake-contract.md](wake-contract.md). Design: `docs/design.md`, Delegation (One run for what
is held, The wake window, The boss speaks only when the person is needed) and Threads and runs.
Tests: `boss_quiet_test.go`. They build the real server; no product package is imported. Builds on
the queued-message design (#150): held Bot messages join the person's queued ones in one run.

## Knob

`ATOLL_WAKE_WINDOW` is a Go duration, 20 s when unset, invalid or not positive. Every test machine
sets it to `200ms` (`_wakeWindow`), so the existing wake tests wait little; the window tests set
`12s` and `6s`.

## Fakes

Two more recordings per provider, no fake code:

- `helper-progress.jsonl`, prompt `Run the checks, then send a progress note.`: as `helper-report`
  (asks to run `make check`, which the tests approve), but replies `Tester here: 2 of 5 checks
  done.` with an empty `blocked`.
- `wake-quiet.jsonl`, prompt `2 of 5 checks done`, only on a resumed boss session: calls `finish_run`
  and writes no text, so the run ends `done` with an empty reply. It is longer than `wake.jsonl`'s
  prompt `Tester`, so it wins for that report.

## What the tests pin

- System prompt of a Bot with a non-empty delegate list: a line `Helper reports`, then lines up to
  the next blank line that hold `finish_run` and `reply`. A Bot with no delegates has no such line.
  Wording beyond that is not pinned.
- A run that ends `done` with no reply adds no `bot` message and its thread's marker in
  `GET /api/bots` is not `new`; one with a reply adds the message and the marker is `new`.
- Helper reports that arrive inside the window start no boss run until it is over; then one boss run
  gets them all: each is its own message (`from_bot_id` the helper Bot) with the same `run_id`, and
  the one prompt holds the headers `Tester reported back from thread {id}.` in arrival order.
- A person's message sent while a report waits starts its run at once and takes the report: same
  `run_id`, the person's text before the header; the window's end starts nothing more.
- A report held for a boss whose run the user stopped starts no run; the next message to the boss
  takes it (the person's text first).
- A restart that interrupted 3 helper runs of one boss thread: one message in the boss thread that
  names all 3 helper thread ids, from Reef (`from_bot_id` empty), first line exactly `A restart
  interrupted 3 helper runs.`, each helper's `Tester's run failed: {error}` in the order the runs
  began, and one boss run.

## Acceptance mapping

| Acceptance                                                                          | Tests                                              |
| ----------------------------------------------------------------------------------- | -------------------------------------------------- |
| A progress-only wake ends with `finish_run` and no visible reply (persona line, e2e) | `TestBossQuiet_PromptTellsBossToStayQuiet`, `TestBossQuiet_ProgressWakeAddsNothing` |
| Reports within the window start one boss run that sees all (also 3 headers in order) | `TestBossQuiet_ReportsInWindowStartOneRun`         |
| A person's message held with a report goes in the same run, person first             | `TestBossQuiet_PersonTakesHeldReports`, `TestBossQuiet_StoppedRunStartsNone` |
| A stopped run still starts none; held messages survive a restart                     | `TestBossQuiet_StoppedRunStartsNone`, `TestWake_SurvivesRestart` (existing) |
| A restart that interrupted N helper runs gives one wake naming all N                 | `TestBossQuiet_RestartWakesOnce/{claude,codex}`    |

Not covered here: the lead's busy share under 25% over a 20-minute round with 3 helpers, and runs
that start within 10 s of the previous end over a 20-run window of a live boss. Both need a live
round; the PR lists the steps. Whether a real model stays quiet on a progress report is not tested:
the fakes play the recording.
