// Allow in this thread: a quiet third choice on a command that may repeat.
// Contract: e2e/allow-in-thread-contract.md.
import { expect, type Locator, type Page, test } from "@playwright/test";
import { api, type Machine, newBotAPI, sidebarRow, startAtoll } from "./atoll";

let m: Machine;
test.beforeEach(async () => {
  m = await startAtoll({ secretStore: true });
});
test.afterEach(() => m?.stop());

const clean = "rm -rf /tmp/codex-gocache-161";
const other = "rm -rf /tmp/codex-gotmp-161";
const run = (...cmds: string[]) => `Run commands. ${JSON.stringify(cmds)}`;

async function openBot(page: Page, name: string) {
  const bot = await newBotAPI(m, name, "claude");
  await page.goto(m.url);
  await sidebarRow(page, name).click();
  await expect(
    page.getByRole("heading", { name: `What should ${name} work on?` }),
  ).toBeVisible();
  return bot;
}

async function send(page: Page, text: string) {
  await page.getByRole("textbox", { name: "Message" }).fill(text);
  await page.getByRole("button", { name: "Send" }).click();
}

async function thread(botID: string) {
  const [t] = await api(m, "GET", `/api/bots/${botID}/threads`);
  return api(m, "GET", `/api/threads/${t.id}`);
}

const idle = (botID: string) =>
  expect.poll(async () => (await thread(botID)).active_run ?? null).toBeNull();

// cardOf is the approval card of a command: the user's message and the reply repeat the text.
const cardOf = (log: Locator) => (cmd: string) =>
  log.getByText(`Wants to run ${cmd}`, { exact: true });

const allowed = (cmd: string) =>
  new RegExp(
    `Allowed\\s+\`?${cmd.replace(/[.*+?^${}()|[\]\\/]/g, "\\$&")}\`?\\s+for this thread`,
  );

test("Allow in this thread lets the same command through, and the thread says so", async ({
  page,
}) => {
  const bot = await openBot(page, "Kelp");
  const log = page.getByRole("log", { name: "Messages" });
  const allow = log.getByRole("button", { name: "Allow", exact: true });
  const deny = log.getByRole("button", { name: "Deny", exact: true });
  const inThread = log.getByRole("button", { name: "Allow in this thread" });
  const card = cardOf(log);

  await send(page, run(clean, clean, other));
  await expect(card(clean)).toBeVisible();
  await expect(allow).toHaveCount(1);
  await expect(deny).toHaveCount(1);
  await expect(inThread).toHaveCount(1);
  await inThread.click();

  // The second `clean` passes without a card; `other` still asks.
  await expect(card(other)).toBeVisible();
  await expect(card(clean)).toHaveCount(0);
  await expect(allow).toHaveCount(1);
  await allow.click();
  await idle(bot.id);
  await expect(allow).toHaveCount(0);

  const line = log.getByText(allowed(clean));
  await expect(line).toHaveCount(1);
  await page.reload();
  await expect(log.getByText(allowed(clean))).toHaveCount(1);

  await send(page, run(clean, other));
  await expect(card(other)).toBeVisible();
  await expect(allow).toHaveCount(1);
  await deny.click();
  await idle(bot.id);
  await expect(log.getByText(allowed(clean))).toHaveCount(1);
});

test("a command that must stay a plain choice shows Allow and Deny alone", async ({
  page,
}) => {
  const bot = await openBot(page, "Moss");
  const log = page.getByRole("log", { name: "Messages" });
  const allow = log.getByRole("button", { name: "Allow", exact: true });
  const deny = log.getByRole("button", { name: "Deny", exact: true });
  const card = cardOf(log);

  const cmds = [
    "git push origin fix/x",
    "gh repo delete o/r --yes",
    "rm -rf /var/lib/reef-nope",
    "gh pr create --title x --body y",
  ];
  await send(page, run(...cmds));
  for (const cmd of cmds) {
    await expect(card(cmd)).toBeVisible();
    await expect(allow).toHaveCount(1);
    await expect(deny).toHaveCount(1);
    await expect(
      log.getByRole("button", { name: "Allow in this thread" }),
    ).toHaveCount(0);
    const a = await allow.boundingBox();
    const d = await deny.boundingBox();
    expect(a && d && Math.abs(a.height - d.height) < 1).toBeTruthy();
    await deny.click();
    await expect(card(cmd)).toHaveCount(0);
  }
  await idle(bot.id);
  await expect(log.getByText(/Allowed .* for this thread/)).toHaveCount(0);
});
