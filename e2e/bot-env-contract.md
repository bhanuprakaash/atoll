# Bot env test contract

For issue #160, against the env strip in [`docs/design.md`](../docs/design.md) → Providers. A Bot
must not reach Reef's secret store (Linux Secret Service) through its env. No product package is
imported and nothing uses the network; the providers are the scripted fakes, which log the env of
every process they start.

## Requirements

- `provider.Env` drops `DBUS_SESSION_BUS_ADDRESS`, `XDG_RUNTIME_DIR` and `DISPLAY` on every OS, for
  both providers. Both of the first two must go; the tests check all three are absent, not one.
- The server itself keeps its env and reads its tokens in-process. A Bot with a GitHub or GitLab
  connection still gets `GH_TOKEN`, or `OAUTH_TOKEN` and `GLAB_CONFIG_DIR`, in its run env.
- `XDG_DATA_HOME`, `XDG_CONFIG_HOME` and `XDG_CACHE_HOME` reach the provider as the server had them.

## Fixtures

The server's env holds the three variables: the private Secret Service session's own values where
a test stores a connection (so Reef can read its tokens), a sentinel for each otherwise
(`unix:path=/nonexistent/reef-bus`, `/nonexistent/reef-run`, `:99`) and a sentinel for each variable
missing from the machine (macOS sets none), so the tests do not depend on the OS. A fake provider
process counts as stripped when none of the three is in its env, even empty. The `--version` probe
of the installed provider runs no Bot and is not checked. The tests also fail when fewer provider
sessions ran than expected.

## Acceptance mapping

| Requirement | Tests |
| --- | --- |
| A Bot on Ask, Auto and Full gets none of the three, on its first run and on the resumed one, for both providers; `XDG_DATA_HOME`, `XDG_CONFIG_HOME`, `XDG_CACHE_HOME` unchanged | `TestBotEnv_NoSecretServiceInAnyMode/{claude,codex}/{auto,ask,full}` |
| Same on a repository thread | `TestBotEnv_NoSecretServiceOnARepoThread/{claude,codex}/{auto,ask,full}` |
| Same for Chief (and its title session) and for a helper a Bot delegated to | `TestBotEnv_NoSecretServiceForChiefAndHelpers/{claude,codex}` |
| What the Bot's shell starts (`gh`, `glab`) inherits none of the three | `TestBotEnv_ToolsRunWithoutSecretService/{claude,codex}` |
| A GitHub-connected Bot still gets `GH_TOKEN` and none of the other gh token variables, and `gh api user` works; none of the three | `TestBotEnv_ReefStillReadsItsGitHubToken/{claude,codex}` |
| A GitLab-connected Bot still gets `OAUTH_TOKEN` (provider and `glab mr create`); none of the three | `TestBotEnv_ReefStillReadsItsGitLabToken/{claude,codex}` |
| Reef's own process env is untouched: the existing keychain specs still pass | `TestGitHub_RunGetsTheConnection`, `TestRepo_FixPushMRAndMerge`, `TestGitLabRefresh_*` (their fixtures now share the three variables through `secretServiceEnv`) |

Not covered: a same-user process opening `/run/user/<uid>/bus` by path (a known gap in the design);
`secret-tool` inside a real provider (a private `dbus-run-session` probe in the fix's Proof); the
`docs/design.md` line (a doc).
