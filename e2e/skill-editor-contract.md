# Skill editor for every file of a skill: test contract

Extends [skill-files-contract.md](skill-files-contract.md) (the skill JSON, `PUT /api/skills/{id}` with
`files`, placement, `import_skill`). Tests: `e2e/skill_editor_test.go` (Go, the new endpoint and the
added script run) and `e2e/ui/skill-editor.spec.ts` (Playwright, Settings > Skills; fixtures in
`e2e/ui/skill-fixture.ts`). Tests build the real server and drive the real UI; no product package is
imported. Each test runs on a machine with a temporary HOME and data folder, never the real
`~/.claude`, and the fake Claude provider.

## Seeding

poteto-mode is the fixture of the skill-files contract: SKILL.md and 22 files (10 executable scripts
including `scripts/check`, 11 playbooks including `ship.md`, `references/improve-report.md`) in
`$HOME/.claude/skills/poteto-mode`. Tests get it into the library the way a user does, by asking Chief
to import it (the `platform-skill-import` scenario), so the skill has a `source`.

## Endpoint: `POST /api/skills/{id}/reimport`

Body `{"replace": bool}`. It re-reads the skill's `source` folder and answers `200` with the JSON
`import_skill` returns (see the skill-files contract): `status` (`replaced`,
`unchanged` or `confirm`; never `created`), `added`, `changed`, `removed`, `skipped`, `home_paths`, `warning`
only on `confirm`, `skill` absent on `confirm`. The tests compare it with what Chief's `import_skill`
returns for the same folder and library: the two are equal.

- `409` when the skill has no `source` (a skill made with `POST /api/skills`), whatever `replace` is.
- `404` for an unknown id.
- Same rules as `import_skill`: nothing differs: `unchanged`, nothing written. Skill not edited in Reef
  since its last import: `replaced`. Edited in Reef (for example by `PUT`) and `replace` not true:
  `confirm`, nothing written, `warning` names `Reef` and `overwritten`; `replace: true`: `replaced`.
  After a replace the Reef edit is gone, so the next call is `unchanged`.
- The source folder is never written (paths, modes, mtimes, bytes the same after).

## Settings > Skills: the skill's right panel

Today's names stay: the panel is a `dialog` named `Edit <skill name>` (`New skill` for a new one), its
`Close` button closes it, `Name` is the name field, the list on the page has one button per skill.
New names, pinned by the tests:

- **File list**: a `list` named `Files`, one `listitem` per file, `SKILL.md` first, then every file by
  path (bytewise). Each listitem holds one `button` whose accessible name is the path (`SKILL.md`,
  `scripts/check`), optionally followed by ` script`. A script (executable) row also holds an element
  whose whole text is `script`; other rows have none (SKILL.md included).
- **`+ File`**: a button. It shows a textbox `File path` and a button `Add`. A valid relative path adds the
  file as a non-script, selects it and shows it empty in the editor, and closes the input. A path with
  `..` or a leading `/` (the `io/fs.ValidPath` rules of the skill-files contract) shows an inline
  element with role `alert`, adds nothing, sends no request, and leaves the input open (so a second
  attempt types into it without clicking `+ File` again).
- **Editor**: selecting a row shows that file's text in a textbox named `Content` (SKILL.md keeps
  today's editor; only files are pinned). Per selected file: a checkbox `Script` (executable) and a
  button `Delete file`; Delete removes the row from the list at once, locally.
- **Script on `#!`**: while the user types in a file, `Script` turns on by itself once the content starts
  with `#!`, until the user toggles the checkbox by hand; after that it stays as they set it.
- **One Save**: a button whose name starts with `Save` (today `Save skill`) sends the whole skill,
  `PUT /api/skills/{id}` with `name`, `content` and `files`. Nothing is stored before it: adding,
  editing, deleting and toggling are local. On a new skill the button's name starts with `Create` and
  `POST /api/skills` is sent only then, with its files; the panel then shows the created skill.
- **Unsaved changes**: closing the panel, clicking another skill in the list behind it (the list stays
  clickable: no scrim), or going to another Settings section (the nav link `General`), while the skill
  differs from what is stored (or a new skill has a name), opens a `dialog` named `Discard unsaved
  changes?` with buttons `Discard` and `Keep editing`. Keep editing closes the dialog and leaves the
  panel, the section and the edits as they were. Discard leaves and drops the edits. With nothing
  unsaved (after Save, after a refused path) leaving asks nothing.
- **`Re-import`**: a button, only when the skill has a `source`. Click sends `POST
  /api/skills/{id}/reimport` with `{"replace": false}`. The result shows, for each non-empty group, a
  `list` named `Added`, `Changed` or `Removed` whose listitems hold exactly the paths. On `unchanged`
  it shows the text `No changes`. On `replaced` and `unchanged` the skill and its file list are
  reloaded. On `confirm` it also shows a warning containing `overwritten` and buttons `Cancel` and
  `Confirm`, and the skill is left as it was. Cancel clears the warning and the buttons and writes
  nothing. Confirm sends `{"replace": true}`, then shows the replaced skill (reloaded) and its lists.

## Acceptance mapping

| Acceptance (#56) | Tests |
| --- | --- |
| poteto-mode: SKILL.md first, then 22 files; `scripts/*` shown as scripts | UI `the panel lists SKILL.md first, then the 22 files by path, scripts marked` |
| Edit `playbooks/ship.md`, Save, reload: kept; the next run's placed `playbooks/ship.md` has it | UI `an edited file is kept after Save and reload, and the next run gets it` (API checked before and after Save; placed file 0444 read from the fake's discovery log) |
| `+ File` `scripts/hello` with `#!/bin/sh` and `echo hi`: Script turns on; after Save a Bot run prints `hi` | UI `a new script turns Script on by itself; after Save a Bot run prints hi` (the placed script is 0555 and the run replies `Ran <run folder>/scripts/hello: hi`); `Script follows #! only until the user toggles it`; Go `TestSkillEditor_BotRunsAddedScript/{claude,codex}` (the fake's script runner) |
| Delete a file, Save: gone from the list and the next run's placed folder | UI `a deleted file is gone from the list and from the next run after Save` |
| `..` or a leading `/` refused inline; nothing saved | UI `a path with .. or a leading slash is refused inline and nothing is saved` (no write request, `GET` unchanged), `refused paths leave nothing unsaved` |
| Leaving with unsaved changes asks first | UI `leaving with unsaved changes asks first` (closing, another skill, another section, a new skill; Keep editing and Discard each) |
| New skill works the same, created only on Create | UI `a new skill with files is created only on Create` |
| Re-import after a Reef edit: lists and warning; Cancel keeps the edit, Confirm replaces it | UI `Re-import › after a Reef edit it lists the changes and warns; Cancel keeps the edit, Confirm replaces it`; `with no Reef edit it replaces at once and shows what changed`; `a skill with no source has no Re-import` |
| The endpoint behind Re-import | Go `TestSkillEditor_ReimportEndpoint/{claude,codex}` (unchanged, replaced, confirm then `replace: true`, equal to `import_skill`, source folder untouched), `TestSkillEditor_ReimportReportsSkipped`, `TestSkillEditor_ReimportRefusals` (`409` without source, `404` unknown id) |

## Fakes

`the poteto-mode script <path>` in a prompt makes the `skills-run` scenario of both fakes run that script
of the placed folder instead of `scripts/check` (still the default). Recordings `skills-run-hello`
(key `the poteto-mode script scripts/hello`) are added for both fakes; the reply is `Ran <path>: <stdout>`.

## Manual only

- The look of the file list, the script marker, the inline error, the dialogs and the Re-import result
  (the tests pin roles, names and texts, not layout).
- Chief or a real provider choosing the skill, and a real Claude or Codex Bot running a placed script.
- Binary files, upload or drag-and-drop and renaming a file are out of scope.
- Not pinned: what Re-import does while the panel has unsaved edits, and a source folder that has been
  deleted or no longer holds SKILL.md.
