# Chief sets a Bot's model and look: test contract

Extends the platform tools of [skills-contract.md](skills-contract.md). Tests: `chief_bot_test.go`.

## Tools

`create_bot` and `edit_bot` declare two more string properties in their `tools/list` `inputSchema`:
`model` and `look`. The fakes send only arguments a tool's schema declares, as a model reading
`tools/list` would.

- `model` is a model `id` as `GET /api/providers` lists it under `models` for a ready provider (for
  example `reef-probe` from the fake Claude). The Bot stores it as given; `GET /api/bots/{id}`
  shows it as `model`.
- `create_bot` without `model` behaves as today: the Bot's `model` is `""`.
- `create_bot` with a `model` no ready provider lists fails, and its text contains the `id` of every
  model the ready providers list. No Bot is created and no change is recorded.
- `look` is one of `jellyfish`, `whale`, `shark`, `penguin`, `anglerfish`, `pufferfish`, `octopus`,
  `crab`, `seal`, `manta`, `scallop`, `narwhal`. Any other value fails on both tools, and its text
  contains all twelve.
  Nothing is created or changed and no change is recorded.
- An `edit_bot` that changes `model` or `look` records one `edit` change (`platform_changed`), and
  its Undo restores the previous value.
- `model` is checked against the Bot's own provider. `edit_bot` checks it whenever the provider or
  the model changes: changing only `provider` to one that does not list the Bot's model fails, its
  text contains the `id` of every model the new provider lists, and nothing changes. Changing
  `provider` and `model` together to a pair the provider lists succeeds. An `edit_bot` that keeps
  both, left out or sent unchanged, does not check, so a model no provider lists stays allowed.
- When the Bot's provider is installed but not signed in, the failure says so: its text contains
  `not signed in` (any case). No Bot is created.

A failure is either a JSON-RPC error or a result with `isError: true`; the tests read its text.

| Acceptance | Test |
| --- | --- |
| `edit_bot` changing only `provider` fails, naming the new provider's models | `TestChiefBot_EditProviderChecksModel` (both ways, both providers signed in) |
| `edit_bot` changing `provider` and `model` to a valid pair succeeds | `TestChiefBot_EditProviderChecksModel` |
| `edit_bot` keeping provider and model skips the check | `TestChiefBot_EditKeepsOldModel/{claude,codex}` |
| A signed-out provider says so instead of listing no models | `TestChiefBot_SignedOutProviderSaysSo/{claude,codex}` |

## Repo access names only registered repos

`repo_access` on `create_bot`, `edit_bot`, `POST /api/bots` and `PUT /api/bots/{id}` is `null`
(All), omitted, or an array of repo ids. Every id in the array must be an `id` `GET /api/repos`
lists.

- An array with an id `GET /api/repos` does not list fails. The error text contains that id and
  the `id` of every registered repo. On the tools it is a tool failure as above; on REST it is `400`
  with that text in `error`. No Bot is created or changed, no `platform_changed` is sent and
  `GET /api/threads/{id}/changes` for the Chief thread stays empty.
- With no repos registered, the text contains the unknown id and says no repos are registered: it
  matches `(?i)\bno (registered )?repos?\b` (for example `no repos are registered`).
- Registered ids, an omitted `repo_access` and `null` work as before: `GET /api/bots/{id}` shows the
  array, or `"all"`.

Tests: `TestChiefBot_CreateWithUnknownRepoFails`, `TestChiefBot_EditWithUnknownRepoFails`,
`TestBots_UnknownRepoRejected`, `TestChiefBot_UnknownRepoWithNoReposRegistered`,
`TestChiefBot_RepoAccessAsToday`.

## Fake

A Chief message `Call a platform tool. {"tool":"<name>","args":{...}}` (recordings
`platform-call.jsonl`) makes that one call. The run's reply is the tool's result text, or
`Error: ` and the error text.

## Not pinned

Error wording beyond the names and words it must contain; the look `create_bot` gives a Bot
without `look`; whether the repo error lists only the unknown ids or every id the call sent;
the order of listed repo ids.
