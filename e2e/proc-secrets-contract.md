# Proc secrets test contract

For issue #173, against the same-user section in [`docs/design.md`](../docs/design.md) → Providers
(the design lands in its own docs PR: `scripts/check-tests` flags `e2e/` mixed with other files). No
product package is imported and nothing uses the network beyond local fakes. The providers are the
scripted fakes. Linux only: the specs read `/proc`; elsewhere they log one line and return.

## Requirements

- A Bot's shell can read no MCP credential of another live run from `/proc/*/environ` or
  `/proc/*/cmdline`: not the run's Reef MCP token, not the GitHub MCP bearer, not a header secret of
  an MCP server attached to the other Bot. Both providers.
- `GH_TOKEN` (and `OAUTH_TOKEN`) stay in the env of the Bot's own provider process and its tools:
  its shell needs them. This is the one secret of another run a same-user process can still read;
  the specs allow exactly that variable in exactly that process and nothing else.
- Reef's own env holds no login token after start: `GH_TOKEN`, `GITHUB_TOKEN`,
  `GH_ENTERPRISE_TOKEN`, `GITHUB_ENTERPRISE_TOKEN`, `GITLAB_TOKEN`, `GITLAB_ACCESS_TOKEN`,
  `OAUTH_TOKEN`, `ANTHROPIC_*`, `CLAUDE*`, `OPENAI_*`, `CODEX_*`. It keeps the three secret-store
  variables, `SSH_AUTH_SOCK`, `HOME`, `PATH` and its own `ATOLL_*` settings.
- Reef's `ATOLL_TOKEN` (the local API credential) is not in its env either, and the API still
  requires it.
- Each Bot still reaches its own MCP servers and GitHub.

## Fixtures

Scenarios `procs-hold` and `procs-scan` (recordings `testdata/{claude,codex}/procs-*.jsonl`, fake
directives in `fakes/fake/procs.go`):

- `procs-hold` (prompt "Hold the secrets watch.") runs the `github-probe` checks, calls `answer` on
  the `docs-remote` MCP server, writes `<provider>.procs.<pid>` beside the fake with the URL of its
  `atoll` server, then waits for `<provider>.procs.<pid>.release`. Its reply is JSON `{github, docs}`.
- `procs-scan` (prompt "Scan the process list for secrets of other runs. needles=<file>") is a
  Bot's shell command: for every `/proc/<pid>` it greps `environ` and `cmdline` for each line of
  the needles file and replies with `[{pid, kind, name}]`: the variable name for `environ`, nothing
  else, so the reply holds no secret. The needles reach `grep` through the file only.

The holder Bot has the GitHub connection and `docs-remote` (a header secret); the scanner Bot has
neither. The scan must find the holder's `GH_TOKEN` in the holder's process: a scan that cannot read
other processes would pass every other check.

## Acceptance mapping

| Requirement | Tests |
| --- | --- |
| Run A's shell finds none of run B's run token, GitHub token (other than `GH_TOKEN`) or header secret; B still reaches GitHub (`gh`, git push, GitHub MCP) and `docs-remote` | `TestProcSecrets_OtherRunReadsNoSecretOfAHeldRun/{claude,codex}` |
| Reef's env holds no login token and keeps what it needs; a Bot's shell finds no login value in any process | `TestProcSecrets_ReefEnvironHoldsNoLoginToken` |
| Reef's env holds no `ATOLL_TOKEN`; the API still answers 200 with the token and 401 without | `TestProcSecrets_ReefKeepsItsAPITokenOutOfEnviron` |

Not covered: macOS (`ps eww` shows a same-user process's env; the specs need `/proc`); a stdio MCP
server's own env (inherent: the server reads its secrets from it); the Bot's tool tokens above;
signals, ptrace and files the same user can read (design gaps, no spec); the `docs/design.md` text.
