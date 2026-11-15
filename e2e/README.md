# e2e

## What it is

A small black-box suite. It builds the real `atoll` binary and drives it over HTTP and SSE (Go tests)
or through the UI it serves (Playwright specs). No product package is imported.

- Providers are fakes: `claude`, `codex`, `glab`, `gh` and an MCP stdio server, in `e2e/fakes/`.
  They replay recordings from `e2e/testdata/{claude,codex}/*.jsonl`, chosen by the prompt text.
  They enforce no sandbox and no permission; they play what the real CLI was seen to do.
- A test runs on both providers only when it reads something they handle differently: argv, stdin,
  env, resume, approvals, steer, transports, isolation, sandbox, auth, recordings, usage limit,
  sign-in or cost; any other test runs on claude, with one doubled smoke test per feature.
- Each test gets a temporary machine: its own `home`, data folder and `bin` (the fakes). Nothing
  touches your real home, logins or repos.
- Nothing uses the network. Git remotes are local bare repos, and the GitHub, GitLab, Slack and
  OAuth servers are local `httptest` or Node servers. `ATOLL_*` knobs point the server at them.
- Fake logs and switches sit beside the fake executable (`<fake>.log`, `.log.prompts`,
  `.log.skills`, `mr-state`, `github-url`, `<fake>.login.release`).

## Run it

```sh
make e2e        # both halves, one after the other
make e2e-go     # Go tests, inside e2e/with-secret-store
make e2e-ui     # Playwright specs
```

One Go test:

```sh
e2e/with-secret-store go test -count=1 -run '^TestName$' ./e2e
```

One spec (after `make ui e2e/ui/node_modules`):

```sh
cd e2e/ui && pnpm exec playwright test <name>.spec.ts
```

The real-Claude tests carry the `realclaude` build tag. They need a signed-in `claude` on PATH, and
neither `make e2e` nor CI runs them:

```sh
go test -tags realclaude -run 'TestClaudeIsolation_Real|TestNoRepoShell_Real' ./e2e
```

## Secrets

`e2e/with-secret-store` gives Linux a private D-Bus session and an encrypted keyring (it needs
`dbus`, `gnome-keyring` and `libsecret-tools`) and checks store, lookup and clear before it runs the
command. On macOS it runs the command as is, and the UI harness makes a temporary login keychain.
Playwright runs outside the wrapper, so `fresh-install.spec.ts` starts its server inside it.

## Test first

A ticket's acceptance lines become tests before the code. They come in `test:` commits, written by
you or a separate session, and a commit that touches the locked files below touches only `e2e/`.
The agent that implements the ticket never edits them. If a test looks wrong, stop and report it;
never edit, skip or weaken it to get green. `test-guard` checks this on every PR.

## Locked and free

Locked:

- `e2e/*_test.go`
- `e2e/ui/*.spec.ts` and the harness, `e2e/ui/*.ts`
- `e2e/with-secret-store`
- `e2e/fakes/`

Free:

- `e2e/testdata/`. The implementer regenerates the golden files and reviews the change:
  `go test ./e2e -run '^TestMemory_PromptGoldenAndBudget$' -update -count=1`. CI never passes `-update`.
- Unit tests outside `e2e/`.

## Flaky tests

- UI e2e retries a failed spec once in CI; a spec that passes on retry is reported as flaky in the log.
- Every flaky report becomes a fix ticket.
- Go e2e has no retry. A Go flake means rerunning the failed job and filing a fix ticket.

## Where things live

- `e2e/*_test.go`: Go tests. `main_test.go` builds `atoll` once.
- `e2e/ui/`: Playwright. `atoll.ts` builds `atoll` and the fakes into a fresh folder and starts a
  temporary machine for each spec that needs its own state. The Tauri shell does not run here: the
  app update spec stands in for it with `window.__TAURI_INTERNALS__.invoke`.
- `e2e/fakes/`: the fakes; `fake/` holds what they share.
- `e2e/testdata/`: recordings in `claude/` and `codex/`, goldens in `golden/`, and old-database
  fixtures (`before-*.sql`).
- `e2e/skills-placement-probe.py`: checks against the installed CLIs where each provider finds
  skills.
