import { expect, test } from '@playwright/test';
import {
  api,
  type Machine,
  newBotAPI,
  openThreadAPI,
  prompts,
  sendMessageAPI,
  nowPane,
  sidebarRow,
  startAtoll,
  threadPane,
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

test('the six-word title is in the thread header and the thread pane', async ({ page }) => {
  const bot = await newBotAPI(m, 'Titles');
  const thread = await openThreadAPI(m, bot.id, prompts.keep);
  await waitStatus(thread.id, 'done');
  const title = threadTitle(prompts.keep);

  await page.goto(m.url);
  await sidebarRow(page, 'Titles').click();
  await expect(page.getByRole('heading', { name: title })).toBeVisible();
  await expect(threadPane(page, 'Titles').getByRole('listitem')).toContainText(title);
});

test('the face shows no activity; Now notes the current verb or the approval prompt, an idle Bot has no row', async ({
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

  await page.goto(`${m.url}/bots/${reply.id}/threads/${done.id}`);
  await expect(
    sidebarRow(page, 'Reply activity').getByRole('img', { name: /, idle$/ }),
  ).toBeVisible();
  expect((await api(m, 'GET', `/api/bots/${reply.id}`)).activity).toBe('ok');

  const now = await nowPane(page);
  const rowOf = (id: string) =>
    now.getByRole('listitem').filter({ has: page.locator(`a[href$="/threads/${id}"]`) });
  await expect(rowOf(running.id)).toContainText('Working activity · Running Run the build');
  await expect(rowOf(waiting.id)).toContainText('Approval activity · ');
  await expect(rowOf(waiting.id)).toContainText('Write hello.txt');
  await expect(rowOf(done.id)).toHaveCount(0);
  await api(m, 'POST', `/api/runs/${running.run.id}/stop`);
});

test('message times use the locale clock in muted 12px text', async ({ page }) => {
  const bot = await newBotAPI(m, 'Times');
  await page.goto(m.url);
  await sidebarRow(page, 'Times').click();
  await send(page, prompts.working);
  const times = page.getByTestId('message-time');
  await expect(times).toHaveCount(2);
  const [thread] = await api(m, 'GET', `/api/bots/${bot.id}/threads`);
  const messages = await api(m, 'GET', `/api/threads/${thread.id}/messages`);
  const want = await page.evaluate(
    (stamps: string[]) =>
      stamps.map((s) => {
        return new Date(s).toLocaleTimeString([], { hour: 'numeric', minute: '2-digit' });
      }),
    messages.map((msg: { created_at: string }) => msg.created_at),
  );
  await expect(times).toHaveText(want);
  await expect(times.first()).toHaveCSS('font-size', '12px');
  await expect(times.first()).toHaveCSS(
    'color',
    await page.getByText(/ · Worked /).evaluate((el) => getComputedStyle(el).color),
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

test('the face menu pins a Bot under Chief and Bot settings exposes pinning', async ({ page }) => {
  const bot = await newBotAPI(m, 'Pin me');
  await page.goto(m.url);
  const row = sidebarRow(page, 'Pin me');
  await row.click({ button: 'right' });
  await page.getByRole('menuitem', { name: 'Pin to sidebar' }).click();
  await expect
    .poll(() =>
      page
        .getByRole('navigation', { name: 'Bots' })
        .locator('a[href^="/bots/"]')
        .evaluateAll((els) => els.slice(0, 2).map((el) => el.getAttribute('href'))),
    )
    .toEqual(['/bots/chief', `/bots/${bot.id}`]);
  await row.click({ button: 'right' });
  await expect(page.getByRole('menuitem', { name: 'Unpin' })).toBeVisible();
  await page.keyboard.press('Escape');
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
  const formerTop = (await log.getByTestId('message-time').first().elementHandle())!;
  const before = await log.evaluate((el, top) => {
    el.scrollTop = 0;
    return top.getBoundingClientRect().y;
  }, formerTop);
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
