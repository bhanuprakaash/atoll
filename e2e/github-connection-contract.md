# GitHub connection test contract

Extends the connections in [README.md](README.md), [gitlab-refresh-contract.md](gitlab-refresh-contract.md)
and [login-isolation-contract.md](login-isolation-contract.md) for issue #62. Sign in to GitHub once;
a Bot given the connection runs `gh`, git and GitHub's hosted MCP server as that sign-in, and a Bot
without it gets none of them. Tests drive the real binary, with a local fake GitHub, the fake `gh`,
the scripted fake providers and real git; no product package is imported and nothing uses the
network. JSON is snake_case, ids are strings, replies may carry fields not listed here.

## Test seams (env the server reads)

For tests and dev, like the other `ATOLL_*` knobs; the real values are the defaults.

| Env | Default | Meaning |
| --- | --- | --- |
| `ATOLL_GITHUB_URL` | login routes on `https://github.com`, REST on `https://api.github.com` | One base URL that replaces both. Routes below are appended to it |
| `ATOLL_GITHUB_MCP_URL` | `https://api.githubcopilot.com/mcp/` | URL of GitHub's MCP server |

With `ATOLL_GITHUB_URL=<base>` the server calls `POST <base>/login/device/code`,
`POST <base>/login/oauth/access_token` and `GET <base>/user`. The URL the Bot's git credential
helper is scoped to (below) is `<base>` too, so a push to `<base>/<owner>/<repo>.git` is a push to
"github.com" in the tests.

## Sign-in (REST and UI)

OAuth App client id `Ov23li5N8B8tlXHi1Rza` (public, device flow, no secret); scopes `repo read:org
workflow`; github.com only. The connection card and routes mirror GitLab's.

| Call | Body | Reply |
| --- | --- | --- |
| `POST /api/connections/github/start` | none | `201` `{id, user_code, verification_uri, interval_ms}` |
| `POST /api/connections/github` | `{connection_id}` | `201`, same shape and the same `id`: Sign in again |
| `POST /api/connections/{id}/poll` | none | `200` `{status: "pending"\|"connected"}` |
| `GET /api/connections`, `GET /api/connections/{id}` | | the connection(s) as `{id, kind, instance_url, status}` |

A GitHub connection has `kind: "github"`, `instance_url: "https://github.com"` (also with the
seam set) and `status` `pending`, `connected` or `needs_sign_in`. `kind` stays `gitlab` for GitLab.

The fake GitHub behaves as the real one does:

- The device code request is a form POST with `client_id` and `scope` (`repo read:org workflow`,
  space-separated); it answers `device_code`, `user_code` `ABCD-1234`, `verification_uri`
  `https://github.com/login/device`, `interval` 1.
- The access token request is a form POST with `client_id`, `device_code` and
  `grant_type=urn:ietf:params:oauth:grant-type:device_code`.
- Both answer form-encoded unless the request has `Accept: application/json`, so the server must send it.
- Errors come with HTTP 200: before the user approves, the access token request answers
  `{"error":"authorization_pending"}` with status 200, which keeps the connection `pending`.
- A poll that comes too early answers `{"error":"slow_down","interval":<seconds>}` and adds 5s to
  the interval for that device code; each later poll sooner than the new interval after the previous
  one gets `slow_down` again (RFC 8628 §3.5). The server, not the UI, keeps to the interval: after a
  `slow_down` it adds 5s and answers `pending` to polls that come sooner, without asking GitHub. The
  fake answers `slow_down` to the next poll when a test asks it to, then enforces the interval.
- Approval answers `{"access_token", "token_type":"bearer", "scope"}`: no refresh token, no expiry.
  Every approval issues a new token and revokes the previous one.
- `GET /user` with `Authorization: Bearer <token>` (or `token <token>`) answers
  `{"login":"reef-fixture"}`; any other token gets `401`.

The token goes to the OS secret store only: service `atoll`, username `github:<connection id>`,
value the access token itself (a bare string). It is in no file under `ATOLL_DATA_DIR`, in no reply
and not in the server's stderr.

Before a run of a Bot that has a GitHub connection uses it, the server calls `GET /user` with the
token. A `401` sets the connection `needs_sign_in`; the run ends `failed` with `error` containing
`GitHub needs sign-in`, and the provider is never started. `POST /api/connections/github` with
`{connection_id}` then runs the device flow for the same connection: it keeps its id and its Bots,
the new token replaces the old one in the secret store, status `connected`, and the next run gets the
new token.

## What a run of a Bot with the connection gets

A Bot's `connections` may hold a GitHub connection id (`POST /api/bots`, `PUT /api/bots/{id}`,
Chief's `create_bot` and `edit_bot`). For a Bot that holds one that is `connected`, the run adds to
the isolation env of [login-isolation-contract.md](login-isolation-contract.md), in the provider
process and in every `gh` and `git` it starts:

- `GH_TOKEN=<token>`. `GITHUB_TOKEN`, `GH_ENTERPRISE_TOKEN` and `GITHUB_ENTERPRISE_TOKEN` stay absent.
  The token is in no file (`GH_CONFIG_DIR` stays the empty folder).
- A git credential helper for the GitHub base URL only (`https://github.com`, or
  `ATOLL_GITHUB_URL`), given through env (`GIT_CONFIG_*`): for it `git credential fill` returns
  `username=x-access-token` and `password=<token>`. For any other host (`git.example`) it returns no
  password, though the user's own `~/.gitconfig` has a helper for it.
- A `github` entry in the run's MCP config next to Reef's own: Claude's inline `--mcp-config` has
  `{"type":"http","url":<ATOLL_GITHUB_MCP_URL>,"headers":{"Authorization":"Bearer <token>"}}`; Codex's
  `mcp_servers.github` has that `url` and the same bearer token as `http_headers` or
  `bearer_token_env_var`. The token is not written to a file.

A Bot without a GitHub connection, even while one is connected, gets none of it: no `GH_TOKEN` or
other gh token variable, `gh api user` fails, no password for the GitHub URL, a `git push` to it
fails, and its MCP config has no `github` entry.

## UI

All in Settings → Connections and Bot settings; roles and visible texts below are what Playwright
selects on.

- The GitHub card (title `GitHub`, brand logo) replaces the "Coming later" row; its detail text is
  `Bots get gh, git and GitHub tools.` in every state. Slack stays `Coming later`.
- With no GitHub connection the card has a `button` "Connect GitHub" and the tag `Not connected`.
  Clicking it starts the device flow, opens `verification_uri` in a popup (window.open, as the
  provider sign-ins do) and shows the user code and a `link` named `https://github.com/login/device`
  with that `href`, until the sign-in connects.
- A connected card shows the tag `Connected` and no Connect button. A `needs_sign_in` card shows the
  tag `Needs sign-in` and a `button` "Sign in again", which runs the device flow for the same
  connection, as GitLab's does.
- Each connection is listed by its own kind: a GitHub connection is never a GitLab row, and it does
  not hide GitLab's `Sign in to GitLab` button while there is no GitLab connection.
- Bot settings → Connections: `Add connection` lists a `menuitem` whose name starts `GitHub` for each
  GitHub connection (name `GitHub · github.com`, brand github), like `GitLab · <host>`; toggling it
  saves `connections` on the Bot, and the Bot's row has `Remove GitHub · github.com`.
- A run that stops with `GitHub needs sign-in` shows that text in the thread's Messages log.

## Chief

`create_bot`, `edit_bot` and `list_connections` need no new arguments. A GitHub connection id in
`connections` is stored as given and shown by `GET /api/bots/{id}`; `list_connections` returns the
GitHub connection with the same `{id, kind: "github", instance_url, status}` as `GET /api/connections`.
The Reef guide in Chief's platform block names GitHub among the connections (it names only GitLab
today), within its existing size limit.

## Fakes

- `e2e/fakes/gh` takes `gh api user` as well as `gh auth status`. The fake GitHub's URL is in the file
  `github-url` beside the executable (as `mr-state` is for glab). It uses the token real gh would:
  `GH_TOKEN`, else `GITHUB_TOKEN`, else the `oauth_token` of `hosts.yml` in `$GH_CONFIG_DIR`, else
  `$XDG_CONFIG_HOME/gh`, else `~/.config/gh`; with none it exits 1. It calls `GET <url>/user` with that
  token, prints the body and exits 0, or exits 1 with `Bad credentials (HTTP 401)`.
- Scripted scenario `github-probe` (recordings `testdata/{claude,codex}/github-probe.jsonl`, prompt
  `Check what this run has for GitHub.`): in the provider process, with the env the server gave it,
  runs `gh api user`; `git credential fill` for the fake GitHub's URL and for `https://git.example`;
  a real `git push` of one commit to `<url>/octo/hello.git` as `refs/heads/reef-push`; and, when its
  MCP config has a `github` server, calls its `get_me` tool. It replies with JSON `{gh, git,
  other_git, push}` as `{exit, output}` and `mcp` as `{connected, text}`. The fake provider reads the
  fake GitHub's URL from `github-url` beside its executable.
  The reply is stored with the run, so it never carries the token: each `password=<x>` line of the
  credential output becomes `password_sha256=<first 12 hex of sha256(x)>` (the test compares the
  digest), and any `GH_TOKEN` value in the other outputs is replaced by `<GH_TOKEN>`.
- The fake GitHub MCP server's `get_me` answers `{"login":"reef-fixture","source":"mcp"}` and 401s
  any request without the valid bearer token. The fake git remote is real `git http-backend` behind
  basic auth: only user `x-access-token` with the valid token gets in.

## Acceptance mapping

| Acceptance | Tests |
| --- | --- |
| Connect shows the code and opens `github.com/login/device`; with the fake approving, `GET /api/connections` lists `github` connected; token in the OS secret store, in no file or log | `TestGitHub_ConnectKeepsTheTokenInTheSecretStore`; `e2e/ui/github-connection.spec.ts` "the GitHub card says what Bots get; Connect shows the code, opens GitHub and connects" |
| A run gets `GH_TOKEN` and the git helper through env; `gh api user` returns the fake user; `git push` authenticates as `x-access-token` | `TestGitHub_RunGetsTheConnection/{claude,codex}` |
| MCP config has a `github` entry with the Bearer header (Claude, Codex); a scripted provider calls one tool and gets the answer | `TestGitHub_RunGetsTheConnection/{claude,codex}` |
| A Bot without the connection gets neither env nor MCP entry | `TestGitHub_BotWithoutTheConnectionGetsNothing/{claude,codex}` |
| Sign-in connects after a `slow_down`, with no early poll after it | `TestGitHub_SignInConnectsAfterSlowDown` |
| A 401 marks `needs_sign_in`; the run stops before the provider with "GitHub needs sign-in"; Sign in again keeps the id | `TestGitHub_RejectedTokenNeedsSignIn`; `e2e/ui/github-connection.spec.ts` "a rejected token shows Needs sign-in; Sign in again reconnects the same connection" |
| The card says what Bots get: `gh`, git, GitHub tools | `e2e/ui/github-connection.spec.ts` (card text) |
| Bot settings has a GitHub switch like GitLab's | `e2e/ui/github-connection.spec.ts` "Bot settings take and drop the GitHub connection like GitLab" |
| Chief's `create_bot`, `edit_bot` and `list_connections` handle GitHub | `TestGitHub_ChiefHandlesTheConnection` |
| The scenario, the fake `gh` and the fake GitHub work before the product does | `TestGitHubFixtures_ProviderScenario/{claude,codex}` (passes without the product) |

Not covered: the `docs/design.md` line (a doc, not software behaviour); the tests-land-first line
(`test-guard`); real github.com (connect, `gh repo view`, listing PRs through the GitHub MCP tools),
which only the user can check. A Bot with both a GitLab and a GitHub connection is not tested: its two
credential helpers must both reach `GIT_CONFIG_*`. GitHub Enterprise and opening PRs from registered
repos are out of scope.
