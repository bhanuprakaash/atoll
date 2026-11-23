// A thread's unsent message stays in its composer when the user leaves and comes back, and after a
// reload; sending or clearing it by hand removes it. A Bot's New thread screen keeps its own draft.
import { expect, type Page, test } from "@playwright/test";
import {
  api,
  type Machine,
  newBotAPI,
  openThreadAPI,
  prompts,
  sidebarRow,
  startAtoll,
  threadTab,
  threadTitle,
  waitStatus,
} from "./atoll";

let m: Machine;
test.beforeAll(async () => {
  m = await startAtoll();
});
test.afterAll(() => m?.stop());

const box = (page: Page, bot: string) =>
  page.getByRole("textbox", { name: new RegExp(`^Message ${bot}`) });

// bot makes a Bot with one finished thread per prompt and returns their ids.
async function bot(name: string, ...first: string[]) {
  const b = await newBotAPI(m, name);
  const threads: string[] = [];
  for (const text of first) {
    const t = await openThreadAPI(m, b.id, text);
    await waitStatus(m, t.id, "done");
    threads.push(t.id);
  }
  return { id: b.id as string, threads };
}

// via opens a thread through its tab, as a user switching threads does, without a reload.
async function via(page: Page, prompt: string, id: string) {
  await threadTab(page, threadTitle(prompt)).click();
  await expect(page).toHaveURL(new RegExp(`/threads/${id}$`));
}

async function onRow(page: Page, name: string, id: string) {
  await sidebarRow(page, name).click();
  await expect(page).toHaveURL(new RegExp(`/threads/${id}$`));
}

const ask = "Which codeword did I give you? Reply with just the word.";
const draft = "half a thought\nsecond  line, not sent";

test("text typed in a thread stays when the user opens another Bot's thread and comes back", async ({
  page,
}) => {
  const a = await bot("Drafty", prompts.pong);
  const other = await bot("Elsewhere", prompts.keep);

  await page.goto(`${m.url}/bots/${a.id}/threads/${a.threads[0]}`);
  await box(page, "Drafty").fill(draft);

  await onRow(page, "Elsewhere", other.threads[0]);
  await expect(box(page, "Elsewhere")).toHaveValue("");
  await onRow(page, "Drafty", a.threads[0]);
  await expect(box(page, "Drafty")).toHaveValue(draft);
});

test("each thread of a Bot keeps its own draft", async ({ page }) => {
  const b = await bot("Pairs", prompts.pong, prompts.keep);
  const [one, two] = b.threads;
  for (const id of b.threads) await api(m, "POST", `/api/threads/${id}/pin`);

  await page.goto(`${m.url}/bots/${b.id}/threads/${one}`);
  await box(page, "Pairs").fill("draft for one");

  await via(page, prompts.keep, two);
  await expect(box(page, "Pairs")).toHaveValue("");
  await box(page, "Pairs").fill("draft for two");

  await via(page, prompts.pong, one);
  await expect(box(page, "Pairs")).toHaveValue("draft for one");
  await via(page, prompts.keep, two);
  await expect(box(page, "Pairs")).toHaveValue("draft for two");
});

test("a draft survives a reload", async ({ page }) => {
  const b = await bot("Reloader", prompts.pong);

  await page.goto(`${m.url}/bots/${b.id}/threads/${b.threads[0]}`);
  await box(page, "Reloader").fill(draft);
  await page.reload();
  await expect(box(page, "Reloader")).toHaveValue(draft);
});

test("sending clears the draft for good", async ({ page }) => {
  const b = await bot("Sender", prompts.keep);
  const other = await bot("Stranger", prompts.pong);
  const log = page.getByRole("log", { name: "Messages" });

  await page.goto(`${m.url}/bots/${b.id}/threads/${b.threads[0]}`);
  await box(page, "Sender").fill(ask);
  await box(page, "Sender").press("Enter");
  await expect(box(page, "Sender")).toHaveValue("");
  await expect(log.getByRole("article", { name: "You" })).toHaveCount(2);
  await expect(log.getByText("kestrel", { exact: true })).toBeVisible();
  await waitStatus(m, b.threads[0], "done");

  await onRow(page, "Stranger", other.threads[0]);
  await onRow(page, "Sender", b.threads[0]);
  await expect(box(page, "Sender")).toHaveValue("");
  await page.reload();
  await expect(box(page, "Sender")).toHaveValue("");
});

test("a draft cleared by hand does not come back", async ({ page }) => {
  const b = await bot("Eraser", prompts.pong);
  const other = await bot("Passerby", prompts.keep);

  await page.goto(`${m.url}/bots/${b.id}/threads/${b.threads[0]}`);
  await box(page, "Eraser").fill(draft);
  await onRow(page, "Passerby", other.threads[0]);
  await onRow(page, "Eraser", b.threads[0]);
  await expect(box(page, "Eraser")).toHaveValue(draft);

  await box(page, "Eraser").fill("");
  await onRow(page, "Passerby", other.threads[0]);
  await onRow(page, "Eraser", b.threads[0]);
  await expect(box(page, "Eraser")).toHaveValue("");
  await page.reload();
  await expect(box(page, "Eraser")).toHaveValue("");
});

test("a Bot's New thread screen keeps its draft until it is sent", async ({
  page,
}) => {
  const b = await bot("Starter", prompts.keep);
  const other = await bot("Visitor", prompts.pong);
  const newThread = () =>
    page.getByRole("button", { name: "New thread", exact: true }).click();
  const heading = page.getByRole("heading", {
    name: "What should Starter work on?",
  });

  await page.goto(`${m.url}/bots/${b.id}/new`);
  await expect(heading).toBeVisible();
  await box(page, "Starter").fill(draft);

  await onRow(page, "Visitor", other.threads[0]);
  await sidebarRow(page, "Starter").click();
  await newThread();
  await expect(heading).toBeVisible();
  await expect(box(page, "Starter")).toHaveValue(draft);

  await page.reload();
  await expect(box(page, "Starter")).toHaveValue(draft);

  await box(page, "Starter").press("Enter");
  await expect(page).toHaveURL(new RegExp(`/bots/${b.id}/threads/`));
  await onRow(page, "Visitor", other.threads[0]);
  await sidebarRow(page, "Starter").click();
  await newThread();
  await expect(heading).toBeVisible();
  await expect(box(page, "Starter")).toHaveValue("");
});
