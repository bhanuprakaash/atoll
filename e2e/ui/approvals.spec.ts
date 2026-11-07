// Approval cards show only open approvals, and links in replies leave the app.
// Contract: e2e/approvals-contract.md.
import { expect, type Page, test } from "@playwright/test";
import { api, type Machine, newBotAPI, sidebarRow, startAtoll } from "./atoll";

let m: Machine;
test.beforeEach(async () => {
  m = await startAtoll({ secretStore: true });
});
test.afterEach(() => m?.stop());

const call = { server: "docs-local", tool: "answer", args: {} };
const use = (n: number) =>
  `Use MCP tools. ${JSON.stringify(Array(n).fill(call))}`;

// openBot creates a Bot that picks the docs-local server and opens its empty thread page.
async function openBot(page: Page, name: string) {
  const server = await api(m, "POST", "/api/mcp-servers", {
    name: "docs-local",
    command: `${process.env.ATOLL_E2E_OUT}/mcpstdio`,
    args: [],
  });
  const bot = await newBotAPI(m, name, "claude", { mcp_servers: [server.id] });
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

// thread is the Bot's only thread, as the API sees it.
async function thread(botID: string) {
  const [t] = await api(m, "GET", `/api/bots/${botID}/threads`);
  return api(m, "GET", `/api/threads/${t.id}`);
}

const openApproval = async (botID: string): Promise<string | undefined> =>
  (await thread(botID)).approval?.id;

test("an answered approval stays gone after reopening the thread mid-run", async ({
  page,
}) => {
  const bot = await openBot(page, "Kelp");
  const log = page.getByRole("log", { name: "Messages" });
  const allow = log.getByRole("button", { name: "Allow", exact: true });

  await send(page, use(2));
  await expect(allow).toHaveCount(1);
  await expect.poll(() => openApproval(bot.id)).toBeTruthy();
  const first = await openApproval(bot.id);
  await allow.click();
  await expect.poll(() => openApproval(bot.id)).not.toBe(first);
  await expect.poll(() => openApproval(bot.id)).toBeTruthy();
  await expect(allow).toHaveCount(1);

  await page.reload();
  await expect(allow).toHaveCount(1);
  await expect(log.getByText("Could not answer approval")).toHaveCount(0);

  await allow.click();
  await expect(
    log.getByText("docs-local/answer: stdio-answer-42").first(),
  ).toBeVisible();
  await expect(allow).toHaveCount(0);
});

test("a card leaves when its approval is answered elsewhere", async ({
  page,
}) => {
  const bot = await openBot(page, "Moss");
  const log = page.getByRole("log", { name: "Messages" });
  const allow = log.getByRole("button", { name: "Allow", exact: true });

  await send(page, use(1));
  await expect(allow).toHaveCount(1);
  await expect.poll(() => openApproval(bot.id)).toBeTruthy();
  await api(m, "POST", `/api/approvals/${await openApproval(bot.id)}`, {
    approve: true,
  });

  await expect(
    log.getByText("docs-local/answer: stdio-answer-42"),
  ).toBeVisible();
  await expect(allow).toHaveCount(0);
});

test("a card whose answer gets 404 leaves without an error", async ({
  page,
}) => {
  const bot = await openBot(page, "Reed");
  const log = page.getByRole("log", { name: "Messages" });
  const allow = log.getByRole("button", { name: "Allow", exact: true });

  await send(page, use(1));
  await expect(allow).toHaveCount(1);
  await page.route("**/api/approvals/*", (route) =>
    route.fulfill({
      status: 404,
      contentType: "text/plain",
      body: "approval not found",
    }),
  );
  await allow.click();
  await expect(allow).toHaveCount(0);
  await expect(log.getByText("Could not answer approval")).toHaveCount(0);

  await page.unroute("**/api/approvals/*");
  const runID = (await thread(bot.id)).active_run?.id;
  if (runID) await api(m, "POST", `/api/runs/${runID}/stop`);
});

test("a link in a reply opens outside the thread, which stays put", async ({
  page,
  context,
}) => {
  await context.route("https://docs.reef.test/**", (route) =>
    route.fulfill({
      status: 200,
      contentType: "text/html",
      body: "<title>Reef docs</title>",
    }),
  );
  await newBotAPI(m, "Tide");
  await page.goto(m.url);
  await sidebarRow(page, "Tide").click();
  await send(page, "Reply with a link to the Reef docs.");

  const log = page.getByRole("log", { name: "Messages" });
  const link = log.getByRole("link", { name: "the Reef docs" });
  await expect(link).toHaveAttribute("href", "https://docs.reef.test/start");
  const at = page.url();

  const popup = page.waitForEvent("popup");
  await link.click();
  await expect(await popup).toHaveURL("https://docs.reef.test/start");
  expect(page.url()).toBe(at);
  await expect(link).toBeVisible();
});
