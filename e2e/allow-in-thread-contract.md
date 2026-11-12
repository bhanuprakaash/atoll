# Allow in this thread: test contract

Extends the thread and approval contracts ([README.md](README.md),
[approvals-contract.md](approvals-contract.md)). Design: "Allow in this thread" in
`docs/design.md`. Tests build the real server; no product package is imported.

## The command-use scenario

The prompt `Run commands. ["<command>", ...]` makes the fake provider run each command as its shell
tool (Claude `Bash`, Codex `commandExecution`), asking for approval first and executing nothing.
Codex's approval request carries `command` as `/bin/bash -lc "<command>"`, `availableDecisions` and
a `proposedExecpolicyAmendment`, as a real request does; Claude's carries `command` as is. The reply
is one line per command, `<command>: ran` or `<command>: denied`, the command trimmed. The fake
asks every time it is asked: whether the user is asked is Reef's decision.

## What the API shows

- The `approval` event and the `approval` of `GET /api/threads/{id}` carry `allow_in_thread: true`
  when the choice is offered, and `false` or nothing otherwise.
- `POST /api/approvals/{id}` takes `{approve: true, in_thread: true}`. With the choice not offered,
  or with `approve: false`, it answers `400` and the approval stays open.
- The thread's changes (`GET /api/threads/{id}/changes`) hold one entry for the choice:
  `entity: "allow"`, `action: "create"`, `after: {command}` with the command text as the card
  shows it (Codex without its `bash -lc` wrapper).

## Acceptance mapping

| Acceptance                                                      | Tests                                                                                                                                                                                       |
| --------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Same command passes without a card, a different one asks (both) | `TestAllowInThread_SameCommandPasses/{claude,codex}`, `TestAllowInThread_SameMeansSameText/{claude,codex}`; UI `Allow in this thread lets the same command through, and the thread says so` |
| Rest of the thread, not other threads                           | `TestAllowInThread_SameCommandPasses` (a second run; another thread of the Bot)                                                                                                             |
| Not offered for push, delete, `-rf` outside roots, `gh` writes  | `TestAllowInThread_NotOffered/{claude,codex}`; UI `a command that must stay a plain choice shows Allow and Deny alone`                                                                      |
| Allow and Deny keep equal weight there                          | UI `a command that must stay a plain choice shows Allow and Deny alone` (both visible, same height)                                                                                         |
| One quiet line "Allowed `<cmd>` for this thread" in the changes | `TestAllowInThread_SameCommandPasses` (the change record); UI (the line, once, after a reload)                                                                                              |

`TestAllowInThread_PlainAnswersAskAgain` and the plain-choice UI test pass before the feature: they
prove the scenario and keep Allow and Deny answering once.

Not covered here: `-rf` inside the thread's worktree (a repo thread's `start_work` path) is offered,
which needs a worktree in the scenario; the implementer proves it with a unit table case. The
quietness of the line (grey, one row) is the owner's eye.
