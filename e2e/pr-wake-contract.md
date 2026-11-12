# Waking a Bot for its tracked PR: test contract

Extends [github-repos-contract.md](github-repos-contract.md) for issue #163; the behaviour is the
"Repos and worktrees" section of `docs/design.md` (the watcher, events, wake). It replaces that
contract's line "PR state is not polled": the server now reads a tracked GitHub PR, and wakes the
thread's Bot when it merges, closes or its CI finishes. Tests drive the real binary with the fake
GitHub, the scripted fake providers and real git; no product package is imported and nothing uses the
network. `ATOLL_REPO_POLL_INTERVAL=100ms` shortens the poll interval, as in the MR tests.

## What the fake GitHub adds

Routes with the shapes of the design's Fixtures list, all behind the connection's token (401
otherwise): `GET /repos/{owner}/{repo}/pulls/{n}` (`state`, `merged`, `title`, `head.sha`, an `ETag`,
`304` for a matching `If-None-Match`), `GET .../commits/{sha}/check-runs` and
`GET .../commits/{sha}/status`. A pull request opened with `POST .../pulls` is open with one head
commit and no checks until a test merges or closes it or sets its checks. The scripted Bot (scenario
`repo-github-fix`) opens and tracks the PR; a wake is played by `testdata/*/pr-wake-{merged,closed,ci}.jsonl`,
which answer a prompt holding `merged`, `closed` or `CI finished` with `Merge noted.`, `Close noted.`
and `CI noted.`.

## What a test sees

- `GET /api/threads/{id}/messages`: a wake is a message with `kind` `merged`, `closed` or `ci`, text
  holding the sentence, and an empty `from_bot_id`. The user's own message has an empty `kind`.
- The provider's `Reef: <sentence>` prompt, from the fake's prompt log, and its count of sessions.
- `GET /api/threads/{id}`: `run_id` is the wake's run, `mr_state` the state read.

## Acceptance mapping

| Acceptance (#163) | Tests |
| --- | --- |
| Merging a tracked PR wakes the thread's Bot within a couple of minutes with "PR #N merged", no user message | `TestPRWake_MergedWakesTheBot/{claude,codex}` |
| Closing it unmerged wakes it with "PR #N closed" | `TestPRWake_ClosedUnmergedWakesTheBot/{claude,codex}` |
| CI finishing (all checks done) wakes it once with pass/fail | `TestPRWake_CIFinishedWakesOnce/{claude,codex}` (a pending check wakes nothing; the sentence is `CI finished on PR #1: 2 passed, 2 failed ("unit", "deploy/preview")` up to the order of the names) |
| Nothing wakes a PR whose thread is closed | `TestPRWake_ClosedThreadIsNotWoken/{claude,codex}` (an open thread's PR merged at the same moment is the control) |
| No duplicate wakes for one event | `TestPRWake_OneWakePerEvent/{claude,codex}` (the same state read again, a title change, a server restart; two provider sessions in all) |

Not covered: the "server log shows one wake per event" half of the last line (the design names no log
line, so the tests count wake messages, Reef prompts and provider sessions instead), the settle read,
GitLab, `needs_sign_in`, back-off, the held-row rules, the thread view's quiet line, and the real
github.com line.
