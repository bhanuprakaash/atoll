# Reef (atoll): instructions for every agent

## Rules

1. One ticket per branch, named `<type>/<slug>` (`feat`, `fix`, `chore`, `docs`), with an MR to
   `main`. Never push `main`; never merge your own MR.
2. Smallest diff that closes the ticket. Nothing speculative: no option, layer or wrapper the ticket
   does not need now.
3. Conventional commits (`feat:`, `fix:`, `chore:`, `docs:`, `build:`): one-line subject, no ticket
   ids in the subject, no AI attribution anywhere (commits, MRs, issues, comments).
4. Prove every acceptance line by running the real thing (binary, API, app). "It compiles" is not
   proof. Put the command and what it showed in the MR under Proof.
5. A change to the design goes into [`docs/design.md`](docs/design.md) in the same MR. Do not
   reopen a settled decision; propose a new one with a reason.
6. Comments only for a constraint the code cannot show.
7. Go follows [`docs/go-style.md`](docs/go-style.md); the web app follows
   [`docs/react-style.md`](docs/react-style.md).

## What Reef is

A Mac app where you configure Bots and chat with them. A Bot answers questions, explores and
investigates; when the work needs code it changes it, tests it, opens an MR and reports back. A Go
server runs as a sidecar inside a Tauri app with a React UI; Claude Code and Codex are the agent
providers. Full design: [`docs/design.md`](docs/design.md).

The UI says Reef. The repo, Go module and binary say `atoll` until the old Reef retires.

## Layout

Target layout. Create a folder only when a ticket needs it.

```text
cmd/atoll/    entry point: wires packages, starts the server
server/       HTTP + SSE API for the UI, Reef's MCP tools
run/          one run: prompt, provider call, finish_run
provider/     Provider interface; provider/claude, provider/codex
memory/       memory items and search
repo/         registered repos and worktrees
skill/        skill library, placed per provider
connection/   sign-in, tokens in the OS secret store
store/        SQLite schema and queries
web/          React UI (Vite, TypeScript)
desktop/      Tauri shell
docs/         design.md, style guides
```

Allowed imports, Go (left may import right, never the reverse):

```text
cmd/atoll -> server -> run -> provider | memory | repo | skill | connection -> store
```

- Packages on the same level never import each other (`memory` does not import `repo`).
- `provider` and `store` import no package of ours; `provider/claude` and `provider/codex` import
  only `provider`.
- `web/` talks to Go only over HTTP and SSE. `desktop/` only starts the sidecar and loads `web/`.
- Inside `web/src`, modules never import other modules (see the React style).

## Commands

Needs Go 1.27, Node 22 and pnpm (`npm i -g pnpm@<version>`, the version pinned in `packageManager`
of `web/package.json`). Nothing else: golangci-lint and lefthook are pinned as tool dependencies in
`go.mod` and run through `go tool`.

```sh
go tool lefthook install   # once per clone: pre-commit hook (gofmt, prettier on staged files)
make check                 # format check, lint, vet, type-check, tests (pnpm install when needed)
make format                # gofmt -w and prettier --write
make run                   # builds the UI, then go run ./cmd/atoll serve
make e2e                   # e2e/: builds the real binary and drives it
```

To upgrade a tool: `go get -tool github.com/golangci/golangci-lint/v2/cmd/golangci-lint@<version>`
(same for `github.com/evilmartians/lefthook/v2`), then `go mod tidy`.

CI (`.gitlab-ci.yml`) runs `make check`, `make e2e` and `test-guard` on every MR. New issues and MRs
start from the templates in `.gitlab/`.

## Tests

- `e2e/` tests come from a ticket's acceptance lines, in their own commit, before the code, written
  by the user or a separate session. The implementing agent never edits `e2e/`.
- If a test looks wrong, stop and report it. Never edit, skip or weaken a test to make code pass.
- Unit tests are for fiddly logic. Add a case to an existing table-driven test, not a test per bug.
- `test-guard` (`scripts/check-tests`) fails an MR that mixes `e2e/` with other code, deletes a
  test, adds a skip or `.only(`, or edits CI. Never add `tests-approved`; only the user does.

## Definition of done

A ticket is done when:

- `make check` passes locally and in the MR pipeline.
- Every acceptance line is proven by running it, and the MR's Proof section shows the command and
  its output.
- Each proven line is ticked in the issue (`glab issue update <n> --description`). A line you
  could not prove stays unticked with a short note. A line only the user can check is listed under
  "Only you can check" with exact steps.
- Design changes are in `docs/design.md` in the same MR.
- Anything you started or made to test (servers, scratch files, temp branches) is removed.

## Slice verification

Work is planned as slices (GitLab issues labeled `slice`). A slice is not done when its last ticket
closes. It is done after a verification pass by an agent on Opus:

1. Re-run every acceptance line of every ticket against `main` with the real thing (binary, API,
   app) and the scripted fake providers; run the real-provider set once. Record each command and
   what it showed.
2. `interrogate` the slice diff (first branch point to `main`): standards, spec against
   `docs/design.md`, correctness, design.
3. `blast-radius` on what the slice changed that earlier slices depend on; prove the one fact it is
   safe because of by running code.
4. Run gremlins (`go run github.com/go-gremlins/gremlins/cmd/gremlins@v0.5.1 unleash <pkg>`) on
   the packages the slice changed. Each surviving mutant becomes a finding.
5. Every finding becomes a ticket or a written reason to skip it.
6. Close the slice with one comment: acceptance lines and proof, interrogate and blast-radius
   summaries, gremlins results, skipped findings with reasons.

Each slice issue carries this checklist in its Verification section.
