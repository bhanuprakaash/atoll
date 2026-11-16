# GitHub token refresh test contract

Issue #493. This extends [github-connection-contract.md](github-connection-contract.md) and
[sign-in-lost-contract.md](sign-in-lost-contract.md). Tests drive the real binary with the fake GitHub
and the OS secret store. No product package is imported.

## The fake GitHub

Like an OAuth App with expiring user tokens, the device-flow reply carries `access_token`
(`fake-gho-…`), `refresh_token` (`fake-ghr-…`), `expires_in` (28800 unless a test sets it),
`refresh_token_expires_in` 15638400, `scope` and `token_type`. An access token is refused with 401
once `expires_in` has passed (plus half a second, for the gap before the reply reaches the server).
`POST /login/oauth/access_token` with `grant_type=refresh_token`, `client_id` and `refresh_token`
and no `client_secret` returns a new pair of the same shape and kills the old pair. A wrong or old
refresh token, or a test asking for it, gets `200 {"error":"bad_refresh_token"}`; a test can also
have the fake close the connection with no reply.

## Stored record

The secret store value (service `atoll`, username `github:<connection id>`) is JSON with
`access_token`, `refresh_token`, `expires_at` and `refresh_expires_at`, both times RFC 3339 as for
GitLab. A value that is not JSON is a bare token: used as it is, no expiry known, never refreshed.

## Refresh rules

- A run that starts with less than 3 h left on the token, expired or not, refreshes first and gets
  the new token in `GH_TOKEN`, the git credential helper and the `github` MCP header. With more
  than 3 h left there is no request.
- The PR and issue watchers refresh only once the token has expired.
- Concurrent callers on one connection cause one request.
- A rejected refresh token (`bad_refresh_token`) marks the connection `needs_sign_in` with
  `signed_out_at`, stops the run before the provider starts with `GitHub needs sign-in` and writes
  one WARN `GitHub sign-in lost` line with `connection_id` and `bot_id`, an attribute naming the
  refresh token as the cause (any attribute other than `msg`; tests look for the words "refresh
  token" ignoring case, `_` and `-`).
- A refresh with no answer changes nothing: the connection stays `connected`, the secret store entry
  is untouched, no `GitHub sign-in lost` line; the next caller refreshes. What the run that hit the
  error does is not specified.
- Each refresh writes one INFO `GitHub token refreshed` line with `connection_id`. No token, access
  or refresh, reaches the log file or a file under the data dir. The new-expiry attribute is not
  pinned.

## Acceptance mapping

| Acceptance | Tests |
| --- | --- |
| Sign-in stores the record as JSON | `TestGitHubRefresh_SignInStoresTheTokenRecord` |
| A bare token works with no refresh | `TestGitHubRefresh_BareTokenStillWorks` |
| A run with under 3 h left refreshes first and gets the new token | `TestGitHubRefresh_RunRefreshesFirst/{expired,under_3h_left}/{claude,codex}` |
| A run with plenty of time does not refresh | `TestGitHubRefresh_PlentyOfTimeDoesNotRefresh` |
| Watchers refresh only after expiry; wakes go on | `TestGitHubRefresh_PRWatcherRefreshesAfterExpiry`, `TestGitHubRefresh_IssueWatcher` |
| One refresh at a time | `TestGitHubRefresh_ConcurrentRunsRefreshOnce` |
| Rejected refresh token signs the connection out | `TestGitHubRefresh_RejectedRefreshTokenNeedsSignIn`; the card text is `ui/github-connection.spec.ts` |
| A network error keeps the connection | `TestGitHubRefresh_NetworkErrorKeepsTheConnection` |
| One INFO line per refresh, no token | `wantRefreshedLines` and `wantNoTokensInLog` in the refresh tests |
