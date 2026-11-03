# Reef (working name: atoll) — design

Status: draft for review. Design only; nothing is built.

## What it is

A Mac app where you configure Bots and give them work in chat. A Bot has a persona, a model,
skills and connections. It does the work, tests it, opens an MR and brings back the result. It also
answers questions, explores and investigates; an MR only comes when the work needs one.

It replaces the old Reef. The old Reef is a source of ideas and code to copy when a ticket needs
it, never a dependency.

## Words

| Word | Meaning |
| --- | --- |
| Bot | A configured agent: persona, Model, skills, connections, repo access, delegate list |
| Thread | A chat with one Bot |
| Run | One message handled: the agent process starts, resumes the Bot's session, replies, exits |
| Repo | A git repo registered once in the app |
| Provider | An installed agent CLI the app drives: Claude Code, Codex |
| Connection | A signed-in external service: GitLab, GitHub, Slack |
| Skill | A `SKILL.md` folder in the app's skill library |
| Memory item | A saved decision, convention, finding, fact or lesson |

## Decisions

| # | Decision | Why |
| --- | --- | --- |
| 1 | Chat threads. Each message starts a run that resumes the Bot's session and exits after replying. No agent process lives between messages. | Feels like chat, but no live state to manage; delegation and responsibility stay simple |
| 2 | Delegation depth is 1: a delegated run cannot delegate. | Cycles become impossible, no detection needed |
| 3 | A Bot's persona holds expertise only. Who it talks to comes from the run. A child's questions go to its parent; approvals come to the user. | One Bot works both directly and as a child; only the user can approve |
| 4 | A delegation is a thread where the parent Bot is the asker. Tools: `delegate(bot, brief)`, `continue(thread, message)`. Continue for the same work, new delegation for different work. | One model for user and Bot askers |
| 5 | Resume by default; fresh session only through a new delegation. Every run ends with `finish_run(done, tried, blocked, next)`. | Resume is built in; the report makes a fresh start and memory possible |
| 6 | Each thread gets its own worktree and branch, created on first edit. Read-only threads use a clean checkout. A child gets its own worktree from the parent's branch; the parent merges it and opens the MR. Worktree removed when the MR merges or the thread closes, uncommitted work saved first. | Parent and child never clash; child work can be discarded |
| 7 | Repos are registered once. Each Bot has repo access All (default) or Only these. A thread works in one repo. No Project concept. | A block list silently grants new repos; most Bots need every repo |
| 8 | A Bot is: name, persona, Model, skills, connections, repo access, delegate list. Every Bot runs in its provider's auto mode. No per-tool lists, no platform hard limits. Approval requests from any run, children included, come to the user in the thread. | The provider already decides what needs approval; per-tool lists were busywork |
| 8a | Model is one field, e.g. `Claude Code · Opus 5.5`, `Codex · GPT-5`. | One choice; no "backend" word |
| 8b | Processes a Bot starts (dev server, watcher, browser) belong to the thread, not the run. They outlive the run. When the thread closes or the app quits, the app asks whether to stop them. | A dev server must outlive a reply |
| 9 | Memory scopes: user, repo, bot. Bots write to all three without approval. Every save shows in the thread with its source (Bot, thread, run) and a Retire button. Nothing is deleted; retire or replace keeps history. | User preferences must work across providers; visibility replaces approval |
| 10 | Mac app on Tauri. The Go server is a sidecar inside the app bundle. React UI. Mobile later, as a remote control for the Mac. | Tauri's ecosystem and mobile path; Go holds the real work |
| 11 | A thread that needs a browser gets its own Chrome, driven by the Bot over the DevTools protocol and streamed live into a pane the user can take over. | Works in Tauri's WebKit view; same experience as the old Reef's pane |
| 12 | Connections: GitLab, GitHub, Slack only, built in, each with a sign-in flow, no token pasting. One sign-in feeds both the service's MCP server and the CLI (`GITLAB_TOKEN`, `GH_TOKEN`). Each Bot picks its connections. GitLab asks for the instance URL (default gitlab.com). Per service: GitLab v1 uses OAuth (device flow) and gives Bots `glab`, because GitLab's own MCP server (`/api/v4/mcp`) is beta and needs Premium/Ultimate plus Duo; GitHub uses its hosted MCP server (`api.githubcopilot.com/mcp/`); Slack needs a small hosted token exchange, because its MCP server (`mcp.slack.com/mcp`) wants a confidential client and desktop PKCE sign-in cannot get bot scopes and expires in 30 days. | Clean setup; connections are where real access lives |
| 13 | Tokens live in the OS secret store through one cross-platform library (Keychain, Secret Service, Credential Manager). v1 targets macOS and desktop Linux; headless Linux is out. | One code path for every OS |
| 14 | Bots see only app connections. The provider's own MCP servers are ignored (`--strict-mcp-config` for Claude Code; a per-run config home for Codex). | A Bot must not inherit tools or sign-ins the user never gave it |
| 15 | A platform skill library in standard `SKILL.md`. Each Bot picks its skills. Per-provider adapters place skills where that provider reads them and strip provider-only fields; fallback is a list in the prompt plus reading the file on demand. Each run gets its own provider config folder, so personal skills and global CLAUDE.md / AGENTS.md are hidden. Placement: Claude Code `<run config dir>/skills`, Codex `$CODEX_HOME/skills`; never written into the repo. Skills the repo commits itself (`.claude/skills`, `.agents/skills`) still load, like the repo's CLAUDE.md. | One format, any provider; Bots get only what the app gives them |
| 16 | "Can manage the platform" is a per-Bot switch: tools to create and edit skills, Bots and repos, served as the app's own MCP tools. Sign-ins stay with the user. Every change shows in the thread with undo and applies from the next run. | Build Reef from chat without exposing it to every Bot |
| 17 | v1 is the cut below; the rest is a queue. | Prove the single-Bot loop before multiplying it |
| 18 | A Providers page: for Claude Code and Codex, detection (login-shell PATH plus common install folders), version, signed-in status with a Sign in button, enable switch, path override, and a read-only list of the provider's local MCP servers and skills marked "not used by Bots". Checked on start, before each run and on Refresh. Codex runs through `codex app-server` (present in Codex 0.165, marked experimental). | A Finder-launched app lacks the shell PATH; clear errors before a run, not during |
| 19 | Worktrees live at `~/Reef/worktrees/<repo>/<branch-folder>`; the root is a setting. The Bot names the branch `<type>/<slug>` (feat, fix, chore, docs) at first edit; the app makes it lowercase, short and unique (`-2`). Fixed once created. Children: `<parent-branch>--<bot>`. | Visible, organised by repo, named after the work |
| 20 | Prompt: system prompt = provider's own prompt + appended platform prompt + persona. First message = up to 8 memory items picked by relevance to the message (ties: rules first), a count line per scope, repo/worktree/branch, the message. On resume: only new relevant items. Everything else through `recall` / `read_memory`. | Less context recalls better; load just in time |
| 21 | The UI says Reef. Repo, Go module, binary and bundle ID say `atoll`; app data lives in `~/Library/Application Support/Atoll`. | Runs beside the old Reef without overwriting its data; rename when it retires |

## v1

| Area | In v1 |
| --- | --- |
| App | Tauri Mac app, Go sidecar, React UI |
| Providers | Claude Code and Codex: the Providers page (18) |
| Bots | Create and edit: persona, Model, skills, repo access |
| Threads | Chat, one run per message, resume, `finish_run`, live tool calls, approvals in the thread |
| Repos | Register, worktree per thread (19), MR via `glab` |
| Connections | GitLab with sign-in |
| Skills | Library (created in the app), picked per Bot |
| Memory | Old Reef's `memory` package copied in, scopes user / repo / bot, `remember`, `recall`, `read_memory`, `retire_memory`, saves shown in the thread |

## Queue (v2, already designed)

Delegation (2–4, child worktrees in 6), GitHub and Slack connections, browser pane (11), background
processes pane (8b), "Can manage the platform" (16), importing local skills and MCP servers, desktop
Linux, mobile, memory pins (up to about 5 items always in the prompt).

An item moves into the build when its absence hurts in daily use.

## Prompt

### Order

| Where | Content |
| --- | --- |
| System prompt | Provider's own prompt, untouched |
| Appended | Platform prompt (below), then the Bot's persona |
| First message | Up to 8 relevant memory items with ids; `Memory: repo 42, user 9, bot 6. Use recall.`; repo, worktree, branch; the user's message |
| Each resumed message | Relevant memory items the session has not seen; the message |
| v2, delegated thread | One line: `{parent} delegated this to you; your replies go to {parent}.` |

Budget: platform prompt about 500 tokens; everything the app adds under about 3–4k tokens.

### Platform prompt (draft)

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

## Open questions

- Codex isolation: a per-run `CODEX_HOME` hides personal MCP servers and skills but also its sign-in (`auth.json`); copy it in, or override `mcp_servers` with `-c` and keep the home. Needs a test.
