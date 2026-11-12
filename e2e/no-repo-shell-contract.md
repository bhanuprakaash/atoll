# A Bot sees only the repos it has: test contract

Issue #169. Design: `docs/design.md` → Bots, Repo access on Claude (the docs PR for #169 merges
first). Tests: `e2e/no_repo_shell_test.go`. They build the real server and run the fake `claude`.

## What the product provides

A Claude run of a Bot that lacks at least one registered repo (repo access is a list that leaves a
registered repo out, or is empty) has, in its `--settings` JSON, in every Mode (Auto, Ask, Full access):

- `sandbox.enabled: true`, `failIfUnavailable: true` (no sandbox, no run), `allowUnsandboxedCommands:
  false`, `autoAllowBashIfSandboxed: false` (Ask still asks for a command), `network.allowedDomains:
  ["*"]` and `filesystem.allowWrite: ["/"]` (the sandbox only hides reads; network and writes are as
  open as before).
- `sandbox.filesystem.denyRead` and `permissions.deny` rules `Read(//<path>/**)` and
  `Edit(//<path>/**)` for each hidden path, as Reef knows it and with symlinks resolved when that
  differs: the path of every registered repo the Bot lacks, `<worktree root>/<repo name>` of each,
  and `<data dir>/threads/<id>` of every thread on it (the read-only checkouts). A repo the Bot has,
  its worktree folder and the folders of threads on it are not hidden. The Bot's own folder is never
  hidden: a deny rule has no way to leave a folder out of a hidden parent, so `<data dir>/threads`
  as a whole is not hidden.

A Bot that lacks no registered repo (All, or a list holding every repo), Chief and the title runs (no
built-in tools: `--tools ""`) get neither the sandbox nor the rules.

## Fake

Scenario `fs-read` (prompt `Read files. ` plus a JSON array of `{tool, path, via}`; tools Bash, Read,
Glob, Grep, Write; a Bash call reads with `head` or `python`) plays the calls as Claude Code 2.1.322
treated them in the #169 spike, from the run's own `--settings`. `Read(//dir/**)` refuses Read, Glob
and Grep, `Edit(//dir/**)` refuses Write (path as given and resolved); a Read rule also refuses Bash
`head` and `cat` of that path but not `python`; a path under `sandbox.filesystem.denyRead` has no file
for any Bash command, unless a longer `allowRead` entry holds it. The reply has a line per call:
`<tool> <path>: <first line | refused: <why>>`. The fake performs the reads and the write for real,
so a test sees what a call got.

## Acceptance mapping

| Acceptance | Tests |
| --- | --- |
| 1. A Claude Bot with no repo access, asked to read a registered repo's README by absolute path (Bash `head`, Bash `python`, Read, Glob, Grep, Write; the repo's read-only checkout in another thread's folder), is refused each time and the file is not written; nothing of the file reaches the reply, in Auto, Ask and Full access | `TestNoRepoShell_BotWithoutAccessCannotReadTheRepo` |
| 1. The run's settings hide each repo the Bot lacks (the repo, its worktree folder, the folders of its threads), with the sandbox settings the design gives, in every Mode | `TestNoRepoShell_ClaudeHidesReposTheBotLacks` |
| 1, generalised: a Bot with access to alpha only cannot read beta; it still reads alpha and its own checkout, which is in `<data dir>/threads` | `TestNoRepoShell_BotWithAccessToAnotherRepoCannotReadThisOne` |
| 2. The same Bot answers a question, uses its skill and calls its connection; its `--mcp-config` and `--allowedTools` stay | `TestNoRepoShell_StillAnswersAndUsesConnectionsAndSkills` |
| A Bot that lacks no repo, Chief and title runs are unchanged | `TestNoRepoShell_UnchangedWhenNothingIsLacking` |
| 1, with real claude (tag `realclaude`; not in `make e2e`) | `TestNoRepoShell_RealClaudeCannotReadRegisteredRepo/{auto,full}` |
| 3. `docs/design.md` says what reach a Bot without repo access has, per provider | the docs PR; not a test |

## Not covered (the fake cannot show it)

- That real Claude Code enforces the settings, in `make e2e` and CI. The fake plays what the #169
  spike showed (Claude Code 2.1.322, Linux, bubblewrap). `TestNoRepoShell_RealClaudeCannotReadRegisteredRepo`
  (build tag `realclaude`: `go test -tags realclaude -run TestNoRepoShell_Real ./e2e`, needs a signed-in
  claude) runs real claude with the argv Reef builds for a Bot with no repo access, in Auto and Full
  access: Bash `head`, Bash python, Read and a checkout in a thread folder must return nothing of the
  README, and `cat` of a file in the Bot's own folder must work. Ask is left out: the stand-in MCP
  server answers no approval. The fix PR's Proof runs it.
- macOS (seatbelt): not spiked.
- A tool call event for the attempt. Claude announces a call before it checks permission, and Reef's
  events hold no result, so a refused read still shows as a `tool_call`. "No tool call read the file"
  is proven by what each call got.
- Codex: the design states it reads other repos and does not change it, so nothing is tested.
