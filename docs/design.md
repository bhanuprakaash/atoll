# Reef (working name: atoll) — design

Status: draft for review. Design only; nothing is built. Organised by topic; each rule is stated once.

## What it is

A Mac app where you configure Bots and chat with them. A Bot works like a typical chat bot: you
can ask it questions, think out loud, explore or investigate. When the work needs code, it does it,
tests it, opens an MR and brings back the result.

It replaces the old Reef. The old Reef is a source of ideas and code to copy when a ticket needs it,
never a dependency.

## Words

| Word | Meaning |
| --- | --- |
| Bot | A configured agent: persona, Model, skills, connections, repo access, delegate list, look |
| Chief | The Bot that exists out of the box and manages the platform |
| Thread | A chat with one Bot |
| Run | One message handled: the agent starts, resumes the Bot's session, replies, exits |
| Repo | A git repo registered once in the app |
| Provider | An installed agent CLI the app drives: Claude Code, Codex |
| Connection | A signed-in external service: GitLab, GitHub, Slack |
| Skill | A `SKILL.md` folder in the app's skill library |
| Memory item | A saved decision, convention, finding, fact or lesson |

## Bots

- Settings: name, persona, Model, skills, connections, repo access, delegate list, look.
- Persona holds expertise only. Who the Bot talks to comes from the run, not the persona.
- Model is one field, e.g. `Claude Code · Opus 5.5`, `Codex · GPT-5`, listing only enabled, ready
  providers. The models come from asking each provider.
- Repo access: All (default) or Only these. *A block list would silently grant new repos.*
- Codex runs in its auto mode and Claude in default mode with Reef's permission prompt tool, *so
  its approvals reach the thread*. No per-tool lists, no platform hard limits. Approval requests
  from any run, children included, come to the user in the thread. *The provider already decides
  what needs approval.*
- Bot settings changed mid-run apply from the next run.

**Chief.** Created on first launch. It has the "Can manage the platform" switch on: tools to create
and edit Bots, skills and repos, and to attach connections, served as Reef's own MCP tools. Sign-ins
stay with the user. Every change shows in the thread with undo. Other Bots can get the switch later.
Undo: each change stores the entity before and after; Undo writes `before` back, or removes an entity
the change created, and keeps the record marked undone. *Records survive restart; no undo of undo.*
Undo of a repo registration is refused while a thread uses the repo.

## Threads and runs

- Thread records retain the tracked MR title and state, close time, and worktree cleanup and snapshot
  status so the chat and thread list can show them after a restart. `track_mr` accepts the MR title
  with its URL. The thread list includes the current work verb and pending approval.
- A thread is a chat with one Bot. Each message starts a run that resumes the Bot's session and
  exits after replying. No agent process lives between messages.
- Resume by default. A fresh session only comes from starting a new thread or delegation.
- Every run ends with `finish_run(done, tried, blocked, next)`, stored as data; the chat reply stays
  2–3 plain lines. *The report feeds memory, parents and fresh starts.* A turn that ends without
  the report gets one more prompt in the same session asking for it; its text is not shown. If
  there is still none, the run ends `done` with `report: null` and a logged warning.
- One provider process per run, in the thread's folder (`<data>/threads/<id>`): a provider
  resumes a session only in the folder that started it. One run per thread at a time; a message
  sent while one is going is refused (`409`).
- Reef's own MCP tools (`finish_run`, and `approve` as Claude's permission prompt tool) are served
  over streamable HTTP on Reef's own server at `/mcp/<run token>`. *They need live server state
  (open approvals, the run being reported); stdio would need a child that calls back anyway.*
  Reef's own tools are allow-listed, so they never ask the user.
- Codex runs with `approvalPolicy: on-request` and the `workspace-write` sandbox, its auto mode;
  approvals come as app-server requests. Claude runs in default mode; approvals come through the
  permission prompt tool.
- Each run's event stream is saved to `<data>/runs/<id>.jsonl`. A run left `running` or `waiting`
  when the server starts is marked `failed` with a reason. A thread's SSE stream first replays the
  going run's latest events (text deltas left out), so a page that opens late or reconnects shows
  the current tool and approvals.
- Processes a Bot starts (dev server, watcher, browser) belong to the thread and outlive the run.
  When the thread closes or the app quits, the app asks whether to stop them.
- A thread that needs a browser gets its own Chrome, driven over the DevTools protocol and streamed
  into a pane the user can take over (v2).

## Delegation (v2)

- A Bot may delegate only to Bots on its delegate list. Depth 1: a delegated run cannot delegate.
  *Cycles become impossible.*
- A delegation is a thread where the parent Bot is the asker: `delegate(bot, brief)`,
  `continue(thread, message)`. Continue for the same work; start a new one for different work.
- A child's questions go to its parent; approvals go to the user. The child gets one line at the
  start: `{parent} delegated this to you; your replies go to {parent}.`
- A child works in its own worktree branched from the parent's branch (`<parent-branch>--<bot>`).
  The parent merges it and opens the MR, and owns the result.
- Children show nested in the parent's thread.

## Repos and worktrees

- Repos are registered once. A thread works in one repo.
- A repo registers without a commit; a thread on it is refused until it has one.
- A thread gets a worktree on its first edit, at `~/Reef/worktrees/<repo>/<branch-folder>` (root is
  a setting). Read-only threads use a clean checkout.
- The Bot names the branch `<type>/<slug>` (feat, fix, chore, docs) at first edit; the app makes it
  lowercase, short and unique (`-2`). Fixed once created.
- The worktree is removed when the MR merges or the thread closes; uncommitted work is saved first.
- The read-only checkout is a detached git worktree of the repo's HEAD at `<data>/threads/<id>`,
  outside the worktree root. It is removed when the thread closes.
- A new branch starts at `origin/HEAD` when it resolves, else HEAD. The server never fetches.
- The slug is lowercased, every run of characters outside `[a-z0-9]` becomes `-`, and it is cut to
  40 characters. The worktree folder is the branch with `/` replaced by `-`.
- Repo names are unique, because the worktree folder uses them.
- Unfinished work is saved as a commit at `refs/atoll/unfinished/<thread id>` before removal. A
  failed save removes nothing and leaves the thread open.
- A thread's cwd moves from the checkout to its worktree. Claude resumes only the sessions of its own
  cwd, so the Claude adapter copies the session file into the new cwd's project folder first.
- Atoll checks each MR with `glab mr view` every minute (`ATOLL_REPO_POLL_INTERVAL`, a Go duration).
  A merged MR with a worktree is cleaned up like a closed thread, but only while no run is going in
  the thread. One watcher runs inside the server and resumes after a restart.

## Providers

- Claude Code and Codex. A Providers page shows, per provider: found or not (login-shell PATH plus
  common install folders), version, signed in, enable switch, path override, and a read-only list of
  the provider's own MCP servers and skills marked "not used by Bots". Checked on start, before each
  run and on Refresh. *A Finder-launched app lacks the shell PATH.*
- Each provider runs from a Reef-owned settings folder (`~/Library/Application Support/Atoll/providers/claude`,
  `…/codex`), signed in once from the Providers page: `CLAUDE_CONFIG_DIR=<dir> claude auth login`,
  `CODEX_HOME=<dir> codex login`. Never copy or link login files; never change `HOME`. *Same pattern
  as T3 Code; Bot sessions stay out of the user's own history.*
- Every run also sets:
  - Claude Code: `--strict-mcp-config` with only Reef's servers, `ENABLE_CLAUDEAI_MCP_SERVERS=false`,
    `disableAllHooks`.
  - Codex: `skills.bundled.enabled=false`; plugins, remote plugins and apps off; each skill in
    `~/.agents/skills` disabled by the path of its `SKILL.md`, since a skill's name can differ from
    its folder (Codex finds that folder through `HOME`).
- Env knobs, for tests and dev: `ATOLL_DATA_DIR` replaces `~/Library/Application Support/Atoll`;
  `ATOLL_PROVIDER_PATH` (PATH syntax) is the only place discovery looks; `ATOLL_CLAUDE_PATH` and
  `ATOLL_CODEX_PATH` are the path override until settings are stored.
- Providers get the parent env minus `ANTHROPIC_*`, `CLAUDE*`, `OPENAI_*` and `CODEX_*`, with PATH
  set to the discovery folders first.
- Transport: Codex through `codex app-server` (JSON-RPC over stdio). Claude Code as
  `claude -p --input-format stream-json --output-format stream-json`, driven from Go. Copy both
  adapters from old Reef (`agent/codex`, `agent/claude`). *The Claude Agent SDK runs the same CLI
  but needs a Node helper; revisit if stream-json gets hard to follow.*
- Verified on codex-cli 0.165 and Claude Code 2.1.317: copying Codex's `auth.json` breaks logins
  (single-use refresh tokens); a fresh `CLAUDE_CONFIG_DIR` is signed out until logged in there; with
  the flags above neither provider shows personal MCP servers, skills or subagents; Bot skills load
  from `<config dir>/skills`; resume works inside the folder. Hiding claude.ai connectors is untested
  (the test account had none).

## Connections and skills

- Bots see only Reef's connections and skills, never the provider's own.
- Connections are built in, each with a sign-in flow, no token pasting. Each Bot picks its own. One
  sign-in feeds both the service's MCP server and its CLI (`OAUTH_TOKEN` for `glab`, `GH_TOKEN`).
  - GitLab (v1): OAuth device flow, instance URL (default gitlab.com); Bots use `glab`. *GitLab's own
    MCP server is beta and needs Premium/Ultimate plus Duo.*
  - GitHub (v2): hosted MCP server `api.githubcopilot.com/mcp/`.
  - Slack (v2): needs a small hosted token exchange. *Its MCP server wants a confidential client;
    desktop PKCE sign-in cannot get bot scopes and expires in 30 days.*
- The device code stays in server memory, so a sign-in pending at restart is started again.
- A run with a connected GitLab connection gets `OAUTH_TOKEN`, `GITLAB_HOST`, `GLAB_CONFIG_DIR` (a
  token-free config marking the host `is_oauth2`) and a git credential helper given only through env
  (`GIT_CONFIG_*`), which clears the user's own helpers. Never `GITLAB_TOKEN`: glab sends it as a
  personal access token, which an OAuth token is not; the user's own is dropped from the env. The
  token is never written to a file or log.
- Tokens live in the OS secret store through one cross-platform library. Targets: macOS and desktop
  Linux; headless Linux is out.
- Skills: one library in standard `SKILL.md`, created in the app; each Bot picks its skills. Each run
  gets read-only copies of its Bot's skills in its own folder under the provider's Atoll folder
  (`runs/<run id>`), removed when the run ends: Claude through `--add-dir` (copies in
  `.claude/skills`), Codex through `skills/extraRoots/set` before the thread starts, with
  `allowed-tools` and `context` stripped. *Shared `<config dir>/skills` would leak one Bot's skills into
  an overlapping run and change files a live run reads.* A provider without skills gets a list in the
  prompt and reads the file on demand. Never written into the repo. Skills the repo commits itself (`.claude/skills`,
  `.agents/skills`) still load, like its CLAUDE.md.

## Memory

- Old Reef's `memory` package, copied in: kinds decision, convention, finding, fact, lesson;
  candidate/accepted/retired; `replaces` keeps history; full-text search, optional embeddings,
  lookalike detection.
- Scopes: user (everywhere, any provider), repo, bot (its own lessons). Bots write all three without
  approval. Every save shows in the thread with its source (Bot, thread, run) and a Retire button.
  Nothing is deleted.
- Tools: `remember`, `recall`, `read_memory`, `retire_memory`.
- Storage: the `memory` package keeps its own tables and migrations in `atoll.db`; ids are random hex.
  The run fills the source (Bot, thread, run) and the repo/Bot scope id; tools see only the run's
  shelf (user, its repo, its Bot). Embeddings stay off: no embedder is wired.

## Prompt

| Where | Content |
| --- | --- |
| System prompt | Provider's own prompt, untouched, then the platform prompt, then the persona |
| First message | Up to 8 memory items picked by relevance to the message across scopes (ties: rules first), with ids; a count line `Memory: repo 42, user 9, bot 6. Use recall.`; repo, worktree, branch; the message |
| Each resumed message | Relevant memory items the session has not seen; the message |

- Relevance counts distinct non-stop-word message words matching body or why; ties favor convention, decision, lesson, finding, fact, then newer items.
- In prompts a body is cut to 400 bytes and a why to 200, at a rune boundary, then `…`.
- Seen items are tracked per provider session id, marked once the turn ends without error.
- Claude uses `--append-system-prompt` and Codex uses `developerInstructions` on start and resume, every run.
- `{user}` is the `user_name` setting, or The user when empty.
- The first message's repo block ends with one line naming start_work and track_mr. *Claude Code
  may not load a tool's description until it searches for the tool; without the line, Bots
  branched in the clean checkout.*

Budget: platform prompt about 500 tokens; everything Reef adds under about 3–4k. *Less context
recalls better; load the rest just in time.*

Platform prompt (draft):

```
You are {bot}, a Bot in Reef. {user} talks to you in a thread; you do the work and bring back the result.

How you talk
- Like a colleague in chat. Lead with the answer. Plain sentences; no headings or templates.
- Match length to the ask. A question gets an answer, not a plan.
- Reversible choice: pick the better option, say why in one line. Ask only when the ask has two real readings, one question at a time.
- Keep apart what you did, what you verified, and what you believe. Label guesses.

How you work
- Read the code and context before changing anything. Fix causes, not symptoms.
- Smallest change that does the job. Nothing the ask didn't need.
- Prove it by running the real thing. "It compiles" is not proof.
- Code changes: branch <type>/<short-slug>, commit, push, open an MR and link it. Never merge.
- Blocked: stop and say exactly what is missing.

Memory
- Before changing code, recall the area you are touching.
- Remember what will save the next run time: a decision and its why, a convention, a finding. Not progress notes.
- Scope: repo for this repo, user for {user}'s preferences, bot for your own lessons. If an item is wrong, replace it; don't add a contradicting one.

Finish
- End every run with finish_run: done, tried and failed, blocked on, next. Then reply in 2-3 lines.
```

## App and UI

- Mac app on Tauri; the Go server is a sidecar inside the bundle; React UI. Mobile later, as a
  remote control for the Mac. Mockups: the "Reef Mac app" canvas
  (https://claude.ai/artifact/Grf7TUVqfAsv4oHU96rWrU); this section is the contract.
- Look: Geist, regular weight. Main `#171717`, sidebar `#1A1A1A`, composer `#222222`, hairlines
  `#262626`, text `#ECECEC`, muted `#9B9B9B`. The light pill (`#ECECEC` on `#161616`) is the primary
  action and the user's own message. Red `#E5484D` means needs you; amber means blocked.
- Bot looks: old Reef's sea creatures, each with its own colour and two eye ticks: jellyfish (Chief,
  as the logo), whale, penguin, shark, octopus, turtle, seahorse, otter, clownfish, manta. A Bot
  stores the creature's name. Idle blinks; needs you is a pulsing red dot; asleep is grey; failed
  has slanted eyes. *Replaces the eight blob shapes: the user preferred the creatures side by side.*
- Working shows by tool kind, with old Reef's rising bubbles: no tool yet, the eyes circle up;
  reading, searching or recalling, the eyes scan; editing, writing, remembering or changing the
  platform, it bounces; running a command, it breathes.
- Sidebar: one row per Bot (creature, name, latest activity, time), Chief first. `+` opens New Bot;
  the bottom user row opens Settings. No group chats.
- Bot header: creature, Bot name, thread title, a Threads button and a Bot settings icon. The
  Threads button carries a red count of threads waiting on the user and opens a panel: Needs you
  (and what it waits for), Running (live verb, branch, MR), Recent, then Closed folded; New thread
  on top. A thread's title is its first message, cut to 40 characters.
- The chat has no cards. The user's message is the only bubble; the Bot replies in plain text with
  markdown. Everything else is one quiet line with its action inline: approvals (Allow, Deny),
  Chief's changes (Undo, struck through once undone), the branch starting, the worktree cleaned up,
  a failed run (Retry), a stopped run, blocked (from `finish_run`'s `blocked`), a refused message
  and reconnecting. Chips are the exception: an MR with its live state, a saved memory with its
  kind and Retire (tap opens kind, scope, why and what it replaced), a closed thread. A Bot Chief
  creates gets one card: its creature, name, model, persona, skills, repos, Open and Undo.
- While a run works, one status line shows the Bot, a verb and its target ("Reading
  repo-step.tsx") and the time; never raw tool calls. When it ends, a summary line ("Worked 1m 12s
  · read 4 files, edited 1, ran tests") sits above the reply. Approvals show a short form per tool:
  a command shows the command, an edit shows the file with its diff behind a toggle, anything else
  shows the tool and its target.
- Composer: the message, then the thread's repo, branch and MR as chips, and Send or Stop. A new
  thread with no messages shows the creature, "What should <Bot> work on?", its setup as chips, the
  composer in the middle and starters; a starter sends its text as the first message.
- Bot settings open as a right panel over the thread and save as you change them, applying from the
  next run: creature, name, model, persona, skills, repos, connections, memory (its own lessons and
  what it saved) and the platform switch. Skills, repos and connections are lists with logos, a
  remove button and an Add row; a connection that is not signed in offers Connect inline with
  GitLab's device code. The model is a searchable select grouped by provider.
- New Bot is a full page: the form on the left, a live preview (the card and the sidebar row) on
  the right, and a link to ask Chief instead. Create opens the new Bot's empty thread.
- Settings: General (name, worktree folder), Providers, Connections, Skills, Repos, Memory, one
  list each. Content to the right of the section navigation is centred, with a maximum width of
  720px. The native repo folder picker is invoked by the Go sidecar using `osascript` over HTTP.
  A skill opens in a wide right panel: name, when to use it, the `SKILL.md` text and the Bots that
  use it. Provider cards show version, models, Bots using them and the CLI path as labelled facts,
  along with sign-in state.
  Sign in runs the provider's own login (`claude auth login`, `codex login`) in Reef's folder for
  it, opens the URL it prints in the browser and updates when it finishes; no Terminal.
- First run is three steps over drifting creatures: the user's name; the providers found, with Sign
  in; Meet Chief with example asks, each opening Chief's thread with that message sent.
- The Go server serves the UI at `/` and the API under `/api/`, one address. Vite builds `web/`
  into `server/ui`, which the binary embeds (`make ui`).
- The desktop shell uses an overlay title bar; the Bot header’s empty space is a window drag region.
- The Tauri shell (`desktop/`, `make app`) starts `atoll serve --addr 127.0.0.1:4317`, loads the
  URL it prints, restarts it when it exits (every 2 s while it keeps failing) and stops it on quit or
  SIGTERM; the page shows Reconnecting meanwhile. A page's `window.open` (provider sign-in) opens in
  the default browser. *A fixed address lets the open page reconnect to a restarted sidecar.*
- The dmg is ad-hoc signed (`signingIdentity: "-"`), not notarized, as old Reef was: free, and a
  web download opens after System Settings → Privacy & Security → Open Anyway once. CI's `dmg` job
  builds it on the atoll-mac runner for every push to `main` (manual on an MR). *Developer ID and
  notarization need the paid Apple Developer Program; add them when outside users need a clean
  first open.*
- Provider sign-in state is cached: checked on start, before each run, on Refresh and when a
  sign-in finishes, never on the sidebar's poll.
- Names: the UI says Reef. Repo, Go module, binary and bundle ID (`dev.atoll.desktop`) say
  `atoll`; app data lives in `~/Library/Application Support/Atoll`. *Runs beside the old Reef;
  rename when it retires.*

## Testing

Agents edit, skip or delete tests to make them pass, and per-bug unit tests bloat the suite. So:

- The contract is a small black-box suite in `e2e/` that builds and drives the real binary. Its
  tests come from a ticket's acceptance lines, in their own commit, before the code, written by the
  user or a separate session. The implementing agent never edits them; CI is the referee.
- Unit tests are for fiddly logic, as table cases. They run shuffled with a 60s timeout.
- `test-guard` fails an MR that mixes `e2e/` with other code, deletes or renames a test file, adds
  a skip or `.only(`, or edits CI or the guard. CI runs the target branch's copy of the guard. Only
  the user's `tests-approved` label lets such an MR pass.
- Slice verification runs mutation testing (gremlins) on changed packages; a surviving mutant is a
  finding.

## v1

| Area | In v1 |
| --- | --- |
| App | Tauri app, Go sidecar, React UI, sidebar, Bot looks |
| Providers | Claude Code and Codex: Providers page, own folders, isolation, transports |
| Bots | Create and edit; Chief with platform tools |
| Threads | Chat, runs, resume, `finish_run`, live tool calls, approvals in the thread |
| Repos | Register, worktree per thread, MR via `glab` |
| Connections | GitLab |
| Skills | Library, picked per Bot |
| Memory | Three scopes, four tools, saves shown in the thread |

## Queue

Delegation, GitHub and Slack connections, browser pane, background processes pane, the platform
switch for Bots other than Chief, importing local skills and MCP servers, desktop Linux, mobile,
memory pins (up to about 5 items always in the prompt), routines (scheduled, repeating runs),
more provider adapters.

An item moves into the build when its absence hurts in daily use.
