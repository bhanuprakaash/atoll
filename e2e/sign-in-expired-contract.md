# Expired sign-in test contract

Extends [mac-app-contract.md](mac-app-contract.md) for issue #32. Playwright drives the real UI
the binary serves, with the scripted fake providers; no product package is imported. JSON is
snake_case and replies may include fields not listed here.

## Fakes and harness

A provider's login is revoked when its credentials file (`.credentials.json` for Claude,
`auth.json` for Codex, in Reef's folder for the provider) holds `{"fake":"revoked"}`;
`Machine.revoke(provider)` in `e2e/ui/atoll.ts` writes it. The fake has no server, so the file
stands in for a token the provider's server refuses. Until a turn calls the model the CLI does not
know: `auth status` / `login status` still say signed in. At the next user turn the fake drops
the login (deletes the file, so status says signed out from then on), then fails the turn the way
the real CLI does:

- Claude (`-p` stream-json): a `system`/`init` line, a synthetic `assistant` message with
  `"error":"authentication_failed"` and text `Failed to authenticate: OAuth token revoked`, a
  `result` with `is_error: true` and that text as `result`, then exit 1. The init line carries
  the session id of the recording for the prompt, so a later `--resume` of it plays.
- Codex (`app-server`): `turn/start` replies with an `inProgress` turn, then `turn/started`, an
  `error` notification and `turn/completed` with `status: "failed"`, both with error
  `{message: "Your access token could not be refreshed because your refresh token was revoked.
  Please log out and sign in again.", codexErrorInfo: "unauthorized"}`. The app-server keeps
  running.

A real revoked login was not reproduced: the Claude text is the one a user saw in a Chief thread;
the Codex text and fields follow codex-cli's own refresh-failure message.

## API

`GET /api/runs/{id}` adds `error_kind`: `"signed_out"` when the run failed because its
provider's sign-in was refused, otherwise empty. After such a run, `GET /api/providers` shows that
provider `signed_in: false`.

## UI contract

- A run with `error_kind: "signed_out"` shows, in the Messages log, the text
  `Claude sign-in expired` (Claude Code Bots) or `Codex sign-in expired` (Codex Bots) and a
  `button` "Sign in again". The provider's raw error text never appears on the page. The line
  comes from the run, so it is there again when the thread is reopened.
- Sign in again starts the same sign-in as Sign in on the Providers page, from the thread: the
  sign-in page opens in a popup at the URL the provider's login prints; for Claude a `textbox`
  "Code" and a `button` "Submit" appear on the page and take the code. The user stays on the
  thread.
- Once signed in, the thread's `button` "Retry" sends the same message and the run completes.
- The Providers page shows that provider "Not signed in" after the failed run.
- A run that fails for any other reason keeps today's line, `Run failed: <error>` with Retry, and
  has no Sign in again.

## Acceptance mapping

| Acceptance | Tests |
| --- | --- |
| Run fails; thread says `<Provider> sign-in expired` with Sign in again, not the raw error | `sign-in-expired.spec.ts` (Claude and Codex) |
| Sign in again runs the provider's sign-in; then Retry sends the same message and completes | `sign-in-expired.spec.ts` (Claude and Codex) |
| Providers page shows the provider signed out after the failed run | `sign-in-expired.spec.ts` (Claude and Codex) |
| Another failure still shows its error | `chat.spec.ts` "a failed run shows a line with Retry" |
