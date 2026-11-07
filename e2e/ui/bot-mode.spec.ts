// Bot mode in Bot settings: Auto, Ask or Full access; Full access says what it means.
import { expect, test } from '@playwright/test';
import { api, type Machine, newBotAPI, sidebarRow, startAtoll } from './atoll';

const fullAccess =
  'Full access: this Bot never asks, even to delete files or push. Use it for Bots that run in a VM.';

let m: Machine;
test.beforeAll(async () => {
  m = await startAtoll();
});
test.afterAll(() => m?.stop());

test('Bot settings pick a Mode; Full access warns and the choice persists', async ({ page }) => {
  const nori = await newBotAPI(m, 'Nori');
  await page.goto(m.url);
  await sidebarRow(page, 'Nori').click();
  await page.getByRole('button', { name: 'Bot settings' }).click();
  const panel = page.getByRole('dialog', { name: 'Bot settings' });
  const mode = panel.getByRole('radiogroup', { name: 'Mode' });

  await expect(mode.getByRole('radio', { name: 'Auto' })).toBeChecked();
  await expect(mode.getByRole('radio', { name: 'Ask' })).not.toBeChecked();
  await expect(panel.getByText(fullAccess, { exact: true })).toHaveCount(0);

  await mode.getByRole('radio', { name: 'Full access' }).check();
  await expect(panel.getByText(fullAccess, { exact: true })).toBeVisible();
  await expect.poll(async () => (await api(m, 'GET', `/api/bots/${nori.id}`)).mode).toBe('full');

  await page.reload();
  await page.getByRole('button', { name: 'Bot settings' }).click();
  await expect(mode.getByRole('radio', { name: 'Full access' })).toBeChecked();
  await expect(panel.getByText(fullAccess, { exact: true })).toBeVisible();

  await mode.getByRole('radio', { name: 'Ask' }).check();
  await expect(panel.getByText(fullAccess, { exact: true })).toHaveCount(0);
  await expect.poll(async () => (await api(m, 'GET', `/api/bots/${nori.id}`)).mode).toBe('ask');
});
