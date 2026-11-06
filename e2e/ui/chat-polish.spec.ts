import { expect, test } from '@playwright/test';
import {
  api,
  type Machine,
  newBotAPI,
  openThreadAPI,
  openThreads,
  prompts,
  sendMessageAPI,
  sidebarRow,
  startAtoll,
  threadTitle,
} from './atoll';

let m: Machine;
test.beforeAll(async () => {
  m = await startAtoll();
});
test.afterAll(() => m?.stop());

async function waitStatus(threadID: string, status: string) {
  await expect
    .poll(async () => (await api(m, 'GET', `/api/threads/${threadID}`)).run_status, {
      timeout: 15_000,
    })
    .toBe(status);
}

async function send(page: import('@playwright/test').Page, text: string) {
  await page.getByRole('textbox', { name: 'Message' }).fill(text);
  await page.getByRole('button', { name: 'Send' }).click();
}

test('the six-word title is in the thread header and Threads panel', async ({ page }) => {
  const bot = await newBotAPI(m, 'Titles');
  const thread = await openThreadAPI(m, bot.id, prompts.keep);
  await waitStatus(thread.id, 'done');
  const title = threadTitle(prompts.keep);

  await page.goto(m.url);
  await sidebarRow(page, 'Titles').click();
  await expect(page.getByRole('heading', { name: title })).toBeVisible();
  const panel = await openThreads(page);
  await expect(panel.getByRole('listitem')).toContainText(title);
});

test('sidebar activity is the reply, the current verb, or the approval prompt', async ({
  page,
}) => {
  const reply = await newBotAPI(m, 'Reply activity');
  const done = await openThreadAPI(m, reply.id, prompts.keep);
  await waitStatus(done.id, 'done');
  const working = await newBotAPI(m, 'Working activity');
  const running = await openThreadAPI(m, working.id, prompts.hang);
  await waitStatus(running.id, 'running');
  const approval = await newBotAPI(m, 'Approval activity');
  const waiting = await openThreadAPI(m, approval.id, prompts.write);
  await waitStatus(waiting.id, 'waiting');

  await page.goto(m.url);
  await expect(sidebarRow(page, 'Reply activity')).toContainText('ok');
  await expect(sidebarRow(page, 'Reply activity')).not.toContainText('Done');
  await expect(sidebarRow(page, 'Working activity')).toContainText('Running make build');
  await expect(sidebarRow(page, 'Approval activity')).toContainText('Needs you: Write hello.txt');
});

test('message times show today as H:MM in muted 12px text', async ({ page }) => {
  const bot = await newBotAPI(m, 'Times');
  await page.goto(m.url);
  await sidebarRow(page, 'Times').click();
  await send(page, prompts.keep);
  const times = page.getByTestId('message-time');
  await expect(times).toHaveCount(2);
  const [thread] = await api(m, 'GET', `/api/bots/${bot.id}/threads`);
  const messages = await api(m, 'GET', `/api/threads/${thread.id}/messages`);
  const want = await page.evaluate(
    (stamps: string[]) =>
      stamps.map((s) => {
        const d = new Date(s);
        return `${d.getHours()}:${String(d.getMinutes()).padStart(2, '0')}`;
      }),
    messages.map((msg: { created_at: string }) => msg.created_at),
  );
  await expect(times).toHaveText(want);
  await expect(times.first()).toHaveCSS('font-size', '12px');
  await expect(times.first()).toHaveCSS(
    'color',
    await page.getByText(/^Worked /).evaluate((el) => getComputedStyle(el).color),
  );
});

test('user-to-reply and reply-to-next-user gaps are at least 12px', async ({ page }) => {
  const bot = await newBotAPI(m, 'Spacing');
  const thread = await api(m, 'POST', `/api/bots/${bot.id}/threads`, {});
  await sendMessageAPI(m, thread.id, prompts.keep);
  await sendMessageAPI(m, thread.id, prompts.keep);
  await page.goto(m.url);
  await sidebarRow(page, 'Spacing').click();

  const log = page.getByRole('log', { name: 'Messages' });
  const messages = log.getByTestId('message');
  await expect(messages).toHaveCount(4);
  const firstUser = await messages.nth(0).boundingBox();
  const firstReply = await messages.nth(1).boundingBox();
  const secondUser = await messages.nth(2).boundingBox();
  expect(firstUser).not.toBeNull();
  expect(firstReply).not.toBeNull();
  expect(secondUser).not.toBeNull();
  const userToReply = firstReply!.y - (firstUser!.y + firstUser!.height);
  const replyToNextUser = secondUser!.y - (firstReply!.y + firstReply!.height);
  expect(userToReply).toBeGreaterThanOrEqual(12);
  expect(replyToNextUser).toBeGreaterThanOrEqual(12);
});

test('pin controls reorder a sidebar row and Bot settings exposes pinning', async ({ page }) => {
  const bot = await newBotAPI(m, 'Pin me');
  await page.goto(m.url);
  const row = sidebarRow(page, 'Pin me');
  await row.hover();
  await expect(row.getByRole('button', { name: 'Pin Pin me' })).toBeVisible();
  await row.getByRole('button', { name: 'Pin Pin me' }).click();
  await expect(row.getByRole('button', { name: 'Unpin Pin me' })).toBeVisible();
  await row.click();
  await page.getByRole('button', { name: 'Bot settings' }).click();
  await expect(
    page
      .getByRole('dialog', { name: 'Bot settings' })
      .getByRole('button', { name: 'Pin to sidebar' }),
  ).toBeVisible();
  expect((await api(m, 'GET', `/api/bots/${bot.id}`)).pinned).toBe(true);
});

test('older messages load at the top without moving the former top message', async ({ page }) => {
  const bot = await newBotAPI(m, 'History UI');
  const thread = await api(m, 'POST', `/api/bots/${bot.id}/threads`, {});
  for (let i = 0; i < 26; i++) {
    await sendMessageAPI(m, thread.id, prompts.keep);
  }
  await expect(await api(m, 'GET', `/api/threads/${thread.id}/messages`)).toHaveLength(50);
  await page.goto(m.url);
  await sidebarRow(page, 'History UI').click();
  const log = page.getByRole('log', { name: 'Messages' });
  await expect(log.getByTestId('message-time')).toHaveCount(50);
  const formerTop = log.getByTestId('message-time').first();
  const before = (await formerTop.boundingBox())!.y;
  await log.evaluate((el) => (el.scrollTop = 0));
  await expect(log.getByTestId('message-time')).toHaveCount(52);
  expect(Math.abs((await formerTop.boundingBox())!.y - before)).toBeLessThanOrEqual(2);
});

test('Jump to latest appears above the composer and marks itself working', async ({ page }) => {
  const bot = await newBotAPI(m, 'Jump');
  const thread = await api(m, 'POST', `/api/bots/${bot.id}/threads`, {});
  for (let i = 0; i < 10; i++) await sendMessageAPI(m, thread.id, prompts.keep);
  await sendMessageAPI(m, thread.id, prompts.chatPolishHang, 'running');
  await page.goto(m.url);
  await sidebarRow(page, 'Jump').click();
  const log = page.getByRole('log', { name: 'Messages' });
  await log.evaluate((el) => (el.scrollTop = 0));
  const jump = page.getByRole('button', { name: 'Jump to latest' });
  await expect(jump).toBeVisible();
  await expect(jump).toHaveAttribute('data-working', 'true');
  await expect(jump.getByRole('img')).toBeVisible();
  await jump.click();
  await expect
    .poll(() => log.evaluate((el) => el.scrollTop + el.clientHeight >= el.scrollHeight - 1))
    .toBe(true);
  await expect(jump).toHaveCount(0);
});
