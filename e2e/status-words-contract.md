# Status words test contract

Extends [mac-app-contract.md](mac-app-contract.md) for the status line and sidebar. The real
binary sends tool events from a scripted Claude and Codex sessions. The status line joins the event's verb
and target, and the sidebar uses the Bot's live activity from the API.

A `cd /a/b && make check` command shows `Running make check`. Reading a background task output
file shows `Waiting on a background task`, with no `.output` in either activity. This includes
`cat`, `tail -f`, a shell loop that assigns the task path to a variable, and a `tail` followed
by `pgrep`. A regular `cat build/report.output` keeps `Running cat`. Chief's `list_*` tools
show `Looking around`; writes keep `Setting up` and their target.

Codex `commandExecution` skips leading `cd` and assignments: the command target is the first
program only, so `cd /a/b && make check` shows `Running make`, `cd e2e/ui && node ...`
shows `Running node`, and `cd x; make` shows `Running make`. Plain Codex commands retain
the existing program-only target.

The program of a command is the first real one (see design.md, Helpers): past `cd`, `VAR=value`
assignments, a leading `timeout <n>` and a leading `cat > file`; a function definition or a
`for`/`while`/`until`/`if` header shows the first command inside; a heredoc's text is never a
command. With a scripted shell the event, the Bot's sidebar activity and the thread's `verb` and
`step` (the Helpers line) all read alike. Claude shows `Running gh pr` for
`e() { gh pr view "$1"; }; e a`, `for n in a b; do gh pr view $n; done` and
`timeout 400 gh pr checks 168 --watch`, `Running cat` for `cat > f <<EOF` with a command in the
heredoc, `Running git log` for `x=$(date); git log`, `Running make check` for
`cat > f && make check`, `Running make e2e` for `timeout 540 make e2e`; Codex shows the program
only (`Running gh`, `Running cat`, `Running git`, `Running make`), for its `step` too. `delegate`
and `continue` read `Handing off` with no target, in the event, the sidebar and a helper's line.

## Acceptance mapping

| Acceptance | Tests |
| --- | --- |
| Claude command after `cd` names the program and argument | `TestRun_StatusWords` |
| Codex commands after `cd` and assignments name the first program | `TestRun_StatusWords/codex` |
| Background task output stays out of status and sidebar; ordinary output files remain commands | `TestRun_StatusWords` |
| Read and write activity words | `TestRun_StatusWords` |
| Shell fragments name the first real program (Claude, Codex; event, sidebar, `verb`, `step`) | `TestRun_StatusWordsShell` |
| `delegate` and `continue` read "Handing off" in the event, sidebar and Helpers line | `TestRun_StatusWordsHandoff`, `ui/helpers.spec.ts` "a helper that hands off reads ..." |
