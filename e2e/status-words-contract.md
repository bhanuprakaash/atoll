# Status words test contract

Extends [mac-app-contract.md](mac-app-contract.md) for the status line and sidebar. The real
binary sends tool events from a scripted Claude session. The status line joins the event's verb
and target, and the sidebar uses the Bot's live activity from the API.

A `cd /a/b && make check` command shows `Running make check`. Reading a background task output
file shows `Waiting on a background task`, with no `.output` in either activity. This includes
`cat`, `tail -f`, a shell loop that assigns the task path to a variable, and a `tail` followed
by `pgrep`. A regular `cat build/report.output` keeps `Running cat`. Chief's `list_*` tools
show `Looking around`; writes keep `Setting up` and their target. If `finish_run` emits a tool
call, its activity does not start with `Setting up`.

## Acceptance mapping

| Acceptance | Tests |
| --- | --- |
| Command after `cd` names the program and argument | `TestRun_StatusWords` |
| Background task output stays out of status and sidebar; ordinary output files remain commands | `TestRun_StatusWords` |
| Read, write and finish activity words | `TestRun_StatusWords` |
