# Slice 4 test contract

Extends the Test contracts of issues #3 and #4. Existing requests, scenarios,
provider isolation, approvals, clean checkouts and worktrees remain valid. All
product tests use the real binary over HTTP/SSE and fake provider executables;
no product package is imported. Implementers must not edit these tests or run
`-update`. The acceptance boxes remain unticked until implementation is proven.

## Provider additions: verified in this sandbox

Verified 2026-11-04 against Claude Code **2.1.306** and codex-cli **0.166.0**:

- `claude --help`: `--append-system-prompt` appends to the default prompt;
  `--system-prompt` replaces it. Ran the real CLI in a temporary cwd/config
  with `claude -p --append-system-prompt REEF_ADDITION_PROBE --output-format json
'Reply ok'`, pointing `ANTHROPIC_BASE_URL` at a local HTTP capture server
  with a synthetic key. Captured request system blocks: 3; both the default
  Claude Code prompt and the appended marker present; CLI exit 0.
- Real `codex app-server`: `initialize`, then `thread/start` with
  `{cwd, developerInstructions: "REEF_ADDITION_PROBE", approvalPolicy: "on-request",
sandbox: "read-only"}`, then `turn/start` with a short text message. The
  rollout retained the default `base_instructions` (22,093 JSON bytes) and
  recorded the marker in a separate `developer` message. No model override.
  Terminated the probe and removed its temporary cwd and rollout file.
  Use `developerInstructions` on `thread/start` and `thread/resume`;
  `baseInstructions` replaces the provider prompt and must stay unset.
  The fake also accepts `developer_instructions` in thread config or `-c`.

These are transport probes, not a real-provider memory acceptance run. The
fakes retain the raw argv/stdin log and write decoded additions and every user
message, without normalizing their text, to `<executable>.log.prompts` as JSONL
`{pid, kind: "system"|"user", text}`. The fixture test independently checks exact
newlines and text in that log and all six scenarios for both providers.

## Memory wire shape

IDs are strings. A memory item has the tested fields:

```json
{
  "id": "item-id",
  "kind": "decision",
  "body": "Use SQLite WAL for the journal.",
  "why": "Readers must not block journal writes.",
  "scope": "repo",
  "scope_id": "repo-id",
  "status": "accepted",
  "source_bot_id": "bot-id",
  "source_thread_id": "thread-id",
  "source_run_id": "run-id"
}
```

Scopes used: `user` (empty `scope_id`), `repo` (registered repo ID), `bot`
(Bot ID). Seeds are accepted; Bot saves are accepted without approval. Retiring
changes status to `retired`, retaining the ID, body, why and source in history.
Extra fields are allowed. Tests do not fix timestamps, numeric storage IDs,
embeddings, lookalikes, candidate workflows or the representation of `replaces`.

## MCP: four tools

Same streamable HTTP transport and per-run context as issue #3. Results are MCP
text content containing JSON, not a JSON-encoded string. These tools must not
cause a user approval event.

| Tool            | Tested arguments                           | Text JSON result                         |
| --------------- | ------------------------------------------ | ---------------------------------------- |
| `remember`      | `{kind:"decision", body, why, scope:"repo" | "user"}`                                 | Memory item |
| `recall`        | `{query:"SQLite journal", limit:8}`        | Array of memory items                    |
| `read_memory`   | `{id}`                                     | Memory item with body and why            |
| `retire_memory` | `{id}`                                     | Same memory item with `status:"retired"` |

The run supplies the source Bot/thread/run and the repo scope ID; the provider
cannot invent those IDs. `recall` searches the current repo, user and current
Bot's shelves. The recall script reads the matched item with `read_memory` and
answers using its actual body and why; it also requires that item's ID and body
in the new thread's first input. The retire script recalls and reads before
retiring. Every scenario still calls real MCP `finish_run`.

## REST additions

| Request                        | Body                                 | Reply                                                 |
| ------------------------------ | ------------------------------------ | ----------------------------------------------------- |
| `PUT /api/settings`            | `{worktree_root, user_name:"Bhanu"}` | `204`                                                 |
| `POST /api/memory`             | `{kind, body, why, scope, scope_id}` | `201` Memory item, accepted                           |
| `GET /api/memory`              |                                      | `200` array including retired items and source fields |
| `POST /api/memory/{id}/retire` | none                                 | `204`                                                 |

Seeds need no fabricated Bot source. These endpoints allow deterministic seeds,
inspection and the UI's Retire action. The tests require no listing order.
`{bot}` in the platform prompt is the Bot's configured name; its persona is the
configured persona. `{user}` is `user_name` from settings. The golden fixture
sets **Ada**, **Bhanu**, and **A careful Go engineer.** explicitly. No provider
login name, OS account name or inferred user identity is involved.

## Event and browser

On the existing thread SSE stream, a save emits `memory_saved` with
`{item: <Memory item>, source_bot_id, thread_id, run_id}`. IDs match the saving
Bot, subscribed thread and active run. The saved item itself carries those
source IDs as `source_bot_id`, `source_thread_id`, `source_run_id`, so REST can
retain provenance. Saves are visible before/at run completion; no replay required.

Playwright creates a Bot/thread using the existing UI and sends the user-scope
save scenario. A region accessible as **Saved memory** shows body, why, source
Bot name and a **Retire** button. Clicking Retire marks it **Retired**, removes
the button, keeps its body visible and changes that same item's REST status to
`retired`. Only verification uses REST; creating and retiring happen in the UI.

A reply's saves fold into one quiet line in that region: a `button` with
`aria-expanded` (`false` by default, not remembered) whose accessible name
contains `Saved 2 memories: a finding, a decision` (kinds in save order) or,
for one, `Saved a memory: <first words>` (the test matches
`/Saved a memory: Use SQLite/`). The summary is one text in the button, so each
body shows exactly once, in its row. Folded shows no **Details** or **Retire**.
Clicking the line opens today's rows (`aria-expanded` `true`): per memory its
body as its own text, why, Bot name, kind, and `button`s **Details** and
**Retire**. Retire works from the open line. The scenario `memory-save-pair`
(prompt "Save the journal finding and decision in user memory.") remembers, in
user scope, the finding "The journal fsync takes 40 ms on CI." then the decision
"Batch journal writes every 50 ms." and replies "Saved a finding and a decision.".

## Prompt layout and golden files

`testdata/golden/system.txt` is the complete draft from `docs/design.md`, with
Ada/Bhanu filled in, then a blank line, `Persona:`, and the persona. It is the
Reef addition only; the provider's own prompt precedes it. No extra preamble.
The golden text file ends in one newline for storage; provider input does not.

`testdata/golden/first-message.txt` pins this layout:

```text
Memory items:
- [<id>] <kind> (<scope>): <body>
  Why: <why>

Memory: repo N, user N, bot N. Use recall.

Repo: <name> (<repo-id>)
Worktree: <absolute current checkout/worktree path>
Branch: <branch, or detached for the clean read-only checkout>

Message:
<original user message>
```

The golden uses three equally relevant SQLite items across all three scopes,
ordered convention, decision, lesson (rules first). Counts are the full accepted,
visible shelf, not just the selected items. At most eight relevant items are
included. Another Bot's private items are excluded. The large-shelf test seeds
12 long, relevant items to exercise the cap and size budget, without specifying
a truncation algorithm. The prompt must include some relevant memory, not drop
all of it to satisfy the budget.

Normalize only fixture-known values before comparing: memory IDs become
`<repo-item>`, `<user-item>`, `<bot-item>`; repo ID becomes `<repo>`; the thread's
absolute checkout path becomes `<checkout>`; the server origin (including port)
becomes `<server>`. Never normalize arbitrary unknown IDs or prose. Both
providers compare to the same golden. Files were authored from the design,
not blessed from the currently missing implementation.

Resumed inputs have just unseen relevant entries in the same item layout,
a blank line, `Message:`, then the raw message. No repeated seen items, count
line or repo header. The resume test seeds A, sends message 1, seeds B, then
sends message 2 in the same provider session: B appears, A does not. Retirement
is tested through MCP and REST, then on both later resumed and fresh inputs;
already-sent history is not erased.

Budgets use UTF-8 bytes / 4, without rounding down: system additions (platform
and persona) **at most 500 tokens**; all Reef additions (system plus message
wrapper/context, excluding the raw user message) **at most 4,000 tokens**.

Human-only golden update, after reviewing the intended prompt change:

```sh
go test ./e2e -run '^TestMemory_PromptGoldenAndBudget$' -update -count=1
```

No CI or ordinary test run invokes `-update`.

## Acceptance mapping

| Issue #5 acceptance                                 | Test                                                                                              |
| --------------------------------------------------- | ------------------------------------------------------------------------------------------------- |
| Decision survives a new thread on the same repo/Bot | `TestMemory_CrossThreadRecall/{claude,codex}` (also source event and persisted provenance)        |
| Golden and size budgets                             | `TestMemory_PromptGoldenAndBudget/{claude,codex}`, `TestMemory_SelectionAndBudget/{claude,codex}` |
| Resume only unseen memory                           | `TestMemory_ResumeOnlyUnseen/{claude,codex}`                                                      |
| Retire excludes later prompts, preserves history    | `TestMemory_RetireKeepsHistory/{claude,codex}/{mcp,rest}`                                         |
| Browser save and Retire                             | `ui/memory.spec.ts`                                                                               |
| Two saves: one "Saved 2 memories" line; open shows Details and Retire per memory; Retire works | `ui/memory.spec.ts` "a reply that saved 2 memories folds into one line; …" |
| Fakes work before implementation exists             | `TestMemoryFixtures_ProviderScenarios/{claude,codex}/{save,save-user,recall,check,resume,retire}` |

All acceptance boxes stay unticked: these tests intentionally fail against
Slices 1–3 until the missing memory APIs, prompt assembly and UI are implemented.
