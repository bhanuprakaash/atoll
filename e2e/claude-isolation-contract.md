# Claude owner-setup isolation test contract

Issue #159. Design: `docs/design.md` → Providers → "Every run also sets" (Claude Code also keeps
the owner's own setup out of a Bot's session). Tests build the real server and run the fake
`claude`; no product package is imported.

## Requirements

Every Claude Bot run (any Mode, a new session and a resumed one, a Bot in a checkout or a worktree):

- `--setting-sources project`.
- `--settings` JSON has `autoMemoryEnabled: false`, so claude makes no `memory/` folder under
  `<data dir>/providers/claude/projects/`. The fake makes that folder as real claude does unless
  `--settings` or `CLAUDE_CODE_DISABLE_AUTO_MEMORY=1` turns it off.
- `--settings` JSON has `claudeMdExcludes`: for each folder strictly above the cwd (up to and including
  `/`), taken from the cwd as Reef gave it and from its `EvalSymlinks` form, duplicates dropped, the two
  entries `<dir>/*.md` and `<dir>/.claude/**`. Each of `\ * ? [ ] { } ( )` in `<dir>` is `?`. The cwd
  itself is never listed. The fake logs its cwd as `getcwd` reports it (symlinks resolved), so the
  tests put the folder as Reef knows it back (data dir, worktree root).
- `--disallowedTools` names exactly `Task` and `Workflow`.
- Env `CLAUDE_CODE_SHELL_PREFIX` is the plain absolute path `<data dir>/bin/claude-shell-prefix` (no
  quotes; the data dir of the tests has a space).

The server writes that script at every start, through `.claude-shell-prefix.tmp` in the same folder
renamed over the old file, mode 0500, owned by the user, with the text the design gives. Run by
itself it removes `CLAUDE*` and `ATOLL_MCP_*` from the command's env, keeps other variables and runs
nested quotes and `$()`.

## Fake

`fake.Record` and the `call` the tests read have the fake's `cwd`. The fake `claude` makes
`projects/<cwd>/memory` under its config folder unless auto memory is off.

## Acceptance mapping

| Acceptance | Tests |
| --- | --- |
| `--setting-sources project`, `claudeMdExcludes`, `autoMemoryEnabled: false`, `--disallowedTools` and the shell-prefix env on a new session and on a resume, in Auto, Ask and Full | `TestClaudeIsolation_EveryModeFreshAndResumed` |
| Ancestors of a cwd with a space and glob characters, reached through a symlinked ancestor, listed as given and resolved with `?`; the cwd never listed | `TestClaudeIsolation_RepoWorktreeWithGlobAndSpace` |
| No `memory/` folder under `projects/` after a run | `TestClaudeIsolation_EveryModeFreshAndResumed`, `TestClaudeIsolation_RepoWorktreeWithGlobAndSpace` |
| The script is written at start, rewritten atomically on restart, mode 0500, owned by the user, the design's text; it clears `CLAUDE*` and `ATOLL_MCP_*` and keeps other variables | `TestClaudeIsolation_ShellPrefixScript` |
| Real claude still reaches its MCP servers with `ATOLL_MCP_*` unset in Bash | `TestClaudeIsolation_RealClaudeReachesMCPServers` (build tag `realclaude`: `go test -tags realclaude -run TestClaudeIsolation_RealClaudeReachesMCPServers ./e2e`; needs a signed-in claude; not in `make e2e` or CI) |

## Not covered

- That real claude hides an owner's `~/.claude/CLAUDE.md` with these patterns, and a `/CLAUDE.md`
  (needs root): the design records the spike; the fake cannot load memory files.
- macOS and a zsh user for the shell prefix script.
- A real Reef thread whose Bot reaches an MCP server through atoll with the unset in place: the real test builds its own `claude` command, because atoll's Claude folder needs a login the test cannot copy. The fix PR's Proof runs it once.
