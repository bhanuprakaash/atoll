# Run cost test contract

Issue #193; the design is in `docs/design.md` (Threads and runs, sidebar, run footer). The real
binary runs scripted Claude and Codex sessions; no product package is imported.

Recordings `testdata/{claude,codex}/run-cost-1.jsonl` and `run-cost-2.jsonl` are a first run and a
run that resumes its session. Claude: model `claude-sonnet-5-5`; run 1 is 7 assistant messages
(`num_turns` says 4, so it is not the count), run 2 is 3. `result.usage` is per process;
`total_cost_usd` and `modelUsage` are session totals (0.19, then 0.24), as in the real recordings
`resume-1` and `resume-2`. Codex: model `gpt-5.6-sol`; run 1 has 3 `thread/tokenUsage/updated`
events, run 2 replays one of the earlier turn first, then 2 of its own, and `total` is
thread-wide.

| Run | calls | input | output | cache read | cache write | cost |
| --- | --- | --- | --- | --- | --- | --- |
| Claude 1 | 7 | 21 | 1037 | 71620 | 13150 | 0.19 |
| Claude 2 | 3 | 8 | 365 | 40000 | 700 | 0.05 |
| Codex 1 | 3 | 1200 | 125 | 26100 | 500 | none |
| Codex 2 | 2 | 200 | 75 | 18500 | 200 | none |

`GET /api/bots` gives the Claude Bot `cost_24h_usd` 0.24 and the Codex Bot none (or 0). The run
footer ends with `Sonnet 5.5, 7 calls, $0.19` (Codex `gpt-5.6-sol, 3 calls`, no `$`) in the muted
colour of the run's time. The sidebar row shows `$0.24` for the Claude Bot and no `$` for the
Codex Bot or a Bot without runs.

## Acceptance mapping

| Acceptance | Tests |
| --- | --- |
| Claude run: `usage` with model, calls, four token counts, `cost_usd`; Codex: tokens, no cost | `TestRunCost_RecordsUsagePerRun` |
| A resumed run counts only itself | `TestRunCost_RecordsUsagePerRun` (second run, first unchanged), `run-cost.spec.ts` footers |
| Run footer one muted line; Bots list last-24 h cost | `run-cost.spec.ts` |
| `make e2e` with fakes checks the fields exist | the same tests |
| Real Claude total within 2% of the transcript's `cost-state` delta | owner (PR, Only you can check) |
