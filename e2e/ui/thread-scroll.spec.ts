import { expect, test } from "@playwright/test";
import { api, newBotAPI, prompts, sendMessageAPI, startAtoll } from "./atoll";

test("long replies stay at the bottom after late layout on fresh loads", async ({
  browser,
}) => {
  const m = await startAtoll();
  try {
    const bot = await newBotAPI(m, "Long reply");
    const thread = await api(m, "POST", `/api/bots/${bot.id}/threads`, {});
    await sendMessageAPI(m, thread.id, "Show a long markdown reply.");
    const failures: number[] = [];
    for (let i = 0; i < 10; i++) {
      const page = await browser.newPage();
      let release!: () => void;
      const imageReady = new Promise<void>((resolve) => (release = resolve));
      await page.route("**/file?path=late.png", async (route) => {
        await imageReady;
        await route.fulfill({
          contentType: "image/svg+xml",
          body: '<svg xmlns="http://www.w3.org/2000/svg" width="400" height="600" />',
        });
      });
      await page.goto(`${m.url}/bots/${bot.id}/threads/${thread.id}`);
      const log = page.getByRole("log", { name: "Messages" });
      await expect(log.getByText("End of long reply.")).toBeAttached();
      release();
      await expect(
        log.getByRole("img", { name: "late image" }),
      ).toHaveJSProperty("complete", true);
      await page.waitForTimeout(200);
      const bottom = await log.evaluate(
        (el) => el.scrollHeight - el.scrollTop - el.clientHeight,
      );
      if (
        bottom > 1 ||
        (await page.getByRole("button", { name: "Jump to latest" }).count())
      )
        failures.push(i + 1);
      await page.close();
    }
    expect(failures, `loads above the bottom: ${failures.join(", ")}`).toEqual(
      [],
    );
  } finally {
    m.stop();
  }
});

test("new events do not move a reader away from older messages", async ({
  page,
}) => {
  const m = await startAtoll();
  try {
    const bot = await newBotAPI(m, "Reader");
    const thread = await api(m, "POST", `/api/bots/${bot.id}/threads`, {});
    for (let i = 0; i < 10; i++)
      await sendMessageAPI(m, thread.id, prompts.keep);
    await page.goto(`${m.url}/bots/${bot.id}/threads/${thread.id}`);
    const log = page.getByRole("log", { name: "Messages" });
    await expect(log.getByTestId("message")).toHaveCount(20);
    await log.evaluate((el) => (el.scrollTop = 100));
    await expect(
      page.getByRole("button", { name: "Jump to latest" }),
    ).toBeVisible();
    const before = await log.evaluate((el) => el.scrollTop);
    await sendMessageAPI(m, thread.id, prompts.keep);
    await expect(log.getByTestId("message")).toHaveCount(22);
    expect(
      Math.abs((await log.evaluate((el) => el.scrollTop)) - before),
    ).toBeLessThanOrEqual(2);
    await expect(
      page.getByRole("button", { name: "Jump to latest" }),
    ).toBeVisible();
  } finally {
    m.stop();
  }
});
