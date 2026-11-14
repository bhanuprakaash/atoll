// While a Bot works the thread shows one status line beside the creature: the Bot's latest line of
// text, or the tool step it is on. No step lines, no streamed text block. The reply appears once,
// as a message, when the run ends; a reload shows the same status line; the sidebar row and the
// island keep the short verb; a reader scrolled up is not moved.
import { connect, createServer, type Socket } from "node:net";
import type { AddressInfo } from "node:net";
import { expect, test, type Locator, type Page } from "@playwright/test";
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

// What the status line reads while narrate's command runs: Claude's Bash call carries a
// description, Codex's command does not.
const toolStatus = { claude: "Run the checks", codex: "Running make" };

const checkCI = "Check CI on PR 398 and tell me what failed.";
const ciReply = "The lint job failed because the generated client is out of date.";

let m: Machine;
test.beforeAll(async () => {
  m = await startAtoll({
    fakes: ["claude", "codex"],
    signedIn: ["claude", "codex"],
  });
});
test.afterAll(() => m?.stop());

const count = (text: string, part: string) => text.split(part).length - 1;

async function openRun(
  page: Page,
  name: string,
  provider = "claude",
  text = narrate,
) {
  const bot = await newBotAPI(m, name, provider);
  const thread = await openThreadAPI(m, bot.id, text);
  await page.goto(`${m.url}/bots/${bot.id}/threads/${thread.id}`);
  const log = page.getByRole("log", { name: "Messages" });
  return { bot, thread, log, status: log.getByRole("status") };
}

const replies = (log: Locator, text = closing) =>
  log.getByTestId("message").filter({ hasText: text });

// watchNotes records whether a step line or a streamed text block is ever drawn in the log, so a
// test can prove they never appeared, not only that they are gone.
async function watchNotes(log: Locator) {
  await log.evaluate((el) => {
    const w = window as unknown as { __notes: number };
    w.__notes = 0;
    const look = () => {
      if (el.querySelector('[data-testid="live-text"], [data-testid="step"]'))
        w.__notes++;
    };
    look();
    new MutationObserver(look).observe(el, {
      subtree: true,
      childList: true,
      characterData: true,
      attributes: true,
    });
  });
}

const notesDrawn = (page: Page) =>
  page.evaluate(() => (window as unknown as { __notes: number }).__notes);

// oneLine reads how the status line is cut: it is one line high, and a longer text ends in "…",
// either in the text itself or clipped by the box with an ellipsis.
async function oneLine(status: Locator) {
  return status.evaluate((el) => {
    const fontSize = parseFloat(getComputedStyle(el).fontSize);
    const text = (el.textContent ?? "").trim();
    const clipped = [el, ...el.querySelectorAll("*")].some(
      (e) =>
        getComputedStyle(e).textOverflow === "ellipsis" &&
        e.scrollWidth > e.clientWidth,
    );
    return {
      oneLine: el.getBoundingClientRect().height < 2 * fontSize,
      cut: text.endsWith("…") || clipped,
    };
  });
}

for (const provider of ["claude", "codex"] as const) {
  test(`${provider}: the thread never draws step lines or streamed text while the Bot works`, async ({
    page,
  }) => {
    const { log, status } = await openRun(page, `Plain log ${provider}`, provider);
    await expect(status).toBeVisible();
    await watchNotes(log);

    await expect(replies(log)).toHaveCount(1, { timeout: 30_000 });
    await expect(log.getByTestId("live-text")).toHaveCount(0);
    await expect(log.getByTestId("step")).toHaveCount(0);
    expect(await notesDrawn(page), "no step or live text was ever drawn").toBe(0);
  });

  test(`${provider}: the status line shows the newest text, then the step, then the next text`, async ({
    page,
  }) => {
    const { log, status } = await openRun(page, `Streamer ${provider}`, provider);

    await expect(status).toHaveText(openingStart);
    await expect(status).toHaveText(opening);
    await expect(
      status,
      provider === "claude"
        ? "a command with a description shows it alone, with no verb"
        : "a command without a description shows Running and its program",
    ).toHaveText(toolStatus[provider]);
    await expect(status).not.toHaveText(toolStatus[provider]);
    await expect(
      replies(log, closingStart),
      "the reply is not a message before the run ends",
    ).toHaveCount(0);

    await expect(replies(log)).toHaveCount(1);
    await expect(status).toHaveCount(0);
  });

  test(`${provider}: the reply lands once, as a message, when the run ends`, async ({
    page,
  }) => {
    const { log, status } = await openRun(page, `Replacer ${provider}`, provider);
    await expect(status).toHaveText(openingStart);
    await log.evaluate(
      (el, [start, full]) => {
        const w = window as unknown as {
          __samples: { replies: number; closing: number; notes: boolean }[];
        };
        w.__samples = [];
        const sample = () => {
          w.__samples.push({
            replies: [
              ...el.querySelectorAll('[data-testid="message"]'),
            ].filter((e) => (e as HTMLElement).innerText.includes(full))
              .length,
            closing: el.innerText.split(start).length - 1,
            notes: !!el.querySelector(
              '[data-testid="live-text"], [data-testid="step"]',
            ),
          });
        };
        new MutationObserver(sample).observe(el, {
          subtree: true,
          childList: true,
          characterData: true,
        });
      },
      [closingStart, closing],
    );

    await expect(replies(log)).toHaveCount(1, { timeout: 30_000 });
    await expect(status).toHaveCount(0);
    await expect(log).not.toContainText(openingStart);
    await expect(replies(log)).not.toContainText(openingStart);
    expect(count(await log.innerText(), closingStart)).toBe(1);

    const samples = await page.evaluate(
      () =>
        (
          window as unknown as {
            __samples: { replies: number; closing: number; notes: boolean }[];
          }
        ).__samples,
    );
    expect(
      Math.max(...samples.map((s) => s.replies)),
      "the reply is drawn as one message, never two",
    ).toBe(1);
    expect(
      Math.max(...samples.map((s) => s.closing)),
      "the closing text is never on screen twice",
    ).toBe(1);
    expect(
      samples.some((s) => s.notes),
      "no step or live text at any time",
    ).toBe(false);

    await page.reload();
    await expect(replies(log)).toHaveCount(1);
    await expect(log).not.toContainText(openingStart);
    await expect(log.getByTestId("live-text")).toHaveCount(0);
  });

  test(`${provider}: reloading mid-run shows the status line with the latest text or step`, async ({
    page,
  }) => {
    const { log, status } = await openRun(page, `Reloader ${provider}`, provider);
    await expect(status).toHaveText(opening);
    await page.reload();
    await expect(status).toHaveText(opening);

    await expect(status).toHaveText(toolStatus[provider]);
    await page.reload();
    await expect(status).toHaveText(toolStatus[provider]);

    await expect(status).not.toHaveText(toolStatus[provider]);
    await page.reload();
    await expect(status).toHaveCount(1);
    await expect(log.getByTestId("live-text")).toHaveCount(0);
    await expect(log.getByTestId("step")).toHaveCount(0);

    await expect(replies(log)).toHaveCount(1);
    await expect(status).toHaveCount(0);
  });

  test(`${provider}: a reconnect of the event stream keeps one status line with the latest text`, async ({
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
      const bot = await newBotAPI(m, `Reconnector ${provider}`, provider);
      const thread = await openThreadAPI(m, bot.id, narrate);
      const via = `http://127.0.0.1:${(proxy.address() as AddressInfo).port}`;
      await page.goto(`${via}/bots/${bot.id}/threads/${thread.id}`);
      const log = page.getByRole("log", { name: "Messages" });
      const status = log.getByRole("status");
      const reconnect = async () => {
        for (const s of sockets) s.destroy();
        await expect(page.getByText("Reconnecting")).toBeVisible();
        await expect(page.getByText("Reconnecting")).toHaveCount(0);
      };
      const known = [opening, toolStatus[provider]];

      await expect(status).toHaveText(opening);
      await reconnect();
      await expect
        .poll(async () => known.includes((await status.innerText()).trim()))
        .toBe(true);
      expect(count(await log.innerText(), openingStart)).toBeLessThanOrEqual(1);

      await expect(status).toHaveText(toolStatus[provider]);
      await reconnect();
      await expect(status).toHaveCount(1);
      expect(count(await log.innerText(), openingStart)).toBe(0);

      await expect(replies(log)).toHaveCount(1);
      await expect(status).toHaveCount(0);
      await expect(log.getByTestId("live-text")).toHaveCount(0);
    } finally {
      for (const s of sockets) s.destroy();
      proxy.close();
    }
  });
}

test("claude: a described command shows its description alone, any other command Running and its program; each text and each call replaces the last", async ({
  page,
}) => {
  const { log, status } = await openRun(page, "Narrator", "claude", checkCI);
  await expect(status).toBeVisible();
  await watchNotes(log);

  await expect(
    status,
    "the latest line of the streamed text",
  ).toHaveText("Checking its CI");
  await expect(status).not.toContainText("Looking at PR #398.");
  await expect(status).toHaveText("Checking its CI runs first.");

  await expect(
    status,
    "the Bash description, not Running <description>",
  ).toHaveText("Check CI on #398");

  await expect(
    status,
    "a text replaces the step",
  ).toHaveText(/^The lint job on #398 failed because/);
  await expect(status).not.toContainText("Check CI on #398");
  expect(await oneLine(status), "a long line is cut to one line with …").toEqual(
    { oneLine: true, cut: true },
  );

  await expect(status, "a Bash call without a description").toHaveText(
    "Running curl",
  );
  await expect(status).not.toContainText("The lint job on #398");

  await expect(replies(log, ciReply)).toHaveCount(1);
  await expect(status).toHaveCount(0);
  expect(await notesDrawn(page), "no step or live text was ever drawn").toBe(0);
});

test("scrolled up, the status line changing and the run ending do not move the view", async ({
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
  const status = log.getByRole("status");
  const moved = () =>
    log.evaluate((el, was) => Math.abs(el.scrollTop - was), before);
  await expect(status).toHaveText(openingStart);
  expect(await moved()).toBeLessThanOrEqual(2);
  await expect(status).toHaveText(opening);
  expect(await moved()).toBeLessThanOrEqual(2);
  await expect(status).toHaveText(toolStatus.claude);
  expect(await moved()).toBeLessThanOrEqual(2);
  await expect(status).not.toHaveText(toolStatus.claude);
  expect(await moved()).toBeLessThanOrEqual(2);
  await expect(replies(log)).toHaveCount(1);
  await expect(status).toHaveCount(0);
  expect(await moved()).toBeLessThanOrEqual(2);
  await expect(jump).toBeVisible();
});

test("the sidebar row and the island keep the short verb and never show the text", async ({
  page,
}) => {
  const { log, status } = await openRun(page, "Quiet");
  const row = sidebarRow(page, "Quiet");
  const header = page.locator("header");
  await Promise.all(
    [page.getByRole("navigation", { name: "Bots" }), header].map((part) =>
      part.evaluate((el) => {
        const w = window as unknown as { __shown?: string[] };
        const shown = (w.__shown ??= []);
        shown.push(el.innerText);
        new MutationObserver(() => shown.push(el.innerText)).observe(el, {
          subtree: true,
          childList: true,
          characterData: true,
        });
      }),
    ),
  );

  await expect(status).toHaveText(openingStart);
  await expect(row).toContainText("Thinking");
  await expect(header).toContainText("Thinking");

  await expect(status).toHaveText(toolStatus.claude);
  await expect(row).toContainText("Running");

  await expect(replies(log)).toHaveCount(1, { timeout: 30_000 });
  const shown = await page.evaluate(() =>
    (window as unknown as { __shown: string[] }).__shown.join("\n"),
  );
  for (const text of [openingStart, opening, closingStart]) {
    expect(shown, `never shows "${text}"`).not.toContain(text);
  }
});

test("a reef-metrics fence still streaming draws no streamed text, then the component once in the reply", async ({
  page,
}) => {
  const bot = await newBotAPI(m, "Gauge");
  const thread = await openThreadAPI(m, bot.id, "Stream the PR metrics live.");
  await page.goto(`${m.url}/bots/${bot.id}/threads/${thread.id}`);
  const log = page.getByRole("log", { name: "Messages" });
  await expect(log.getByRole("status")).toBeVisible();
  await watchNotes(log);

  await expect(replies(log, "All green.")).toHaveCount(1, { timeout: 30_000 });
  await expect(log.locator('[data-component="metrics"]')).toHaveCount(1);
  await expect(log.getByRole("status")).toHaveCount(0);
  await expect(log.getByTestId("live-text")).toHaveCount(0);
  expect(await notesDrawn(page), "no streamed text block was ever drawn").toBe(0);
});
