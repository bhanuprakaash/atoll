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
- `look` is one of `jellyfish`, `whale`, `penguin`, `shark`, `octopus`, `turtle`, `seahorse`,
  `otter`, `clownfish`, `manta`. Any other value fails on both tools, and its text contains all ten.
  Nothing is created or changed and no change is recorded.
- An `edit_bot` that changes `model` or `look` records one `edit` change (`platform_changed`), and
  its Undo restores the previous value.

A failure is either a JSON-RPC error or a result with `isError: true`; the tests read its text.

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

Error wording beyond the names it must contain; whether `create_bot` checks `model` against the
Bot's own provider or every ready provider (tests have one ready provider); whether `edit_bot`
checks `model`; the look `create_bot` gives a Bot without `look`; whether the repo error lists only
the unknown ids or every id the call sent; the order of listed repo ids.
