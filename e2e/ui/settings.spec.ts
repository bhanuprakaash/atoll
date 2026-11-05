// Slice 6 Settings: Providers show where each CLI was found, its version, models and sign-in
// state, and Sign in runs the provider's own login with no Terminal; General saves the name.
import { expect, test } from '@playwright/test';
import { api, loginURL, type Machine, startAtoll } from './atoll';

let m: Machine;
test.beforeAll(async () => {
  m = await startAtoll({ fakes: ['claude', 'codex'] });
});
test.afterAll(() => m?.stop());

test('Providers page shows each provider and signs in Codex', async ({ page, context }) => {
  await context.route('https://login.reef.test/**', (r) => r.fulfill({ body: 'signing in' }));
  await page.goto(m.url);
  await page
    .getByRole('navigation', { name: 'Bots' })
    .getByRole('link', { name: /Settings/ })
    .click();
  await page.getByRole('link', { name: 'Providers' }).click();

  const claude = page.getByRole('group', { name: 'Claude Code' });
  await expect(claude).toContainText(`${m.bin}/claude`);
  await expect(claude).toContainText('2.1.306');
  await expect(claude).toContainText('Reef Probe');
  await expect(claude).toContainText('Signed in');

  const codex = page.getByRole('group', { name: 'Codex' });
  await expect(codex).toContainText('0.166.0');
  await expect(codex).toContainText('Reef Probe Codex');
  await expect(codex).toContainText('Not signed in');
  const popup = page.waitForEvent('popup');
  await codex.getByRole('button', { name: 'Sign in' }).click();
  expect((await popup).url()).toBe(loginURL.codex);
  m.release('codex');
  await expect(codex).toContainText('Signed in');
  expect((await api(m, 'GET', '/api/providers'))[1].signed_in).toBe(true);
});

test('General saves the name', async ({ page }) => {
  await page.goto(m.url);
  await page
    .getByRole('navigation', { name: 'Bots' })
    .getByRole('link', { name: /Settings/ })
    .click();
  await page.getByRole('link', { name: 'General' }).click();
  const name = page.getByRole('textbox', { name: 'Your name' });
  await expect(name).toHaveValue('Ada');
  await expect(page.getByRole('textbox', { name: 'Worktree folder' })).toHaveValue(
    `${m.dir}/worktrees`,
  );
  await name.fill('Grace');
  await name.blur();
  await expect.poll(async () => (await api(m, 'GET', '/api/settings')).user_name).toBe('Grace');
});
