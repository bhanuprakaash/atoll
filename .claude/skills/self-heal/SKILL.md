---
name: self-heal
description: Run Reef's self-heal loop - Reef's own Bots fix Reef. Drive the real app through its UI like a user, have Bots fix real atoll issues in parallel through delegation, watch every run live with observer subagents, review each PR, rebuild and verify, and log every gap. Use for "self heal", "dogfood Reef", "next round", "drive the bots", or picking up a handed-off round.
---

# Self-heal: Reef fixes Reef

You are the person in charge: product owner, PM and reviewer. Reef's Bots do the coding. You talk to them in plain
words like a human would, never with tool-call recipes. The owner merges every PR; you never merge.

## Pick up first
1. Read the latest **"Handoff"** comment on the tracking issue (#118, `gh issue view 118 --comments`). It has the round,
   open tickets and PRs, the decisions made, the Bot ids and what's next.
2. `$REEF_HOME` (default `~/reef-dogfood`) is machine-local: data, keyring, worktrees, clone, build, logs, findings.
   If it's gone (new machine), do Setup. If it's there, check Reef answers on `http://127.0.0.1:4317/api/settings`
   and start it if not, detached: `setsid nohup scripts/run-reef.sh >> $REEF_HOME/logs/server.log 2>&1 < /dev/null &`.
   Never as a harness background task: those stop after 2 hours and take Reef and every running Bot down.
3. Never redo setup steps that already hold: sign-ins, connections and Bots live in `$REEF_HOME/data`.

## Setup (only on a fresh machine)
- Clone atoll to `$REEF_HOME/atoll` (the repo Bots work in). `git worktree add $REEF_HOME/build -b dogfood/integration
  origin/main` for the build; never push that branch.
- Build: in `build`, `make ui && go build -o $REEF_HOME/bin/atoll ./cmd/atoll`. Start with `scripts/run-reef.sh`
  (own D-Bus + keyring under `$REEF_HOME/keyring`; without it, saving a token fails with `dbus-launch` not found).
  Set `worktree_root` to `$REEF_HOME/worktrees` (`PUT /api/settings`).
- Onboarding in the UI. Provider sign-ins happen in Reef's own folders, not the shell's: the owner opens the link
  you give them. Collect codes with the private secret card (`request_secret`, delivery file), never in chat.
  Claude: post the code to `POST /api/providers/claude/sign-in/code`. Codex: its callback goes to
  `127.0.0.1:1455` on this machine; the owner pastes the failed callback URL, you `curl` it, then also load
  `http://127.0.0.1:1455/success` or the login never exits. GitHub: device code at github.com/login/device.
- Ask Chief, in plain words, to register atoll, import the owner's agents and skills from `~/.claude`, and create the
  team. Current team: poteto-agent (lead, Sonnet, delegates to the others, plans only), poteto-coder (Sonnet),
  codex-coder (Codex, Full access: this machine is a VM), comment-sicko, reviewer (Opus). All on atoll + GitHub,
  PRs to `main`, each Bot its own look.

## Owner's taste
`references/owner-taste.md` is the living statement of what the owner wants. Whenever the owner reacts to something,
work out the intent behind it, write it there as a general principle (not the literal remark), and pass it on: tell
the lead in its next brief, ask Chief to remember it as a user memory so every Bot gets it, and make sure the next
observers read it. Apply it yourself when triaging and reviewing.

## A round
1. **Triage.** Turn open findings from the tracking issue into tickets (`.github/ISSUE_TEMPLATE/default.md`: Goal,
   Acceptance lines anyone can observe, Out of scope). Group small findings that touch the same files into one
   ticket. Check each against the code before filing.
2. **Kick off.** In a new thread with the lead (repo atoll), say which tickets, the order, and the house rules:
   the owner's principles that apply (from owner-taste.md), test PR by one coder and fix PR by another, reviewer checks both, at most 3 helpers busy, only targeted specs
   locally (CI runs `make e2e`), merges can lag so keep moving, send a status when a PR is ready for review.
3. **Observers, live.** Start five Sonnet subagents in the background (delegate_task, role review), one per lens
   (backend, ui, product, bots, llm), each told to read `references/observer-brief.md`, `references/owner-taste.md`
   and `references/round-live.md`
   and write `$REEF_HOME/findings/round<N>-<lens>.md`. They watch while Bots work and challenge each other at the end.
4. **Log every action.** Right after each thing you do in Reef, `scripts/act.sh "<what>" <thread|PR> "<expected>"`.
   Every observer judges every action from its lens; that's how bugs on any side surface.
5. **Drive.** Run `scripts/wait-any.sh $(date +%s%6N)` in the background after every action you take; it wakes you for
   an approval card, a lead reply that asks for review or a decision, a failed run, or low memory. Answer decisions
   quickly (the lead stalls on them). Judge approval cards by the command (`scripts/pending.sh <bot>`) and click in the
   UI. Nudge the lead when slots sit idle.
6. **Review.** For each PR the lead says is ready: read the diff, check CI (an e2e test PR fails only on its new test),
   comment on the PR like a colleague, send fixes back through the lead. Tell the owner which PRs are ready, with
   checks. Test PRs merge before fix PRs (test-guard). Tell the lead when the owner merged, since nothing wakes it.
7. **Integrate without waiting.** Merge reviewed PR branches into `dogfood/integration` (`scripts/rebuild.sh
   origin/<branch> …`); Bots keep branching from `origin/main`, so PRs stay clean.
8. **Feed misses back.** When the owner (or you) finds something no observer reported, write why each lens missed it
   and what changes, tell the running observers (append to their brief on disk), and add the lesson to
   `references/round-live.md` "Blind spots".
9. **Close the round.** `touch $REEF_HOME/round-<N>.done` (observers challenge each other, then finish). Rebuild,
   stop Reef, swap `bin/atoll.new` in, restart on the same data (tests upgrade + restart recovery), reload the
   browser. Start one verify subagent: re-run every acceptance line of the round's tickets against the running app
   and record command + output. Do the real-provider lines yourself (e.g. a real Chief run). Merge confirmed findings
   into the tracking issue, tick ticket lines with proof, then post a new "Handoff" comment.

## Rules that cost us before
- **Recording**: the preview recorder holds video in browser memory (2.7 min = 3.5 GB RSS, 19 MB file; 50 MB cap).
  Record short clips of key moments only; copy them to `$REEF_HOME/recordings`. A long recording hangs on stop and
  is lost.
- **Reef must outlive your tools**: start it with `setsid nohup` (see Pick up). Round 1 lost Reef at the
  2-hour background limit; restart recovery worked (the dead run failed cleanly and woke its boss).
- **Memory**: 15 GB, no swap possible. At most 3 Bots building at once; full `make e2e` only in CI.
- **Never `pkill -f <pattern>`** in a command line that contains the pattern: it kills your own shell.
- **Secrets**: never print env values; tokens sit in `ATOLL_MCP_<n>`. An observer once leaked the GitHub token this way.
- **Check every message landed**: Reef refuses a message while the thread's run is going (until #150), shown only as
  red text under the composer. After sending, confirm the newest `messages` row is yours; if not, wait for the run
  to end and send again.
- **Waiters**: pass a fresh `since`; a stale one hides lead messages and the lead sits blocked on you.
- Codex on Auto asks approval for every worktree step and network call (#122); Full access is the VM workaround.
- Chief has no repo access: it can't read AGENTS.md, so tell it repo rules (PRs go to `main`) yourself.
- Evidence before logging: reproduce or capture the log/DB state; observers' findings get a spot check.

## Handoff comment (post on the tracking issue at round end, or when asked to hand off)
`## Handoff <date>` then: round and state; tickets with their test PR, fix PR and status; PRs ready for the owner;
decisions made and why; what the lead is doing; Bot names and ids (`sqlite3 $REEF_HOME/data/atoll.db "select
id,name,provider,mode from bots"`); integration branch tip; next steps.
