# Status words test contract

Extends [mac-app-contract.md](mac-app-contract.md) for the status line and sidebar. The real
binary sends tool events from a scripted Claude and Codex sessions. The status line joins the event's verb
and target, and the sidebar uses the Bot's live activity from the API.

A `cd /a/b && make check` command shows `Running make check`. Reading a background task output
file shows `Waiting on a background task`, with no `.output` in either activity. This includes
`cat`, `tail -f`, a shell loop that assigns the task path to a variable, and a `tail` followed
by `pgrep`. A regular `cat build/report.output` keeps `Running cat`. Chief's `list_*` tools
show `Looking around`; writes keep `Setting up` and their target. If `finish_run` emits a tool
call, its activity does not start with `Setting up`.

Codex `commandExecution` skips leading `cd` and assignments: the command target is the first
program only, so `cd /a/b && make check` shows `Running make`, `cd e2e/ui && node ...`
shows `Running node`, and `cd x; make` shows `Running make`. Plain Codex commands retain
the existing program-only target.

## Acceptance mapping

| Acceptance | Tests |
| --- | --- |
| Claude command after `cd` names the program and argument | `TestRun_StatusWords` |
| Codex commands after `cd` and assignments name the first program | `TestRun_StatusWords/codex` |
| Background task output stays out of status and sidebar; ordinary output files remain commands | `TestRun_StatusWords` |
| Read, write and finish activity words | `TestRun_StatusWords` |
