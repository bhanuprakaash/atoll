// Slice 6 Bots: New Bot is a full page with a live preview, and Create opens the new Bot's empty
// thread; the Bot settings panel saves as you change it and applies from the next run.
import { expect, test } from '@playwright/test';
import {
  api,
  type Machine,
  newBotAPI,
  pickModel,
  prompts,
  sidebarRow,
  startAtoll,
  threadTitle,
} from './atoll';

let m: Machine;
test.beforeAll(async () => {
  m = await startAtoll();
});
test.afterAll(() => m?.stop());

const bot = async (name: string) =>
  (await api(m, 'GET', '/api/bots')).find((b: { name: string }) => b.name === name);

test('New Bot creates the Bot and opens its empty thread; a starter sends', async ({ page }) => {
  await page.goto(m.url);
  await page.getByRole('button', { name: 'New Bot' }).click();
  await expect(page.getByRole('heading', { name: 'New Bot' })).toBeVisible();
  await page.getByRole('textbox', { name: 'Name' }).fill('Coral');
  await page.getByRole('textbox', { name: 'Persona' }).fill('Reviews Go carefully.');
  await page
    .getByRole('radiogroup', { name: 'Creature' })
    .getByRole('radio', { name: 'Seal' })
    .check();
  await pickModel(page, page, 'Claude Code', 'Reef Probe');
  await expect(page.getByRole('region', { name: 'Preview' })).toContainText('Coral');
  await page.getByRole('button', { name: 'Create Bot' }).click();

  await expect(page.getByRole('heading', { name: 'What should Coral work on?' })).toBeVisible();
  await expect(sidebarRow(page, 'Coral').getByRole('img', { name: 'Seal, idle' })).toBeVisible();
  const coral = await bot('Coral');
  expect(coral).toMatchObject({
    look: 'seal',
    model: 'reef-probe',
    provider: 'claude',
  });

  const starter = page.getByRole('group', { name: 'Starters' }).getByRole('button').first();
  const text = ((await starter.textContent()) ?? '').trim();
  await starter.click();
  await expect(
    page.getByRole('log', { name: 'Messages' }).getByRole('article', { name: 'You' }),
  ).toHaveText(text);
  const [thread] = await api(m, 'GET', `/api/bots/${coral.id}/threads`);
  expect(thread.title).toBe(threadTitle(text));
});

test('Bot settings save as you change them and apply from the next run', async ({ page }) => {
  const kelp = await newBotAPI(m, 'Kelp', 'claude', { look: 'whale' });
  await page.goto(m.url);
  await sidebarRow(page, 'Kelp').click();
  await page.getByRole('button', { name: 'Bot settings' }).click();
  const panel = page.getByRole('dialog', { name: 'Bot settings' });

  const name = panel.getByRole('textbox', { name: 'Name' });
  await name.fill('Kelp Two');
  await name.blur();
  await panel
    .getByRole('radiogroup', { name: 'Creature' })
    .getByRole('radio', { name: 'Narwhal' })
    .check();
  await panel.getByRole('textbox', { name: 'Persona' }).fill('Writes release notes.');
  await panel.getByRole('textbox', { name: 'Persona' }).blur();
  await pickModel(page, panel, 'Claude Code', 'Sonnet');
  await expect(panel.getByText(/Saved/)).toBeVisible();
  await expect(panel.getByRole('button', { name: /^Save$/ })).toHaveCount(0);

  await expect
    .poll(async () => api(m, 'GET', `/api/bots/${kelp.id}`))
    .toMatchObject({
      name: 'Kelp Two',
      look: 'narwhal',
      persona: 'Writes release notes.',
      model: 'sonnet',
    });
  await expect(
    sidebarRow(page, 'Kelp Two').getByRole('img', { name: 'Narwhal, idle' }),
  ).toBeVisible();

  await panel.getByRole('button', { name: 'Close' }).click();
  await page.getByRole('textbox', { name: 'Message' }).fill(prompts.keep);
  await page.getByRole('button', { name: 'Send' }).click();
  await expect(
    page.getByRole('log', { name: 'Messages' }).getByText('ok', { exact: true }),
  ).toBeVisible();
  const withModel = m
    .calls('claude')
    .filter((c) => c.argv?.includes('-p'))
    .map((c) => c.argv![c.argv!.indexOf('--model') + 1]);
  expect(withModel).toContain('sonnet');
});
