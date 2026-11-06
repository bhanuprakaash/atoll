# Slice 6 test contract

Extends the contracts of issues #2–#5 ([memory](memory-contract.md),
[skills](skills-contract.md)) for issue #7, the Mac app. Earlier scenarios, isolation, prompt
goldens, approvals, memory and platform events stay as they are. Go tests drive the real binary
over HTTP/SSE; Playwright specs drive the real UI the binary serves. Both use the scripted fake
providers. No product package is imported. `docs/design.md` → App and UI is the source; this file
fixes the names the tests use. JSON is snake_case with string IDs. Replies list only the fields
the tests read, and extra fields are allowed.

## Verified against the real CLIs

Probed on 2026-11-05 with **Claude Code 2.1.317** and **codex-cli 0.165.0**. Each provider ran
signed out, with a fresh `CLAUDE_CONFIG_DIR` / `CODEX_HOME`, no API key and no inference. Both
model lists come back offline:

- Claude: `claude -p --input-format stream-json --output-format stream-json --verbose`, stdin
  `{"type":"control_request","request_id":"r1","request":{"subtype":"initialize"}}`. The
  `control_response.response.response.models` is an array of
  `{value, resolvedModel, displayName, description, …}` (`default`, `opus`, `fable`, `sonnet`, …).
- Codex: `codex app-server`, `initialize` then `model/list`. `result.data` is an array of
  `{id, model, displayName, description, hidden, isDefault, …}`.

The fakes answer the same requests in the same shapes. Each fake list has a model only a fake
knows (`reef-probe` / "Reef Probe" for Claude, `reef-probe-codex` / "Reef Probe Codex" for Codex),
which proves a list came from the provider. A process that only lists models gets no user turn.
Tests count only processes that got a turn as sessions (`sessions()` in `provider_test.go`). The
fake Claude writes its prompt and skill logs at the first turn, so a probe adds no record to them.

`claude auth login` and `codex login` were **not** run: they open the real browser and sign in to
real accounts. The fakes model one assumption: the login prints its URL on stdout or stderr and
exits 0 once the browser step is done. Codex prints `http://localhost:1455` first, so the
contract is **the first `https://` URL** the login prints. The real behaviour is under "Only you
can check".

## REST additions

| Call | Body | Reply |
| --- | --- | --- |
| `GET /api/providers` | | `200` `[{name, label, found, path, version, signed_in, models: [{id, name}]}]`, `claude` then `codex` |
| `POST /api/providers/refresh` | | `200` the same list, checked again |
| `POST /api/providers/{name}/sign-in` | | `202` `{status, url, error}`; while one is going, the same sign-in, with no second login process |
| `GET /api/providers/{name}/sign-in` | | `200` `{status, url, error}` |
| `POST /api/bots/{id}/threads` | `{repo_id?, text?}` | `201` thread; with `text`, also `message` and `run` (the first message sent) |
| `GET /api/threads/{id}`, `GET /api/bots/{id}/threads` | | thread with `title`, `last_activity`, `run_id`, `run_status`, `approval` |
| `POST /api/runs/{id}/stop` | | `204`; `409` when the run has ended; `404` for an unknown run |
| `GET /api/bots`, `GET /api/bots/{id}` | | Bot with `look`, `model`, `state`, `activity`, `last_activity`, `repo_access`, `connections` |
| `POST /api/bots`, `PUT /api/bots/{id}` | adds `look`, `model` | `look` outside the ten creatures → `400` |
| `GET /api/runs/{id}` | | adds `started_at`, `finished_at`, `tools`; `status` may be `stopped` |

An unknown provider name gives `404`.

**Providers.** `label` is `Claude Code` or `Codex`. `version` is the bare number (`2.1.306`,
`0.166.0`). A provider not found has `found:false`, empty `path`, empty `models`. Models are
listed for every found provider, signed in or not. `id` is the provider's own id (Claude `value`,
Codex `id`) and `name` its display name. Sign-in runs `claude auth login` / `codex login` with
`CLAUDE_CONFIG_DIR` / `CODEX_HOME` set to Atoll's folder for that provider and `HOME` unchanged.
`status` goes `starting` → `waiting` (with `url`, the first `https://` URL printed) →
`signed_in`, or `failed` with `error`. After `signed_in`, `GET /api/providers` shows
`signed_in:true`.

**Bots.** `look` is a creature name: `jellyfish`, `whale`, `penguin`, `shark`, `octopus`,
`turtle`, `seahorse`, `otter`, `clownfish` or `manta`. Chief is `jellyfish`. A Bot created without
a look gets one of the ten. `model` is a model `id` from the Bot's provider. A run passes it to
the provider: Claude as `--model <id>`, Codex as `model` on `thread/start` / `thread/resume` (or in
their `config`). `state` is the first that holds:

| `state` | When |
| --- | --- |
| `needs_you` | A thread of the Bot waits on an approval |
| `working` | A run of the Bot is going |
| `failed` | The Bot's latest run failed |
| `asleep` | The Bot's provider is not found or not signed in |
| `idle` | Otherwise |

`activity` is the Bot's latest activity line for the sidebar. It is non-empty once the Bot has
run. `last_activity` is when that happened.

**Threads.** `title` is the first six whitespace-separated words of the thread's first message,
with trailing punctuation removed from the last word, or empty before one. `last_activity` is set from creation on.
`run_id` and `run_status` belong to the thread's latest run (empty with no run). `approval` is the
open approval `{id, tool, input}` the thread waits on, else `null`. The Bot's thread list is
newest `last_activity` first.

**Runs and tool kinds.** Each `tool_call` SSE event gains `kind` and `target`. The UI's working
line and summary come from these, so it never shows a provider's tool names.

| `kind` | Claude | Codex | `target` |
| --- | --- | --- | --- |
| `search` | Grep, Glob | `commandExecution` with `commandActions[0].type` `search` | the pattern or query |
| `read` | Read | `commandActions` `read` | file base name |
| `edit` | Edit, Write | `fileChange` | file base name (first change) |
| `command` | Bash | other `commandExecution` | the command (`commandActions[0].command`, without the shell wrapper) |
| `memory`, `platform`, `work`, `other` | Atoll's memory, platform, `start_work`/`track_mr` tools; anything else | | not tested here |

`tools` on the run counts the run's calls by kind; the tests read `search`, `read`, `edit` and
`command`. Atoll's own `finish_run` is not counted. `started_at` and `finished_at` bound the run.

**Stop.** Stopping a `running` or `waiting` run ends it with status `stopped`: `run_finished`
carries `status:"stopped"`, the run's provider process exits within 5 s (the fake ignores
interrupts, as a stuck command would), and an open approval closes (`POST /api/approvals/{id}` →
`404`). The thread then takes the next message.

## UI contract

Found by role and accessible name. Colours, sizes and motion curves are not tested.

- **Sidebar**: `navigation` "Bots". One `link` per Bot, Chief first, with its creature as an `img`
  named `<Creature>, <state>` (`Jellyfish, idle`, `Shark, needs you`, `Turtle, asleep`,
  `Shark, working`, `Shark, failed`). A needs-you row's activity names what it waits for. Also a
  `button` "New Bot" and a `link` whose name contains "Settings".
- **Bot page**: a `button` named `Threads` (when threads wait on the user, the name also carries
  their count, e.g. `Threads, 1 waiting`) and a `button` "Bot settings". Clicking a Bot with no
  threads opens its empty thread.
- **Threads panel**: `dialog` "Threads with <Bot>", with a `button` "New thread" and `list`s
  "Needs you", "Running", "Recent" of `listitem`s showing each title. A Needs you item also shows
  what it waits for, and opens its thread. Closed is folded: the `list` "Closed" appears after
  clicking the `button` whose name starts with "Closed".
- **Empty thread**: `heading` "What should <Bot> work on?"; a `group` "Starters" of `button`s
  (clicking one sends its text, without surrounding quotes, as the first message); `combobox` "Repo".
- **Chat**: `log` "Messages". The user's message is the only `article`, named "You". While a run
  works there is exactly one `status` in the log: `Searching <target>`, `Reading <target>`,
  `Editing <target>`, `Running <target>` by kind. No text the page ever shows matches a raw tool
  name (`Grep`, `Read`, `Edit`, `Write`, `Bash`, `ToolSearch`, `commandExecution`, `fileChange`,
  `mcpToolCall`, `mcp__…`, `finish_run`). After the run, a line matching
  `Worked <n>s · …` (or `<m>m <n>s`) containing "read 1 file" and "edited 1" sits above the
  reply and is still there after a reload.
- **Lines**: an approval shows its target, `button`s "Allow" and "Deny", and "Show diff" for an
  edit; once answered (Allow or Deny) the line leaves the log. `blocked` from `finish_run` shows
  `Blocked: <blocked>`. A stopped run shows `Stopped by you after <n>s`. A failed run shows
  `Run failed…` with a `button` "Retry". A dropped connection shows "Reconnecting".
- **Chips**: the MR is a `link` named with `!<iid>`, `href` the MR URL, showing its state
  ("Open"). A `group` "Thread context" in the composer shows repo, branch and MR. The composer
  shows `button` "Stop" instead of "Send" while a run goes. The memory chip keeps Slice 4's
  "Saved memory" region, now with its kind.
- **New Bot**: `heading` "New Bot", `textbox`es "Name" and "Persona", `radiogroup` "Creature" of
  `radio`s named by creature, `combobox` "Model", `region` "Preview", `button` "Add connection"
  opening `menuitem`s, `button` "Create Bot". Create opens the Bot's empty thread.
- **Model select**: `combobox` "Model". A native `<select>` uses one `optgroup` per provider label.
  Otherwise a listbox with a `group` per provider label and `option`s named by model name. Only
  signed-in providers are listed.
- **Bot settings**: `dialog` "Bot settings" with the same Name, Persona, Creature and Model
  controls, and a `button` "Close". Changes save without a Save button, show "Saved", and apply from the next run.
- **Settings**: `link`s "General", "Providers", "Connections", "Repos". General has `textbox`es
  "Your name" and "Worktree folder". Providers shows a `group` per provider label with path,
  version, model names and "Signed in" / "Not signed in" / "Not found", and a `button`
  "Sign in". Connections and Repos keep their Slice 3 names ("GitLab instance URL", "Sign in to
  GitLab", "Repo name", "Local path", "Register repo").
- **Sign in** opens the provider's URL in a new window (a popup in the browser) and updates when
  the login finishes.
- **First run** shows while the `user_name` setting is empty. Step 1: `heading` "Welcome to
  Reef", `textbox` "What should your Bots call you?", `button` "Continue". Step 2: a heading with
  the name, then a `group` per provider as on the Providers page. Step 3: `heading` "Meet
  Chief", a `group` "Example asks" of `button`s (each opens Chief's thread with its text, without surrounding quotes, sent)
  and a `button` "Open Chief".
- **Reduced motion**: with `prefers-reduced-motion: reduce`, `document.getAnimations()` has
  none running, on a page that otherwise animates (a working Bot, first run).

## Fakes and harness

- New scenarios (scripted, not real-model output): `working` for both providers
  (search → read → edit → command, each held 1 s by a `pause` directive, then `finish_run`), and
  Claude `fresh-fix` (a Bash step that asks approval, then the existing `repo-fix` directive).
- `claude auth login` and `codex login` print their URL, wait for `<executable>.login.release`,
  then write the login into the config folder.
- Claude answers `initialize` with models; Codex answers `model/list`.
- `e2e/ui/atoll.ts` builds once and starts a temporary machine per spec that needs its own state.
  The shared machine of the earlier specs now has `user_name` set, so first run is done there.
- `fresh-install.spec.ts` runs its server inside `e2e/with-secret-store`, since `make e2e` runs
  Playwright outside it.

## Earlier tests changed

The design overturns three things the Slice 2/4/5 UI specs asserted. They change in this commit:

- `bots.spec.ts`: Approve/Reject → Allow/Deny; the raw `Bash`/`Read` names it expected are now
  forbidden and replaced by the working line; the model is picked from the grouped select; Create
  opens the empty thread.
- `memory.spec.ts`: model select and Create as above; also checks the chip shows its kind.
- `skills.spec.ts`: New thread is in the Threads panel.
- `provider_test.go`: `sessions()` counts only processes that got a user turn, so a model probe
  is not a session.

## Sidecar lifecycle contract

Reef starts its sidecar as `atoll serve --addr 127.0.0.1:4317 --exit-on-stdin-close` and keeps
the write end of its stdin pipe open. If Reef is force-quit, closing that pipe makes the sidecar
exit within 3 seconds. Without the flag, stdin EOF does not stop `atoll serve`. A non-Atoll
listener on the requested address makes `atoll serve` exit non-zero within 3 seconds and write
`<addr> is in use by another program` to stderr. SIGTERM stops the sidecar cleanly; after restart,
no interrupted run remains running.

## Acceptance mapping

| Acceptance (issue #7) | Tests |
| --- | --- |
| Fresh install → provider and GitLab sign-in, repo, fix, approve, MR link; no terminal | `ui/fresh-install.spec.ts`; parts: `TestProviders_SignIn/{claude,codex}`, `TestProviders_List`, `TestProviders_RefreshFindsNewInstall`, `ui/first-run.spec.ts`, `ui/settings.spec.ts`; real providers and gitlab.com: Only you can check |
| Killed server comes back; the open thread recovers | `TestRestart_ThreadRecovers/{claude,codex}`, `ui/recover.spec.ts`; Tauri restarting the sidecar: Only you can check |
| Sidebar states on the creatures | `TestBots_State`, `ui/sidebar.spec.ts` (rows), `ui/chat.spec.ts` (needs you, failed) |
| Waiting thread under Needs you with what it waits for; Threads button counts it | `TestThreads_Summary/{claude,codex}`, `ui/threads.spec.ts` |
| One working line with verb and target, no raw tool calls; summary then reply; user message the only bubble | `TestRun_ToolKinds/{claude,codex}`, `ui/chat.spec.ts`, `ui/bots.spec.ts` |
| Stop ends a running run; chat says stopped | `TestRun_Stop/{claude,codex}`, `TestRun_StopWhileWaiting/{claude,codex}`, `ui/chat.spec.ts` |
| Reduced motion stops the animations | `ui/sidebar.spec.ts`, `ui/first-run.spec.ts` |
| Scope: Bot look and model, repos and connections returned, models per provider | `TestBots_LookAndModel/{claude,codex}`, `TestProviders_List`, `ui/new-bot.spec.ts` |
| Scope: Bot settings panel, New Bot, Settings | `ui/new-bot.spec.ts`, `ui/settings.spec.ts` |
| Fakes work before the product | `TestMacAppFixtures_Models`, `TestMacAppFixtures_Login`, `TestMacAppFixtures_Working` (each `{claude,codex}`) |
| Sidecar exits with its force-quit parent and reports an occupied port | `TestServe_ExitsWhenStdinCloses`, `TestServe_ReportsAddressAlreadyInUse`, `TestServe_SIGTERMLeavesNoRunningRunAfterRestart` |

## Only you can check

1. **Sidecar restart** (Tauri). Open the built app with a thread open. Run
   `pkill -9 -f 'atoll serve'`. Within a few seconds `pgrep -f 'atoll serve'` shows a new PID, the
   chat shows "Reconnecting" and then the thread again, and a new message gets a reply.
2. **Bundle and data**. `defaults read /Applications/Reef.app/Contents/Info.plist
   CFBundleIdentifier` names `atoll`. App data appears under
   `~/Library/Application Support/Atoll`.
3. **Signed dmg** (atoll-mac runner). `spctl -a -vv -t install Reef.dmg` reports `accepted` with
   your Developer ID. `codesign --verify --deep --strict -v /Applications/Reef.app` passes.
   Opening it from Finder shows no Gatekeeper warning.
4. **Real provider sign-in**. On the Providers page, with Claude Code and Codex installed and
   signed out in Reef: click Sign in for each. The browser opens the provider's page. After you
   finish there, the row shows Signed in with no Terminal. `ls ~/Library/Application\
   Support/Atoll/providers/{claude,codex}` holds the login, and your own `claude auth status` /
   `codex login status` is unchanged. Note whether `claude auth login` needed a TTY or printed no
   `https://` URL: that breaks the assumption above.
5. **Real GitLab and real fix**. Settings → Connections with `https://gitlab.com`. Enter the shown
   code at gitlab.com. Register a repo of yours with a known bug. Ask a Bot to fix it, Allow its
   step, and open the MR link it posts. The MR exists on gitlab.com.
6. **Real models**. The Model select lists the models `claude` and `codex` report (not the fake
   `Reef Probe`), grouped by provider.
7. **Force-quit and relaunch**. Open Reef.app, run `kill -9 <Reef.app PID>`, then relaunch it.
   The app appears within 5 seconds.
8. **Occupied sidecar port**. Start another program listening on `127.0.0.1:4317`, launch
   Reef.app, and confirm its window shows the port error.

## Known limits

- On macOS, specs and Go tests that save a GitLab token (`fresh-install.spec.ts`,
  `TestRepo_FixPushMRAndMerge`, `TestSkills_ChiefConfiguresAndUndoes`) cannot pass locally. The
  harness sets `HOME` to a temporary folder with no keychain, so the store fails, and outside a
  sandbox macOS shows a "Keychain Not Found" dialog (choose Cancel). Linux CI is the referee.
- Not pinned: verb wording for `memory`, `platform` and `work` kinds, `Thinking`, the exact
  `activity` text, timestamps' format beyond RFC 3339, the creature picked when none is given, the
  skill editor, and the creature motion per tool kind (only reduced motion is tested).
