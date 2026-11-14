# Chief read tools test contract

Extends [skills-contract.md](skills-contract.md) → Platform MCP tools. The six write tools, their
change records and Undo stay as they are.

## Tools

Five more names on the same per-run `atoll` MCP server. They appear in `tools/list` only when
the run's Bot has `can_manage_platform:true`, like the write tools. Each takes `{}` and returns
MCP text holding a JSON array. Extra fields are allowed unless a line says otherwise.

| Tool | Each item |
| --- | --- |
| `list_bots` | Every Bot `GET /api/bots` returns, with the same values for `id`, `name`, `model`, `look`, `skills`, `repo_access`, `connections` (a field `GET /api/bots` omits may be omitted too) |
| `list_skills` | `{id, name}` of each library skill; no `content` field |
| `list_connections` | `{id, kind, instance_url, status}`, the same values as `GET /api/connections` |
| `list_repos` | `{id, name, path}` of each registered repo |
| `list_models` | `{name, label, models: [{id, name}]}` for each provider with `signed_in:true` in `GET /api/providers`, the same values; no entry for a provider not signed in |

"Enabled, ready" in the ticket means what the New Bot model picker shows: providers with
`signed_in:true` (which implies `found`).

Read tools record no change: no `platform_changed` event, and `GET /api/threads/{id}/changes`
stays `[]`. The thread's change card is drawn from those two, so it shows none.

`list_models` and the `create_bot`/`edit_bot` model check take the models from the providers'
cached state, the source of `GET /api/providers`. Once `GET /api/providers` has answered, a Chief
run that calls `list_models` starts no provider process (fake call logs) beyond the run's own
session and one sign-in status check for its launch: no `--version`, no model probe.

A read tool's `tool_call` event has `kind: "platform"` on Codex too, where the tool's name has no
server prefix (`list_bots`).

The fake Codex starts an `mcpToolCall` item (`item/started`, server `atoll`, the tool's name and
arguments) before each platform tool call, as app-server does. The fake Claude still makes them
without a `tool_use` line.

## Fake scenarios

- `platform-read` (prompt `List every Bot, skill, connection, repo and model.`): requires all
  eleven platform tools in `tools/list`, else the run fails with `platform tool missing: <name>`.
  Calls the five read tools in the order above, then replies. The reply is one JSON object:
  each read tool's result by tool name.
- `platform-bots` (prompt `Which Bots do I have?`): calls `list_bots` only and replies
  `Bots: <names sorted, comma separated>.`, e.g. `Bots: Chief, Lagoon.`
- `platform-denied` now also fails when a non-platform run receives any read tool.

## Acceptance mapping

| Acceptance | Tests |
| --- | --- |
| Switch on lists the five tools; switch off does not | `TestChiefRead_Tools/{claude,codex}` (on); `TestSkills_EditUndoAndNoPlatformTools/{claude,codex}` (off, via `platform-denied`) |
| `list_bots` fields as `GET /api/bots` | `TestChiefRead_Tools/{claude,codex}` |
| `list_skills`, `list_connections`, `list_repos` fields | `TestChiefRead_Tools/{claude,codex}` |
| `list_models` only signed-in providers, as the picker | `TestChiefRead_Tools/{claude,codex}` (both installed, one signed in) |
| "Which Bots do I have?" answers from `list_bots` | `TestChiefRead_WhichBots/{claude,codex}`; reading no file is real-provider only |
| Read tools record no change, no change card | `TestChiefRead_Tools/{claude,codex}` |
| Fakes work before the product | `TestSkillsFixtures_ProviderScenarios/{claude,codex}/{read,bots}` |
| A `list_models` call starts no provider process | `TestChiefRead_ListModelsStartsNoProcess` |
| Codex read calls are platform tool calls, not "Thinking" | `TestChiefRead_CodexReadToolIsPlatform` |
