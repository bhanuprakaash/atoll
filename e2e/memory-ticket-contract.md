# Retiring memory about a ticket: test contract

Extends [memory-contract.md](memory-contract.md) and [pr-wake-contract.md](pr-wake-contract.md) for
issue #195; the behaviour is the "Memory" section of `docs/design.md` (ticket, retire on close, the
wake's memory clause), settled in the docs PR this one follows. Tests: `memory_ticket_test.go`. They
drive the real binary with the fake GitHub, the scripted fake providers and real git; no product
package is imported. `ATOLL_REPO_POLL_INTERVAL=100ms` shortens the poll interval.

## What the tests use

- `POST /api/memory` takes `ticket` (`"#7"`); every item in `GET /api/memory` and in the reply has
  `ticket` (empty when none) and `retired_reason` (empty unless Reef retired it, then
  `issue closed`).
- The fake GitHub adds `GET /repos/{owner}/{repo}/issues/{n}` (`state`, `state_reason`, an `ETag`,
  `304` for a matching `If-None-Match`, behind the connection's token) and a `body` on the pull
  request. An issue no test closed is open. It counts reads and `304`s per issue.
- Injection is read from the provider's prompt log: the first message of a new thread lists items as
  `[<id>] ...`.

## Acceptance mapping

| Acceptance (#195)                                                                                                | Tests                                                                                                                                                                                                                                                                                                                              |
| ---------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Items name an issue in `ticket`; Reef stops injecting those of a closed issue, shown retired with "issue closed" | `TestMemoryTicket_ClosedIssueRetiresItems/{claude,codex}` (injected while open; retired on `completed` and `not_planned` with body, why and ticket kept; other items stay; a conditional read per ticket answered `304`; no read once no accepted item is left; a user's retire has no reason), `TestMemoryTicket_OnlyOnRepoItems` |
| The merged-PR wake names the memory items about the ticket and asks to replace or retire them                    | `TestMemoryTicket_MergedWakeNamesItems/{claude,codex}`, `TestMemoryTicket_MergedWakeWithoutItemsIsPlain/{claude,codex}`, `TestMemoryTicket_MergeThatClosesTheIssueWakesBeforeRetiring/{claude,codex}`                                                                                                                              |
| After the round, `select count(*) ... body like 'Round %'` is 0                                                  | Not specced: owner only, see the PR                                                                                                                                                                                                                                                                                                |

Not covered: `remember` with `ticket` through MCP (no recording passes it; REST seeds the same store),
the Settings > Memory tag `Retired · issue closed`, a repo not on GitHub, a failed or `401` issue
read, back-off, and the wording beyond the pinned phrases.
