// Threads that need the user: a thread waiting on an approval gets a tab with the light dot and
// shows in the island's notification list with what it waits for, and the island counts it.
import { expect, test } from '@playwright/test';
import {
  api,
  type Machine,
  marks,
  moreButton,
  newBotAPI,
  openThreadAPI,
  prompts,
  sidebarRow,
  startAtoll,
  threadTab,
  threadTitle,
  waitStatus,
} from './atoll';

let m: Machine;
test.beforeAll(async () => {
  m = await startAtoll();
});
test.afterAll(() => m?.stop());

test('Needs you lists the waiting thread and what it waits for; the island counts it', async ({
  page,
}) => {
  const tide = await newBotAPI(m, 'Tide');
  const closed = await openThreadAPI(m, tide.id, prompts.keep);
  await waitStatus(m, closed.id, 'done');
  await api(m, 'POST', `/api/threads/${closed.id}/close`);
  const recent = await openThreadAPI(m, tide.id, prompts.pong);
  await waitStatus(m, recent.id, 'done');
  const waiting = await openThreadAPI(m, tide.id, prompts.write);
  await waitStatus(m, waiting.id, 'waiting');

  await page.goto(`${m.url}/bots/${tide.id}/threads/${recent.id}`);
  const asking = threadTab(page, threadTitle(prompts.write));
  await expect.poll(async () => (await marks(asking)).map((d) => d.colour)).toEqual(['light']);
  await expect(threadTab(page, threadTitle(prompts.keep))).toHaveCount(0);
  await expect(moreButton(page)).toHaveCount(0);
  await page.keyboard.press('Escape');
  const island = page
    .locator('header')
    .getByRole('button', { name: 'Notifications, 1 waiting', exact: true });
  await expect(island).toBeVisible();

  await island.click();
  const list = page.getByRole('dialog', { name: 'Notifications' });
  const needs = list.getByRole('list', { name: 'Needs you' }).getByRole('listitem');
  await expect(needs).toHaveCount(1);
  await expect(needs).toContainText(threadTitle(prompts.write));
  await expect(needs).toContainText('hello.txt');

  await needs.getByRole('button', { name: 'Allow', exact: true }).click();
  await waitStatus(m, waiting.id, 'done');
  await expect(
    page.locator('header').getByRole('button', { name: 'Notifications', exact: true }),
  ).toBeVisible();
  await expect(sidebarRow(page, 'Tide')).not.toContainText('Wants to');
});
