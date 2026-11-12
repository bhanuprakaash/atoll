# Secret store down test contract

Extends [mcp-servers-contract.md](mcp-servers-contract.md) for issue #179. The design is in
`docs/design.md` (OS secret store). `ATOLL_SECRET_STORE_UNAVAILABLE=1` makes every secret read
fail; the tests sign in or add the server with the store up, then restart the same data with it
down.

## API

A run of a Bot that needs a secret (a connection's token, an MCP server's env or header secret)
while the store is down:

- `GET /api/runs/{id}`: `status: "failed"`, `error_kind: "secret_store"`, `error` exactly
  `Reef can't reach its secret store. Restart Reef, or unlock it, then send again.`
- The provider is never started.
- `GET /api/threads/{id}/messages`: the user message is kept, with `not_read: true`.
- `logs/server.log` (the server's stderr): exactly one `WARN` JSON line with `run_id` and
  `error_kind: "secret_store"` for the run.
- A run of a Bot that needs no secret finishes `done` with a reply.

## UI

- The Messages log shows the sentence above, not `Run failed: ...`, and no Retry. No cause text
  (`unavailable`, `keyring`, the secret's name, `MCP server`) and no JSON.
- The user message shows "Not read" and a Resend button.
- With the store back, Resend sends the same text as a new message and it completes; the old
  message keeps "Not read".

## Acceptance mapping

| Acceptance | Tests |
| --- | --- |
| Run fails `secret_store`; thread shows the sentence, no raw error | `TestSecretStoreDown_RunFailsPlainlyAndKeepsTheMessage`, `secret-store-down.spec.ts` |
| One WARN line per failed run with kind and run id | `TestSecretStoreDown_RunFailsPlainlyAndKeepsTheMessage` |
| The message is marked not read with a resend action | `TestSecretStoreDown_RunFailsPlainlyAndKeepsTheMessage`, `secret-store-down.spec.ts` |
| A Bot with no connection still works | `TestSecretStoreDown_RunWithoutSecretsStillWorks` |
