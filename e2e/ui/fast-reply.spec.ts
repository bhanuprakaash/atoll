// A reply stored after the open chat read its messages, and before the chat subscribed to the
// thread's events, still shows without a reload.
import { expect, test } from '@playwright/test';
import { type Machine, newBotAPI, prompts, sidebarRow, startAtoll } from './atoll';

let m: Machine;
test.beforeAll(async () => {
  m = await startAtoll();
});
test.afterAll(() => m?.stop());

test('a reply stored between the chat reading messages and subscribing still shows', async ({
  page,
}) => {
  await newBotAPI(m, 'Quick');
  let subscribed!: () => void;
  const opened = new Promise<void>((resolve) => (subscribed = resolve));
  page.on('response', (r) => {
    if (/\/api\/threads\/[^/]+\/events$/.test(r.url())) subscribed();
  });
  let first = true;
  await page.route(/\/api\/threads\/[^/]+\/messages$/, async (route) => {
    if (!first) return route.continue();
    first = false;
    const response = await route.fetch();
    const read: { role: string }[] = await response.json();
    await opened;
    await new Promise((resolve) => setTimeout(resolve, 300));
    await route.fulfill({ response, json: read.filter((x) => x.role !== 'bot') });
  });

  await page.goto(m.url);
  await sidebarRow(page, 'Quick').click();
  await page.getByRole('textbox', { name: 'Message' }).fill(prompts.pong);
  await page.getByRole('button', { name: 'Send' }).click();

  const log = page.getByRole('log', { name: 'Messages' });
  await expect(log.getByText(prompts.pong, { exact: true })).toBeVisible();
  await expect(log.getByText('pong', { exact: true })).toBeVisible();
  await page.unrouteAll({ behavior: 'ignoreErrors' });
});
