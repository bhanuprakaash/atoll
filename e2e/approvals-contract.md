# Approval cards and external links: test contract

Extends the thread, approval and Mac app contracts ([README.md](README.md),
[mac-app-contract.md](mac-app-contract.md)). Tests build the real server; no product package is
imported.

## Closed approvals

When an approval is answered (allow or deny) or dropped because its run or the provider's request
ended, the thread's event stream gets `approval_closed` with `{run_id, id}`, `id` being the
approval's. A stream opened mid-run replays the run's events; every replayed `approval` whose
approval is no longer open is followed by its `approval_closed`, so the open approvals a client
derives from the replay are exactly the open ones. (Leaving answered approvals out of the replay
entirely also passes.) `POST /api/approvals/{id}` on a closed approval stays `404`.

Two approvals in one run come from the `mcp-use` scenario: the prompt
`Use MCP tools. [<call>, <call>]` makes the fake provider call the Bot's MCP server twice, asking
before each call.

The UI drops a card on `approval_closed`, so after a reconnect or a reload mid-run only open
approvals show as cards. A card whose answer gets `404` disappears and shows no "Could not answer
approval".

## Claude's MCP tool timeout

Claude Code cancels an MCP tool call after 60 seconds unless `MCP_TOOL_TIMEOUT` (milliseconds)
says otherwise, and Reef's permission prompt tool is such a call. Every Claude run gets
`MCP_TOOL_TIMEOUT` of at least `600000` in its env.

## Links

A link in a chat reply opens in a new browsing context (the Mac app's `on_new_window` sends it to
the default browser); the thread's page stays where it was. The `link` scenario
(`Reply with a link to the Reef docs.`, Claude) replies with the markdown link
`[the Reef docs](https://docs.reef.test/start)`.

## Acceptance mapping

| Acceptance                                      | Tests                                                                                                                                                           |
| ----------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `approval_closed` on answer, replayed           | `TestApproval_ClosedOnAnswer/{claude,codex}`                                                                                                                    |
| `approval_closed` when the run ends             | `TestApproval_ClosedOnStop/{claude,codex}`                                                                                                                      |
| Only open approvals show after reconnect/reopen | `TestApproval_ClosedOnAnswer`; UI `an answered approval stays gone after reopening the thread mid-run`, `a card leaves when its approval is answered elsewhere` |
| Claude approval answerable after 10 minutes     | `TestApproval_ClaudeToolTimeout` (env); a real Claude run answered after more than 60s remains implementer proof                                                |
| `404` card disappears                           | UI `a card whose answer gets 404 leaves without an error`                                                                                                       |
| Reply link opens outside, thread stays          | UI `a link in a reply opens outside the thread, which stays put`                                                                                                |

Not covered here, implementer proof in the real app: an approval dropped because Claude's prompt
request ended (the 60-second cancel the timeout line removes), and `on_navigation` in
`desktop/src-tauri` opening any non-Reef URL in the default browser while the window stays on
Reef.
