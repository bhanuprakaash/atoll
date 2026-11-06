# Skills keep their scripts, playbooks and references: test contract

Extends [skills-contract.md](skills-contract.md) and [skill-import-contract.md](skill-import-contract.md).
Tests: `e2e/skill_files_test.go` (fixtures and helpers in `e2e/skill_files_fixtures_test.go`). Tests
build the real server; no product package is imported. HOME is a temporary folder, never the real one.

## Skill JSON and REST

A skill is `{id, name, content, source, files}`. `content` is SKILL.md. `files` is an array of
`{path, content, executable}` sorted by `path` (bytewise), never null: `[]` when there are none.
`source` is the absolute folder the skill was imported from, `""` otherwise. `GET /api/skills` and
`GET /api/skills/{id}` return full skills, files included.

| Request | Body | Reply |
| --- | --- | --- |
| `POST /api/skills` | `{name, content, files?}` | `201` skill |
| `PUT /api/skills/{id}` | `{name, content, files?}` | `200` skill |

On `PUT`, a `files` key replaces the whole set in one transaction (files not in it are removed;
`[]` removes all); with no `files` key the files are kept.

A file path is relative and slash-separated (Go `io/fs.ValidPath`: no `.` or `..` element, no empty
element, so no leading `/`, no `a//b`, no trailing `/`), is not `SKILL.md`, is unique in the request,
and is not both a file and the folder of another path. A file's content is valid UTF-8 without a NUL
byte. Any violation, in any file, answers `400` and nothing changes: a later `GET` returns the skill
byte for byte as before, and a rejected `POST` adds no skill. The request body itself must be valid
UTF-8: a Go JSON decoder turns invalid bytes into U+FFFD, so the product checks the raw body
(`utf8.Valid`) and answers `400` for non-UTF-8 bytes inside a file's content. The tests send such
bytes raw (`"\xff\xfe"` inside the JSON string).

## Placement

Each run gets copies of its Bot's skills under its run folder, `runs/<run id>/` in the provider's
Atoll folder (`<ATOLL_DATA_DIR>/providers/<provider>/runs/<run id>/`): Claude
`.claude/skills/<name>/`, Codex `<name>/`. A copy holds SKILL.md and every file at its path. Files
with `executable: true` are mode 0555; every other file, SKILL.md included, is 0444. The run folder
is removed when the run ends. A live run keeps its copy when the skill changes meanwhile; the next
run gets the new files.

## Chief tool `import_skill`

An Atoll MCP platform tool for Bots with the platform switch only, like `create_skill`: a Bot
without the switch neither lists it nor can call it by guessing. Input `{path: string, replace?:
boolean}`; both are declared in the tool's input schema (the fake sends only declared arguments).
`path` is a local folder holding SKILL.md; a leading `~/` is the server's `$HOME`.

It reads every regular file under the folder. Text files (valid UTF-8, no NUL byte) are taken. A
`.git` folder is skipped whole, and so is any binary file; each is reported. `executable` is true
when any execute bit is set on the source file. In SKILL.md only, `~/.claude/skills/<folder
name>/` is replaced with nothing, so `S=~/.claude/skills/poteto-mode/scripts` becomes
`S=scripts`; every other file is stored byte for byte. The source folder is never written: its
paths, modes, modification times and bytes are the same after. The skill's name is SKILL.md's
frontmatter `name:`, else the folder's name. `source` is the absolute, cleaned folder path. An
existing skill is the one with that `source`, else the one with that name.

The result is MCP text holding one JSON object:

```json
{
  "status": "created" | "replaced" | "unchanged" | "confirm",
  "skill": {"id": "", "name": "", "source": "", "files": [{"path": "", "executable": false}]},
  "added": [], "changed": [], "removed": [],
  "skipped": [{"path": ".git", "reason": "git"}, {"path": "assets/logo.png", "reason": "binary"}],
  "home_paths": ["~/.reef-verify/<id>"],
  "warning": "..."
}
```

- `skill` is the stored skill, its `files` the same as the API's without SKILL.md; absent on `confirm`.
- `added`, `changed`, `removed` are sorted paths against the existing skill's stored files; SKILL.md
  counts as a path; all `[]` on `created`. A file is `changed` when its stored bytes differ from what
  the import would store, so a file edited in Reef (the folder unchanged) is `changed`: replacing
  would revert it.
- `skipped` has `.git` (reason `git`) once, not its files, and every binary by its path relative to
  the folder (reason `binary`).
- `home_paths` are the `~/` paths left in the stored SKILL.md, once each, in order of first
  appearance. A path is the run of non-space characters starting at `~/`.
- `warning` only with `confirm`.

Statuses:

- No existing skill: `created`; one `platform_changed` (entity `skill`, action `create`); Undo
  deletes the skill with its files.
- Existing, nothing differs: `unchanged`; nothing is written and no change is recorded.
- Existing, its files not edited in Reef since the last import: `replaced`; one `platform_changed`
  (entity `skill`, action `edit`) and Undo restores the previous content and files.
- Existing and edited in Reef since the last import (for example `PUT /api/skills/{id}`), with
  `replace` not true: `confirm`; `warning` says Reef edits would be overwritten (the tests pin the
  words `Reef` and `overwritten`); nothing is written and no change is recorded. Called again with
  `replace: true`: `replaced`.

## Chief's prompt

The `Import a local skill` section of the platform block names `import_skill` and `persona`, and the
whole block stays at most 2000 bytes (`TestPrompt_ChiefPlatformBlock`). Nothing else of it is
pinned. This replaces the pinned words of [skill-import-contract.md](skill-import-contract.md)
(`create_skill`, `SKILL.md`, `persona`); its other rows still hold.

## Fixtures and fakes

The fixture under the temporary HOME is `~/.claude/skills/poteto-mode/`: SKILL.md (frontmatter
`name: poteto-mode`; the line `` `S=~/.claude/skills/poteto-mode/scripts`. Always call scripts by
that full path. ``; `~/.reef-verify/<id>` twice and `~/.poteto/state` once), `scripts/` with 10
executable (0755) shell scripts, one of them `scripts/check` (`#!/bin/sh\necho check-ok\n`),
`playbooks/` with 11 `.md` files (each naming `~/.claude/skills/poteto-mode/scripts/check`) and
`references/improve-report.md`: SKILL.md and 22 files. The skip fixture is `~/.claude/skills/reef-snaps/`
with a small PNG (real magic bytes) at the top and in `assets/`, and `.git/HEAD`, `.git/config`.

Two scenarios are added to both fakes (recordings keyed on message text, like the others):

| Recording key | Fake directive | What it does |
| --- | --- | --- |
| `the skill folder ` | `platform-skill-import` | Chief. Calls `import_skill({path: <word after the key>})`, adding `replace: true` when the message contains `Yes, replace`. Replies one sentence, a newline, then the result as one line of JSON. The sentence is `Import <status>: <skill name>. Skipped: <path> (<reason>), ... .` (`none` when empty), or on `confirm` `Import needs confirmation: <warning> Added: .... Changed: .... Removed: ....` |
| `the poteto-mode script scripts/check` | `skills-run` | Bot. Finds `poteto-mode` among the skills its provider discovered, runs `scripts/check` from that folder and replies `Ran <path of the script>: <stdout>`. A message with `Wait, then` first holds as `skills-hold` does and, once released, logs the folder's files again (path, mode, bytes), so a test sees what the live run still had. |

When the Bot lists none of the platform tools, the `platform-skill-import` scenario instead checks
that `import_skill` is not listed, guesses a call that must fail, and replies `import_skill denied.`

Every discovered skill's log entry now lists the files of its folder with mode and bytes
(`fake.SkillCopy.Files`).
`TestSkillFilesFixtures_ProviderScenarios` drives both scenarios on both fakes before the product
exists.

## Acceptance mapping

| Acceptance | Tests |
| --- | --- |
| Chief imports `~/.claude/skills/poteto-mode`: SKILL.md plus 22 files, `scripts/*` executable, Chief names what it skipped | `TestSkillFiles_ChiefImportsPotetoMode/{claude,codex}` (result, `GET /api/skills`, one change, Undo, `home_paths`); the skipped part in `TestSkillFiles_ImportSkipsBinariesAndGit` |
| No `~/.claude` in SKILL.md, `S=scripts`; source folder byte-for-byte unchanged | `TestSkillFiles_ChiefImportsPotetoMode` (checksum of paths, modes, mtimes and bytes before and after) |
| Bot runs the placed `scripts/check`, not `~/.claude`, on Claude and Codex | `TestSkillFiles_BotRunsPlacedScript/{claude,codex}` |
| Placed modes (`scripts/*` 0555, rest 0444); run folder gone after the run | `TestSkillFiles_BotRunsPlacedScript` |
| PNG and `.git` not stored, listed as skipped | `TestSkillFiles_ImportSkipsBinariesAndGit/{claude,codex}` |
| `PUT` with `..`, absolute path or non-UTF-8 content: 400, nothing changes | `TestSkillFiles_RejectsInvalidFiles/{PUT,POST}/*` (also `scripts/../../x`, `SKILL.md`, empty, `.`, `//`, trailing `/`, duplicate, file and folder, NUL) |
| `PUT` with a smaller set removes the rest; the next run's folder matches | `TestSkillFiles_SmallerSetIsPlaced/{claude,codex}`; shapes in `TestSkillFiles_StoreAndReplace` |
| Re-import after a Reef edit: lists, warning, replaces only after yes | `TestSkillFiles_ReimportAfterReefEditAsksFirst/{claude,codex}`; the no-edit paths in `TestSkillFiles_ReimportWithoutReefEdit` |
| Live run keeps its copy; the next run has the new files | `TestSkillFiles_LiveRunKeepsItsCopy/{claude,codex}` |
| The rule is in Chief's prompt | `TestPrompt_ChiefImportRule/{claude,codex}` (now pins `import_skill` and `persona`) |
| `import_skill` is Chief only | `TestSkillFiles_ImportSkillIsChiefOnly/{claude,codex}` (passes before the product exists: `import_skill` is not listed yet; it guards the switch once it is) |

`TestSkills_ChiefImportsLocalSkill` still passes unchanged: `create_skill` stays a tool and a skill it
creates has no files.

Real-provider only (a fake replays a scripted answer): Chief choosing `import_skill` and passing the
path the user named; Chief naming what it skipped and asking before replacing, in its own words; a
real Claude or Codex Bot running the placed script by its path; the provider's own approval for
reading `~/`. The tests cover the deterministic parts behind them: the tool's result, the stored
files and the placed folder. The proof for the real-provider set is the ticket's.
