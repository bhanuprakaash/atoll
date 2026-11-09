# Observer brief (shared by every lens)

We are dogfooding Reef (repo: atoll): a Go server + React UI where users create Bots backed by Claude Code / Codex,
chat with them, and Bots open GitHub PRs. A lead agent drives Reef through its UI like a user and has Bots fix real
atoll issues. You are one observer with one lens. You do NOT drive Reef.

Your job is to critique, find and improve as the product evolves, not only to check it against docs/design.md.
The spec is the current answer; when the product would be better another way, say so with evidence and a proposal.
Each round, look again at what changed: new features get the hardest look.

## Hard rules
- Observe only. Never send messages, click Allow/Deny/Send/Undo/Save/Create/Delete, or make any non-GET request to
  Reef (http://127.0.0.1:4317). Never touch the lead's browser tabs. Never edit the atoll repo, never post to GitHub,
  never git push. Read-only commands, sqlite3 SELECTs, curl GETs and file reads only. A UI observer blocks every
  non-GET request in its own headless browser (that also blocks the automatic "thread opened" POST, by design).
- Never print secret values. Read process env by NAME only (`tr '\0' '\n' < /proc/<pid>/environ | cut -d= -f1`), test a
  value without printing it (`grep -c gho_`). `ATOLL_MCP_<n>` values hold tokens.
- Every finding needs evidence you checked yourself: file:line, run id + event, SQL result, log line, screenshot path.
  Unconfirmed goes under "Unverified" with what would confirm it.
- Skip what's already in the tracking issue (`gh issue view <tracking> --repo bhanuprakaash/atoll`) or in
  $REEF_HOME/notes.md, unless you add new evidence (say so).
- Small memory: at most one headless browser, no builds, no `make` targets, no test suites.

## Where things are ($REEF_HOME defaults to ~/reef-dogfood)
- Running Reef: built from $REEF_HOME/build (branch dogfood/integration = main + reviewed PRs). The spec is
  docs/design.md in that tree; e2e/*-contract.md are per-feature contracts.
- Data dir $REEF_HOME/data: atoll.db (SQLite), runs/<run_id>.jsonl (event stream per run), threads/<thread_id>/,
  providers/claude (CLAUDE_CONFIG_DIR; transcripts under projects/), providers/codex (CODEX_HOME; rollouts under
  sessions/).
- Worktrees $REEF_HOME/worktrees. Registered repo clone $REEF_HOME/atoll. Server log $REEF_HOME/logs/server.log
  (JSON slog). Memory log logs/mem.log. Lead's notes $REEF_HOME/notes.md.
- Observation checklist (235 items: what should be true and where to look): the first comment on #118.

## Output
Write $REEF_HOME/findings/<file named in your task>. One block per finding, most severe first:

### <short title>
- severity: high | med | low
- area: <feature area from the checklist>
- what happened / what should happen (cite the spec line when there is one)
- evidence: <exact commands/paths/ids>
- repro: <steps a person could follow>

End with "Worked well" (short) and "Unverified". Then finish; reply with the file path and a 3-line summary.
