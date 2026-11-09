# GitLab token refresh test contract

This extends the Slice 3 connection contract in [README.md](README.md). Tests use the same
device-flow endpoints, a local OAuth server, the fake `glab` and the OS secret store.

## Stored tokens

The device-flow token response carries `refresh_token` and `expires_in`. The secret store
value (service `atoll`, username `gitlab:<connection_id>`, as before) is JSON:
`{"access_token", "refresh_token", "expires_at"}`, `expires_at` in RFC3339. A value that is
not JSON is a legacy bare access token: it is used as is, its expiry is unknown, and no
refresh is attempted.

## Refresh before a run

Before a run uses a connection, if the token expires within 10 minutes, Atoll POSTs the
connection's `/oauth/token` with form `grant_type=refresh_token`, `refresh_token` and a
nonempty `client_id`. It stores the returned pair and expiry in place of the old record and
gives the run the new access token (`OAUTH_TOKEN`). Neither token is written to a file or log.
A token with more than 10 minutes left causes no request.

`glab`'s `config.yml` for the run keeps `is_oauth2` and also sets `oauth2_expiry_date:` to a
nonempty value, including for a legacy token. The fake `glab` fails with
`could not parse "" as an expiry date` when `is_oauth2` is set and the date is empty. Only
presence is tested: the date format is not specified.

## Needs sign-in

A refresh answered `400 {"error":"invalid_grant"}` sets the connection status to
`needs_sign_in` (`GET /api/connections` and `GET /api/connections/{id}`). The run that
tried it ends with status `failed` and its `error` (`GET /api/runs/{id}`, the
`run_finished` event) contains `needs sign-in`, e.g. `GitLab needs sign-in`. No provider
run gets `OAUTH_TOKEN` or `GLAB_CONFIG_DIR` for that connection.

`POST /api/connections/gitlab` with `{"connection_id": "<id>"}` starts the device flow for
that existing connection and replies `201` like `/api/connections/gitlab/start`, with the
same `id`. Polling it to completion stores the new record and sets status `connected`. No
second connection is created, and Bots that had it still list it.

When `ATOLL_SECRET_STORE_UNAVAILABLE` is non-empty, every secret-store read and write fails on
Linux and macOS. A secret save failure reports that the system keychain could not save the
credentials, with an HTTP status of 500 or greater.

## Acceptance mapping

| Acceptance | Tests |
| --- | --- |
| With `ATOLL_SECRET_STORE_UNAVAILABLE=1`, a keychain save failure returns HTTP 500 or greater, names the keychain, exits Pending, offers Sign in again, and displays no raw JSON | `TestKeychainFailure_Connections/GitLab`; `ui/keychain-failure.spec.ts` |
| Expired token refreshed once; the run pushes | `TestGitLabRefresh_ExpiredTokenIsRefreshedForTheRun` |
| Stored tokens replaced; next run does not refresh | `TestGitLabRefresh_RefreshedTokensReplaceTheOldOnes` |
| `invalid_grant` gives `needs_sign_in` and a run error | `TestGitLabRefresh_InvalidGrantNeedsSignIn` |
| Sign in again keeps id and Bots | `TestGitLabRefresh_SignInAgainKeepsTheConnection`, `e2e/ui/gitlab-refresh.spec.ts` |
| Legacy bare token works | `TestGitLabRefresh_LegacyBareTokenStillWorks` |
| glab accepts the config | the fake `glab` in every test that runs `mr create` |
