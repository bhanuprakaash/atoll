# Import a local skill: test contract

Extends [prompt-contract.md](prompt-contract.md) ("Platform block text") and
[skills-contract.md](skills-contract.md). Tests: `e2e/skill_import_test.go`.

## What the product provides

No new tool, field or endpoint. Chief reads `~/.claude/agents/<agent>.md` and
`~/.claude/skills/<skill>/SKILL.md` with the provider's own file tool (Claude `Read`, Codex shell),
under that provider's normal approval rules, and imports with the existing `create_skill` and
`create_bot`. The provider's `HOME` stays the user's `HOME` (already pinned by the isolation tests),
so `~` in a path the user gives resolves to their own folder.

The product change is one section of Chief's platform block:

| Section | Heading line | Fixed |
| --- | --- | --- |
| Import rule | `Import a local skill` | its lines name `create_skill`, `SKILL.md` and `persona` |

The section follows the section rules of prompt-contract.md (exact heading line, non-empty lines up
to the next blank line). The whole platform block stays at most 2000 bytes
(`TestPrompt_ChiefPlatformBlock`). The rule tells Chief to: read the agent file, find the local
skills it names, create each with `create_skill` from its `SKILL.md`, attach them through
`create_bot`'s `skills`, keep `~/.claude` paths out of the persona, and say that files besides
`SKILL.md` are not imported before creating anything. Only the three words above are pinned.

## Fixture and fake

The test writes a fixture under the machine's temporary `HOME`, never the real one:

- `~/.claude/agents/lagoon.md`: frontmatter `name: lagoon`; body
  `You greet visitors to the lagoon. Before you answer, read ~/.claude/skills/reef-greeting/SKILL.md and follow it.`
- `~/.claude/skills/reef-greeting/SKILL.md`: `fake.GreetingSkill`
- `~/.claude/skills/reef-greeting/scripts/greet.sh`

The `platform-import` recordings answer a message containing
`Make a Bot from ~/.claude/agents/lagoon.md`. The fake reads the agent file from its `HOME` (as the
provider's file tool would), calls `create_skill({name: "reef-greeting", content: <SKILL.md>})`,
then `create_bot({name: "lagoon", persona, provider, skills: [<returned id>]})` with each
`~/.claude/skills/<name>/SKILL.md` in the body replaced by `the <name> skill`, and replies
`Imported reef-greeting and created lagoon.` The provider is `codex` when the message says
`using codex`, else `claude`. Neither fake discovers skills in `~/.claude/skills`, so the new Bot's
skill can only come from Reef's placement.

## Acceptance mapping

| Acceptance | Test |
| --- | --- |
| Creates the skill from `SKILL.md`, then the Bot with it attached | `TestSkills_ChiefImportsLocalSkill/{claude,codex}`: library holds exactly `SKILL.md`'s text; Bot's `skills` is that id; one `platform_changed` each |
| Persona has no `~/.claude` path | `TestSkills_ChiefImportsLocalSkill`: persona and the new Bot's system prompt |
| `GET /api/skills` lists it; a run of the new Bot has it placed | `TestSkills_ChiefImportsLocalSkill`: run after the fixture `~/.claude` is deleted; placed copy outside `HOME`, without `scripts/` |
| Undo of the Bot leaves the skill; Undo of the skill removes it | `TestSkills_ChiefImportsLocalSkill` |
| The rule is in Chief's prompt | `TestPrompt_ChiefImportRule/{claude,codex}` |

Real-provider only (a fake replays a scripted answer): asking the missing items first; telling the
user that files besides `SKILL.md` are not imported before creating anything; Claude asking
approval to read outside its folder. `TestPrompt_ChiefImportRule` covers the deterministic part
behind them. `TestSkills_ChiefImportsLocalSkill` passes before the code change: the tools it uses
exist; it pins the flow the rule asks Chief to follow.
