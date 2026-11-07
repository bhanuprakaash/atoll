# MCP sign-in (hosted servers): test contract

Extends [mcp-servers-contract.md](mcp-servers-contract.md) (library, secrets, Bot picks, what a run
gets), [gitlab-refresh-contract.md](gitlab-refresh-contract.md) (the same refresh and needs-sign-in
rules for another token) and [README.md](README.md) (OS secret store). Tests: `mcp_oauth_test.go`
(Go, with the fake in `mcp_oauth_fixtures_test.go`) and `ui/mcp-oauth.spec.ts` (Playwright, with the
fake in `ui/oauth-mcp-fixture.ts`). Tests build the real server; no product package is imported. No
network is used: the hosted server and its authorization server are `httptest` servers (a Node
server in the UI spec). Go tests run through `e2e/with-secret-store`.

## Changes to #64 shapes

- The server JSON gains `status`: `""` (absent) when no sign-in is involved (local servers, remote
  servers with no sign-in or with `headers`, Context7), else one of `not_connected`, `connected`,
  `needs_sign_in`, `needs_app`. Never a token. `header_names` stays `[]` for a signed-in server: the
  bearer token is not one of the user's headers.
- The catalog gains three hosted entries, in this order after Context7 (so the catalog has four):

  | id | name | url | status |
  | --- | --- | --- | --- |
  | `notion` | `Notion` | `https://mcp.notion.com/mcp` | `not_connected` |
  | `figma` | `Figma` | `https://mcp.figma.com/mcp` | `not_connected` |
  | `cloudflare` | `Cloudflare` | `https://mcp.cloudflare.com/mcp` | `not_connected` |

  They are `kind: "catalog"`, remote, picked by Bots like Context7, and cannot be edited or removed.
  The #64 tests that pinned a one-entry catalog (library list, the refusals count, the UI catalog
  count, Bot switches, the Remove list) were updated to four entries in this commit; nothing else
  in them changed.
- `POST /api/mcp-servers` with `url` and no `headers` first sends the URL one `POST` (an MCP
  `initialize` request, no `Authorization`). Answered `401` with `WWW-Authenticate: Bearer
  resource_metadata="<url>"`, Reef reads the protected-resource metadata (`authorization_servers`)
  and the first auth server's `/.well-known/oauth-authorization-server`, and the `201` reply has
  `status`: `needs_app` when the metadata has no `registration_endpoint`, else `not_connected`.
  Any other answer (`200`, a `401` without that header, an error, no connection) adds the server
  with no status, as before; the probe takes under 5 seconds. With `headers` there is no probe
  (they are the user's own sign-in) and no request to the server.

## Connect

| Request | Reply |
| --- | --- |
| `POST /api/mcp-servers/{id}/connect` (no body) | `200 {authorize_url}` |
| `GET /oauth/callback?code=&state=` | `200 text/html` when it finishes a Connect; `400` for a state Reef did not issue |

- `connect` works for a user server and a hosted catalog entry. `404` for an unknown id, `400` for a
  server that needs no sign-in (`context7`, a local server, a server with no status), `409` with
  `error` containing `registered app` for `needs_app`. It works from `not_connected` and from
  `needs_sign_in` (Sign in again); Reef sends no request to the auth server's `registration_endpoint`
  for `needs_app`. A `registration_endpoint` that refuses the registration (`401` or `403`, as
  Figma's does for a client it has not approved) also gets `409` with `registered app`, and the
  status becomes `needs_app`; the row then says so in place of Connect, with no `internal error`.
- Reef registers a client (`POST registration_endpoint`, JSON, one per Connect or reused: a Sign in
  again may do either) with `client_name` (nonempty), `redirect_uris` exactly
  `["<redirect>"]`, `grant_types` containing `authorization_code` and `refresh_token`,
  `response_types ["code"]` and `token_endpoint_auth_method: "none"`. The reply's `client_id` is used
  everywhere below.
- **Redirect**: `<redirect>` is `http://127.0.0.1:<port>/oauth/callback`, the fixed callback on
  Reef's own server, `<port>` that of the address Reef serves on (the tests read it from
  `listening on http://127.0.0.1:<port>`). The callback is a route of the same server; no other
  listener is opened.
- `authorize_url` is the auth server's `authorization_endpoint` with the query `response_type=code`,
  `client_id`, `redirect_uri=<redirect>`, `code_challenge` (S256 of a random verifier of 43 or more
  characters), `code_challenge_method=S256`, `state` (unguessable, one per Connect) and `resource`
  (the MCP server's URL). Reef opens nothing itself: the caller opens the URL (the UI in a popup,
  see below; the tests with an HTTP client). The status stays as it was until the callback.
- The callback checks `state`, then POSTs the auth server's `token_endpoint` (form,
  `grant_type=authorization_code`, `code`, `redirect_uri`, `client_id`, `code_verifier`,
  `resource`), stores the result and answers the HTML page, all before it replies. The status is
  then `connected`. A `state` is used once.
- The tests "click Allow" by `GET authorize_url` with redirects off: the fake auth server answers
  `302` to `<redirect>?code=<code>&state=<state>`; the test then `GET`s that Location on Reef.

## Tokens

The record is one OS secret store entry: service `atoll`, username `mcp:<server id>:oauth`, value
JSON `{"client_id", "access_token", "refresh_token", "expires_at"}` (`expires_at` RFC3339, from the
token reply's `expires_in`). The tests read it with `secret-tool lookup` (`security` on macOS), not
through Atoll. Client ID, access token and refresh token are in no other place: no API reply (the
`authorize_url` of Connect names the client and is the one exception for the client ID), SSE event,
MCP tool result, `atoll.db`, file under `ATOLL_DATA_DIR` (checked after runs, so the provider's
config is not left in a file), or the server's stderr.

- **Run**: a Bot's run gets the server in its provider config as a remote server with the header
  `Authorization: Bearer <access token>` (Claude `headers`, Codex `http_headers`), whatever the
  header names are otherwise. The tool call is an approval, as for any user-added server.
- **Refresh before a run**: before a run uses the server, if the access token expires within ten
  minutes, Reef POSTs the `token_endpoint` (form `grant_type=refresh_token`, `refresh_token`,
  `client_id`), stores the new access token, refresh token and expiry in place of the old record
  (same `client_id`) and the run gets the new token. More than ten minutes left: no request.
- **Needs sign-in**: a refresh answered `400 {"error":"invalid_grant"}` sets `status` to
  `needs_sign_in`. The run that tried it ends `failed`, before the provider starts, and its `error`
  is a plain message containing `<server name> needs sign-in` (for `docs-oauth`: `docs-oauth needs
  sign-in`), never `invalid_grant` or a token. Sign in again is `connect` again: the server keeps
  its id and every Bot's pick, and the record is replaced. A Bot that picks a server that was never
  connected is not specified here.
- **Remove** (`DELETE /api/mcp-servers/{id}`) also deletes the `mcp:<id>:oauth` entry and drops the
  pick from every Bot (the #64 rule). No request to the auth server is required. Signing out of a
  catalog entry is not specified.

## UI

Every item of `Catalog` and `Your servers` whose `status` is not empty shows it as its own element
(so `getByText(..., {exact: true})` matches) and, beside Edit and Remove for the user's servers:

| `status` | text | button |
| --- | --- | --- |
| `not_connected` | `Not connected` | `Connect` |
| `connected` | `Connected` | none |
| `needs_sign_in` | `Needs sign-in` | `Sign in again` |
| `needs_app` | text matching `this server needs a registered app` (case-insensitive) | none |

- `Connect` and `Sign in again` send `POST /api/mcp-servers/{id}/connect` and open `authorize_url`
  with `window.open` (a popup; the tests wait for the `popup` event). While waiting, the page polls
  `GET /api/mcp-servers` at least once a second, and when the status changes the item shows it with
  no reload.
- `Add by URL` is as in #64 (`Name`, `URL`, `Headers`, `Add`). Its item then shows what the
  `201` reply's `status` says: a server that answers `401` with resource metadata offers `Connect`;
  one that answers `200` shows no status and no Connect button.
- The catalog items of Notion, Figma and Cloudflare show their URL, `Not connected` and `Connect`;
  Context7 shows no status and no button. Tests never click Connect on them.

## Fakes

- Go `oauthMCP` (two `httptest` servers): the MCP server answers `401` to a request without the
  latest access token (`WWW-Authenticate: Bearer resource_metadata="<mcp origin>/.well-known/
  oauth-protected-resource"`), serves that metadata (`resource`, `authorization_servers` naming the
  second origin) and tool `answer` (`oauth-answer-42`) to the latest access token. The auth server
  serves `/.well-known/oauth-authorization-server` (`authorization_endpoint`, `token_endpoint`,
  `registration_endpoint` unless switched off, `code_challenge_methods_supported ["S256"]`),
  `/register` (a unique `client_id`), `/authorize` (checks the registered client and redirect URI,
  `S256`, `state`, `resource`; redirects at once with a single-use code) and `/token` (checks the
  PKCE verifier against the challenge, the redirect URI, the client and `resource`; issues unique
  access and refresh tokens with `expires_in` 7200 unless the test sets less; refresh issues a new
  pair, or `invalid_grant` when told to or when the refresh token is not the latest). A wrong
  request is a `t.Errorf` in the test. Token values read `fake-oauth-access-...`,
  `fake-oauth-refresh-...`, `fake-oauth-client-...`.
- `ui/oauth-mcp-fixture.ts` is the same on one origin, with `config` switches: `registration`,
  `requireAuth`, `ttl`, `reject`.
- The fake providers need no change: they send the config's headers to the HTTP server and the
  scenario `mcp-use` calls it, so a call that succeeds proves the header arrived.

## Acceptance mapping (#65)

| Acceptance | Tests |
| --- | --- |
| Connect registers a client, returns the authorize URL, the callback finishes, Connected; client ID and tokens in the OS secret store only | Go `TestMCPOAuth_ConnectStoresTokensInTheOSStore` (registration, authorize query, PKCE and state at the fake, record in the keyring, scan of replies, `atoll.db`, data dir, log); UI `Add by URL to a server that needs sign-in offers Connect; Allow in the popup shows Connected` |
| A Bot run gets `Authorization: Bearer <token>`; the scripted tool call succeeds | Go `TestMCPOAuth_RunGetsBearerToken/{claude,codex}` (the fake rejects any other header; scan after the run) |
| Token expiring within ten minutes is refreshed before the run; rejected refresh gives Needs sign-in and a run that stops before the provider; Sign in again keeps id and picks | Go `TestMCPOAuth_ExpiringTokenIsRefreshedBeforeTheRun`, `TestMCPOAuth_RejectedRefreshNeedsSignIn`; UI `a rejected refresh shows Needs sign-in; Sign in again reconnects the same server` |
| Add by URL: 401 with resource metadata offers Connect; 200 adds without sign-in | Go `TestMCPOAuth_AddByURL`; UI the two Add by URL tests |
| No `registration_endpoint`: "this server needs a registered app" instead of Connect | Go `TestMCPOAuth_AddByURL` (`needs_app`, `409`); UI `a server that answers 200 is added without Connect; one with no registration says it needs a registered app` |
| Catalog shows Notion, Figma, Cloudflare with Connect | Go `TestMCPOAuth_CatalogListsHostedServers`; UI `the catalog lists Context7, Notion, Figma and Cloudflare; the hosted ones offer Connect` |
| Remove signs out: tokens deleted from the secret store | Go `TestMCPOAuth_RemoveSignsOut` |

## Not covered

- **#64, later, after merging `feat/github-connection`**: the two GitHub-row lines ("GitHub row
  read-only from the GitHub connection"; "a URL that is GitHub's MCP URL shows `GitHub has a
  connection` with `Use connection` / `Add anyway`; a Bot with both gets only the connection's
  entry"). They need the GitHub connection and go into a later test commit.
- `docs/design.md` (MCP sign-in flow; Reef owns the tokens, providers never run their own MCP
  OAuth) and "the tests land first" (not software-checkable here).
- Real Notion, Figma and Cloudflare, real Claude and Codex reading an `Authorization` header from
  the config, and whether Figma refuses an unknown client (only the user can check).
- Connect on the catalog's hosted entries (no network); sign-out of a catalog entry; `PUT` on a
  server that has a sign-in; a Bot that picks a server that was never connected; an Allow page the
  user denies (`error=` on the callback); a token reply with no refresh token or no `expires_in`;
  a restart between Connect and the callback.
- The look of the page; only roles, names and texts are pinned.
