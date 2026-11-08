// GitHub repos in the browser: Settings → Repos shows a repo whose origin is on GitHub as GitHub,
// and a thread shows the pull request a Bot opened with gh as it shows an MR: link and number.
// GitHub is a local server and the repo's origin is a URL on it, as in the Go tests; the push
// itself goes to a local bare repo through the origin's pushurl. Contract: github-repos-contract.md.
import { execFileSync } from "node:child_process";
import { createServer, type Server } from "node:http";
import { mkdirSync, mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { expect, test } from "@playwright/test";
import {
  api,
  choose,
  type Machine,
  newBotAPI,
  sidebarRow,
  startAtoll,
} from "./atoll";

const fixPrompt = "Fix the seeded bug and open a PR.";
const prTitle = "fix: correct seeded answer";

let m: Machine;
let github: Server;
let githubURL = "";
let root = "";
let token = "";
let pulls: { head: string; base: string; title: string }[] = [];

function git(dir: string, ...args: string[]) {
  execFileSync("git", args, {
    cwd: dir,
    env: {
      ...process.env,
      GIT_AUTHOR_NAME: "Fixture",
      GIT_AUTHOR_EMAIL: "fixture@example.test",
      GIT_COMMITTER_NAME: "Fixture",
      GIT_COMMITTER_EMAIL: "fixture@example.test",
      GIT_CONFIG_NOSYSTEM: "1",
    },
  });
}

// seed makes a repo with answer.txt "41" on main and the given origin; the pushurl, when there is
// one, is where a push goes.
function seed(name: string, origin: string, pushurl?: string): string {
  const path = join(root, name);
  mkdirSync(path);
  git(path, "init", "-b", "main");
  writeFileSync(join(path, "answer.txt"), "41\n");
  git(path, "add", "answer.txt");
  git(path, "commit", "-m", "seed bug");
  git(path, "remote", "add", "origin", origin);
  if (pushurl) git(path, "config", "remote.origin.pushurl", pushurl);
  return path;
}

test.beforeAll(async () => {
  github = createServer((req, res) => {
    let body = "";
    req.on("data", (c) => (body += c));
    req.on("end", () => {
      const send = (status: number, value: unknown) => {
        const json = (req.headers.accept ?? "").includes("application/json");
        const flat = value as Record<string, string | number>;
        res.writeHead(status, {
          "Content-Type":
            json || status !== 200
              ? "application/json"
              : "application/x-www-form-urlencoded",
        });
        res.end(
          json || status !== 200
            ? JSON.stringify(value)
            : new URLSearchParams(
                Object.entries(flat).map(([k, v]) => [k, String(v)]),
              ).toString(),
        );
      };
      const authorised = [`Bearer ${token}`, `token ${token}`].includes(
        req.headers.authorization ?? "",
      );
      if (req.method === "POST" && req.url === "/login/device/code") {
        send(200, {
          device_code: "fixture-device-code",
          user_code: "ABCD-1234",
          verification_uri: "https://github.com/login/device",
          expires_in: 900,
          interval: 1,
        });
      } else if (
        req.method === "POST" &&
        req.url === "/login/oauth/access_token"
      ) {
        token = "fake-gho-ui-repos";
        send(200, {
          access_token: token,
          token_type: "bearer",
          scope: "repo,read:org,workflow",
        });
      } else if (req.method === "GET" && req.url === "/user") {
        send(
          authorised ? 200 : 401,
          authorised
            ? { login: "reef-fixture", id: 1 }
            : { message: "Bad credentials" },
        );
      } else if (
        req.method === "POST" &&
        req.url === "/repos/octo/hello/pulls"
      ) {
        if (!authorised) return send(401, { message: "Bad credentials" });
        pulls.push(JSON.parse(body));
        send(201, {
          number: pulls.length,
          html_url: `${githubURL}/octo/hello/pull/${pulls.length}`,
        });
      } else {
        res.writeHead(404).end("{}");
      }
    });
  });
  await new Promise<void>((ok) => github.listen(0, "127.0.0.1", ok));
  const addr = github.address();
  githubURL = `http://127.0.0.1:${typeof addr === "object" && addr ? addr.port : 0}`;
  m = await startAtoll({
    fakes: ["claude", "gh"],
    secretStore: true,
    env: {
      ATOLL_GITHUB_URL: githubURL,
      ATOLL_GITHUB_MCP_URL: `${githubURL}/mcp/`,
    },
  });
  writeFileSync(join(m.bin, "github-url"), githubURL);
  root = mkdtempSync(join(tmpdir(), "atoll-ui-github-repos-"));
});
test.afterAll(() => {
  m?.stop();
  github?.close();
  if (root) rmSync(root, { recursive: true, force: true });
});

test("Settings → Repos shows a repo on GitHub as GitHub and leaves others alone", async ({
  page,
}) => {
  await api(m, "POST", "/api/repos", {
    name: "octo-hello",
    path: seed("listed-hello", `${githubURL}/octo/hello.git`),
  });
  await api(m, "POST", "/api/repos", {
    name: "plain-repo",
    path: seed("listed-plain", "https://gitlab.example/fixtures/seeded.git"),
  });
  await page.goto(m.url);
  await page
    .getByRole("navigation", { name: "Bots" })
    .getByRole("link", { name: /Settings/ })
    .click();
  await page.getByRole("link", { name: "Repos" }).click();
  await expect(page.getByText("octo-hello", { exact: true })).toBeVisible();
  await expect(page.getByText("plain-repo", { exact: true })).toBeVisible();
  await expect(page.getByText("GitHub", { exact: true })).toHaveCount(1);
  await expect(
    page
      .locator("div")
      .filter({ has: page.getByText("octo-hello", { exact: true }) })
      .filter({ hasNot: page.getByText("plain-repo", { exact: true }) })
      .filter({ has: page.getByText("GitHub", { exact: true }) })
      .first(),
  ).toBeVisible();
});

test("a thread shows the pull request a Bot opened with gh: link and number", async ({
  page,
}) => {
  test.setTimeout(120_000);
  const bare = join(root, "hello.git");
  mkdirSync(bare);
  git(bare, "init", "--bare", "-b", "main");
  const path = seed("hello", `${githubURL}/octo/hello.git`, bare);
  git(path, "push", bare, "main");
  await api(m, "POST", "/api/repos", { name: "scratch-pr", path });

  const start = await api(m, "POST", "/api/connections/github/start");
  for (let i = 0; i < 5; i++) {
    await new Promise((ok) => setTimeout(ok, 1100));
    if (
      (await api(m, "POST", `/api/connections/${start.id}/poll`)).status ===
      "connected"
    )
      break;
  }
  await newBotAPI(m, "Octo", "claude", { connections: [start.id] });

  await page.goto(m.url);
  await sidebarRow(page, "Octo").click();
  await choose(page, page.getByRole("combobox", { name: "Repo" }), "scratch-pr");
  await page.getByRole("textbox", { name: "Message" }).fill(fixPrompt);
  await page.getByRole("button", { name: "Send" }).click();

  const log = page.getByRole("log", { name: "Messages" });
  const context = page.getByRole("group", { name: "Thread context" });
  const pr = context.getByRole("link", { name: "#1", exact: true });
  await expect(pr).toHaveAttribute("href", `${githubURL}/octo/hello/pull/1`, {
    timeout: 30_000,
  });
  await expect(pr).toHaveAttribute("title", prTitle);
  await expect(log.getByRole("link", { name: /#1\b/ })).toHaveCount(0);
  await expect(context).toContainText("scratch-pr");
  await expect(context).toContainText("fix/seeded-bug");
  await expect(context).toContainText("#1");
  expect(pulls).toEqual([
    {
      head: "fix/seeded-bug",
      base: "main",
      title: prTitle,
      body: expect.any(String),
    },
  ]);
});
