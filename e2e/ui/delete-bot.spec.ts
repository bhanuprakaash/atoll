import { expect, test } from '@playwright/test';
import { type Machine, newBotAPI, sidebarRow, startAtoll } from './atoll';

let m: Machine;
test.beforeAll(async () => {
  m = await startAtoll();
  await newBotAPI(m, 'Mako');
});
test.afterAll(() => m?.stop());

test('Bot settings delete a Bot after confirmation, but not Chief', async ({ page }) => {
  await page.goto(m.url);
  await sidebarRow(page, 'Mako').click();
  await page.getByRole('button', { name: 'Bot settings' }).click();
  const settings = page.getByRole('dialog', { name: 'Bot settings' });
  await expect(settings.getByRole('button', { name: 'Delete Bot' })).toBeVisible();
  await settings.getByRole('button', { name: 'Delete Bot' }).click();

  const confirmation = page.getByRole('dialog').filter({ hasText: 'Mako' });
  await expect(confirmation).toBeVisible();
  await confirmation.getByRole('button', { name: 'Delete', exact: true }).click();
  await expect(sidebarRow(page, 'Mako')).toHaveCount(0);
  await expect(page.getByRole('heading', { name: 'Chief' })).toBeVisible();

  await page.getByRole('button', { name: 'Bot settings' }).click();
  await expect(
    page
      .getByRole('dialog', { name: 'Bot settings' })
      .getByRole('button', { name: 'Delete Bot' }),
  ).toHaveCount(0);
});
