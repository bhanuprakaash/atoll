# App update test contract

Issue #221. Reef updates itself from GitHub Releases. The update itself (download, signature
check, replace the bundle, restart) runs in the Tauri shell, which the e2e harness does not start.
So the specs drive the real UI the binary serves and stand in for the shell: before the page loads,
they define `window.__TAURI_INTERNALS__` with an `invoke(cmd, args)` that answers the two commands
below. The web app calls the shell only through `window.__TAURI_INTERNALS__.invoke` (directly or
through `@tauri-apps/api/core`'s `invoke`, which calls it).

## Shell commands

| Command          | Args | Answer                                                                                                                             |
| ---------------- | ---- | ---------------------------------------------------------------------------------------------------------------------------------- |
| `check_update`   | none | `{current: "0.1.1", available: "0.1.2"}`, or `available: null` when up to date; rejects with a message string when the check fails |
| `install_update` | none | downloads, installs and restarts Reef, so on success the page goes away; rejects with a message string when it fails               |

## UI

Found by role and accessible name.

- **Desktop app** means `__TAURI_INTERNALS__` is defined. In a plain browser the app never calls
  `invoke` and shows no `Software update` group.
- In the desktop app, the app calls `check_update` once when it loads. When `available` is set,
  the sidebar `link` whose name contains `Settings` also contains `Update available`.
- **Settings > General** has a `group` named `Software update` showing `Reef <current>` and a
  `button` `Check for updates`. A check found at load shows in the group without a click.
- After a check: `Up to date`, or `Reef <available> is available` with a `button`
  `Update and restart`. A failed check shows `Couldn't check for updates. Try again.` and the
  message only inside a collapsed `Details` disclosure (`<details>`/`<summary>`).
- `Update and restart` calls `install_update`. While it runs, the button shows `Updating…` and is
  disabled. A failed install shows `Couldn't update. Try again.` with the message under `Details`,
  and the button is enabled again.

## Acceptance mapping

| Acceptance (issue #221)                      | Tests                                                                                                         |
| -------------------------------------------- | ------------------------------------------------------------------------------------------------------------- |
| Software update group shows the version      | `ui/app-update.spec.ts` "shows the version and finds an update"                                               |
| Check: up to date / available / failed       | `ui/app-update.spec.ts` "shows the version and finds an update", "says up to date", "explains a failed check" |
| Update and restart asks the shell to install | `ui/app-update.spec.ts` "update and restart installs"                                                         |
| Settings link says Update available          | `ui/app-update.spec.ts` "shows the version and finds an update"                                               |
| No group and no check in a plain browser     | `ui/app-update.spec.ts` "plain browser has no updater"                                                        |
| Release script and the real update           | Only you can check                                                                                            |

## Only you can check

1. **Release.** With `TAURI_SIGNING_PRIVATE_KEY` set, `scripts/release 0.1.1` publishes GitHub
   release `v0.1.1` with `Reef.app.tar.gz`, `Reef.app.tar.gz.sig` and `latest.json`, and
   `curl -sL https://github.com/bhanuprakaash/atoll/releases/latest/download/latest.json` shows
   version `0.1.1` with a `darwin-aarch64` entry.
2. **Real update.** Install Reef 0.1.1, publish 0.1.2, open Settings > General, click
   `Check for updates`, then `Update and restart`. Reef comes back showing `Reef 0.1.2`;
   `pgrep -fl 'atoll serve'` shows one sidecar; Bots and sign-ins are still there; no Gatekeeper
   prompt.
