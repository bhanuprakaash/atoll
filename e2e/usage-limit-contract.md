# Usage limit test contract

Extends [sign-in-expired-contract.md](sign-in-expired-contract.md) and
[wake-contract.md](wake-contract.md) for issue #165; the design is in `docs/design.md` (App and UI,
Delegation). Go tests drive the real binary, Playwright the real UI, both with the scripted fake
providers; no product package is imported. JSON is snake_case and replies may include fields not
listed here.

## Fakes and harness

A provider's usage limit is reached when its credentials file (`.credentials.json` for Claude,
`auth.json` for Codex, in Reef's folder for the provider) holds
`{"fake":"limited","resets_at":<epoch seconds>}`; `machine.limit(t, provider, at)` and
`Machine.limit(provider, at)` in `e2e/ui/atoll.ts` write it. Until that time every user turn
fails the way the real CLI does; after it the file is ignored and turns play as any other. The login
stays valid: `auth status` / `login status` say signed in throughout.

- Claude (`-p` stream-json): a `system`/`init` line, a `rate_limit_event` (`rate_limit_info`:
  `status: "rejected"`, `resetsAt`, `rateLimitType: "five_hour"`, `isUsingOverage: false`), a
  synthetic `assistant` message with `error: "rate_limit"`, `api_error: "usage_limit_reached"`,
  `api_error_status: 429`, the same `rate_limit_info` in `api_error_params`, and text
  `You've hit your 5-hour limit · resets 3pm (UTC)`, then a `result` with `is_error: true`,
  `api_error_status: 429` and that text, then exit 1.
- Codex (`app-server`): `turn/start` replies with an `inProgress` turn, then `turn/started`, an
  `account/rateLimits/updated` notification (`primary`: `usedPercent: 100`, `resetsAt`;
  `secondary`: `usedPercent: 41`, `resetsAt` six days later; `rateLimitReachedType:
  "rate_limit_reached"`), an `error` notification and `turn/completed` with `status: "failed"`, both
  with error `{message: "You've hit your usage limit. Upgrade to Plus to continue using Codex
  (https://chatgpt.com/explore/plus), or try again later.", codexErrorInfo: "usageLimitExceeded"}`.
  The message holds no time: the reset time is `primary.resetsAt`, the window at 100%.

No real limit was reproduced. The shapes are read from claude 2.1.322's own schema strings
(`rate_limit_event`, `api_error: usage_limit_reached`, `api_error_params.rate_limit_info`) and
codex-cli 0.167's `codex app-server generate-json-schema` (`CodexErrorInfo.usageLimitExceeded`,
`AccountRateLimitsUpdatedNotification`, `RateLimitWindow.resetsAt`). The message texts are
the ones those CLIs print.

## API

`GET /api/runs/{id}` of a run a usage limit stopped has `status: "failed"`,
`error_kind: "rate_limited"`, `provider` and `resets_at` (RFC 3339, the provider's reset time to the
second). `GET /api/providers` still shows the provider signed in.

The wake message from a helper whose run a usage limit stopped is in `GET
/api/threads/{boss}/messages` with `from_bot_id` the helper Bot's id, and holds the helper thread
id, `Tester is out until <resets_at in UTC, 2026-11-11T15:42:00Z>.` and `Do not send it work until
then.` It holds none of the provider's text.

## UI contract

- The run's line in the Messages log is the text `<Bot> hit its <Claude|Codex> limit; back at
  <time>`, for the run's provider. `<time>` is the clock in the viewer's zone (`3:42 PM`) on the
  viewer's today, `Tomorrow 3:42 PM` on the next day, and the weekday and clock (`Sat 3:42 PM`) later
  in the week. The spec runs in `Asia/Kolkata` with the page's clock set to the reset, a day before
  it and three days before it. There is no `button` "Retry", no "Run failed", no
  element drawn in the failed line's danger colour, and none of the provider's text ever appears on
  the page. The line comes from the run, so it is there again when the thread is reopened.

## Acceptance mapping

| Acceptance | Tests |
| --- | --- |
| A limit-stopped run is stored `rate_limited` with the reset time (both providers) | `TestUsageLimit_StoredWithResetTime/{claude,codex}`, `usage-limit.spec.ts` (Claude and Codex) |
| The thread shows one calm line, no red, no Retry | `usage-limit.spec.ts` (Claude and Codex) |
| The boss's wake message says the helper is out until the time, without the provider's text | `TestUsageLimit_WakeSaysHelperIsOut/{claude,codex}` |
| After the reset time the next message starts a run normally | `TestUsageLimit_NextMessageAfterResetRuns/{claude,codex}` |

Not covered here: a limit with no reset time (`try again later`), the sidebar dot and the header
island for such a run, and a message sent before the reset time.
