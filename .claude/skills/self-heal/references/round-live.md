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

## The driver's actions: every lens judges every one
The driver logs each action it takes (a message to a Bot, an approval answered, a merge it reports, a rebuild, a
restart, a setting changed) to `$REEF_HOME/actions.jsonl` (`{t, us, what, ref, expect}`) with `scripts/act.sh`.
Each pass, read the new lines. For every action, judge it from your lens within a few minutes and append one line to
your file under "Actions": `<t> <what> -> ok | finding <title>`, with evidence. Backend: did it store, start, log,
clean up? UI: what did the screen show, before and after, and does it match the data? Product: friction, waiting,
dead ends? Bots: how did the Bot take it? LLM: what did it cost, did it break the cache? `expect` is what the driver
thought would happen; a difference is a finding even when nothing crashed.

## Blind spots (round 1 missed a person's message being refused and lost; owner found it)
- Every "refused / blocked / 409 / disabled" rule in the spec or checklist is a candidate finding: ask what a
  person does now, and judge it by owner-taste.md (conversation first).
- Walk every user action end to end, including failure: in your own browser, intercept the non-GET request and
  answer it yourself with each realistic failure (409 busy, 401, 500, offline, slow); judge what the person sees.
  Real data is still never changed.
- A refusal or failure a person hit must leave a trace (log line, event); missing observability is a finding.
- Watch the person's side too: compare the driver's actions (messages typed, clicks) with the `messages` table and
  run starts; any gap is a finding.

## Lens focus (one per observer, Sonnet)
- backend: run lifecycle, delegation (helper threads, inbox, wake, 3-helper cap, helper worktrees and cleanup),
  approvals pairing, provider argv/env isolation per provider and mode, skill placement/removal, log errors,
  leftovers, memory pressure.
- ui: own headless browser (non-GET blocked). Two halves, both required:
  1. Truth: Helpers region, handoff lines, helper reports, helper threads, island notifications, sidebar tree
     (N more, popup), New markers, PR chips, status words; compare each screen with the runs/*.jsonl and DB at the
     same moment.
  2. Product design, as a user and a designer, not as a spec checker. docs/design.md is the current answer, not the
     limit: challenge it where the product would be better. Use Reef for real (read long threads, follow parallel
     helpers, answer approvals, browse memory and skills) and judge how it feels: chat layout and reading width,
     message density and rhythm, typography and alignment, streaming and perceived speed (does text appear while a
     Bot works?), cards (approval, change, memory, report, PR), empty, loading and error states, sidebar and island
     at scale, keyboard flow, motion, dark/light, narrow widths, microcopy. Compare with the best chat and agent UIs
     you know (Claude.ai, ChatGPT, Cursor, Linear, Slack) and say what they do better or worse. For each idea: the
     screenshot of today, the problem in one sentence, the proposal, and where it pays off; build a small HTML mockup
     under $REEF_HOME/findings/mockups/ and screenshot it when words aren't enough. Mark these "experiment" so they're
     kept apart from bugs; name the design.md decision an idea would reopen, with the reason.
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
