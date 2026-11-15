// Header island: the Bot header shows the open thread's run state and opens the notification list
// on a click only.
import { expect, test } from '@playwright/test';
import {
  api,
  type Machine,
  newBotAPI,
  openThreadAPI,
  prompts,
  sidebarRow,
  startAtoll,
  waitStatus,
} from './atoll';

let m: Machine;
test.beforeAll(async () => {
  m = await startAtoll();
});
test.afterAll(() => m?.stop());

test('the header shows the open thread run state', async ({ page }) => {
  const bot = await newBotAPI(m, 'Pilot');
  const busy = await openThreadAPI(m, bot.id, prompts.hang);
  const waiting = await openThreadAPI(m, bot.id, prompts.write);
  const failed = await openThreadAPI(m, bot.id, prompts.none);
  const done = await openThreadAPI(m, bot.id, prompts.keep);
  await waitStatus(m, busy.id, 'running');
  await waitStatus(m, waiting.id, 'waiting');
  await waitStatus(m, failed.id, 'failed');
  await waitStatus(m, done.id, 'done');

  const header = page.locator('header');
  await page.goto(`${m.url}/bots/${bot.id}/threads/${busy.id}`);
  await expect(header).toContainText('Thinking');
  await page.goto(`${m.url}/bots/${bot.id}/threads/${waiting.id}`);
  await expect(header).toContainText('Needs you');
  // Answered here, so its approval does not open the island on the threads below.
  await page
    .getByRole('log', { name: 'Messages' })
    .getByRole('button', { name: 'Allow', exact: true })
    .click();
  await waitStatus(m, waiting.id, 'done');
  await page.goto(`${m.url}/bots/${bot.id}/threads/${failed.id}`);
  await expect(header).toContainText('Run failed');
  await page.goto(`${m.url}/bots/${bot.id}/threads/${done.id}`);
  await expect(header.getByRole('heading', { name: /^Remember this fact/ })).toBeVisible();
  await expect(header).not.toContainText('Thinking');
  await expect(header).not.toContainText('Needs you');
  await expect(header).not.toContainText('Run failed');

  await api(m, 'POST', `/api/runs/${busy.run.id}/stop`);
});

test('hovering the island does not open the notification list; a click does', async ({ page }) => {
  await newBotAPI(m, 'Hover');
  await page.goto(m.url);
  await sidebarRow(page, 'Hover').click();
  const island = page.locator('header').getByRole('button', { name: /^Notifications/ });
  await island.hover();
  await page.waitForTimeout(600);
  await expect(page.getByRole('dialog', { name: 'Notifications' })).toHaveCount(0);
  await island.click();
  await expect(page.getByRole('dialog', { name: 'Notifications' })).toBeVisible();
});
