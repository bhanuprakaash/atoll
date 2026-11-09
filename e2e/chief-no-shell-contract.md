# Chief has no shell and no file tools: test contract

Extends [chief-read-contract.md](chief-read-contract.md) and
[skill-files-contract.md](skill-files-contract.md). Tests: `e2e/chief_no_shell_test.go`. The design
is in `docs/design.md` (Bots, Chief; Prompt).

## What the product provides

- A Claude run of Chief has `--tools` with an empty value in its argv in every Mode (Ask, Auto,
  Full access), and keeps `--mcp-config` (with the `atoll` server) and `--allowedTools mcp__atoll`.
  No other Bot's argv has `--tools`: not a Bot with the platform switch, a Bot without it or a repo Bot.
- A Codex Chief gets no such setting yet (a documented gap). It still runs, so a user who has only
  Codex can use Chief. The tests assert nothing else about it.
- A platform tool `import_agent({path})`, offered only to a Bot with the platform switch, records no
  change. `path` is absolute or starts with `~/` (resolved against `HOME`). It answers
  `{name, path, content}`: `name` is the file name without `.md`, `path` the resolved path. Symlinks
  and `..` in the path and in `~/.claude/agents` are resolved first (a symlinked `~/.claude/agents` works);
  the resolved file must be a regular `.md` file directly inside the resolved `~/.claude/agents`.
  Anything else fails with JSON-RPC invalid params (-32602): outside the folder (names the allowed
  folder), a nested file, a symlink to a file outside, not `.md` (says only `.md` files are read), a
  directory or FIFO, over 256 KiB, not UTF-8, missing (`not found`).
- `import_skill` takes only a folder that, once resolved, is a direct child of `~/.claude/skills` or
  `~/.codex/skills`. Any other folder (a registered repo, `/tmp`, `.agents/skills`, a deeper folder,
  the skills folder itself, `..` out of it) fails with invalid params (-32602), naming the allowed
  folders. A folder under either root imports as before.
- Chief's `Import a local skill` section names `import_agent` and no longer tells Chief to read the
  agent file itself. The platform block stays at most 2000 bytes (`TestPrompt_ChiefPlatformBlock`).

## Fake

`platform-call` replies, after the error text of a JSON-RPC error, with a line
`(JSON-RPC error <code>)`; a tool result with `isError` has no such line. `platform-denied` also fails
when a non-platform Bot lists `import_agent` or its guessed call works.

## Acceptance mapping

| Acceptance | Tests |
| --- | --- |
| Claude Chief runs with `--tools ""` plus its MCP config in every Mode; no other Bot gets `--tools` | `TestChiefNoShell_ClaudeRunsWithoutTools` |
| A Codex Chief still runs | `TestChiefNoShell_CodexChiefStillRuns` |
| `import_agent` reads a `.md` file in `~/.claude/agents`; refuses the rest with invalid params | `TestChiefNoShell_ImportAgent` |
| A symlinked `~/.claude/agents` works | `TestChiefNoShell_ImportAgentThroughSymlinkedFolder` |
| `import_agent` is for a platform Bot only | `TestChiefNoShell_ImportAgentOnlyForPlatformBots` |
| `import_skill` refuses folders outside `~/.claude/skills` and `~/.codex/skills`; the two still import | `TestChiefNoShell_ImportSkillOnlyFromSkillFolders` |
| The prompt names `import_agent`, not a file read | `TestChiefNoShell_PromptNamesImportAgent/{claude,codex}` |
| Chief registers a repo, imports an agent and a skill, creates a Bot | `TestChiefNoShell_ChiefStillDoesItsJob` |
| Chief makes no tool call that reads a repo file | real Claude only: no existing e2e test runs a real provider, so none here |
