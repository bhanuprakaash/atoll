// A message sent while a Bot works is queued, never refused: it shows at once as the user's bubble
// marked Queued, can be edited or removed, goes out when the run ends, or at once with Stop and send.
import { expect, test } from "@playwright/test";
import {
  api,
  type Machine,
  newBotAPI,
  openThreadAPI,
  prompts,
  startAtoll,
} from "./atoll";

let m: Machine;
test.beforeAll(async () => {
  m = await startAtoll();
});
test.afterAll(() => m?.stop());

const slow = "Take your time, then reply with just: ok";
const ask = "Which codeword did I give you? Reply with just the word.";

async function open(
  page: import("@playwright/test").Page,
  name: string,
  first: string,
) {
  const bot = await newBotAPI(m, name);
  const opened = await openThreadAPI(m, bot.id, first);
  const id: string = opened.thread?.id ?? opened.id;
  await page.goto(`${m.url}/bots/${bot.id}/threads/${id}`);
  const log = page.getByRole("log", { name: "Messages" });
  await expect(log.getByRole("article", { name: "You" })).toHaveText(first);
  return { id, log, runID: (opened.run?.id ?? opened.run_id) as string };
}

async function send(page: import("@playwright/test").Page, text: string) {
  await page.getByRole("textbox", { name: "Message" }).fill(text);
  await page.getByRole("button", { name: "Send" }).click({ timeout: 3000 });
}

test("a message sent during a run is a Queued bubble with no refusal, then goes out when the run ends", async ({
  page,
}) => {
  const { log } = await open(page, "Quill", slow);
  await send(page, ask);

  const bubble = log
    .getByRole("article", { name: "You" })
    .filter({ hasText: ask });
  await expect(bubble).toContainText("Queued");
  await expect(page.getByRole("textbox", { name: "Message" })).toHaveValue("");
  await expect(page.getByText(/run going|"error"|internal error/)).toHaveCount(
    0,
  );

  await expect(bubble).not.toContainText("Queued");
  await expect(log.getByRole("article", { name: "You" })).toHaveCount(2);
  await expect(log.getByText("kestrel", { exact: true })).toBeVisible();
});

test("a queued message can be edited and removed until it goes out", async ({
  page,
}) => {
  const { id, runID, log } = await open(page, "Edda", prompts.hang);
  await send(page, "one");
  await send(page, "three");

  const one = log
    .getByRole("article", { name: "You" })
    .filter({ hasText: "one" });
  await one.getByRole("button", { name: "Edit" }).click();
  await page.getByRole("textbox", { name: "Edit message" }).fill("two");
  await page.getByRole("button", { name: "Save" }).click();
  const two = log
    .getByRole("article", { name: "You" })
    .filter({ hasText: "two" });
  await expect(two).toContainText("Queued");
  await expect(one).toHaveCount(0);

  const three = log
    .getByRole("article", { name: "You" })
    .filter({ hasText: "three" });
  await three.getByRole("button", { name: "Remove" }).click();
  await expect(three).toHaveCount(0);
  const queued = (await api(m, "GET", `/api/threads/${id}`)).queued;
  expect(queued.map((q: { text: string }) => q.text)).toEqual(["two"]);

  await api(m, "POST", `/api/runs/${runID}/stop`);
});

test("Stop and send stops the run and sends the queued message now", async ({
  page,
}) => {
  const { id, runID, log } = await open(page, "Sable", prompts.hang);
  await send(page, prompts.keep);

  const bubble = log
    .getByRole("article", { name: "You" })
    .filter({ hasText: prompts.keep });
  await expect(bubble).toContainText("Queued");
  await bubble.getByRole("button", { name: "Stop and send" }).click();

  await expect(bubble).not.toContainText("Queued");
  await expect
    .poll(async () => (await api(m, "GET", `/api/runs/${runID}`)).status)
    .toBe("stopped");
  await expect
    .poll(async () => (await api(m, "GET", `/api/threads/${id}`)).run_status)
    .toBe("done");
  await expect(log.getByRole("article", { name: "You" })).toHaveCount(2);
});
