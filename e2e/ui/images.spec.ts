// Images in the composer and thread. Contract: images-contract.md.
import { execFileSync } from "node:child_process";
import { mkdirSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { expect, type Page, test } from "@playwright/test";
import {
  api,
  type Machine,
  newBotAPI,
  prompts,
  sidebarRow,
  startAtoll,
} from "./atoll";
import { give, png, red } from "./image-fixture";

const showShot = "Show me the screenshot at shot.png.";

let m: Machine;
let repo: { id: string };
test.beforeAll(async () => {
  m = await startAtoll();
  const path = join(m.dir, "seeded");
  mkdirSync(path);
  writeFileSync(join(path, "shot.png"), png(8, [200, 30, 30]));
  const git = (...args: string[]) =>
    execFileSync("git", args, {
      cwd: path,
      env: {
        ...process.env,
        GIT_AUTHOR_NAME: "Fixture",
        GIT_AUTHOR_EMAIL: "fixture@example.test",
        GIT_COMMITTER_NAME: "Fixture",
        GIT_COMMITTER_EMAIL: "fixture@example.test",
        GIT_CONFIG_NOSYSTEM: "1",
      },
    });
  git("init", "-b", "main");
  git("add", "shot.png");
  git("commit", "-m", "seed");
  repo = await api(m, "POST", "/api/repos", { name: "seeded", path });
});
test.afterAll(() => m?.stop());

const box = (page: Page) => page.getByRole("textbox", { name: "Message" });

test("a pasted or dropped image shows a removable thumbnail", async ({
  page,
}) => {
  await newBotAPI(m, "Pasta");
  await page.goto(m.url);
  await sidebarRow(page, "Pasta").click();
  const thumbs = page.getByTestId("composer-attachment");

  await give(box(page), "paste", red);
  await expect(thumbs).toHaveCount(1);
  await give(box(page), "drop", red);
  await expect(thumbs).toHaveCount(2);

  await thumbs.first().getByRole("button", { name: "Remove image" }).click();
  await expect(thumbs).toHaveCount(1);
});

test("a file over 5 MB or a non-image file is refused with a message", async ({
  page,
}) => {
  await newBotAPI(m, "Picky");
  await page.goto(m.url);
  await sidebarRow(page, "Picky").click();

  await give(box(page), "paste", {
    name: "big.png",
    type: "image/png",
    size: 5 * 1024 * 1024 + 1,
  });
  await expect(page.getByRole("alert")).toBeVisible();
  await expect(page.getByTestId("composer-attachment")).toHaveCount(0);

  await page.reload();
  await sidebarRow(page, "Picky").click();
  await give(box(page), "drop", {
    name: "notes.txt",
    type: "text/plain",
    size: 12,
  });
  await expect(page.getByRole("alert")).toBeVisible();
  await expect(page.getByTestId("composer-attachment")).toHaveCount(0);
});

test("a sent image shows in the message, survives reload and opens full size", async ({
  page,
}) => {
  const bot = await newBotAPI(m, "Viewer");
  await page.goto(m.url);
  await sidebarRow(page, "Viewer").click();
  await give(box(page), "paste", red);
  await expect(page.getByTestId("composer-attachment")).toHaveCount(1);
  await box(page).fill(prompts.keep);
  await page.getByRole("button", { name: "Send" }).click();

  const log = page.getByRole("log", { name: "Messages" });
  const attached = log.getByTestId("message-attachment");
  await expect(attached).toHaveCount(1);
  await expect(page.getByTestId("composer-attachment")).toHaveCount(0);
  await expect(log.getByText("ok", { exact: true })).toBeVisible();

  const [thread] = await api(m, "GET", `/api/bots/${bot.id}/threads`);
  await page.goto(`${m.url}/bots/${bot.id}/threads/${thread.id}`);
  await expect(attached).toHaveCount(1);

  const dialog = page.getByRole("dialog", { name: "Image" });
  await attached.click();
  await expect(dialog.locator("img")).toBeVisible();
  await page.keyboard.press("Escape");
  await expect(dialog).toHaveCount(0);

  await attached.click();
  await expect(dialog).toBeVisible();
  await page.mouse.click(2, 2);
  await expect(dialog).toHaveCount(0);
});

test("an image in a reply loads from the work folder and opens full size", async ({
  page,
}) => {
  const bot = await newBotAPI(m, "Shots", "claude", { repo_access: [repo.id] });
  const thread = await api(m, "POST", `/api/bots/${bot.id}/threads`, {
    text: showShot,
    repo_id: repo.id,
  });
  await expect
    .poll(
      async () => (await api(m, "GET", `/api/threads/${thread.id}`)).run_status,
    )
    .toBe("done");

  await page.goto(`${m.url}/bots/${bot.id}/threads/${thread.id}`);
  const img = page
    .getByRole("log", { name: "Messages" })
    .getByTestId("message")
    .nth(1)
    .locator("img");
  await expect(img).toHaveCount(1);
  await expect
    .poll(() => img.evaluate((el: HTMLImageElement) => el.naturalWidth))
    .toBeGreaterThan(0);

  await img.click();
  await expect(
    page.getByRole("dialog", { name: "Image" }).locator("img"),
  ).toBeVisible();
});
