// Bot settings pick who a Bot may hand work to. Contract: delegates-contract.md.
import { expect, test } from '@playwright/test';
import { api, type Machine, newBotAPI, sidebarRow, startAtoll } from './atoll';

let m: Machine;
test.beforeAll(async () => {
  m = await startAtoll();
});
test.afterAll(() => m?.stop());

test('Bot settings pick the other Bots a Bot can hand work to', async ({ page }) => {
  const poteto = await newBotAPI(m, 'Poteto');
  const lagoon = await newBotAPI(m, 'Lagoon');
  await newBotAPI(m, 'Mako');
  await page.goto(m.url);
  await sidebarRow(page, 'Poteto').click();
  await page.getByRole('button', { name: 'Bot settings' }).click();
  const panel = page.getByRole('dialog', { name: 'Bot settings' });

  await expect(panel.getByRole('heading', { name: 'Can hand work to' })).toBeVisible();
  await expect(
    panel.getByText('Poteto can start up to 3 helper threads at once with these Bots'),
  ).toBeVisible();

  await panel.getByRole('button', { name: 'Add Bot' }).click();
  const add = page.getByRole('dialog', { name: 'Add Bot' });
  await expect(add.getByRole('menuitem', { name: /Lagoon/ })).toBeVisible();
  await expect(add.getByRole('menuitem', { name: /Mako/ })).toBeVisible();
  await expect(add.getByRole('menuitem', { name: /Poteto/ })).toHaveCount(0);
  await add.getByRole('menuitem', { name: /Lagoon/ }).click();
  await add.getByRole('button', { name: 'Done' }).click();

  await expect(panel.getByRole('button', { name: 'Remove Lagoon' })).toBeVisible();
  await expect
    .poll(async () => (await api(m, 'GET', `/api/bots/${poteto.id}`)).delegates)
    .toEqual([lagoon.id]);
});
