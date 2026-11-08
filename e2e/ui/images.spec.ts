// Images in the composer and thread. Contract: images-contract.md.
import { execFileSync } from "node:child_process";
import { mkdirSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { deflateSync } from "node:zlib";
import { expect, type Locator, type Page, test } from "@playwright/test";
import {
  api,
  type Machine,
  newBotAPI,
  prompts,
  sidebarRow,
  startAtoll,
} from "./atoll";

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

// png is a size×size solid-colour PNG.
function png(size: number, [r, g, b]: number[]): Buffer {
  const crcTable = Array.from({ length: 256 }, (_, n) => {
    let c = n;
    for (let k = 0; k < 8; k++) c = c & 1 ? 0xedb88320 ^ (c >>> 1) : c >>> 1;
    return c >>> 0;
  });
  const crc = (buf: Buffer) => {
    let c = 0xffffffff;
    for (const byte of buf) c = crcTable[(c ^ byte) & 0xff] ^ (c >>> 8);
    return (c ^ 0xffffffff) >>> 0;
  };
  const chunk = (type: string, data: Buffer) => {
    const body = Buffer.concat([Buffer.from(type), data]);
    const out = Buffer.alloc(body.length + 8);
    out.writeUInt32BE(data.length, 0);
    body.copy(out, 4);
    out.writeUInt32BE(crc(body), body.length + 4);
    return out;
  };
  const ihdr = Buffer.alloc(13);
  ihdr.writeUInt32BE(size, 0);
  ihdr.writeUInt32BE(size, 4);
  ihdr.set([8, 2, 0, 0, 0], 8);
  const row = Buffer.concat([
    Buffer.from([0]),
    Buffer.from(Array(size).fill([r, g, b]).flat()),
  ]);
  return Buffer.concat([
    Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]),
    chunk("IHDR", ihdr),
    chunk("IDAT", deflateSync(Buffer.concat(Array(size).fill(row)))),
    chunk("IEND", Buffer.alloc(0)),
  ]);
}

const box = (page: Page) => page.getByRole("textbox", { name: "Message" });

// give fires a paste or drop of one file on the composer.
async function give(
  target: Locator,
  kind: "paste" | "drop",
  file: { name: string; type: string; bytes?: number[]; size?: number },
) {
  await target.evaluate(
    (el, { kind, file }) => {
      const data = new DataTransfer();
      const content = file.bytes
        ? new Uint8Array(file.bytes)
        : new Uint8Array(file.size ?? 0);
      data.items.add(new File([content], file.name, { type: file.type }));
      const init = { bubbles: true, cancelable: true };
      el.dispatchEvent(
        kind === "paste"
          ? new ClipboardEvent("paste", { ...init, clipboardData: data })
          : new DragEvent("drop", { ...init, dataTransfer: data }),
      );
    },
    { kind, file },
  );
}

const red = {
  name: "red.png",
  type: "image/png",
  bytes: [...png(8, [200, 30, 30])],
};

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
