# Round N: live observation (read observer-brief.md first; all its rules apply)

Bots are fixing this round's tickets in parallel. The lead Bot delegates to the coders, comment-sicko and reviewer;
each ticket is a test PR then a fix PR. Reef runs from the local integration build.

## Watch live, not after
- Loop until the file $REEF_HOME/round-<N>.done exists. Each pass (about every 2 minutes; use
  `until [ -f … ]; do sleep 20; done`-style waits, never long blind sleeps):
  - new lines in logs/server.log (WARN/ERROR, run started/finished),
  - new or growing data/runs/*.jsonl (tool calls, approvals, approval_closed, run_finished),
  - the runs, threads, messages, inbox and changes tables,
  - live claude/codex processes (`ps -ww`, env NAMES only), worktrees, /tmp leftovers, logs/mem.log.
- Append each finding as soon as it's confirmed, with a UTC time and the run/thread id, so the lead can act mid-round.
  Mark ones still being checked "checking".
- Don't repeat findings already in the tracking issue or earlier in your file.

## When round-<N>.done appears
Read the other lens files for this round (round<N>-*.md). Add a "Challenges" section to yours: for each of their
findings you think is wrong, overstated or a duplicate, say why with evidence; confirm the ones you re-checked.
Then finish and reply with your file path and a 3-line summary.

## Lens focus (one per observer, Sonnet)
- backend: run lifecycle, delegation (helper threads, inbox, wake, 3-helper cap, helper worktrees and cleanup),
  approvals pairing, provider argv/env isolation per provider and mode, skill placement/removal, log errors,
  leftovers, memory pressure.
- ui: own headless browser (non-GET blocked); Helpers region, handoff lines, helper reports, helper threads, island
  notifications, sidebar tree (N more, popup), New markers, PR chips, status words; compare each screen with the
  runs/*.jsonl and DB at the same moment.
- product: friction and features worth adding with evidence: how the lead plans, how often the user steps in,
  waiting, approvals that add no safety, what a user needs to trust parallel work; cite docs/design.md (and Queue).
- bots: judge every run against its brief, persona and AGENTS.md (test-first, e2e untouched by fixers, smallest diff,
  conventional commits, real Proof, honest replies and finish_run, memory use, clear delegate briefs, no wasted
  turns); compare Claude and Codex; name what in Reef (prompt, tools, missing context) made a Bot fail.
- llm (cost, context, caching): per Bot and model, tokens in / cache-write / cache-read / out and estimated cost
  (Claude transcripts carry `usage`; Codex rollouts carry `token_count`); context growth across resumed runs and
  compaction; prompt-prefix stability and cache survival between runs (5-minute TTL); repeated big reads and pasted
  logs; waste (no-op runs, boss wake runs, nudges, title sessions, model choice); what Reef records and shows about
  spend. Each finding: numbers, estimated saving, smallest change in Reef.
