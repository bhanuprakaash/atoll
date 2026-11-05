// Slice 6 Threads panel: a thread waiting on an approval shows under Needs you with what it waits
// for, and the Threads button counts it.
import { expect, test } from '@playwright/test';
import {
  api,
  cut40,
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

test('Needs you lists the waiting thread and what it waits for; the Threads button counts it', async ({
  page,
}) => {
  const tide = await newBotAPI(m, 'Tide');
  const closed = await openThreadAPI(m, tide.id, prompts.keep);
  await waitStatus(closed.id, 'done');
  await api(m, 'POST', `/api/threads/${closed.id}/close`);
  const recent = await openThreadAPI(m, tide.id, prompts.keep);
  await waitStatus(recent.id, 'done');
  const waiting = await openThreadAPI(m, tide.id, prompts.write);
  await waitStatus(waiting.id, 'waiting');

  await page.goto(m.url);
  await sidebarRow(page, 'Tide').click();
  const threads = page.getByRole('button', { name: /^Threads/ });
  await expect(threads).toHaveAccessibleName(/^Threads\b.*\b1\b/);

  const panel = await openThreads(page);
  await expect(panel.getByRole('button', { name: 'New thread' })).toBeVisible();
  const needs = panel.getByRole('list', { name: 'Needs you' }).getByRole('listitem');
  await expect(needs).toHaveCount(1);
  await expect(needs).toContainText(cut40(prompts.write));
  await expect(needs).toContainText('hello.txt');
  const recentList = panel.getByRole('list', { name: 'Recent' });
  await expect(recentList.getByRole('listitem')).toHaveCount(1);
  await expect(recentList).toContainText(cut40(prompts.keep));
  await expect(recentList).not.toContainText(cut40(prompts.write));

  const closedList = panel.getByRole('list', { name: 'Closed' });
  await expect(closedList).toHaveCount(0);
  await panel.getByRole('button', { name: /^Closed/ }).click();
  await expect(closedList.getByRole('listitem')).toHaveCount(1);

  await needs.click();
  const log = page.getByRole('log', { name: 'Messages' });
  await expect(log.getByRole('article', { name: 'You' })).toHaveText(prompts.write);
  await log.getByRole('button', { name: 'Allow', exact: true }).click();
  await waitStatus(waiting.id, 'done');
  await expect(threads).toHaveAccessibleName('Threads');
});

test('a running thread shows under Running', async ({ page }) => {
  const reed = await newBotAPI(m, 'Reed');
  const busy = await openThreadAPI(m, reed.id, prompts.hang);
  await waitStatus(busy.id, 'running');
  await page.goto(m.url);
  await sidebarRow(page, 'Reed').click();
  const panel = await openThreads(page);
  await expect(panel.getByRole('list', { name: 'Running' })).toContainText(cut40(prompts.hang));
  await api(m, 'POST', `/api/runs/${busy.run.id}/stop`);
});
