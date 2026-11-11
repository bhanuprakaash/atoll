# finish_run report: test contract

Tests: `finish_report_test.go`. They build the real server; no product package is imported. The
fake provider calls `finish_run` over MCP with the arguments in the prompt
(`testdata/*/finish-report.jsonl`), so the server's own answer and the run's stored report are what
is checked. The thread's Blocked line is drawn from the stored report's `blocked`, so an empty
`blocked` in `GET /api/runs/{id}` is what shows no line.

| Call                                                                                                         | Answer                                                                                  | Stored report (`GET /api/runs/{id}`)              |
| ------------------------------------------------------------------------------------------------------------ | --------------------------------------------------------------------------------------- | ------------------------------------------------- |
| `done` absent, empty or only whitespace                                                                      | a tool error (not "Report saved.") that names `done`; the Bot can fix it and call again | `report` is `null`: the run does not finish on it |
| `blocked` is "Nothing blocking", "None", "nothing" or "n/a" in any case, with spaces or trailing punctuation | `Report saved.`                                                                         | the report as given, with `blocked` empty         |
| `blocked` is any other text, including text that only starts with such a word                                | `Report saved.`                                                                         | the report as given, `blocked` unchanged          |
