# Skill descriptions read as YAML: test contract

Tests: `e2e/ui/skill-descriptions.spec.ts` (Playwright). Each test runs on a machine of its own with
the fake Claude provider and creates its skills through `POST /api/skills`; no product package is
imported.

A skill's `description:` is a YAML value, not the rest of a line. Double-quoted (with `\"`
escapes), single-quoted (with `''`), plain and folded (`>`, continuation lines indented) all show
as their text, without quotes, escapes or the `>`, in Settings > Skills (the list row and the
`When to use it` textbox of the `Edit <name>` panel), in the `Bot settings` dialog and in the
`Add skill` dialog of New Bot. Editing `When to use it` and saving writes SKILL.md frontmatter
that a YAML parser reads as `{name, description: <the typed text>}` (one `description` key, the
other keys kept), whatever the description was before, and leaves the body as it was. That covers a
bare `description:` (null) and `description: # comment`, both shown as an empty field, and a plain
value that is not valid YAML, `description: Use when: needed`, shown as `Use when: needed`, not
blank. A SKILL.md with CRLF line endings shows its description the same way.

| Acceptance (#126)                                                                                                           | Test                                                                                                                                                                             |
| --------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| The four styles show as text in Settings > Skills                                                                           | `Settings > Skills shows quoted, single-quoted, plain and folded descriptions as text`                                                                                           |
| The same in Bot settings                                                                                                    | `Bot settings shows the descriptions of its skills as text`                                                                                                                      |
| The same in the New Bot skill list                                                                                          | `the New Bot skill list shows the descriptions as text`                                                                                                                          |
| Editing writes valid YAML and keeps the text, from a quoted and from a folded description                                   | `editing the description of quoted-skill writes YAML that parses back to the same text`, `editing the description of folded-skill writes YAML that parses back to the same text` |
| Editing a bare, comment-only or invalid plain (`Use when: needed`) description writes valid YAML with one `description` key | `editing the description of null-skill writes YAML that parses back to the same text`, `...of comment-skill ...`, `...of colon-skill ...`                                        |
| An invalid plain description shows its text, not a blank                                                                    | `a plain description with a colon shows its text, not a blank`                                                                                                                   |
| CRLF frontmatter shows its description in Settings > Skills and Bot settings                                                | `a SKILL.md with CRLF line endings shows its description`                                                                                                                        |
