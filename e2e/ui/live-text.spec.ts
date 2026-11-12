// While a Bot works the thread shows what it says: its text streams under the status line with the
// finished tool steps between as quiet lines, the reply replaces the stream when the run ends, a
// reload shows the text so far, a reader scrolled up is not moved, and the sidebar row and island
// keep the short verb.
import { connect, createServer, type Socket } from "node:net";
import type { AddressInfo } from "node:net";
import { expect, test, type Page } from "@playwright/test";
import {
  api,
  type Machine,
  newBotAPI,
  openThreadAPI,
  prompts,
  sendMessageAPI,
  sidebarRow,
  startAtoll,
} from "./atoll";

const narrate = "Narrate the build check as you go.";
const opening = "Looking at the build config and the test setup first.";
const openingStart = "Looking at the build config";
const closingStart = "The build is green";
const closing = "The build is green and all 4 checks pass.";

let m: Machine;
test.beforeAll(async () => {
  m = await startAtoll({
    fakes: ["claude", "codex"],
    signedIn: ["claude", "codex"],
  });
});
test.afterAll(() => m?.stop());

const count = (text: string, part: string) => text.split(part).length - 1;

async function openRun(page: Page, name: string, provider = "claude") {
  const bot = await newBotAPI(m, name, provider);
  const thread = await openThreadAPI(m, bot.id, narrate);
  await page.goto(`${m.url}/bots/${bot.id}/threads/${thread.id}`);
  const log = page.getByRole("log", { name: "Messages" });
  return {
    bot,
    thread,
    log,
    live: log.getByTestId("live-text"),
    status: log.getByRole("status"),
  };
}

for (const provider of ["claude", "codex"]) {
  test(`${provider}: text streams under the status line, finished tool steps are quiet lines between`, async ({
    page,
  }) => {
    const { log, live, status } = await openRun(
      page,
      `Streamer ${provider}`,
      provider,
    );

    await expect(live).toContainText(openingStart);
    await expect(live).not.toContainText("test setup first");
    await expect(live).toContainText(opening);
    const [s, l] = await Promise.all([
      status.boundingBox(),
      live.boundingBox(),
    ]);
    expect(s!.y, "the status line is above the text").toBeLessThan(l!.y);

    await expect(status).toContainText("Running");
    const running = (await status.textContent())!.trim();
    await expect(
      live.getByTestId("step"),
      "the running step is the status line only",
    ).toHaveCount(0);

    await expect(live).toContainText(closingStart);
    const steps = live.getByTestId("step");
    await expect(steps).toHaveCount(1);
    await expect(steps).toHaveText(running);
    const text = await live.innerText();
    expect(text.indexOf(opening)).toBeGreaterThanOrEqual(0);
    expect(text.indexOf(opening)).toBeLessThan(text.indexOf(running));
    expect(text.indexOf(running)).toBeLessThan(text.indexOf(closingStart));
    expect(count(await log.innerText(), openingStart)).toBe(1);
  });

  test(`${provider}: the reply replaces the stream once, with no gap, and the working notes leave`, async ({
    page,
  }) => {
    const { log, live } = await openRun(page, `Replacer ${provider}`, provider);
    await expect(live).toContainText(openingStart);
    await log.evaluate((el, sentinel) => {
      const w = window as unknown as {
        __samples: { n: number; live: boolean }[];
      };
      w.__samples = [];
      const sample = () => {
        const n = el.innerText.split(sentinel).length - 1;
        w.__samples.push({
          n,
          live: !!el.querySelector('[data-testid="live-text"]'),
        });
      };
      new MutationObserver(sample).observe(el, {
        subtree: true,
        childList: true,
        characterData: true,
      });
    }, closingStart);

    const reply = log.getByTestId("message").filter({ hasText: closing });
    await expect(reply).toHaveCount(1);
    await expect(live).toHaveCount(0);
    await expect(log.getByTestId("step")).toHaveCount(0);
    await expect(log).not.toContainText(openingStart);
    await expect(reply).not.toContainText(openingStart);
    expect(count(await log.innerText(), closingStart)).toBe(1);

    const samples = await page.evaluate(
      () =>
        (window as unknown as { __samples: { n: number; live: boolean }[] })
          .__samples,
    );
    const first = samples.findIndex((s) => s.n > 0);
    expect(first, "the closing text was drawn").toBeGreaterThanOrEqual(0);
    expect(samples[first].live, "its first appearance is in the stream").toBe(
      true,
    );
    expect(
      samples.slice(first).map((s) => s.n),
      "once at every step: never twice, never a gap",
    ).toEqual(samples.slice(first).map(() => 1));

    await page.reload();
    await expect(reply).toHaveCount(1);
    await expect(log).not.toContainText(openingStart);
    await expect(live).toHaveCount(0);
  });

  test(`${provider}: reloading mid-run shows the text so far, once`, async ({
    page,
  }) => {
    const { log, live } = await openRun(page, `Reloader ${provider}`, provider);
    await expect(live).toContainText(opening);
    await page.reload();
    await expect(live).toContainText(opening);
    expect(count(await live.innerText(), openingStart)).toBe(1);

    await expect(live).toContainText(closingStart);
    const text = await live.innerText();
    expect(text.indexOf(opening)).toBeLessThan(text.indexOf(closingStart));
    await expect(live.getByTestId("step")).toHaveCount(1);
    await expect(
      log.getByTestId("message").filter({ hasText: closing }),
    ).toHaveCount(1);
  });
}

test("a reconnect of the event stream does not draw the text twice", async ({
  page,
}) => {
  const sockets = new Set<Socket>();
  const target = new URL(m.url);
  const proxy = createServer((client) => {
    const upstream = connect(Number(target.port), target.hostname);
    for (const s of [client, upstream]) {
      sockets.add(s);
      s.on("error", () => s.destroy());
      s.on("close", () => sockets.delete(s));
    }
    client.pipe(upstream).pipe(client);
  });
  await new Promise<void>((ok) => proxy.listen(0, "127.0.0.1", ok));
  try {
    const bot = await newBotAPI(m, "Reconnector");
    const thread = await openThreadAPI(m, bot.id, narrate);
    const via = `http://127.0.0.1:${(proxy.address() as AddressInfo).port}`;
    await page.goto(`${via}/bots/${bot.id}/threads/${thread.id}`);
    const live = page
      .getByRole("log", { name: "Messages" })
      .getByTestId("live-text");
    await expect(live).toContainText(opening);
    for (const s of sockets) s.destroy();
    await expect(page.getByText("Reconnecting")).toBeVisible();
    await expect(page.getByText("Reconnecting")).toHaveCount(0);
    await expect(live).toContainText(opening);
    expect(count(await live.innerText(), openingStart)).toBe(1);
  } finally {
    for (const s of sockets) s.destroy();
    proxy.close();
  }
});

test("scrolled up, streaming text and the run ending do not move the view", async ({
  page,
}) => {
  const bot = await newBotAPI(m, "Scroller");
  const thread = await api(m, "POST", `/api/bots/${bot.id}/threads`, {});
  for (let i = 0; i < 10; i++) await sendMessageAPI(m, thread.id, prompts.keep);
  await page.goto(`${m.url}/bots/${bot.id}/threads/${thread.id}`);
  const log = page.getByRole("log", { name: "Messages" });
  await expect(log.getByTestId("message")).toHaveCount(20);
  await log.evaluate((el) => (el.scrollTop = 100));
  const jump = page.getByRole("button", { name: "Jump to latest" });
  await expect(jump).toBeVisible();
  const before = await log.evaluate((el) => el.scrollTop);

  await sendMessageAPI(m, thread.id, narrate, "running");
  const live = log.getByTestId("live-text");
  const moved = () =>
    log.evaluate((el, was) => Math.abs(el.scrollTop - was), before);
  await expect(live).toContainText(openingStart);
  expect(await moved()).toBeLessThanOrEqual(2);
  await expect(live).toContainText(opening);
  expect(await moved()).toBeLessThanOrEqual(2);
  await expect(live).toContainText(closingStart);
  expect(await moved()).toBeLessThanOrEqual(2);
  await expect(
    log.getByTestId("message").filter({ hasText: closing }),
  ).toHaveCount(1);
  await expect(live).toHaveCount(0);
  expect(await moved()).toBeLessThanOrEqual(2);
  await expect(jump).toBeVisible();
});

test("the sidebar row and the island keep the short verb and never show the text", async ({
  page,
}) => {
  const { live, status } = await openRun(page, "Quiet");
  const row = sidebarRow(page, "Quiet");
  const header = page.locator("header");

  await expect(live).toContainText(openingStart);
  await expect(row).toContainText("Thinking");
  await expect(header).toContainText("Thinking");
  for (const part of [row, header]) {
    await expect(part).not.toContainText(openingStart);
  }

  await expect(live).toContainText(closingStart);
  const verb = (await status.textContent())!.trim();
  await expect(row).toContainText(verb);
  for (const part of [row, header]) {
    await expect(part).not.toContainText(openingStart);
    await expect(part).not.toContainText(closingStart);
  }
});

test("a reef-metrics fence still streaming shows Metrics… until it closes, then the component", async ({
  page,
}) => {
  const bot = await newBotAPI(m, "Gauge");
  const thread = await openThreadAPI(m, bot.id, "Stream the PR metrics live.");
  await page.goto(`${m.url}/bots/${bot.id}/threads/${thread.id}`);
  const log = page.getByRole("log", { name: "Messages" });
  const live = log.getByTestId("live-text");

  await expect(live).toContainText("Live numbers:");
  await expect(live.getByText("Metrics…", { exact: true })).toBeVisible();
  await expect(live).not.toContainText("Open PRs");
  await expect(live.locator('[data-component="code"]')).toHaveCount(0);

  await expect(live.locator('[data-component="metrics"]')).toHaveCount(1);
  await expect(live.getByText("Metrics…")).toHaveCount(0);
  await expect(log.locator('[data-component="metrics"]')).toHaveCount(1);
});
