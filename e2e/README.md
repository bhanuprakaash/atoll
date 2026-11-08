# Slice 3 test contract

This extends issue #3's Test contract. Existing repo-less threads, API shapes, provider
isolation, approvals and recordings continue to work. Tests build the real server; no
product package is imported. No network is used: Git pushes go to a local bare origin,
OAuth goes to `httptest`, and `glab` is a logged executable on the provider PATH.
The implementer separately proves a real provider opening an MR on gitlab.com.

## First edit and working directory

The Bot calls Atoll MCP `start_work({type, slug})` before its first edit. Allowed types
are `feat`, `fix`, `chore`, `docs`. The app normalises `Seeded BUG` to `seeded-bug`,
creates `fix/seeded-bug` (then `fix/seeded-bug-2` when taken), creates the worktree
under the configured root, and returns text JSON `{path, branch}` with an absolute
path. The Bot changes directory there for this run's edits. The branch is fixed for
that thread; repeated `start_work` returns its existing path/branch. Later runs start
in that path and resume the same provider session. The fake's second run reads the
file there; the first changes `answer.txt` from `41` to `42`, commits and pushes.

A repo thread starts read-only in a clean checkout of HEAD, outside the worktree
root. Local uncommitted edits in the registered checkout must neither leak into the
answer nor be overwritten. A question does not call `start_work` and leaves the
worktree root empty. The thread's repo is fixed at creation. Repo-less threads keep
the Slice 2 folder behaviour. `start_work` is an Atoll tool and never asks approval.

## REST additions

JSON uses snake_case and string IDs, as in Slice 2. Bodies/replies below list only
fields used by the tests. Additional fields are allowed.

| Call | Body | Reply |
| --- | --- | --- |
| `PUT /api/settings` | `{worktree_root: <absolute path>}` | `204` |
| `POST /api/repos` | `{name, path: <existing local git repo>}` | `201` `{id, name, path}` |
| `GET /api/repos` | | `200` array of registered repos |
| `POST /api/bots` | Slice 2 fields plus `repo_access`, `connections` | `201` Bot |
| `POST /api/bots/{id}/threads` | `{repo_id}` | `201` `{id, bot_id, repo_id}`; `403` if the Bot lacks access |
| `POST /api/threads/{id}/close` | none | `204` after save and removal |
| `POST /api/connections/gitlab/start` | `{instance_url}` | `201` `{id, user_code, verification_uri}` |
| `POST /api/connections/{id}/poll` | none | `200` `{status: "pending"\|"connected"}` |
| `GET /api/connections/{id}` | | `200` `{status: "pending"\|"connected"}` |

`repo_access` is `"all"` (default) or an array of allowed repo IDs. An empty array
allows no repos. `connections` is an array of connection IDs; a selected GitLab
connection gives the provider and `glab` on its PATH `OAUTH_TOKEN=<token>`,
`GITLAB_HOST=<instance url>` and `GLAB_CONFIG_DIR=<dir>`, and no `GITLAB_TOKEN`.
`<dir>` is a folder owned by Atoll (it may live under `ATOLL_DATA_DIR`) whose
`config.yml` sets `is_oauth2: "true"` for the instance's host under `hosts:` and holds
no token. Real glab 1.123 sends `GITLAB_TOKEN` as `PRIVATE-TOKEN`, but a device-flow
OAuth token works only as Bearer, which glab uses for `OAUTH_TOKEN` when the host has
`is_oauth2`. These
fields are optional, preserving issue #3's create-Bot contract. A closed thread
refuses new messages with `409`.

The GitLab instance URL selects the real OAuth routes, so the tests need no OAuth
backdoor. Start sends form POST `/oauth/authorize_device` with a nonempty
`client_id` and `scope` containing `api`. Poll sends form POST `/oauth/token` with
`client_id`, `device_code`, and `grant_type=urn:ietf:params:oauth:grant-type:device_code`.
GitLab's `authorization_pending` stays pending; a successful access token is saved
in the OS secret store. No endpoint returns the token. The fake advertises a one
second interval and tests wait before the second poll.

## MR tracking and events

After `glab mr create`, the Bot calls Atoll MCP `track_mr({url})`; this records the
thread's MR and emits `mr_updated` with state `opened`. The reply may have any text.
The Bot calls `finish_run` and replies with the MR URL. Atoll polls `glab mr view`
for the tracked URL, requesting JSON with at least `state`. A transition to `merged`
removes the worktree. Polling is independent of provider runs and resumes on server
restart. `ATOLL_REPO_POLL_INTERVAL=100ms` shortens the default 60-second interval for tests.
The fake's `mr-state` file, beside its executable, controls `opened` / `merged`.
Its `mr create` output is `https://gitlab.example/fixtures/seeded/-/merge_requests/1`.

These additive SSE events use the issue #3 stream; subscribe before sending.
All include `run_id` (the thread's latest run, even for cleanup after it ends).

| Event | Additional data |
| --- | --- |
| `worktree_created` | `{repo_id, path, branch}` |
| `mr_updated` | `{url, state}` |
| `worktree_removed` | `{path, reason: "merged"\|"closed", snapshot_ref}` |

Removal is reported after the folder is gone. Before removal, tracked edits and
untracked files are saved as a commit at `refs/atoll/unfinished/<thread_id>` in the
registered repo's Git object store. `snapshot_ref` gives that ref, or is empty when
nothing needed saving. Tests recover both files with `git show <ref>:<path>` after
removal. A failed save must leave the worktree intact. Closing does not lose the
branch or committed history. The same save-before-removal rule applies on merge.

## Secret Service in Linux CI

`make e2e` runs Go tests through `e2e/with-secret-store`. Linux needs `dbus`,
`gnome-keyring`, and `libsecret-tools`; the CI e2e job installs them. The wrapper
creates a private D-Bus session and an encrypted login keyring in a temporary XDG
data directory, unlocks it through stdin, and proves store/lookup/clear before
running tests. No display, test-only file backend, or pasted GitLab token is used.
The Linux keyring identity is service `atoll`, username `gitlab:<connection_id>`;
this lets `secret-tool lookup` independently prove that the token reached the OS
secret store, rather than trusting the status endpoint.

Only D-Bus and XDG session env are added to the isolated server env. The tests
scan every file beneath `ATOLL_DATA_DIR` and the worktree root and scan the server's
stderr, including after restart. They assert the token is present in the provider
and `glab` env. Fake CLI argv/env logs live in the separate fake-bin fixture, not
in those roots; they intentionally record synthetic credentials and are deleted
with the test. The temporary encrypted keyring is removed when the session ends.

## Acceptance mapping

| Acceptance | Tests |
| --- | --- |
| Seeded bug → branch, commit, push, MR, thread link | `TestRepo_FixPushMRAndMerge/{claude,codex}`; real gitlab.com MR remains implementer proof |
| Question creates no worktree | `TestRepo_QuestionCreatesNoWorktree/{claude,codex}` |
| Merge cleanup and closed-thread recovery | `TestRepo_FixPushMRAndMerge/{claude,codex}`, `TestRepo_CloseSavesUncommittedWork/{claude,codex}` |
| Only these cannot use another repo | `TestRepo_OnlyTheseDeniesOtherRepo` (includes empty allow-list) |
| OS secret store, no file/log leak, CLI env | device-flow and Secret Service assertions inside `TestRepo_FixPushMRAndMerge/{claude,codex}` |
| Lowercase/unique branch and distinct worktrees | `TestRepo_BranchCollision` |

`TestRepoFixtures_ProviderScenarios` independently drives all eight new provider
scenarios against a fixture MCP endpoint to prove real Git operations, dynamic
replies and `finish_run` work before the product APIs exist.

The contract does not specify truncation length, branch-folder encoding, UI,
refresh/revocation flows, or cleanup recovery UI. Implementer decisions beyond
these tested boundaries belong in `docs/design.md`, not this tests-only MR.

Slice 4 extends this contract in [memory-contract.md](memory-contract.md).

Slice 5 extends these contracts in [skills-contract.md](skills-contract.md).

GitLab token refresh extends the Slice 3 contract in [gitlab-refresh-contract.md](gitlab-refresh-contract.md).

The platform prompt split extends these contracts in [prompt-contract.md](prompt-contract.md).

Importing a local skill extends these contracts in [skill-import-contract.md](skill-import-contract.md).

Skills keeping their scripts, playbooks and references extends these contracts in [skill-files-contract.md](skill-files-contract.md).

The skill editor for every file of a skill extends these contracts in [skill-editor-contract.md](skill-editor-contract.md).

Run login isolation (a Bot uses only the logins Reef gives it) extends these contracts in [login-isolation-contract.md](login-isolation-contract.md).

GitHub connection (gh, git and GitHub's MCP server from one sign-in) extends these contracts in [github-connection-contract.md](github-connection-contract.md).

MCP servers (a library, a pick per Bot, secrets in the OS store) extend these contracts in [mcp-servers-contract.md](mcp-servers-contract.md).

GitHub repos (a repo on GitHub: push and open a pull request with `gh`) extends these contracts in [github-repos-contract.md](github-repos-contract.md).

MCP sign-in for hosted servers (Connect, tokens in the OS secret store, refresh) extends these contracts in [mcp-oauth-contract.md](mcp-oauth-contract.md).

Approval cards that close and links that leave the app extend these contracts in [approvals-contract.md](approvals-contract.md).

Bot mode (Auto, Ask or Full access, and what each gives Claude and Codex) extends these contracts in [bot-mode-contract.md](bot-mode-contract.md).

The delegate list (who a Bot may hand work to) extends these contracts in [delegates-contract.md](delegates-contract.md).

Delegate and continue (a Bot hands work to a helper thread) extend these contracts in [delegation-contract.md](delegation-contract.md).

Waking the boss (a helper's reply and report start a boss run) extends these contracts in [wake-contract.md](wake-contract.md).

Helper code work (helpers branch off their boss; the boss merges) extends these contracts in [helper-work-contract.md](helper-work-contract.md).

Helpers in the UI (above the composer, in the island, a read-only helper thread) extend these contracts in [helpers-ui-contract.md](helpers-ui-contract.md).

The sidebar thread tree, threads popup, Bot groups and island notifications extend these contracts in [sidebar-tree-contract.md](sidebar-tree-contract.md).
