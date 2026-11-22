// A file path a Bot writes as inline code is a link; a click opens the file from the thread's
// checkout in the right panel, read through GET /api/threads/{id}/file, scrolled to the line. The
// Bot reply plays testdata/claude/file-links.jsonl. The repo is github-fixture.ts's, with the files
// below committed on top, so the reply's PR link opens the PR panel against the fake GitHub.
import { execFileSync } from "node:child_process";
import { mkdirSync, mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import { expect, type Locator, type Page, test } from "@playwright/test";
import { api, type Machine, newBotAPI, startAtoll, waitStatus } from "./atoll";
import {
  githubEnv,
  type GitHubFake,
  seedRepo,
  startGitHub,
} from "./github-fixture";
import {
  box,
  delegate,
  helperThreads,
  makeBoss,
  send,
} from "./helpers-fixture";
import { png } from "./image-fixture";

const filePrompt = "Show me where the file panel code lives.";
const notPaths = [
  "make check",
  "finish_run",
  "m.mu",
  "origin/main",
  "e.g.",
  "feat/slug",
];

let m: Machine;
let github: GitHubFake;
let root = "";
let bot: { id: string };
let repo: { id: string; name: string };
let connection: { id: string };

const git = (dir: string, ...args: string[]) =>
  execFileSync("git", args, {
    cwd: dir,
    encoding: "utf8",
    env: {
      ...process.env,
      GIT_AUTHOR_NAME: "Fixture",
      GIT_AUTHOR_EMAIL: "fixture@example.test",
      GIT_COMMITTER_NAME: "Fixture",
      GIT_COMMITTER_EMAIL: "fixture@example.test",
      GIT_CONFIG_NOSYSTEM: "1",
    },
  }).trim();

const numbered = (name: string, n: number) =>
  Array.from({ length: n }, (_, i) => `// ${name} line ${i + 1}\n`).join("");

// seedFiles commits what the reply's paths point at into the repo at dir.
function seedFiles(dir: string) {
  const files: Record<string, string | Buffer> = {
    "go.mod": "module example.test/hello\n",
    "server/attachments.go": numbered("attachments", 200),
    "run/work.go": numbered("work", 600),
    "cmd/a/main.go": "// a main\n",
    "cmd/b/main.go": "// b main\n",
    "data/blob.txt": Buffer.from("text first\0then a NUL byte\n", "binary"),
    "data/huge.txt": "a".repeat(1024 * 1024 + 1),
    "shots/x.png": png(16, [30, 120, 200]),
  };
  for (const [path, content] of Object.entries(files)) {
    mkdirSync(dirname(join(dir, path)), { recursive: true });
    writeFileSync(join(dir, path), content);
  }
  git(dir, "add", ".");
  git(dir, "commit", "-m", "seed files");
}

test.beforeEach(async () => {
  github = await startGitHub();
  root = mkdtempSync(join(tmpdir(), "atoll-file-panel-"));
  m = await startAtoll({
    fakes: ["claude", "gh"],
    secretStore: true,
    env: githubEnv(github),
  });
  const seeded = await seedRepo(m, github, root);
  repo = seeded.repo;
  connection = seeded.connection;
  seedFiles(join(root, "hello"));
  bot = await newBotAPI(m, "Coder", "claude", {
    repo_access: [repo.id],
    connections: [connection.id],
  });
});
test.afterEach(() => {
  m?.stop();
  github?.close();
  if (root) rmSync(root, { recursive: true, force: true });
});

const aside = (page: Page) => page.getByRole("complementary", { name: "File" });
const prAside = (page: Page) =>
  page.getByRole("complementary", { name: "Pull request" });
const messages = (page: Page) => page.getByRole("log", { name: "Messages" });
const handle = (page: Page) =>
  page.getByRole("separator", { name: "Resize panel" });
const prChip = (page: Page) =>
  messages(page).getByRole("link", { name: /^PR 1\b/ });
// pathLink is the clickable inline code: a link or a button named by the code's text.
const pathLink = (scope: Locator, text: string) =>
  scope
    .getByRole("link", { name: text, exact: true })
    .or(scope.getByRole("button", { name: text, exact: true }));
const line = (panel: Locator, n: number) => panel.locator(`[data-line="${n}"]`);
const tinted = (panel: Locator) => panel.locator("[data-tinted]");

// thread starts a thread whose first message plays the file-links reply and waits for it.
async function thread(
  b: { id: string } = bot,
  repoID: string | undefined = repo.id,
) {
  const t = await api(m, "POST", `/api/bots/${b.id}/threads`, {
    text: filePrompt,
    ...(repoID && { repo_id: repoID }),
  });
  await waitStatus(m, t.id, "done", 30_000);
  return await api(m, "GET", `/api/threads/${t.id}`);
}

// open goes to the thread and waits for the reply.
async function open(page: Page, t: { id: string; bot_id: string }) {
  await page.goto(`${m.url}/bots/${t.bot_id}/threads/${t.id}`);
  await expect(
    messages(page).getByText("make check", { exact: true }),
  ).toBeVisible();
}

async function click(page: Page, path: string) {
  await pathLink(messages(page), path).click();
  const panel = aside(page);
  await expect(panel).toBeVisible();
  return panel;
}

// checkout is the thread's folder: its worktree, else its read-only checkout.
async function checkout(id: string) {
  const t = await api(m, "GET", `/api/threads/${id}`);
  return (t.worktree as string) || join(m.data, "threads", id);
}

async function drag(page: Page, dx: number) {
  const b = await box(handle(page));
  const x = b.x + b.width / 2;
  const y = b.y + b.height / 2;
  await page.mouse.move(x, y);
  await page.mouse.down();
  await page.mouse.move(x + dx, y, { steps: 10 });
  await page.mouse.up();
}

async function expectWidth(panel: Locator, px: number) {
  await expect
    .poll(async () => {
      const w = (await panel.boundingBox())?.width ?? 0;
      return Math.abs(w - px) <= 1 ? px : w;
    })
    .toBe(px);
}

test("a path in a reply is a link; a click opens the whole file with that line tinted and in view", async ({
  page,
}) => {
  await page.setViewportSize({ width: 1280, height: 800 });
  const t = await thread();
  await open(page, t);
  await expect(
    pathLink(messages(page), "server/attachments.go:74"),
  ).toHaveCount(1);
  await expect(aside(page)).toHaveCount(0);

  const panel = await click(page, "server/attachments.go:74");
  await expect(panel).toContainText("server/attachments.go");
  await expect(panel.locator("[data-line]")).toHaveCount(200);
  await expect(line(panel, 74)).toContainText("// attachments line 74");
  await expect(tinted(panel)).toHaveCount(1);
  await expect(tinted(panel)).toHaveAttribute("data-line", "74");
  await expect(line(panel, 74)).toBeInViewport();
  await expect(line(panel, 1)).not.toBeInViewport();
  expect(page.url()).toBe(`${m.url}/bots/${t.bot_id}/threads/${t.id}`);
});

test("a range tints every line in it", async ({ page }) => {
  await page.setViewportSize({ width: 1280, height: 800 });
  await open(page, await thread());
  const panel = await click(page, "run/work.go:530-540");
  await expect(panel).toContainText("run/work.go");
  await expect(tinted(panel)).toHaveCount(11);
  await expect(tinted(panel).first()).toHaveAttribute("data-line", "530");
  await expect(tinted(panel).last()).toHaveAttribute("data-line", "540");
  await expect(line(panel, 530)).toBeInViewport();
});

test("a bare name with one tracked match opens it; one with several shows a pick list", async ({
  page,
}) => {
  await open(page, await thread());
  let panel = await click(page, "attachments.go:74");
  await expect(panel).toContainText("server/attachments.go");
  await expect(tinted(panel)).toHaveAttribute("data-line", "74");

  panel = await click(page, "main.go");
  await expect(pathLink(panel, "cmd/a/main.go")).toBeVisible();
  await expect(pathLink(panel, "cmd/b/main.go")).toBeVisible();
  await expect(panel.locator("[data-line]")).toHaveCount(0);
  await pathLink(panel, "cmd/b/main.go").click();
  await expect(panel).toContainText("cmd/b/main.go");
  await expect(line(panel, 1)).toContainText("// b main");
  await expect(panel).not.toContainText("// a main");
});

test("on a read-only checkout the header shows the path and at <short commit>; Close and Escape dismiss; the composer works while open", async ({
  page,
}) => {
  const t = await thread();
  const short = git(await checkout(t.id), "rev-parse", "--short=7", "HEAD");
  await open(page, t);
  let panel = await click(page, "go.mod");
  await expect(panel).toContainText("go.mod");
  await expect(panel.getByText(`at ${short}`, { exact: true })).toBeVisible();
  await expect(panel.getByRole("button", { name: "Reload" })).toBeVisible();
  await expect(line(panel, 1)).toContainText("module example.test/hello");

  await panel.getByRole("button", { name: "Close" }).click();
  await expect(aside(page)).toHaveCount(0);
  await click(page, "go.mod");
  await page.keyboard.press("Escape");
  await expect(aside(page)).toHaveCount(0);

  panel = await click(page, "go.mod");
  await send(page, "Thanks, reading it now.");
  await expect(
    messages(page).getByText("Thanks, reading it now.", { exact: true }),
  ).toBeVisible();
  await expect(panel).toBeVisible();
});

test("on a worktree the header shows the thread's branch; the file is read as it is now", async ({
  page,
}) => {
  const t = await thread();
  await api(m, "POST", `/api/threads/${t.id}/messages`, {
    text: 'Use MCP tools. [{"server":"atoll","tool":"start_work","args":{"branch":"fix/file-panel"}}]',
  });
  await waitStatus(m, t.id, "done", 30_000);
  const work = await checkout(t.id);
  expect(git(work, "branch", "--show-current")).toBe("fix/file-panel");
  writeFileSync(join(work, "go.mod"), "module example.test/edited\n");

  await open(page, t);
  const panel = await click(page, "go.mod");
  await expect(
    panel.getByText("fix/file-panel", { exact: true }),
  ).toBeVisible();
  await expect(panel).not.toContainText(/\bat [0-9a-f]{7}\b/);
  await expect(line(panel, 1)).toContainText("module example.test/edited");

  writeFileSync(join(work, "go.mod"), "module example.test/reloaded\n");
  await panel.getByRole("button", { name: "Reload" }).click();
  await expect(line(panel, 1)).toContainText("module example.test/reloaded");
});

test("dragging the left edge resizes within 360px and 60% of the window; the width survives a reload and is the PR panel width too", async ({
  page,
}) => {
  await page.setViewportSize({ width: 1600, height: 900 });
  github.open("Linked change");
  await open(page, await thread());
  let panel = await click(page, "go.mod");
  await expect(handle(page)).toBeVisible();
  const start = (await box(panel)).width;
  expect(start).toBeGreaterThan(500);
  expect(start).toBeLessThan(540);

  await drag(page, -2000);
  await expect
    .poll(async () => (await box(panel)).width)
    .toBeGreaterThan(start + 50);
  expect((await box(panel)).width).toBeLessThanOrEqual(1600 * 0.6 + 1);
  expect(Math.round((await box(panel)).x + (await box(panel)).width)).toBe(
    1600,
  );

  await drag(page, 2000);
  await expectWidth(panel, 360);

  await drag(page, -40);
  await expectWidth(panel, 400);

  await page.reload();
  await expect(
    messages(page).getByText("make check", { exact: true }),
  ).toBeVisible();
  panel = await click(page, "go.mod");
  await expectWidth(panel, 400);

  await panel.getByRole("button", { name: "Close" }).click();
  await prChip(page).click();
  await expect(prAside(page)).toBeVisible();
  await expectWidth(prAside(page), 400);
});

test("a file replaces the PR panel and a PR replaces the file panel", async ({
  page,
}) => {
  github.open("Linked change");
  await open(page, await thread());
  await prChip(page).click();
  await expect(prAside(page)).toBeVisible();

  await click(page, "go.mod");
  await expect(prAside(page)).toHaveCount(0);

  await prChip(page).click();
  await expect(prAside(page)).toBeVisible();
  await expect(aside(page)).toHaveCount(0);
});

test("a missing, a binary and a too large file each show a plain line, never raw JSON", async ({
  page,
}) => {
  await open(page, await thread());
  for (const [path, words] of [
    ["docs/gone.md", /^Not in this thread's checkout\.?$/],
    ["data/blob.txt", /^This is a binary file\.?$/],
    ["data/huge.txt", /^Too large to show here \(over 1 MB\)\.?$/],
  ] as const) {
    const panel = await click(page, path);
    await expect(panel.getByText(words)).toBeVisible();
    await expect(panel).not.toContainText(
      /[{}]|"code"|not_found|too_large|\b41[35]\b|\b404\b/,
    );
    await expect(panel.locator("[data-line]")).toHaveCount(0);
  }
});

test("an image path shows the image", async ({ page }) => {
  await open(page, await thread());
  const panel = await click(page, "shots/x.png");
  const img = panel.locator("img");
  await expect(img).toBeVisible();
  await expect
    .poll(() => img.evaluate((el: HTMLImageElement) => el.naturalWidth))
    .toBe(16);
  await expect(panel.locator("[data-line]")).toHaveCount(0);
});

test("inline code that is not a path is not a link, and fenced code blocks are unchanged", async ({
  page,
}) => {
  await open(page, await thread());
  const log = messages(page);
  for (const text of [...notPaths, "/api/threads/{id}/file"]) {
    await expect(log.getByText(text, { exact: true })).toBeVisible();
    await expect(pathLink(log, text)).toHaveCount(0);
  }
  for (const text of [
    "run/work.go:530-540",
    "attachments.go:74",
    "main.go",
    "shots/x.png",
  ])
    await expect(pathLink(log, text)).toHaveCount(1);
  const fenced = log.locator('[data-component="code"]');
  await expect(fenced).toHaveCount(1);
  await expect(fenced).toContainText("// server/attachments.go:74");
  await expect(fenced).toContainText("func threadFile() {}");
  await expect(fenced.getByRole("link")).toHaveCount(0);
  await expect(pathLink(fenced, "server/attachments.go:74")).toHaveCount(0);
});

test("a thread with no repo and a closed thread show no path links", async ({
  page,
}) => {
  const plain = await thread(bot, "");
  await open(page, plain);
  await expect(
    messages(page).getByText("server/attachments.go:74", { exact: true }),
  ).toBeVisible();
  await expect(
    pathLink(messages(page), "server/attachments.go:74"),
  ).toHaveCount(0);
  await expect(pathLink(messages(page), "go.mod")).toHaveCount(0);

  const closed = await thread();
  await api(m, "POST", `/api/threads/${closed.id}/close`);
  await open(page, closed);
  await expect(
    messages(page).getByText("server/attachments.go:74", { exact: true }),
  ).toBeVisible();
  await expect(
    pathLink(messages(page), "server/attachments.go:74"),
  ).toHaveCount(0);
  await expect(pathLink(messages(page), "go.mod")).toHaveCount(0);
});

test("a helper thread's path opens the helper's own checkout", async ({
  page,
}) => {
  test.setTimeout(120_000);
  const coder = { repo_access: [repo.id], connections: [connection.id] };
  const { poteto, bots } = await makeBoss(m, ["Tester"], { Tester: coder });
  const boss = await api(m, "POST", `/api/bots/${poteto.id}/threads`, {
    text: delegate([["Tester", filePrompt, repo.name]]),
  });
  const [helper] = await helperThreads(m, boss.id, 1);
  expect(helper.bot_id).toBe(bots.Tester.id);
  await waitStatus(m, helper.id, "done", 60_000);
  writeFileSync(
    join(await checkout(helper.id), "go.mod"),
    "module example.test/helper\n",
  );

  await open(page, await api(m, "GET", `/api/threads/${helper.id}`));
  const panel = await click(page, "go.mod");
  await expect(line(panel, 1)).toContainText("module example.test/helper");
});
