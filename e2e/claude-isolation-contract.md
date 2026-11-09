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
- `--tools Bash,Edit,Glob,Grep,NotebookEdit,Read,Skill,WebFetch,WebSearch,Write` (one argument, in that order) is the whole set of built-in tools, and `--disallowedTools` does not name `Task` or `Workflow`: an allowlist keeps a tool a later Claude Code adds closed. Chief and every title run (one per thread; no count is pinned) get `--tools ""`; a platform Bot that is not Chief gets the allowlist. Reef's MCP tools stay available through `--mcp-config` and `--allowedTools`.
- Env `CLAUDE_CODE_SHELL_PREFIX` is the plain absolute path `<data dir>/bin/claude-shell-prefix` (no
  quotes; the data dir of the tests has a space).

The server writes that script at every start, through `.claude-shell-prefix.tmp` in the same folder
renamed over the old file, mode 0500, owned by the user, with the text the design gives. A temp file a
crashed server left there (mode 0500, half written) does not stop the next start: it is replaced and
no temp is left. While the old script is in place, a restart never lets a reader see it empty or cut
short. A relative `ATOLL_DATA_DIR` still gives an absolute `CLAUDE_CODE_SHELL_PREFIX`. Run by
itself it removes `CLAUDE*` and `ATOLL_MCP_*` from the command's env, keeps other variables and runs
nested quotes and `$()`.

## Fake

`fake.Record` and the `call` the tests read have the fake's `cwd`. The fake `claude` makes
`projects/<cwd>/memory` under its config folder unless auto memory is off.

## Acceptance mapping

| Acceptance | Tests |
| --- | --- |
| `--setting-sources project`, `claudeMdExcludes`, `autoMemoryEnabled: false`, the `--tools` allowlist (no `--disallowedTools` Task or Workflow) and the shell-prefix env on a new session and on a resume, in Auto, Ask and Full | `TestClaudeIsolation_EveryModeFreshAndResumed` |
| Ancestors of a cwd with a space and glob characters, reached through a symlinked ancestor, listed as given and resolved with `?`; the cwd never listed | `TestClaudeIsolation_RepoWorktreeWithGlobAndSpace` |
| No `memory/` folder under `projects/` after a run | `TestClaudeIsolation_EveryModeFreshAndResumed`, `TestClaudeIsolation_RepoWorktreeWithGlobAndSpace` |
| The script is written at start, rewritten atomically on restart, mode 0500, owned by the user, the design's text; it clears `CLAUDE*` and `ATOLL_MCP_*` and keeps other variables | `TestClaudeIsolation_ShellPrefixScript` |
| A stale `.claude-shell-prefix.tmp` (mode 0500) does not stop the start; it is replaced and gone; the temp lives in `bin/` | `TestClaudeIsolation_ShellPrefixSurvivesStaleTemp` |
| A reader of the script during a restart sees only the whole old or whole new text (a read loop; a write in place is caught when a read lands inside it, so this is a probe, not a proof) | `TestClaudeIsolation_ShellPrefixReplacedWholeOnRestart` |
| A relative `ATOLL_DATA_DIR` gives an absolute `CLAUDE_CODE_SHELL_PREFIX` in the Bot's env | `TestClaudeIsolation_ShellPrefixIsAbsoluteForRelativeDataDir` |
| Chief and every title run have `--tools ""` (Chief keeps its atoll MCP config and `--allowedTools mcp__atoll`); a platform Bot that is not Chief and a plain Bot have the allowlist | `TestClaudeIsolation_ToolsByKindOfRun` (Chief's part passes only once the Chief no-shell change, PR #202, is on main) |
| Real claude still reaches its MCP servers with `ATOLL_MCP_*` unset in Bash | `TestClaudeIsolation_RealClaudeReachesMCPServers` (build tag `realclaude`: `go test -tags realclaude -run TestClaudeIsolation_Real ./e2e`; needs a signed-in claude; not in `make e2e` or CI) |
| With the argv and env atoll builds for a Bot in Ask, Auto and Full (captured from the fake) and a stand-in MCP server, real claude's `system/init` tool list is exactly the ten allowlisted built-ins plus the stand-in's tools (not the `--permission-prompt-tool` one, which claude keeps out of the list); a tool a later Claude Code adds fails it only if the allowlist stops closing it | `TestClaudeIsolation_RealClaudeToolList/{ask,auto,full}` (same tag) |

## Not covered

- That real claude hides an owner's `~/.claude/CLAUDE.md` with these patterns, and a `/CLAUDE.md`
  (needs root): the design records the spike; the fake cannot load memory files.
- macOS and a zsh user for the shell prefix script.
- Real claude 2.1.319 lists `Glob` and `Grep` in its init event (the design says it does not); the test expects them.
- A real Reef thread whose Bot reaches an MCP server through atoll with the unset in place: the real test builds its own `claude` command, because atoll's Claude folder needs a login the test cannot copy. The fix PR's Proof runs it once.
