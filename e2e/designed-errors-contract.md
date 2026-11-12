# Designed errors test contract

Extends [mac-app-contract.md](mac-app-contract.md) for issue #149. Design: `docs/design.md`, App and
UI, "Errors are designed, never raw" and the code table. Playwright drives the real UI the binary
serves and Go drives the real API, both with the scripted fake providers; no product package is
imported. JSON is snake_case and replies may include fields not listed here.

## Fakes and harness

Every failure is a real one. A folder picker that exits 3 is a fake `zenity` (Linux) or
`osascript` (macOS) script in the machine's provider folder. An unreachable GitLab is
`http://127.0.0.1:1`. A taken MCP server name is made through the API before the page adds it; a Bot deleted elsewhere is
made through the API after the page loaded. A failed run is a prompt with no recording; a provider that is not installed is a Bot on
Codex on a machine with only the Claude fake. A server that goes away is `Machine.kill()`, back
with `Machine.restart()`. A keychain that fails is `ATOLL_SECRET_STORE_UNAVAILABLE=1`. One spec
fulfils a gateway's HTML 502 through Playwright's `page.route`: no fake server answers like a
proxy.

## API

A failed request answers `{"error": "<sentence>", "code": "<id>"}`:

| Cause | Status | `code` | `error` |
| --- | --- | --- | --- |
| a thing that is not there, an unknown route or method | 404, 405 | `not_found` | `That no longer exists.` |
| a message to a closed thread | 409 | `closed` | `This thread is closed.` |
| closing a thread whose run is going | 409 | `busy` | `A run is going in this thread.` |
| a name already taken | 409 | `exists` | `Something with that name already exists.` |
| a rule that refuses (Chief cannot be deleted) | 409 | `conflict` | the rule's own sentence |
| a bad input | 400 | `invalid` | a plain sentence, no package prefix (`invalid MCP server: `) |
| a host Reef cannot reach | 502 | `unreachable` | `Reef couldn't reach 127.0.0.1:1.` |
| a sign-in GitHub says expired | 401 | `signed_out` | `That sign-in expired or was refused.` |
| the system keychain fails to save | 500 | `keychain` | `Reef couldn't save the GitHub sign-in to the system keychain.` (GitLab likewise; MCP `Reef couldn't save <what> to the system keychain.`), no cause |
| no folder picker on this system | 501 | `unsupported` | `No folder picker on this system.` |
| anything else | 500 | `internal` | `Something went wrong on our side.` |

The Go error chain of an `internal` failure is in the server log (`request failed`, `method`,
`path`, `err`), never in the reply. A run on a provider that is not installed is `failed` with
`error_kind: "provider_off"` and `error` `Codex isn't installed or isn't signed in.`.

The MCP sign-in callback page keeps its status codes and says the keychain sentence above with no
cause, then the next step, then a closed `<details>` whose `<summary>` is `Details`.

## UI contract

The notice is a `role="alert"` block. It shows, in this order: the title; one line of what to do
(a chat run line has none: its action stands in); the action as a `button` or `link` when there is
one; a `button` "Details" with `aria-expanded="false"`. While Details is closed the page shows no
Go chain, status text, provider text, JSON (`{"error":`), `internal error` or `Failed to fetch`.
Details opens to `<code> · <status> · <METHOD> <path>` for a failed request, and to the provider's
raw error for a run line. No text in the notice is the danger colour (`rgb(229, 72, 77)`); only its
icon may be.

- Generic failure: title `Something went wrong on our side.`, next `Try again. If it keeps
  happening, copy the details into a bug report.`, action `Try again`, which runs the action again.
- Unreachable: title `Reef couldn't reach 127.0.0.1:1.`, next `Check the address and your
  connection, then try again.`, action `Try again`.
- Name taken: title `Something with that name already exists.`, next `Pick another name.`
- Gone: title `That no longer exists.`, next `It was deleted or moved. Go back and pick another.`
- Keychain: title `Reef couldn't save the <GitHub|GitLab> sign-in to the system keychain.` (an MCP
  secret: `Reef couldn't save <what> to the system keychain.`), next `Unlock the keychain or restart
  Reef, then try again.`, action `Sign in again` on a connection and `Try again` on an MCP server.
- A failed run: the line starts `Didn't run: `, has a `button` "Retry" and Details with the
  provider's raw error. A provider that is not installed: `Didn't run: Codex isn't installed or
  isn't signed in.` with an `Open settings` button or link.
- Losing the server: one `role="status"` "Reconnecting" on every page, no `role="alert"`, no error
  text for the request made meanwhile; it leaves once the server answers.
- The callback page: the title, a line with `close this window`, a closed `<details>` with the
  summary `Details`, no JSON.
- Source: only `web/src/components/error-notice.tsx` reads `.message`; each file of web/src that
  rendered an error before (the list in `no-raw-errors.spec.ts`) uses `ErrorNotice`.

## Acceptance mapping

| Acceptance | Tests |
| --- | --- |
| One component everywhere an action can fail: title, next step, action, collapsed Details, calm red | `designed-errors.spec.ts` (generic, gateway, unreachable, name taken, gone, failed run, provider off), `keychain-failure.spec.ts` (GitHub, GitLab, MCP) |
| No screen renders `error.message`, JSON or a chain; the grep shows every former site on the component | `no-raw-errors.spec.ts`, the `raw` checks in each spec above |
| Server answers `{error, code}`; known causes have codes; unknown is generic, the chain is logged | `TestErrors_KnownCausesHaveCodeAndSentence`, `TestErrors_UnreachableNamesTheHostNotTheChain`, `TestErrors_BusyThread`, `TestErrors_UnknownCauseIsGenericAndTheChainIsLogged`, `TestErrors_ExpiredSignInIsSignedOut`, `TestKeychainFailure_*`, `TestRepos_PickFolderLinux/no_zenity` |
| Run failures and callback pages follow the same shape | `designed-errors.spec.ts` (failed run, provider off, callback page), `TestErrors_ProviderOffFailsTheRunPlainly`, `TestKeychainFailure_MCPOAuth` |
| One app-level Reconnecting notice | `designed-errors.spec.ts` (losing the server) |
| `docs/design.md` describes the component and the codes | the docs PR, reviewed by the owner |
