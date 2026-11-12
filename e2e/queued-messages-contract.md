# Queued messages: test contract

Tests: `queued_messages_test.go`, `ui/queued-messages.spec.ts`. They build the real server; no
product package is imported. Design: [`docs/design.md`](../docs/design.md), Threads and runs
(Queued messages), Delegation (`continue`) and App and UI (composer).

## API

| Call                                         | Reply                                                                                                                  |
| -------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------- |
| `POST /api/threads/{id}/messages`, run going | `202 {message: {id, role: "user", text, queued: true}, run: null}`; INFO `queued` with the thread id in the server log |
| `GET /api/threads/{id}`                      | `queued: [{id, text}]`, oldest first; the messages list does not hold them                                             |
| `PATCH /api/threads/{id}/messages/{mid}`     | `{text}` → `200` message; `400` empty; `404` unknown; `409` once delivered                                             |
| `DELETE /api/threads/{id}/messages/{mid}`    | `204`; `404` unknown; `409` once delivered                                                                             |
| `POST /api/threads/{id}/messages/{mid}/send` | stops the going run, starts the one run of the queued messages: `202 {run}`; `409` once delivered                      |

When the run ends (not stopped by the person) all queued messages start one run: one provider
prompt, the texts in order joined by a blank line. Each keeps its id and is then a normal message
of that run. A plain Stop leaves them queued. A helper thread answers `409` to `PATCH` and `DELETE`.

A boss's `continue` to a helper whose run is going answers `atoll/continue: Queued...` (the tool
result starts `Queued`), the message is in the helper thread's `queued` list until its run ends.

## UI

While a run works the composer shows Send beside Stop. A sent message is the user's `article`
named "You" with the text "Queued"; inside it the buttons Edit, Remove and Stop and send. Edit shows
a textbox named "Edit message" with Save and Cancel. No text like `thread has a run going` shows.

## Fakes

`queued-slow.jsonl` for claude and codex: prompt `Take your time, then reply with just: ok`, the
`run-fact` recording with a 6 s pause, so a run is going while the test sends. The follow-up
`Which codeword did I give you? Reply with just the word.` resumes its session. A run that waits
for the person's Stop plays `_hangPrompt`.

## Acceptance mapping

| Acceptance                                                          | Tests                                                                             |
| ------------------------------------------------------------------- | --------------------------------------------------------------------------------- |
| Send during a run is stored and queued, delivered when the run ends | `TestQueue_UserMessageWhileRunGoes/{claude,codex}`; UI: Queued bubble, no refusal |
| Several queued messages arrive in order as one run                  | `TestQueue_SeveralArriveInOrderAsOneRun/{claude,codex}`                           |
| A queued message can be edited or removed until it is delivered     | `TestQueue_EditAndRemove/{claude,codex}`; UI: edited and removed                  |
| Stop and send stops the run and sends now                           | `TestQueue_StopAndSend/{claude,codex}`; UI: Stop and send                         |
| A boss's `continue` to a busy helper is queued, not refused         | `TestQueue_BossContinueToBusyHelper/{claude,codex}`                               |
| No refusal text under the composer for a busy thread                | UI: Queued bubble                                                                 |
| Every queued send logs one INFO line with the thread id             | `TestQueue_UserMessageWhileRunGoes`                                               |

Not covered here: a send while an approval is open (#146), Bots' messages merging into one run
(#190), Send now after a Stop in the UI, a restart with queued messages.
