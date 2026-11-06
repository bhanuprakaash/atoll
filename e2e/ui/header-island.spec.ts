// Header island: the Bot header shows the open thread's run state, opens the Threads panel on a
// click only, offers search past 8 open threads, and keeps Bot settings visible.
import { expect, test } from '@playwright/test';
import {
  api,
  type Machine,
  newBotAPI,
  openThreadAPI,
  openThreads,
  prompts,
  sidebarRow,
  startAtoll,
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

test('the header shows the open thread run state', async ({ page }) => {
  const bot = await newBotAPI(m, 'Pilot');
  const busy = await openThreadAPI(m, bot.id, prompts.hang);
  const waiting = await openThreadAPI(m, bot.id, prompts.write);
  const failed = await openThreadAPI(m, bot.id, prompts.none);
  const done = await openThreadAPI(m, bot.id, prompts.keep);
  await waitStatus(busy.id, 'running');
  await waitStatus(waiting.id, 'waiting');
  await waitStatus(failed.id, 'failed');
  await waitStatus(done.id, 'done');

  const header = page.locator('header');
  await page.goto(`${m.url}/bots/${bot.id}/threads/${busy.id}`);
  await expect(header).toContainText('Thinking');
  await page.goto(`${m.url}/bots/${bot.id}/threads/${waiting.id}`);
  await expect(header).toContainText('Needs you');
  await page.goto(`${m.url}/bots/${bot.id}/threads/${failed.id}`);
  await expect(header).toContainText('Run failed');
  await page.goto(`${m.url}/bots/${bot.id}/threads/${done.id}`);
  await expect(header.getByRole('heading', { name: /^Remember this fact/ })).toBeVisible();
  await expect(header).not.toContainText('Thinking');
  await expect(header).not.toContainText('Needs you');
  await expect(header).not.toContainText('Run failed');

  await api(m, 'POST', `/api/runs/${busy.run.id}/stop`);
});

test('search shows past 8 open threads and filters by title', async ({ page }) => {
  const names = ['Alpha', 'Bravo', 'Gamma', 'Delta', 'Echo', 'Foxtrot', 'Hotel', 'India', 'Juliet'];
  const many = await newBotAPI(m, 'Many');
  const eight = await newBotAPI(m, 'Eight');
  const threads = await Promise.all([
    ...names.map(() => openThreadAPI(m, many.id, prompts.keep)),
    ...names.slice(1).map(() => openThreadAPI(m, eight.id, prompts.keep)),
  ]);
  await Promise.all(threads.map((t) => waitStatus(t.id, 'done')));
  for (const [i, name] of names.entries()) {
    await api(m, 'PATCH', `/api/threads/${threads[i].id}`, { title: `${name} notes` });
  }

  await page.goto(m.url);
  await sidebarRow(page, 'Many').click();
  let panel = await openThreads(page);
  const search = panel.getByRole('searchbox', { name: 'Search threads' });
  await expect(search).toBeVisible();
  await search.fill('Gamma');
  const items = panel.getByRole('list').getByRole('listitem');
  await expect(items).toHaveCount(1);
  await expect(items).toContainText('Gamma notes');

  await page.goto(m.url);
  await sidebarRow(page, 'Eight').click();
  panel = await openThreads(page);
  await expect(panel.getByRole('searchbox', { name: 'Search threads' })).toHaveCount(0);
});

test('hovering Threads does not open the panel; a click does', async ({ page }) => {
  await newBotAPI(m, 'Hover');
  await page.goto(m.url);
  await sidebarRow(page, 'Hover').click();
  await page.getByRole('button', { name: /^Threads/ }).hover();
  await page.waitForTimeout(600);
  await expect(page.getByRole('dialog', { name: 'Threads with Hover' })).toHaveCount(0);
  await expect(await openThreads(page)).toHaveAccessibleName('Threads with Hover');
});

test('Bot settings is visible in the header', async ({ page }) => {
  await newBotAPI(m, 'Gear');
  await page.goto(m.url);
  await sidebarRow(page, 'Gear').click();
  await expect(page.getByRole('button', { name: 'Bot settings' })).toBeVisible();
});
