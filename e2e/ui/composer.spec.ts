// The composer sends what is in it once: it clears on submit, so text typed or an image added while
// a send settles is all the next send carries, and a send that fails keeps its text and images.
import { expect, type Page, test } from "@playwright/test";
import {
  api,
  type Machine,
  newBotAPI,
  prompts,
  sendMessageAPI,
  startAtoll,
} from "./atoll";
import { blue, give, red } from "./image-fixture";

let m: Machine;
test.beforeAll(async () => {
  m = await startAtoll();
});
test.afterAll(() => m?.stop());

type Sent = { text: string; images?: { mime: string; data: string }[] };

// openThread opens a thread that already has a run, so the composer is the thread's own.
async function openThread(page: Page, name: string) {
  const bot = await newBotAPI(m, name);
  const thread = await api(m, "POST", `/api/bots/${bot.id}/threads`, {});
  await sendMessageAPI(m, thread.id, prompts.pong);
  await page.goto(`${m.url}/bots/${bot.id}/threads/${thread.id}`);
  await expect(
    page
      .getByRole("log", { name: "Messages" })
      .getByText("pong", { exact: true }),
  ).toBeVisible();
  return thread.id as string;
}

// holdSends records every message the page sends to the thread and holds the first one's request
// until release is called.
async function holdSends(page: Page, threadID: string) {
  const sent: Sent[] = [];
  let release!: () => void;
  const held = new Promise<void>((ok) => (release = ok));
  await page.route(`**/api/threads/${threadID}/messages`, async (route) => {
    if (route.request().method() !== "POST") return route.fallback();
    sent.push(route.request().postDataJSON());
    if (sent.length === 1) await held;
    await route.continue();
  });
  return { sent, release };
}

const box = (page: Page) => page.getByRole("textbox", { name: /^Message/ });
const attachments = (page: Page) => page.getByTestId("composer-attachment");

test("text typed while a send settles is all the box keeps, and the next send carries only it", async ({
  page,
}) => {
  const id = await openThread(page, "Typist");
  const { sent, release } = await holdSends(page, id);

  await box(page).pressSequentially("hello");
  await box(page).press("Enter");
  await expect.poll(() => sent.length).toBe(1);
  await box(page).pressSequentially(" more");
  const settled = page.waitForResponse(
    (r) =>
      r.url().endsWith(`/api/threads/${id}/messages`) &&
      r.request().method() === "POST",
  );
  release();
  await settled;

  await expect(box(page)).toHaveValue(/^ ?more$/);
  await expect(
    page.getByRole("button", { name: "Send", exact: true }),
  ).toBeEnabled();
  await box(page).press("Enter");
  await expect.poll(() => sent.length).toBe(2);
  expect(sent.map((s) => s.text.trim())).toEqual(["hello", "more"]);
});

test("an image sent once is not attached to the next send", async ({
  page,
}) => {
  const id = await openThread(page, "Painter");
  const { sent, release } = await holdSends(page, id);

  await give(box(page), "paste", red);
  await expect(attachments(page)).toHaveCount(1);
  await box(page).fill("the red one");
  await box(page).press("Enter");
  await expect.poll(() => sent.length).toBe(1);
  await give(box(page), "paste", blue);
  const settled = page.waitForResponse(
    (r) =>
      r.url().endsWith(`/api/threads/${id}/messages`) &&
      r.request().method() === "POST",
  );
  release();
  await settled;

  await expect(attachments(page)).toHaveCount(1);
  await page.getByRole("button", { name: "Send", exact: true }).click();
  await expect.poll(() => sent.length).toBe(2);
  const data = (f: { bytes: number[] }) =>
    Buffer.from(f.bytes).toString("base64");
  expect(sent[0].images?.map((i) => i.data)).toEqual([data(red)]);
  expect(sent[1].images?.map((i) => i.data)).toEqual([data(blue)]);
});

test("a send that fails keeps its text and images in the box", async ({
  page,
}) => {
  const id = await openThread(page, "Keeper");
  await page.route(`**/api/threads/${id}/messages`, (route) =>
    route.request().method() === "POST"
      ? route.fulfill({ status: 500, body: "" })
      : route.fallback(),
  );

  await give(box(page), "paste", red);
  await expect(attachments(page)).toHaveCount(1);
  await box(page).fill("keep me");
  await box(page).press("Enter");
  await expect(page.getByText("Something went wrong on our side.")).toBeVisible();
  await expect(box(page)).toHaveValue("keep me");
  await expect(attachments(page)).toHaveCount(1);
});
