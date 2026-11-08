// Threads that need the user: a thread waiting on an approval shows under its Bot as Needs you and
// in the island's notification list with what it waits for, and the island counts it.
import { expect, test } from '@playwright/test';
import {
  api,
  type Machine,
  newBotAPI,
  openThreadAPI,
  prompts,
  sidebarRow,
  startAtoll,
  threadTitle,
  threadTree,
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

test('Needs you lists the waiting thread and what it waits for; the island counts it', async ({
  page,
}) => {
  const tide = await newBotAPI(m, 'Tide');
  const closed = await openThreadAPI(m, tide.id, prompts.keep);
  await waitStatus(closed.id, 'done');
  await api(m, 'POST', `/api/threads/${closed.id}/close`);
  const recent = await openThreadAPI(m, tide.id, prompts.pong);
  await waitStatus(recent.id, 'done');
  const waiting = await openThreadAPI(m, tide.id, prompts.write);
  await waitStatus(waiting.id, 'waiting');

  await page.goto(`${m.url}/bots/${tide.id}/threads/${recent.id}`);
  const tree = threadTree(page, 'Tide');
  await expect(tree.locator('[data-marker="needs-you"]')).toContainText(threadTitle(prompts.write));
  await expect(tree).not.toContainText(threadTitle(prompts.keep));
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
  await waitStatus(waiting.id, 'done');
  await expect(
    page.locator('header').getByRole('button', { name: 'Notifications', exact: true }),
  ).toBeVisible();
  await expect(sidebarRow(page, 'Tide')).not.toContainText('Needs you');
});

test('a running thread shows under its Bot with wave dots', async ({ page }) => {
  const reed = await newBotAPI(m, 'Reed');
  const busy = await openThreadAPI(m, reed.id, prompts.hang);
  await waitStatus(busy.id, 'running');
  await page.goto(m.url);
  await expect(threadTree(page, 'Reed').locator('[data-marker="running"]')).toContainText(
    threadTitle(prompts.hang),
  );
  await api(m, 'POST', `/api/runs/${busy.run.id}/stop`);
});
