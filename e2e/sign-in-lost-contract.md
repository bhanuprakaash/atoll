# GitHub sign-in lost test contract

Issue #397. The GitHub connection went `needs_sign_in` while GitHub never revoked the token, and
nothing recorded why. These tests drive the real binary with the fake GitHub; no product package is
imported. JSON is snake_case and replies may include fields not listed here.

## Log file

`atoll serve` writes every log line it writes to stderr (the same JSON, one object per line) also
to `<data dir>/logs/server.log` (data dir is `ATOLL_DATA_DIR`), creating `logs/`. A restart appends
to the file.

## Sign-in lost line

When a GitHub 401 marks a connection `needs_sign_in` (the pre-run `GET /user` check, or the MR
watcher's reads), the server writes exactly one WARN line with msg `GitHub sign-in lost` and attrs:

- `connection_id`, `bot_id`
- `endpoint`: the URL that answered 401
- `github_message`: the `message` field of GitHub's 401 body, e.g. `Bad credentials`
- `github_request_id`: the `X-GitHub-Request-Id` response header, empty if absent

The token never appears in the log file. The fake GitHub answers every 401 with
`X-GitHub-Request-Id: E2E0:1397:5EED:401`.

## API

`GET /api/connections` and `GET /api/connections/{id}` carry `signed_out_at`: the RFC 3339 UTC time
the connection went `needs_sign_in`; omitted for any other status. Signing in again (status
`connected`) removes it.

## UI

Settings > Connections: a GitHub card whose connection is `needs_sign_in` shows `Signed out`
followed by the local date and time (tests match `/Signed out /`, not a format). It disappears once
the connection is connected.

Tests: `TestServerLog_MirrorsStderrAndAppends`, `TestGitHub_RejectedTokenNeedsSignIn`,
`TestPRWake_RejectedTokenLogsSignInLost`, and the UI case `a rejected token shows Needs sign-in;
Sign in again reconnects the same connection`.
