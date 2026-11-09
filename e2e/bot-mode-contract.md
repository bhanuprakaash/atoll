# Bot mode: test contract

Extends the Bot and Chief contracts ([mac-app-contract.md](mac-app-contract.md),
[chief-bot-contract.md](chief-bot-contract.md)). Tests: `bot_mode_test.go`, UI `ui/bot-mode.spec.ts`.
They build the real server; no product package is imported.

## API

A Bot has `mode`: `auto`, `ask` or `full`. `POST /api/bots`, `PUT /api/bots/{id}` (partial, as for
`look`), `GET /api/bots` and `GET /api/bots/{id}` carry it. A Bot created without `mode` gets
`auto`. Any other value is `400`; its `error` contains `auto`, `ask` and `full`, and nothing is
created or changed.

A launch moves every Bot of an older install to `auto`, once: a Bot set to `ask` after that launch
stays `ask` across a restart. `testdata/before-modes.sql` is the `atoll.db` the server left before
Bots had a mode (schema version 11, Chief and two Bots); the test loads it into a fresh data dir,
then launches.

## What reaches the provider

Claude on Auto runs Claude Code's auto mode: its classifier lets safe steps run and blocks risky
ones, and Claude reports a block back. Explicit ask rules still reach the permission prompt tool.

| Mode | Claude argv | Codex `thread/start` / `thread/resume` |
| --- | --- | --- |
| `auto` | `--permission-mode auto`; `--permission-prompt-tool mcp__atoll__approve` | `sandbox: workspace-write`, `approvalPolicy: on-request` |
| `ask` | no `--permission-mode`, or `--permission-mode default`; `--permission-prompt-tool mcp__atoll__approve` | `approvalPolicy: untrusted`; sandbox not `danger-full-access` |
| `full` | `--permission-mode bypassPermissions` | `sandbox: danger-full-access`, `approvalPolicy: never` |

Codex may carry them as params or in `config` (`sandbox_mode`, `approval_policy`). A mode changed
while a run goes applies from the next run; the going run keeps the mode it started with.

Codex on Auto in a repo thread also starts every run, `thread/start` and `thread/resume` alike, with
`config.sandbox_workspace_write.writable_roots` holding exactly these six folders, plus the Bot's cache root
(next section), in any order: the `worktree_root` folder, the git common dir's `objects`, `refs`, `logs` and `worktrees`, and the
absolute git dir of the session's `cwd` (`git -C <cwd> rev-parse --absolute-git-dir`); and
`config.sandbox_workspace_write.network_access` true. The cwd is a linked worktree in both
sessions, the first in the repo's clean checkout and a resumed one in the thread's worktree, so
that last root is `<repo>/.git/worktrees/<id>` and differs per session. Codex keeps the cwd's own
git dir read-only unless it is a root itself, even inside a writable `worktrees`; the worktree
`start_work` makes during a run is covered by `worktrees`. A root must be a folder. The common dir itself, its `config` and its `hooks` are not roots, so the
Bot cannot change the repo's config or hooks. Codex reads those keys there and nothing else, so
the same values anywhere else do not count. Paths compare after symlinks resolve and a trailing
slash is dropped. Ask and Full access have no `sandbox_workspace_write` key. The spec runs a plain
clone.

## Tool caches

Codex on Auto cannot build with the tools' default caches: they live under the user's home, outside
its writable roots. Every Codex Auto run in a repo thread, `thread/start` and `thread/resume` alike,
gets a Reef-owned cache root, `<data>/cache/<bot id>/`: one per Bot, shared by all of that Bot's runs
so caches stay warm, never shared across Bots and never under the user's `~/.cache` or `~/go`. The
root is one of the thread's `writable_roots` and exists, writable, before the provider starts. The
run's env sets each of these to a folder strictly under the root: `XDG_CACHE_HOME`, `XDG_DATA_HOME`
and `XDG_STATE_HOME` (three different folders), `GOCACHE`, `GOLANGCI_LINT_CACHE`, `pnpm_config_store_dir`
(pnpm's store-dir setting) and `PNPM_HOME`. pnpm also keeps state under the XDG data and state
folders, so a store dir alone is not enough. Ask, Full access and Claude Bots get none of this: no
cache env under `<data>/cache/`, no cache folder made. A Bot run leaves the user's `~/.cache` and
`~/go` as they were (files, modes, mtimes). Deleting a Bot removes its cache folder and no other.
Paths compare after symlinks resolve; the data dir may contain a space. Not covered: Codex Auto in a
thread without a repo.

`git push -u` cannot save the upstream when `config` is read-only, so `start_work` sets it when it
makes the branch: `branch.<branch>.remote` is `origin` and `branch.<branch>.merge` is
`refs/heads/<branch>`, with no run pushing anything. A repo without an `origin` remote is not
covered.

A writable git dir lets a Bot plant routes that make git run a program, and a Claude Bot on Auto has
no sandbox at all. Atoll's own git runs outside any sandbox and is one function, so none of its calls
in the repo may run a program the repo names, whichever provider the Bot uses. The spec plants each
route the way a Bot would, with a program that records every git call except the fake provider's
(which sets `GIT_AUTHOR_NAME` to `Fixture Bot`), then drives the thread's clean checkout, `start_work`, a run that leaves unsaved
work, and an ending, and expects no record after any step:

| Route | Planted |
| --- | --- |
| `hooks` | `post-checkout`, `reference-transaction` and `post-index-change` in `<common>/hooks` |
| `fsmonitor-in-config` | `core.fsmonitor` in `<common>/config` |
| `filter-driver` | `filter.x.clean` in `<common>/config` and `* filter=x` in `<common>/info/attributes`; asserted on the close only |
| `filter-name-with-equals` | `filter.x=y.clean` in `<common>/config` and `* filter=x=y` in `<common>/info/attributes`; a filter name may hold `=`, which a `-c` override reads as a different key; asserted on the close only |
| `commondir-rewritten` | `<common>/worktrees/<name>/commondir` pointing at a copy of the common dir whose config sets `core.fsmonitor` |
| `gitfile-rewritten` | the worktree's `.git` file pointing at a copy of its git dir, whose `commondir` points at that copy; the ending may be refused, because git will not remove such a worktree |
| `worktree-config` | `extensions.worktreeConfig` true in `<common>/config` and `core.fsmonitor` in `<common>/worktrees/<name>/config.worktree` |

The endings are the thread's close (which saves the unsaved work) and the Bot's deletion, which closes
each of its threads, so it runs the same git as a close. Every ending
returns `204`, except for `gitfile-rewritten`. After a close, `refs/atoll/unfinished/<thread id>` is a
commit in the repo itself: git writes a ref into the common dir a worktree's `commondir` file names,
even when `GIT_COMMON_DIR` says otherwise, so a redirected `commondir` would send the snapshot to a
folder the Bot controls and the close would then remove the work.

Each route also has a control, `control/<route>`, that makes the same repo and plants the same route,
then runs the operations Atoll runs (worktree add for the clean checkout and for a new branch, status,
add, write-tree, update-ref and worktree remove) with plain git and expects the program to run. Every
route fires under plain git with those commands. It
uses no product code, so it passes with and without the fix: it proves the route is live and the
program records, so that a route or a recorder that stopped working cannot make a leg pass. The
recorder does not depend on anything Atoll sets, so dropping an Atoll environment variable cannot
make a leg pass either.

A Bot that can write the worktree root and `<common>/worktrees` can also swap its own worktree folder
for a symlink to another linked worktree of the repo, one the user made outside the root (its path
is in `<common>/worktrees/*/gitdir`), and point its own entry's `gitdir` file at nothing. Git then
resolves the symlink when asked to remove the folder, matches the user's worktree and removes it
with its uncommitted files. After such a plant, closing the thread may be refused but must leave the
user's worktree and its uncommitted file in place (`TestRepo_CloseKeepsOtherWorktrees/{codex,claude}/close`).
Its `control` runs `git worktree remove --force` with plain git and expects the user's file gone.

Filters are neutralised only where Atoll stages the Bot's tree, the snapshot of a close (`Save`'s
`status`, `add -A` and `write-tree`, which each can run a clean filter), not in `worktree add`: a user's own filter, such as Git LFS's, runs at checkout. So
the `filter-driver` and `filter-name-with-equals` legs assert on the close only and nothing about
filters during a checkout. A user's own config may define a filter as required and the repo's
`.gitattributes` may apply it; the thread's clean checkout and `start_work` must still make their
worktrees, and the checkout's smudge may run (`TestRepo_UserFilterThatMustRunStillWorks`). The race
in which a Bot process that outlives the run swaps the folder between Atoll's symlink check and its
git call is a documented residual risk (`docs/design.md`), not pinned.

Not pinned: a filter driver in `config.worktree`, which no command-line flag of git suppresses and
which a list of filters read from the trusted config does not name, and any other program a repo config can name that none of Atoll's calls runs.
The "delete" of the issue's third line is covered in part. Deleting a Bot closes each of its threads,
so the Bot-deletion ending runs `CloseThread`: `Save` (status, rev-parse, add, write-tree,
commit-tree, update-ref) and `Remove` (`git worktree remove`). `DeleteBranch` (`git branch -D`) is
not reached by any leg: Atoll runs it only when `start_work` fails and when a delegate's helper is
abandoned (`run/work.go`, `run/delegate.go`). Not driven separately either: `CreateFrom` and the
reads `IsRepo`, `Origin` and `HasCommit`, which share the one exec site. Git names a worktree's git
dir after its folder, and Atoll's folders are unique per repo (`-2`, `-3`), so a git-dir id that git
suffixes cannot be made through the API.

## Chief

`create_bot` and `edit_bot` declare a string `mode` in their `inputSchema` (the fakes send only
declared arguments). `create_bot` without `mode` gives `auto`. An unknown mode fails as an unknown
look does: the text contains `auto`, `ask` and `full`, and nothing changes. An `edit_bot` of `mode`
records one `edit` change (`platform_changed`); its Undo restores the previous mode. `list_bots`
shows each Bot's `mode`.

## UI

Bot settings has a radio group `Mode` with `Auto`, `Ask` and `Full access`; a new Bot shows `Auto`.
With `Full access` picked, the panel shows exactly: "Full access: this Bot never asks, even to delete
files or push. Use it for Bots that run in a VM." With another mode the text is gone. The choice
saves as you change it and shows again after a reload.

## Acceptance mapping

| Acceptance | Tests |
| --- | --- |
| `GET/PUT /api/bots` round-trip `mode`; unknown → 400 naming the three; new Bot gets `auto` | `TestBotMode_APIRoundTrip` |
| A launch moves every existing Bot to `auto` once | `TestBotMode_LaunchMovesOldBotsToAutoOnce` |
| Each mode's flags or config reach Claude and Codex | `TestBotMode_ReachesProvider/{claude,codex}` |
| Bot settings Mode control; Full access text | UI `Bot settings pick a Mode; Full access warns and the choice persists` |
| Chief `create_bot`/`edit_bot` take `mode`; `list_bots` shows it; change with Undo | `TestBotMode_Chief/{claude,codex}` |
| A change of mode mid-run applies from the next run | `TestBotMode_ChangeMidRunAppliesFromNextRun/{claude,codex}` |
| Closing a thread whose folder a Bot swapped for a symlink to a user's other worktree leaves that worktree in place | `TestRepo_CloseKeepsOtherWorktrees/{control,codex/close,claude/close}` |
| A user's required filter (Git LFS style) does not stop the clean checkout or `start_work` | `TestRepo_UserFilterThatMustRunStillWorks` |
| `start_work` sets the new branch's upstream to `origin` | `TestRepo_StartWorkSetsUpstream` |
| None of Atoll's own git calls in the repo (clean checkout, `start_work`, close with unsaved work) runs a program a planted hook, `core.fsmonitor`, filter driver, `commondir`, `.git` file or `config.worktree` names, and a close keeps the snapshot in the repo, for a Codex and a Claude Bot | `TestRepo_ReefGitRunsNoRepoCode/{codex/close,codex/delete,claude/close}/<route>`, each with `TestRepo_ReefGitRunsNoRepoCode/control/<route>` for `<route>` in `hooks`, `fsmonitor-in-config`, `filter-driver`, `filter-name-with-equals`, `commondir-rewritten`, `gitfile-rewritten`, `worktree-config` |
| Codex Auto starts and resumes with the `worktree_root` folder, the git common dir's `objects`, `refs`, `logs` and `worktrees` and the session cwd's git dir (not the common dir, `config` or `hooks`) as writable roots, plus the Bot's cache root, and network access; Ask and Full access carry neither | `TestCodexAuto_SandboxCoversWorktreeAndNetwork/{auto,ask,full}` |
| A Codex Auto run's env sets `XDG_CACHE_HOME`, `XDG_DATA_HOME`, `XDG_STATE_HOME`, `GOCACHE`, `GOLANGCI_LINT_CACHE`, `pnpm_config_store_dir` and `PNPM_HOME` under `<data>/cache/<bot id>/`, a writable root that exists before the run starts, in a data dir with or without a space | `TestCodexAuto_RunGetsReefOwnedCaches/{plain data dir,data dir with a space}` |
| The user's `~/.cache` and `~/go` are untouched by a Bot run | `TestCodexAuto_RunGetsReefOwnedCaches/*` (snapshot of files, modes, mtimes) |
| The cache root is per Bot, shared across its runs, and deleting the Bot removes only its folder | `TestCodexAuto_CachesArePerBotAndGoWithTheBot` |
| Codex on Ask and Full access, and Claude Bots, get no cache env under `<data>/cache/` and no folder | `TestCodexAuto_OtherModesAndClaudeGetNoCaches/{codex,claude}/<mode>` |

Not covered here: the spike and the real-Claude line are implementer and user proof. Not pinned:
Codex's sandbox on Ask beyond not being `danger-full-access`; whether Claude on Full access still
gets `--permission-prompt-tool`; error wording beyond the three names.
