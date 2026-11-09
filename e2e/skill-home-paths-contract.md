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
  line (refs on one line keep their column order; the design does not say so yet). Nothing is
  stored. The reason reads `skill "<name>" refers to files in your home folder
  that a Bot cannot use: <file>:<line> <ref>, ...`. Fenced examples count.
- The own folder's root is the one the skill was imported from: a skill imported from
  `~/.codex/skills/<folder>` rewrites `~/.codex/skills/<folder>` and refuses
  `~/.claude/skills/<folder>`, and the other way round.
- A skill folder may be a symlink to a sibling direct child of the skills folder (`~/.claude/skills/x`
  to `~/.claude/skills/actual`; the folder rule accepts it). Its own folder is the entry the owner
  imported from: `~/`, `$HOME/`, `${HOME}/` and absolute forms of the entry path rewrite, and so do
  refs through the resolved path (`actual`). Another sibling (`other`) is still refused. The skill
  is recorded under the entry (`source` is the entry path, not its target).
- Placement quoting: the token is replaced by the copy's path as is when every character is in
  `[A-Za-z0-9_./-]`; otherwise by the path single-quoted for the shell, a `'` written `'\''`
  (macOS's `Application Support` has a space), so `{{ATOLL_SKILL_DIR}}/scripts/run.sh` becomes
  `'/a b/x'/scripts/run.sh`. The command lines a skill writes with the token run through `sh -c`
  both in SKILL.md and inside a script (`. {{ATOLL_SKILL_DIR}}/lib.sh`).
- Scanner (refuse, never rewrite): `${HOME` followed by a character that is not a name character,
  `}` or `/` (`${HOME:-x}`, `${HOME%/}`, `${HOME#..}`, `${HOME^}`) is a home reference;
  `${HOMEWORK}` and `${HOME_X}` are not. A bare `~` that starts a word (not preceded by a letter,
  digit, `.`, `_`, `-`, `/` or `~`) followed by the end of the line, whitespace, `/`, `+`, `-`, `:`,
  `)`, `"`, `'`, a letter or `_` is one (`cd ~`, `~root`, `~+`, `~-`, `$(echo ~)`); `~5`,
  `~3 minutes` and `a~b` are not. Known gaps of a static scan, pinned neither way: `~` and `/` split
  by a line break, `HO""ME`.
- Absolute home paths have boundaries: `<home>x/foo` (the home path followed by a path character)
  is not a reference, `/mnt<home>/foo` (preceded by one) is neither rewritten nor refused;
  `<home>` followed by `/`, the end, whitespace or a quote is a reference.
- The refused answer has exactly the keys `status`, `name`, `reason`, `refs`.
- A successful answer carries `rewritten` (file and count). The tests pin only that it is present
  and names each rewritten file; the exact shape is not in the design.
- The folder rule (the skill folder resolves to a direct child of `~/.claude/skills` or
  `~/.codex/skills`) runs first: outside it, the answer is the folder error (REST `400`), not
  `refused`. The refuse check runs before `unchanged` and `confirm`. The folder rule is a separate
  design (Chief without a shell): `TestSkillHome_FolderRuleComesFirst` passes only once that change
  is merged, so merge it before the fix PR for this ticket.
- Re-import of a stored skill whose source is now refused keeps the stored skill and its files as
  they were.
- At placement, `{{ATOLL_SKILL_DIR}}` in every placed text file becomes the absolute path of the
  run's copy: `<run folder>/.claude/skills/<name>` for Claude, `<run folder>/<name>` for Codex. The
  stored text keeps the token, and a literal token in a source skill is replaced like any other.
- A skill row (`GET /api/skills`) carries no `warnings` field.
- Chief's prompt, in "Import a local skill", says what `import_skill` refused (not flagged).

## Fixtures and fakes

- Machines use a HOME that is already resolved (`EvalSymlinks`), so the server's own home is the same
  text either way (macOS temporary folders sit behind `/var -> /private/var`).
- The own-folder skill is `~/.claude/skills/poteto-mode/` (and the same under `~/.codex/skills/`) under a HOME with a space in its name
  (resolved with `EvalSymlinks`): SKILL.md with the five forms, `scripts/check`, `scripts/run.sh`,
  `scripts/helper.sh`, `notes.md` (one `${HOME}` form and a literal token).
- Both fakes' `platform-skill-import` scenario takes the rest of the line as the path, so a HOME
  with a space works, and on `refused` replies `Import refused: <reason>` then the result as one
  line of JSON. The tool's reason is what names the skill, file, line and ref.

## Acceptance mapping

| Requirement | Tests |
| --- | --- |
| Own-folder forms become `{{ATOLL_SKILL_DIR}}` in SKILL.md and every text file; no `~/.claude`, `$HOME`, `${HOME}` or absolute home in the stored skill; source untouched; no `warnings` field | `TestSkillHome_OwnFolderBecomesToken/{claude,codex}.{.claude,.codex}` (import root; `rewritten` names each file) |
| The token is replaced by the run's copy path at placement; the stored text keeps it; a literal token is replaced | `TestSkillHome_TokenIsReplacedAtPlacement/{claude,codex}.{.claude,.codex}` |
| Other home references refuse with exact refs (another skill, `~/.ssh`, `$HOME`, `${HOME}`, `$HOME/x`, absolute home path, a fenced example, `<folder>-v2`, the other root's folder), ordered by file then line; nothing stored; Chief's reply names skill, file, line, ref | `TestSkillHome_RefusesOtherHomeRefs/{claude,codex}/*` |
| `https://x/~/a`, `a~/b` and the other preceded forms are not references; `$HOMEWORK` and `$HOME_X` are not either (the design is silent on what may follow `$HOME`: a guard) | `TestSkillHome_NotHomeReferences` |
| A symlinked entry: entry-path and resolved-path refs rewrite, another sibling is refused, source is the entry | `TestSkillHome_SymlinkedEntryIsTheOwnFolder/{claude,codex}.{.claude,.codex}/{rewrites,refuses-other-sibling}` |
| Placed command lines run with a path holding a space and a quote (quoted), and with a plain path (unquoted), from SKILL.md and from inside a script | `TestSkillHome_PlacedCommandsRun/{claude,codex}/{plain,space-and-quote}` |
| `${HOME:-x}`, `${HOME%/}`, `${HOME#..}`, `${HOME^}`, bare `~`, `~user`, `~+`, `~-`, `$(echo ~)` refuse with exact refs | `TestSkillHome_ScannerRefusesTheWholeClass/*` |
| `${HOMEWORK}`, `${HOME_X}`, `~5`, `~3 minutes`, `a~b` are not references | `TestSkillHome_NotHomeReferences` |
| Absolute home path boundaries (`<home>x/foo`, `/mnt<home>/foo` pass untouched; `<home>` before `/`, end, space or quote refuses) | `TestSkillHome_AbsoluteHomeBoundaries` |
| Refused answer is exactly `{status, name, reason, refs}`, from `import_skill` and from reimport | `TestSkillHome_RefusedAnswerHasFourKeys` |
| Re-import refused (HTTP 200, before `confirm`, with and without `replace`) keeps the stored skill; same answer as `import_skill` | `TestSkillHome_ReimportRefusedKeepsStored/{claude,codex}` |
| The folder rule wins when both fire | `TestSkillHome_FolderRuleComesFirst` |
| Chief's prompt says refused | `TestPrompt_ChiefImportRefusal/{claude,codex}` |

Not testable with a fake: Chief saying in its own words how to fix a refusal (edit the skill or
copy the file into the skill folder and point at it as `~/.claude/skills/<folder>/<file>`). The
real-provider check is the ticket's proof.
