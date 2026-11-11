# GitHub repos test contract

Extends [github-connection-contract.md](github-connection-contract.md) and the repo flow of
[README.md](README.md) (Slice 3: `start_work`, `track_mr`, `mr_updated`) for issue #63. A repo whose
`origin` is on GitHub works like a GitLab repo: the Bot branches in its worktree, pushes and opens a
pull request with `gh`, and the thread shows the PR. Tests drive the real binary with the fake
GitHub (a `git http-backend` remote behind basic auth, plus a pull request route), the fake `gh`,
the scripted fake providers and real git; no product package is imported and nothing uses the
network. JSON is snake_case, ids are strings, replies may carry fields not listed here.

## What counts as GitHub

A repo is on GitHub when the host of its `origin` URL equals the host of the GitHub base URL:
`github.com` by default, the host and port of `ATOLL_GITHUB_URL` when it is set (so a test's
`http://127.0.0.1:41234/octo/hello.git` is "on github.com" next to `ATOLL_GITHUB_URL=http://127.0.0.1:41234`).
The origin may be any form git takes (`https://`, `http://`, `ssh://`, `git@host:owner/repo.git`);
the tests use the `http` form. No origin, a local path or another host is not GitHub.

`POST /api/repos` and `GET /api/repos` give every repo a `host`: `"github"` or `"gitlab"`. Everything
that is not GitHub is `"gitlab"`, as today every other repo is handled as a GitLab one. The server
reads the origin with `git remote get-url origin` when it registers or lists (either is fine);
nothing is fetched and nothing touches the network.

## A GitHub repo and the Bot's connection

- A Bot with a `connected` GitHub connection runs with the env of the GitHub connection contract
  (`GH_TOKEN`, the git credential helper scoped to the GitHub URL), in the provider process and in
  every `gh` and `git` it starts. `git push -u origin <branch>` from the worktree authenticates as
  `x-access-token`; `gh pr create` calls `POST <github>/repos/<owner>/<repo>/pulls` with the same token.
- `start_work` in a thread of a GitHub repo, for a Bot with no connected GitHub connection (no
  connection at all, or only GitLab's), is a tool error (`isError: true`) whose text contains
  `give this Bot the GitHub connection`, for example `This repo is on GitHub and this Bot has no
  GitHub connection: give this Bot the GitHub connection in its settings, then ask again.` It makes
  no worktree and no branch and emits no `worktree_created`. The run itself is not failed: the
  provider gets the text and the Bot tells the user, so the thread shows it and the user never meets
  a git auth error (`could not read Username`, `Authentication failed`).
- Only editing needs the connection. A question in a thread of a GitHub repo (no `start_work`) is
  answered from the read-only checkout as for any repo, with or without the connection.
- A connection that is `needs_sign_in` still stops the run with `GitHub needs sign-in`, before the
  provider starts, as in the GitHub connection contract.

## The pull request in the thread

The Bot calls `track_mr({url, title})` with the URL `gh pr create` printed, which is
`<github base>/<owner>/<repo>/pull/<number>` (`https://github.com/octo/hello/pull/7` for real). The
tool is the one GitLab uses: it records the URL and title on the thread with state `opened` and sends
`mr_updated` `{url, state: "opened", title}`; `GET /api/threads/{id}` has `branch`, `mr_url`,
`mr_state` and `mr_title`.

A tracked PR's state, merge and CI are read by the watcher, and wake the Bot: see
[pr-wake-contract.md](pr-wake-contract.md). Merging does not remove the worktree; it goes with
`POST /api/threads/{id}/close` as before. The server never runs `glab` for a thread of a GitHub repo,
`glab mr view` included. (The polling loop and `track_mr` run `glab mr view` for GitLab repos only.)

## UI

Roles and texts Playwright selects on.

- Settings → Repos: the row of a repo with `host: "github"` shows the tag `GitHub` (exact text, like
  the `Connected` tag of a connection), once per such repo. A repo with `host: "gitlab"` shows no tag.
- A thread with a tracked GitHub PR shows the same chip an MR has, in the `log` named `Messages` and
  as before in the `group` named `Thread context`: a `link` to the PR URL (`href` is the URL, opens in
  a new tab) named `#<number> <title> Open`, where `<number>` is the digits after the last `/pull/`
  of the URL. The state tag reads `Open`, `Merged` or `Closed` as for MRs. The context group holds the
  repo name, the branch and `#<number>`. An MR keeps `!<iid>` as today.

## Fakes

- `e2e/fakes/gh` takes `gh pr create --head <branch> [--base <branch>] --title <t> [--body <b>]`, the
  flags a Bot uses. Like gh it opens the PR in the repo named by the current directory's `origin`
  (the last two path segments of its URL, without `.git`), against base `main` when `--base` is
  absent. It calls `POST <github-url>/repos/<owner>/<repo>/pulls` with `Authorization: token <t>`
  (the token as in the GitHub connection contract) and the JSON `{title, body, head, base}`, prints
  the reply's `html_url` and exits 0, or exits 1 with the status (`Bad credentials (HTTP 401)`).
- The fake GitHub's `POST /repos/{owner}/{repo}/pulls` answers `401` to an invalid token and `422`
  without `head` or `title`; otherwise `201` `{number, state: "open", title, html_url}` with `number`
  counting from 1 and `html_url` `<fake GitHub URL>/<owner>/<repo>/pull/<number>`. It records each
  accepted request.
- Scripted scenario `repo-github-fix` (recordings `testdata/{claude,codex}/repo-github-fix.jsonl`,
  prompt `Fix the seeded bug and open a PR.`): the `repo-fix` scenario for a GitHub repo.
  `start_work` (`fix`, `Seeded BUG`), writes `answer.txt` `42`, commits, `git push -u origin
  <branch>`, `gh pr create --head <branch> --base main --title "fix: correct seeded answer" --body
  "Correct the fixture answer."`, then `track_mr` with `{url, title}` and `finish_run`. When
  `start_work` is a tool error it stops there and replies `start_work failed: <the error text>`.

## Acceptance mapping

| Acceptance | Tests |
| --- | --- |
| Registering a repo with a GitHub remote shows it as GitHub in Settings → Repos | `TestGitHubRepos_RepoShowsItsHost` (API `host`); `e2e/ui/github-repos.spec.ts` "Settings → Repos shows a repo on GitHub as GitHub and leaves others alone" |
| A Bot with repo access and the connection runs `start_work`, commits, pushes and opens a PR; the fake saw `POST /repos/<owner>/<repo>/pulls` with the thread's branch and base | `TestGitHubRepos_BotPushesAndOpensAPullRequest/{claude,codex}` |
| The thread shows the PR as it shows an MR (link, number) | same test (`mr_updated`, `GET /api/threads/{id}`, Bot reply); `e2e/ui/github-repos.spec.ts` "a thread shows the pull request a Bot opened with gh: link and number" |
| A GitHub repo without the connection fails with a plain "give this Bot the GitHub connection" message, not a git auth error | `TestGitHubRepos_WithoutTheConnectionStartWorkSaysWhatToDo/{claude,codex}` |
| GitLab repos are unchanged | the existing `TestRepo_FixPushMRAndMerge`, `TestRepo_QuestionCreatesNoWorktree`, `TestRepo_CloseSavesUncommittedWork`, `TestRepo_BranchCollision`, `TestRepo_OnlyTheseDeniesOtherRepo`, `TestRepo_StartWorkReply*`, `TestRepo_LostAccessRunsWithoutRepo`, the GitLab refresh tests and `e2e/ui/fresh-install.spec.ts` (`!1` link), all untouched; `TestGitHubRepos_RepoShowsItsHost` also pins that non-GitHub repos stay `gitlab` and untagged |
| The scenario, the fake `gh pr create` and the fake GitHub's pull request route work before the product does | `TestGitHubReposFixtures_ProviderScenario/{claude,codex}` (passes without the product) |

Not covered: the real github.com line (only the user can check it); the `docs/design.md` line (a
doc); the tests-land-first line (`test-guard`). The `git@github.com:` and `ssh://` origin forms are
not tested (under the test seam the GitHub host is `127.0.0.1:<port>`, and the credential helper
only serves HTTPS). Merge cleanup, reviews, GitHub Enterprise and a Bot with
both a GitLab and a GitHub connection are out of scope.
