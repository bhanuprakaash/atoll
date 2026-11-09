# Home paths in imported skills: test contract

Extends [skill-files-contract.md](skill-files-contract.md) and
[skill-editor-contract.md](skill-editor-contract.md). Tests: `e2e/skill_home_paths_test.go` (Go).
Design: Connections and skills, "Home paths in imported skills", in `docs/design.md`.

## Requirements

- `import_skill` and Re-import go over `SKILL.md` and every stored text file. A home reference is
  `~/` (not preceded by a letter, digit, `.`, `_`, `-`, `/` or `~`), `$HOME` or `${HOME}` (with or
  without a following `/`), or the server's own home as an absolute path.
- A home reference to the skill's own folder (`~/`, `$HOME/`, `${HOME}/` plus `.claude/skills/<folder>`
  of the root it was imported from, or the folder's absolute path; the name ends at `/` or at a
  character that is not a letter, digit, `.`, `_` or `-`) is stored as `{{ATOLL_SKILL_DIR}}`.
- Any other home reference left refuses the import: HTTP 200 (REST) with
  `{status: "refused", name, reason, refs: [{file, line, ref}]}`. `file` is relative to the skill
  folder, `line` is 1-based, `ref` runs to the next space or quote; refs are ordered by file, then
  line. Nothing is stored. The reason reads `skill "<name>" refers to files in your home folder
  that a Bot cannot use: <file>:<line> <ref>, ...`. Fenced examples count.
- The folder rule (the skill folder resolves to a direct child of `~/.claude/skills` or
  `~/.codex/skills`) runs first: outside it, the answer is the folder error (REST `400`), not
  `refused`. The refuse check runs before `unchanged` and `confirm`.
- Re-import of a stored skill whose source is now refused keeps the stored skill and its files as
  they were.
- At placement, `{{ATOLL_SKILL_DIR}}` in every placed text file becomes the absolute path of the
  run's copy: `<run folder>/.claude/skills/<name>` for Claude, `<run folder>/<name>` for Codex. The
  stored text keeps the token, and a literal token in a source skill is replaced like any other.
- A skill row (`GET /api/skills`) carries no `warnings` field.
- Chief's prompt, in "Import a local skill", says what `import_skill` refused (not flagged).

## Fixtures and fakes

- The own-folder skill is `~/.claude/skills/poteto-mode/` under a HOME with a space in its name
  (resolved with `EvalSymlinks`): SKILL.md with the five forms, `scripts/check`, `scripts/run.sh`,
  `scripts/helper.sh`, `notes.md` (one `${HOME}` form and a literal token).
- Both fakes' `platform-skill-import` scenario takes the rest of the line as the path, so a HOME
  with a space works, and on `refused` replies `Import refused: <reason>` then the result as one
  line of JSON. The tool's reason is what names the skill, file, line and ref.

## Acceptance mapping

| Requirement | Tests |
| --- | --- |
| Own-folder forms become `{{ATOLL_SKILL_DIR}}` in SKILL.md and every text file; no `~/.claude`, `$HOME`, `${HOME}` or absolute home in the stored skill; source untouched; no `warnings` field | `TestSkillHome_OwnFolderBecomesToken/{claude,codex}` |
| The token is replaced by the run's copy path at placement; the stored text keeps it; a literal token is replaced | `TestSkillHome_TokenIsReplacedAtPlacement/{claude,codex}` |
| Other home references refuse with exact refs (another skill, `~/.ssh`, `$HOME`, `${HOME}`, `$HOME/x`, absolute home path, a fenced example, `<folder>-v2`), ordered by file then line; nothing stored; Chief's reply names skill, file, line, ref | `TestSkillHome_RefusesOtherHomeRefs/{claude,codex}/*` |
| `https://x/~/a`, `a~/b` and the other preceded forms are not references | `TestSkillHome_NotHomeReferences` |
| Re-import refused (HTTP 200, before `confirm`, with and without `replace`) keeps the stored skill; same answer as `import_skill` | `TestSkillHome_ReimportRefusedKeepsStored/{claude,codex}` |
| The folder rule wins when both fire | `TestSkillHome_FolderRuleComesFirst` |
| Chief's prompt says refused | `TestPrompt_ChiefImportRefusal/{claude,codex}` |

Not testable with a fake: Chief saying in its own words how to fix a refusal (edit the skill or
copy the file into the skill folder and point at it as `~/.claude/skills/<folder>/<file>`). The
real-provider check is the ticket's proof.
