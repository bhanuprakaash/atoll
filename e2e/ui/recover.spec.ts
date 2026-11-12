// Slice 6: when the server dies the app says it is reconnecting; served again on the same
// address, the open thread recovers and takes the next message. (The Tauri shell restarting a
// killed sidecar is in mac-app-contract.md, "Only you can check".)
import { expect, test } from '@playwright/test';
import {
  type Machine,
  newBotAPI,
  openThreadAPI,
  prompts,
  api,
  sidebarRow,
  startAtoll,
} from './atoll';

let m: Machine;
test.beforeAll(async () => {
  m = await startAtoll();
});
test.afterAll(() => m?.stop());

test('the open thread recovers after the server is killed and served again', async ({ page }) => {
  const b = await newBotAPI(m, 'Pearl');
  const th = await openThreadAPI(m, b.id, prompts.keep);
  await expect
    .poll(async () => (await api(m, 'GET', `/api/threads/${th.id}`)).run_status, {
      timeout: 15_000,
    })
    .toBe('done');
  await page.goto(m.url);
  await sidebarRow(page, 'Pearl').click();
  const log = page.getByRole('log', { name: 'Messages' });
  await expect(log.getByText('ok', { exact: true })).toBeVisible();

  m.kill();
  await expect(page.getByRole('status').filter({ hasText: 'Reconnecting' })).toHaveCount(1);
  await expect(page.getByText('Reconnecting')).toHaveCount(1);
  await m.restart();
  await expect(page.getByText('Reconnecting')).toHaveCount(0, {
    timeout: 20_000,
  });
  await expect(log.getByRole('article', { name: 'You' })).toHaveText(prompts.keep);

  await page
    .getByRole('textbox', { name: 'Message' })
    .fill('Which codeword did I give you? Reply with just the word.');
  await page.getByRole('button', { name: 'Send' }).click();
  await expect(log.getByText('kestrel', { exact: true })).toBeVisible();
});
