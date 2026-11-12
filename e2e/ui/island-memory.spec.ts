// The island remembers what happened while the app was closed: on connect Earlier is built from
// rows newer than one server-side timestamp (island_seen_at), which closing the list or Mark all
// read sets. A close is the page closed (or the server killed) and a new page opened.
// Design: docs/design.md, "The island remembers across a close of the app".
import { expect, type Page, test } from "@playwright/test";
import {
  api,
  type Machine,
  newBotAPI,
  openThreadAPI,
  prompts,
  startAtoll,
  threadTitle,
  threadTree,
} from "./atoll";

let m: Machine;
test.beforeEach(async () => {
  m = await startAtoll();
});
test.afterEach(() => m?.stop());

async function waitStatus(threadID: string, status: string) {
  await expect
    .poll(
      async () => (await api(m, "GET", `/api/threads/${threadID}`)).run_status,
      {
        timeout: 15_000,
        intervals: [100],
      },
    )
    .toBe(status);
}

// open loads the empty thread screen of a Bot with no thread, so no thread is on screen to be left
// out of the list (a Bot's page opens its latest thread).
async function open(page: Page, botID: string) {
  await page.goto(`${m.url}/bots/${botID}`);
  const header = page.locator("header");
  const island = header.getByRole("button", { name: /^Notifications/ });
  await expect(island).toBeVisible();
  return {
    header,
    island,
    list: page.getByRole("dialog", { name: "Notifications" }),
  };
}

test("runs that ended while the page was closed are under Earlier on the next open, and the count is the sidebar New count", async ({
  page,
  context,
}) => {
  const pilot = await newBotAPI(m, "Pilot");
  const home = await newBotAPI(m, "Home");
  await open(page, home.id);
  await page.close();

  const done = [
    await openThreadAPI(m, pilot.id, prompts.pong),
    await openThreadAPI(m, pilot.id, prompts.keep),
  ];
  const failed = await openThreadAPI(m, pilot.id, prompts.none);
  for (const t of done) await waitStatus(t.id, "done");
  await waitStatus(failed.id, "failed");

  const fresh = await context.newPage();
  const { island, list } = await open(fresh, home.id);
  await expect(
    threadTree(fresh, "Pilot").locator('[data-marker="new"]'),
  ).toHaveCount(3);
  await island.click();
  const earlier = list
    .getByRole("list", { name: "Earlier" })
    .getByRole("listitem");
  await expect(earlier).toHaveCount(3);
  await expect(list).not.toContainText("Nothing new");
  await expect(
    earlier.filter({ hasText: `finished ${threadTitle(prompts.pong)}` }),
  ).toHaveCount(1);
  await expect(
    earlier.filter({ hasText: `finished ${threadTitle(prompts.keep)}` }),
  ).toHaveCount(1);
  await expect(
    earlier.filter({ hasText: `failed ${threadTitle(prompts.none)}` }),
  ).toHaveCount(1);
  await expect(list.getByRole("list", { name: "Needs you" })).toHaveCount(0);
  await expect(island).toHaveAccessibleName("Notifications");
});

test("closing the list sets the timestamp: a reload shows nothing older, only what ended after", async ({
  page,
  context,
}) => {
  const pilot = await newBotAPI(m, "Pilot");
  const home = await newBotAPI(m, "Home");
  const before = await openThreadAPI(m, pilot.id, prompts.pong);
  await waitStatus(before.id, "done");

  const { island, list } = await open(page, home.id);
  await island.click();
  await expect(
    list.getByRole("list", { name: "Earlier" }).getByRole("listitem"),
  ).toHaveCount(1);
  await island.click();
  await expect(list).toHaveCount(0);

  await page.reload();
  await island.click();
  await expect(list).toContainText("Nothing new");
  await expect(list.getByRole("list", { name: "Earlier" })).toHaveCount(0);
  await island.click();
  await expect(list).toHaveCount(0);

  await page.close();
  const after = await openThreadAPI(m, pilot.id, prompts.keep);
  await waitStatus(after.id, "done");
  const again = await context.newPage();
  const second = await open(again, home.id);
  await second.island.click();
  const earlier = second.list
    .getByRole("list", { name: "Earlier" })
    .getByRole("listitem");
  await expect(earlier).toHaveCount(1);
  await expect(earlier).toContainText(threadTitle(prompts.keep));
});

test("Mark all read empties Earlier at once, leaves the list open, and a reload shows nothing older", async ({
  page,
}) => {
  const pilot = await newBotAPI(m, "Pilot");
  const home = await newBotAPI(m, "Home");
  for (const prompt of [prompts.pong, prompts.keep]) {
    await waitStatus((await openThreadAPI(m, pilot.id, prompt)).id, "done");
  }

  const { island, list } = await open(page, home.id);
  await island.click();
  await expect(
    list.getByRole("list", { name: "Earlier" }).getByRole("listitem"),
  ).toHaveCount(2);
  await list.getByRole("button", { name: "Mark all read" }).click();
  await expect(list.getByRole("list", { name: "Earlier" })).toHaveCount(0);
  await expect(list).toContainText("Nothing new");
  await expect(list.getByRole("button", { name: "Mark all read" })).toHaveCount(
    0,
  );

  await page.reload();
  await island.click();
  await expect(list).toContainText("Nothing new");
  await expect(list.getByRole("list", { name: "Earlier" })).toHaveCount(0);
});

test("a run cut short by a restart and an approval it waited on are under Earlier as failed; nothing waits", async ({
  page,
  context,
}) => {
  const pilot = await newBotAPI(m, "Pilot");
  const home = await newBotAPI(m, "Home");
  const busy = await openThreadAPI(m, pilot.id, prompts.hang);
  const waiting = await openThreadAPI(m, pilot.id, prompts.write);
  await waitStatus(busy.id, "running");
  await waitStatus(waiting.id, "waiting");
  await open(page, home.id);
  await page.close();
  m.kill();
  await m.restart();

  const fresh = await context.newPage();
  const { island, list } = await open(fresh, home.id);
  await island.click();
  const earlier = list
    .getByRole("list", { name: "Earlier" })
    .getByRole("listitem");
  await expect(earlier).toHaveCount(2);
  await expect(
    earlier.filter({ hasText: `failed ${threadTitle(prompts.hang)}` }),
  ).toHaveCount(1);
  await expect(
    earlier.filter({ hasText: `failed ${threadTitle(prompts.write)}` }),
  ).toHaveCount(1);
  await expect(list.getByRole("list", { name: "Needs you" })).toHaveCount(0);
  await expect(island).toHaveAccessibleName("Notifications");
});

test("an approval still open when the page was closed comes back under Needs you, not under Earlier", async ({
  page,
  context,
}) => {
  const pilot = await newBotAPI(m, "Pilot");
  const home = await newBotAPI(m, "Home");
  const seen = await openThreadAPI(m, pilot.id, prompts.keep);
  await waitStatus(seen.id, "done");
  await open(page, home.id);
  await page.close();
  const waiting = await openThreadAPI(m, pilot.id, prompts.write);
  await waitStatus(waiting.id, "waiting");

  const fresh = await context.newPage();
  const { header, island, list } = await open(fresh, home.id);
  await expect(
    header.getByRole("button", { name: "Allow", exact: true }),
  ).toBeVisible();
  await fresh.keyboard.press("Escape");
  await expect(island).toHaveAccessibleName("Notifications, 1 waiting");
  await island.click();
  const needs = list
    .getByRole("list", { name: "Needs you" })
    .getByRole("listitem");
  await expect(needs).toHaveCount(1);
  await expect(needs).toContainText(threadTitle(prompts.write));
  const earlier = list
    .getByRole("list", { name: "Earlier" })
    .getByRole("listitem");
  await expect(earlier).toHaveCount(1);
  await expect(earlier).toContainText(threadTitle(prompts.keep));
  await expect(
    earlier.filter({ hasText: threadTitle(prompts.write) }),
  ).toHaveCount(0);

  await needs.getByRole("button", { name: "Allow", exact: true }).click();
  await waitStatus(waiting.id, "done");
  await expect(list.getByRole("list", { name: "Needs you" })).toHaveCount(0);
  await expect(island).toHaveAccessibleName("Notifications");
});

test("an MR opened and a Chief change made while the page was closed are under Earlier", async ({
  page,
  context,
}) => {
  const pilot = await newBotAPI(m, "Pilot");
  const home = await newBotAPI(m, "Home");
  await open(page, home.id);
  await page.close();

  const mrURL = "https://gitlab.example/fixtures/seeded/-/merge_requests/7";
  const mr = await openThreadAPI(
    m,
    pilot.id,
    `Use MCP tools. [{"server":"atoll","tool":"track_mr","args":{"url":"${mrURL}","title":"Tidy the logs"}}]`,
  );
  await waitStatus(mr.id, "done");
  const chief = await openThreadAPI(
    m,
    "chief",
    "Create reef-greeting and a Lagoon Bot using claude.",
  );
  await waitStatus(chief.id, "done");

  const fresh = await context.newPage();
  const { island, list } = await open(fresh, home.id);
  await island.click();
  const earlier = list
    .getByRole("list", { name: "Earlier" })
    .getByRole("listitem");
  const opened = earlier.filter({ hasText: "Pilot opened !7" });
  await expect(opened).toHaveCount(1);
  await expect(opened).toContainText("Tidy the logs");
  await expect(
    earlier.filter({ hasText: /Chief created Bot Lagoon/ }),
  ).toHaveCount(1);
});
