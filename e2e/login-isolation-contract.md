# Login isolation test contract

Extends [skills-contract.md](skills-contract.md)'s provider isolation and the GitLab connection in
[README.md](README.md) for issue #61. A Bot reaches GitLab or GitHub only through a connection Reef
gave it, never through the user's own logins. No product package is imported and nothing uses the
network; the CLIs are fakes on the provider PATH, git is the real git.

## Rule under test

Every run of every Bot gets empty CLI configs and cleared credential helpers; only a connection
fills them. The tests do not say how: any way works that makes the three checks below pass in the
run, in the provider process and in what it starts. There is no new API, route or UI.

## Fixtures

The server's `HOME` is a temp folder (`XDG_CONFIG_HOME` unset, or in one case the temp
`$HOME/xdg`) holding the user's own logins:

- `<config>/glab-cli/config.yml`: `hosts: gitlab.com: token: fake-glab-token-1`
- `<config>/gh/hosts.yml`: `github.com: oauth_token: fake-gh-oauth-1`
- `$HOME/.gitconfig` with `credential.helper` set to a script that answers
  `username=fake-user` / `password=fake-git-password-1` for host `git.example`

`<config>` is `$HOME/.config`, or `$XDG_CONFIG_HOME` when set. The server's env also holds
`GH_TOKEN`, `GITHUB_TOKEN`, `GH_ENTERPRISE_TOKEN` and `GITHUB_ENTERPRISE_TOKEN` (`fake-gh-token-1`
to `-4`). Before each run the test proves the logins are live: with that `HOME` (and no tokens) each
CLI finds its login, and `git credential fill` returns the password.

## Fakes

- `e2e/fakes/glab` gains `glab auth status`: logged in (exit 0, `Logged in to gitlab.com`) when
  `GITLAB_TOKEN`, `GITLAB_ACCESS_TOKEN` or `OAUTH_TOKEN` is set, or when `config.yml` in
  `$GLAB_CONFIG_DIR`, else `$XDG_CONFIG_HOME/glab-cli`, else `~/.config/glab-cli` has a non-empty
  `token:`; otherwise exit 1. It ignores a missing `config.yml`, as real glab does.
- `e2e/fakes/gh` is new and takes `gh auth status`: logged in (exit 0, `Logged in to github.com`)
  when any of the four token variables is set, or when `hosts.yml` in `$GH_CONFIG_DIR`, else
  `$XDG_CONFIG_HOME/gh`, else `~/.config/gh` has a non-empty `oauth_token:`; otherwise exit 1.
- Scripted scenario `logins-probe` (recordings `testdata/{claude,codex}/logins-probe.jsonl`, prompt
  `Check which logins this run has.`): in the provider process, with the env the server gave it,
  runs `glab auth status`, `gh auth status` and `git credential fill` (stdin
  `protocol=https`, `host=git.example`, `GIT_TERMINAL_PROMPT=0`), then replies with JSON
  `{"glab":{"exit":N,"output":"..."},"gh":{...},"git":{...}}`.

## What a run with no connection must show

- `glab auth status` exits nonzero and does not say `Logged in`, though `config.yml` has a token.
- `gh auth status` exits nonzero and does not say `Logged in`, though `hosts.yml` has a token.
- `git credential fill` for `git.example` returns no `password=` line.
- `GH_TOKEN`, `GITHUB_TOKEN`, `GH_ENTERPRISE_TOKEN` and `GITHUB_ENTERPRISE_TOKEN` are absent (not
  merely empty) from the env of the provider process and of every `glab` and `gh` it starts.

## Acceptance mapping

| Acceptance | Tests |
| --- | --- |
| No connection: `glab auth status` shows no login; `git credential fill` returns no password | `TestRun_UsesOnlyLoginsReefGives/{claude,codex,claude-xdg}` |
| Same run: `gh auth status` shows no login; the four token variables are absent | `TestRun_UsesOnlyLoginsReefGives/{claude,codex,claude-xdg}` |
| A Bot with the GitLab connection still pushes and runs `glab` as that connection | existing `TestRepo_FixPushMRAndMerge/{claude,codex}` (`OAUTH_TOKEN`, `GLAB_CONFIG_DIR` in the provider and in `glab mr create`; push to origin; MR tracked) |

Not covered: the `docs/design.md` line (a doc, not software behaviour) and the tests-land-first
line (`test-guard`). A real `gh`, `glab` or git credential helper on a real machine is not run.
