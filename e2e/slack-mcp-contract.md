# Slack MCP server (per-workspace Slack app): test contract

Extends [mcp-oauth-contract.md](mcp-oauth-contract.md) (discovery, PKCE S256, the fixed redirect,
tokens in the OS secret store, refresh within ten minutes, `needs_sign_in`, the UI statuses),
[mcp-servers-contract.md](mcp-servers-contract.md) (catalog, Bot picks, what a run gets) and
[README.md](README.md) (OS secret store). Tests: `slack_mcp_test.go` (Go, with the fake in
`slack_fixtures_test.go`) and `ui/slack-mcp.spec.ts` (Playwright, with the fake in
`ui/slack-fixture.ts`). Tests build the real server; no product package is imported. No network is
used: Slack is an `httptest` server (a Node server in the UI spec). Go tests run through
`e2e/with-secret-store`.

Slack's MCP server (`https://mcp.slack.com/mcp`) has no dynamic client registration and accepts only
internal or Marketplace apps. So each workspace creates its own internal Slack app, and the user
pastes its Client ID once (Set up). After that, Connect and Sign in again work as for Notion: PKCE
public client, no client secret, user token.

## Test seam (env the server reads)

| Env | Default | Meaning |
| --- | --- | --- |
| `ATOLL_SLACK_MCP_URL` | `https://mcp.slack.com/mcp` | URL of Slack's MCP server. Everything else is discovered from it, as in #65 |

The fake serves, on one origin: `POST /mcp` (`401` with `WWW-Authenticate: Bearer
resource_metadata="<origin>/.well-known/oauth-protected-resource"` to anything but the latest access
token), the protected-resource metadata (`resource`, `authorization_servers` naming the origin and
`scopes_supported`, the user scopes), and `/.well-known/oauth-authorization-server` with
`authorization_endpoint` (`<origin>/oauth/v2_user/authorize`), `token_endpoint`
(`<origin>/api/oauth.v2.user.access`), `code_challenge_methods_supported ["S256"]` and **no
`registration_endpoint`**. Reef sends nothing to `/register`.

## Status and Set up

The catalog gains a fifth hosted entry, last (after Cloudflare):

| id | name | url | status |
| --- | --- | --- | --- |
| `slack` | `Slack` | `https://mcp.slack.com/mcp`, or `ATOLL_SLACK_MCP_URL` | `needs_setup` |

It is `kind: "catalog"`, remote, picked by Bots like the others, and cannot be edited or removed. The
config key a run gives the provider is its name, `Slack` (tools read `mcp__Slack__<tool>`).

- `status` gains `needs_setup`: Slack has no Client ID yet. Listing the catalog sends no request to
  Slack. After Set up the status is `not_connected`, then `connected` / `needs_sign_in` as in #65.
  Never `needs_app` for Slack.
- Existing tests that pinned four catalog entries (the #64 list, the refusals count, the UI catalog
  count, the Bot switches, the Remove list and the #65 UI server list) were updated to five in this
  commit; nothing else in them changed.

| Request | Reply |
| --- | --- |
| `GET /api/mcp-servers/slack/setup` | `200 {redirect_uri, scopes}` |
| `POST /api/mcp-servers/slack/setup` `{client_id}` | `204` |

- `redirect_uri` is the one fixed `http://127.0.0.1:<Reef port>/oauth/callback` of #65 (the same on
  every call), and the exact `redirect_uri` of the authorize URL and of the code exchange.
  `scopes` is the `scopes_supported` array of Slack's protected-resource metadata (Reef reads it from
  Slack when asked; this is the only request `GET` sends).
- `POST` stores the Client ID in the OS secret store, service `atoll`, username
  `mcp:slack:client_id`, value the bare Client ID. **Decision**: the Client ID is public (it is in
  the authorize URL) but is kept as the #65 record keeps it: in the secret store and no other place
  (no API reply, `atoll.db`, file under `ATOLL_DATA_DIR`, SSE event or log). It sends no request to
  Slack. `400` with `error` for a missing or empty `client_id`; status stays `needs_setup`. There is
  no secret field: Slack's PKCE flow has no client secret, and a body `client_secret` is not read.
- `GET` and `POST` of `/api/mcp-servers/{id}/setup` for any other id (`notion`, `context7`, an
  unknown id) are `404`.
- `POST /api/mcp-servers/slack/connect` from `needs_setup` is `409` with `error` containing
  `client ID` (any case). From `not_connected` and `needs_sign_in` it is `200 {authorize_url}`.
  Setting a Client ID again while connected is not specified.

## Connect

Connect is #65's, with the saved Client ID in place of a registered one:

- `authorize_url` is the `authorization_endpoint` with `response_type=code`, `client_id` (the saved
  one), `redirect_uri=<redirect_uri of Set up>`, `code_challenge` (S256 of a random verifier of 43 or
  more characters), `code_challenge_method=S256`, `state` (unguessable, one per Connect) and `scope`:
  the `scopes` of Set up joined by single spaces. `resource` is not pinned for Slack.
- The callback checks `state`, then POSTs the `token_endpoint` (form, `grant_type=authorization_code`,
  `code`, `redirect_uri`, `client_id`, `code_verifier`) with **no `client_secret` and no
  `Authorization` header**, stores the result and answers the HTML page, all before it replies.
- Slack's token endpoint answers JSON with HTTP 200: success is `{"ok":true,"access_token",
  "refresh_token","expires_in":43200,...}`; failure is `{"ok":false,"error":"..."}`. Reef treats
  `ok:false` as a rejection whatever the status. A reply with `ok:true` and the three fields is the
  only success.
- Tokens: the record is #65's, `mcp:slack:oauth`, value JSON `{"client_id", "access_token",
  "refresh_token", "expires_at"}` (`client_id` is the saved Client ID, `expires_at` about twelve
  hours ahead). Access and refresh tokens are in no other place (leak rules of #65).

## Run, refresh, sign in again

- **Run**: a Bot that picks `slack` gets, as for any signed-in server, a remote server entry named
  `Slack` with `Authorization: Bearer <access token>` (Claude `headers`, Codex `http_headers`). The
  tool call is one approval.
- **Refresh before a run**: when the access token expires within ten minutes, Reef POSTs the
  `token_endpoint` (form `grant_type=refresh_token`, `refresh_token`, `client_id`, no
  `client_secret`, no `Authorization`) and stores the new access token, the **new refresh token**
  and the new expiry. Slack's refresh tokens are single use: it rejects the old one once it issued a
  new one. The test sets `expires_in` to 300 for both the code grant and the first refresh, so the
  second run refreshes again and succeeds only if Reef saved and sent the new refresh token. More
  than ten minutes left: no request.
- **Needs sign-in**: a refresh answered `{"ok":false,"error":"invalid_refresh_token"}` (HTTP 200) sets
  `status` to `needs_sign_in`. The run that tried it ends `failed` before the provider starts and
  its `error` contains `Slack needs sign-in` (any case), never `invalid_refresh_token` or a token.
  Sign in again is `connect` again with the same saved Client ID (no new Set up); the Bot picks
  stay.

## UI

Slack's item of the `Catalog` list shows its URL and its status as its own element, with:

| `status` | text | button |
| --- | --- | --- |
| `needs_setup` | `Needs setup` | `Set up` |
| `not_connected` | `Not connected` | `Connect` |
| `connected` | `Connected` | none |
| `needs_sign_in` | `Needs sign-in` | `Sign in again` |

(The last three are #65's.) `Set up` opens a `dialog` named `Set up Slack` with: an ordered `list`
named `Steps` (at least one `listitem`) that tells the user how to create the internal Slack app; the
dialog's text mentions `PKCE`, shows the exact `redirect_uri`, and shows each scope of `scopes`; a
`textbox` named `Client ID` and a button `Save` (sends `POST .../setup`). It has no other textbox
(no secret field). `Save` closes the dialog; the item shows `Not connected` and `Connect` and no
`Set up`, with no reload. `Connect` and `Sign in again` work as in #65 (popup, polling). The Client
ID and tokens are never shown on the page.

## Acceptance mapping (#66)

| Acceptance | Tests |
| --- | --- |
| The Slack row shows Set up with the steps (PKCE, the redirect URL, user scopes) and a Client ID field; no secret field | UI `the Slack row offers Set up with the steps and a Client ID field; saving it offers Connect`; Go `TestSlackMCP_SetupSavesTheClientIDInTheOSStore` (`needs_setup`, `GET setup`, refusals, `mcp:slack:client_id` in the keyring, scan) |
| Against a fake Slack: Connect, Allow, Connected; PKCE and no `client_secret`; tokens in the secret store only | Go `TestSlackMCP_ConnectStoresTokensInTheOSStore` (authorize query, exchange form, no registration, record in the keyring, scan of replies, `atoll.db`, data dir, log); UI `Connect opens the Allow page in a popup and the Slack row shows Connected` |
| A run gets Slack with the bearer token; the scripted tool call succeeds | Go `TestSlackMCP_RunGetsBearerToken/{claude,codex}` |
| An expired access token is refreshed with the single-use refresh token and the new one is saved; a rejected refresh shows Needs sign-in; Sign in again | Go `TestSlackMCP_RefreshUsesTheSingleUseTokenAndSavesTheNewOne`, `TestSlackMCP_RejectedRefreshNeedsSignIn`; UI `a rejected refresh shows Needs sign-in on the Slack row; Sign in again reconnects` |
| Redirect is one fixed localhost URL Reef shows in Setup | Go `TestSlackMCP_SetupSavesTheClientIDInTheOSStore` (same `redirect_uri` twice), `TestSlackMCP_ConnectStoresTokensInTheOSStore` (the exact `redirect_uri` of authorize and exchange); UI the Set up dialog test and the Connect test |

## Not covered

- Real Slack (an internal app in a real workspace, whether it needs an MCP toggle or admin approval,
  whether `/oauth/v2_user/authorize` accepts PKCE and `scope`) and the exact `scope` and `resource`
  parameters real Slack wants: only the user can check.
- `docs/design.md` and "the tests land first" (not software-checkable here).
- Setting a new Client ID while connected; signing out of Slack; Connect on a machine with Slack's
  real URL (no network); Slack's token reply with no `refresh_token` or `expires_in`; a denied Allow
  page; a restart between Connect and the callback; a Bot that picks Slack before Set up.
- The look of the dialog and the row; only roles, names and texts are pinned.
