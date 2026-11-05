# Slice 5 test contract

Extends issues #2–#5 without changing their scenarios, isolation, prompt goldens,
reports, approvals, memory events or Retire action. Tests own only `e2e/`; the
implementing session owns product code and any corresponding design update.
Acceptance remains unchecked until implementation passes.

## Placement verified against the real CLIs

Ran `python3 e2e/skills-placement-probe.py` on 2026-11-04 with **Claude Code
2.1.306** and **codex-cli 0.166.0**. This reproducible probe uses temporary
provider folders, leaves HOME unchanged, never copies authentication, and removes
its state. Claude's real request goes to a local capture server with a synthetic
key and response; Codex's real `skills/list` needs no inference. No model override.
This proves discovery, not real-model task completion.

Output:

```text
2.1.306 (Claude Code)
codex-cli 0.166.0
Claude checkout: Bot + .claude repo skill discovered; .agents absent
Claude worktree: Bot + .claude repo skill discovered; .agents absent
Codex checkout: extra root Bot + .agents repo skill only; path disable works
Codex worktree: extra root Bot + .agents repo skill only; path disable works
```

Chosen approaches (provider settings/auth folders remain shared by all its Bots):

- **Claude:** place a run's selected skill copies in an immutable run directory's
  `.claude/skills/<folder>/SKILL.md`, outside the repo, and pass that directory
  through `--add-dir`. Reason: native discovery includes exactly the selected
  Bot skills without rewriting the shared `<CLAUDE_CONFIG_DIR>/skills`.
- **Codex:** place immutable run copies in a separate skill root and send
  `skills/extraRoots/set({extraRoots:[<absolute root>]})` after initialize and
  before `thread/start` or `thread/resume`. Reason: the extra roots belong to
  that app-server process, so overlapping Bots never change shared discovery.

Run directories may be under the provider's Atoll folder; their exact names are
not pinned. Do not place the library/other runs under shared automatic discovery
roots (`<CLAUDE_CONFIG_DIR>/skills`, `$CODEX_HOME/skills`). Do not change HOME or
create per-Bot authentication folders. Never overwrite files a live run can read;
retaining an immutable copy until that run exits is sufficient. Repo cwd and
session isolation from Slices 1–3 remain intact.

Claude was probed with `--strict-mcp-config`, `disableAllHooks:true`, and
`ENABLE_CLAUDEAI_MCP_SERVERS=false`. Its captured request contains both the Bot
skill description and `.claude/skills` description in detached clean checkouts
and linked edit worktrees, but no `.agents/skills` description. Codex was probed
with bundled skills, apps, plugins and remote plugins disabled, and personal
`~/.agents/skills/*/SKILL.md` paths individually disabled. Its enabled inventory
is exactly the Bot skill and repo `.agents/skills` skill in both directories;
a disabled shared-folder skill is still in `skills/list` with `enabled:false`.
A folder's name need not equal its frontmatter `name`.

An exploratory Claude request with `--disallowedTools=Skill(beta)` still included
beta's description. Deny rules therefore do not give an exact discovery list;
the selected Claude approach uses no shared-library deny filter.

Tested frontmatter: `name` and `description` are common. `allowed-tools: Read`
and `context: fork` are Claude invocation controls: preserve them for Claude,
strip both from the placed Codex copy, and preserve the canonical library text.
Codex's discovery also accepts these unknown fields; accepting a field is not
supporting its behavior. This contract pins these two fields, not an exhaustive
future provider metadata schema. The probe supplies them to Claude and uses the
stripped copy on Codex. Repo-owned skills are never rewritten or stripped.

## Chief and Bot fields

A fresh data directory has exactly one Bot: `{id:"chief", name:"Chief",
can_manage_platform:true}`. Choose the first installed, signed-in provider:
Claude before Codex. With neither ready, keep Chief configured for Claude so a
later sign-in makes it usable. Do not require sign-in to list Chief. Restarting
neither duplicates nor resets it. The persona is not pinned.

Existing Bot JSON gains `skills` (array of library skill IDs, default empty),
`connections` (existing Slice 3 array), and `can_manage_platform` (default false).
`POST /api/bots` accepts `skills`; existing bodies remain valid. `GET /api/bots`
returns an array including Chief; `GET /api/bots/{id}` returns the Bot.
`PUT /api/bots/{id}` accepts partial settings, returns `200` updated Bot and
preserves unspecified fields. The switch's UI for other Bots remains outside
this slice; ordinary Bots do not acquire platform permission by asking in chat.

## Platform MCP tools

Same per-run streamable HTTP server `atoll`, transport and authorization context
as the earlier slices. These six names appear in `tools/list` only when the
run's Bot has `can_manage_platform:true`. Other tools, including `finish_run`,
remain available as before. A non-platform run must also be denied if it guesses
a platform `tools/call`; hiding the name alone is not authorization.
Atoll tools never cause a user approval event. Sign-ins stay with the user.

Results are MCP text containing JSON objects, not JSON strings containing JSON.
Additional fields are allowed. Entity IDs are strings generated by Atoll.

| Tool | Arguments | Text JSON result |
| --- | --- | --- |
| `create_bot` | `{name, persona, provider, skills?:[skill_id], connections?:[connection_id]}` | Bot, including `id`, selected skills and default-false platform switch |
| `edit_bot` | `{id, ...partial Bot settings}` | Updated Bot; unspecified settings preserved |
| `create_skill` | `{name, content:<complete SKILL.md text>}` | Skill `{id,name,content}` |
| `edit_skill` | `{id, name?, content?}` | Updated Skill |
| `register_repo` | `{name, path:<existing absolute local git path>}` | Repo `{id,name,path}`, same registration rules as Slice 3 |
| `attach_connection` | `{bot_id, connection_id:<already connected connection>}` | Updated Bot with that connection selected, preserving other settings |

No token, device code or sign-in ability is returned by these tools. Existing
resource validation remains in force. A successful mutation emits its own change
record; creating a skill and a Bot is two changes, not a transaction spanning a
chat. Undo the Bot creation before undoing its selected skill's creation.

## Skill REST API

JSON uses snake_case. `content` is the entire standard `SKILL.md`, including its
frontmatter and body. IDs, not names or file paths, select skills on a Bot.

| Request | Body | Reply |
| --- | --- | --- |
| `POST /api/skills` | `{name,content}` | `201` Skill |
| `GET /api/skills` | | `200` array of Skills |
| `GET /api/skills/{id}` | | `200` Skill |
| `PUT /api/skills/{id}` | `{name?,content?}` | `200` updated Skill |
| `POST /api/platform/changes/{id}/undo` | none | `204` after restoring the previous state |

No delete endpoint is required here: undoing creation removes that entity from
current listings; undoing an edit restores its former settings/content. Library
text retains provider fields even when a placed copy strips them.

Bot settings and selected skill contents are a run snapshot. Edits and Undo
become visible to a subsequent run, including a resumed run in the same thread.
An active run continues to list/use its original files. An overlapping Bot with
no selected skills sees none of the library, even while a skilled Bot is running.

## Change event, provenance and Undo

On the existing thread SSE stream, emit `platform_changed` before/at run
completion. As with `memory_saved`, subscribe before sending; no replay is
required. Each event has:

```json
{
  "source_bot_id": "chief",
  "thread_id": "thread-id",
  "run_id": "run-id",
  "change": {
    "id": "change-id",
    "entity": "bot",
    "entity_id": "changed-bot-id",
    "action": "create",
    "before": null,
    "after": {"id":"changed-bot-id", "name":"Lagoon"},
    "undone": false,
    "source_bot_id": "chief",
    "source_thread_id": "thread-id",
    "source_run_id": "run-id"
  }
}
```

`entity` is `bot`, `skill` or `repo`; `action` is `create` or `edit`.
`before` is null for creation and the previous entity for edits; `after` is the
resulting entity. Attaching a connection edits a Bot. The run supplies all source
IDs, never the model. Change records and their before-state must survive restart.
The tests use event IDs to call Undo and verify the actual entity state via REST;
undoing creation stays undone after restart. No secret belongs in snapshots.

A thread displays a region accessible as **Platform change**, containing the
changed entity's name, source Bot name, and an **Undo** button. Clicking Undo
restores state, shows **Undone**, removes the button, and retains the change text
in the thread. This reuses Slice 4's **Saved memory** / **Retire** interaction
shape without changing memory's semantics. The browser test asks Chief and
clicks Undo through the UI; REST is used only to verify the result.

## Fakes and acceptance mapping

New recordings reuse the existing recorded transport envelopes and session IDs;
platform/skill actions are scripted directives, not claimed real-model outputs.
Both fakes initialize MCP and retain the actual `tools/list` response. Chief's
scenario calls `create_skill`, consumes its returned ID in `create_bot`, calls
`finish_run`, and answers. Editing uses `edit_bot`; configuration uses `edit_skill`, `register_repo` and
`attach_connection`, consuming real replies. A non-platform scenario
checks the received tools list, requires a guessed `create_bot` call to be
rejected, then finishes.

For skill scenarios, the fake scans only provider-native roots: shared provider
skills, repo skills from cwd to its Git boundary, Claude `--add-dir` skills, and
Codex `skills/extraRoots/set` roots. Codex `skills.config` entries disable by
absolute `SKILL.md` path. The fake reads frontmatter names/descriptions, logs
`{pid,skills:[{name,path,text}]}` to `<executable>.log.skills`, and reads the
selected file again on use. Putting skill text in the prompt never supplies a
skill. The fake models these verified fixture layouts, not every CLI plugin or
frontmatter feature.

The hold scenario writes `<executable>.ready.<pid>` after discovery and waits for
`<that file>.release`. While held, the test edits the library and runs an
unskilled Bot. Re-reading the original file detects mutable shared placement;
a later resumed run must read the edited content. All fixtures/processes are
temporary and the harness removes them.

| Acceptance | Tests |
| --- | --- |
| Fresh folder lists Chief | `TestSkills_ChiefFirstLaunch` (Claude, Codex, both, neither; restart) |
| Chief creates skill/Bot; next run lists and uses it on both providers | `TestSkills_ChiefCreatesAndUndo/{claude,codex}`; also checks provider-only field stripping |
| Undo restores previous state | `TestSkills_ChiefCreatesAndUndo` (remove creations, restart), `TestSkills_EditUndoAndNoPlatformTools` (restore edit after restart), `TestSkills_ChiefConfiguresAndUndoes` (skill edit, repo registration, connection attachment) |
| Bot without switch has no platform tools | `TestSkills_EditUndoAndNoPlatformTools/{claude,codex}` inspects actual tools/list through fake |
| Repo skills still load | `TestSkills_RepoAndBotSkills/{claude,codex}` in clean checkout and edit worktree alongside Bot skills |
| Next-run semantics and concurrent isolation | `TestSkills_ConcurrentRunsAndNextRun/{claude,codex}` |
| UI chat → change with Undo → restored state | `ui/skills.spec.ts` |
| Fakes operate before implementation | `TestSkillsFixtures_ProviderScenarios/{claude,codex}` (create, create-codex, configure, edit, denied, use, prompt-only, disabled, worktree, hold) |

The contract does not require a platform prompt change: skill discovery is native
and MCP tools carry their schemas. Neither Slice 4 golden file is changed.
Real-model completion, broader field translation, validation errors, conflicting
Undo after intervening changes, the Skills settings page and optional provider
features remain implementation/verification follow-ups; no untested behavior is
blessed as acceptance proof.
