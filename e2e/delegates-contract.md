# Delegate list: test contract

Extends the Bot and Chief contracts ([README.md](README.md),
[chief-bot-contract.md](chief-bot-contract.md)). Tests build the real server; no product package is
imported.

## API

Every Bot has `delegates`: the ids of the Bots it may hand work to. `GET /api/bots` and
`GET /api/bots/{id}` always return it as an array, `[]` by default (never null or absent).
`POST /api/bots` takes it; `PUT /api/bots/{id}` takes it and, when the body leaves it out, keeps
the list. Order is not tested.

A list that names the Bot itself, or an id no Bot has, is refused with `400` and
`{error}` naming the valid Bots by name (as the repo-access `400` names the registered repos). A
refused `PUT` changes nothing; a refused `POST` creates no Bot.

Deleting a Bot removes its id from every other Bot's `delegates`.

## Chief

`create_bot` and `edit_bot` take `delegates` (Bot ids), declared in their input schema; `list_bots`
returns each Bot's `delegates`. An `edit_bot` that changes the list emits `platform_changed` whose
`before.delegates` and `after.delegates` are the old and new lists, and the change is listed under
the thread's changes. Undo writes the old list back.

## UI

Bot settings has a "Can hand work to" section in the Add-row pattern used for skills and repos: an
"Add Bot" button opens an "Add Bot" dialog listing the other Bots as menu items (never the Bot
itself); a picked Bot shows as a row with "Remove <name>". Under it is the line
"<Bot name> can start up to 3 helper threads at once with these Bots". A pick saves at once.

## Acceptance mapping

| Acceptance                                                         | Tests                                                        |
| ------------------------------------------------------------------ | ------------------------------------------------------------ |
| `delegates` round-trips, `[]` by default; self or unknown id → 400 | `TestDelegates_API`                                          |
| Bot settings "Can hand work to" picker and the 3-helpers line      | UI `Bot settings pick the other Bots a Bot can hand work to` |
| Chief `create_bot`/`edit_bot` take it, `list_bots` shows it, Undo  | `TestDelegates_Chief/{claude,codex}`                         |
| Deleting a Bot drops it from every delegate list                   | `TestDelegates_DeleteRemovesFromLists`                       |

Not covered here: whether Chief may be listed as a delegate, and the wording of the change line in
the thread beyond its Undo.
