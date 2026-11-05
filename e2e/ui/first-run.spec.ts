// Slice 6 first run: three steps over drifting creatures. The user's name; the providers found,
// with Sign in running the provider's own login; Meet Chief, whose example asks open Chief's
// thread with that message sent.
import { expect, test } from '@playwright/test';
import { api, cut40, loginURL, type Machine, running, startAtoll } from './atoll';

let m: Machine;
test.beforeAll(async () => {
  // Fresh install: claude installed but signed out, codex not installed, no user name.
  m = await startAtoll({ fakes: ['claude'], signedIn: [], userName: null });
});
test.afterAll(() => m?.stop());
test.describe.configure({ mode: 'serial' });

test('first run animates, and reduced motion stops it', async ({ page }) => {
  await page.emulateMedia({ reducedMotion: 'no-preference' });
  await page.goto(m.url);
  await expect(page.getByRole('heading', { name: 'Welcome to Reef' })).toBeVisible();
  await expect.poll(() => running(page)).toBeGreaterThan(0);
  await page.emulateMedia({ reducedMotion: 'reduce' });
  await expect.poll(() => running(page)).toBe(0);
});

test('name, sign in a provider, meet Chief and send an example ask', async ({ page, context }) => {
  await context.route('https://login.reef.test/**', (r) => r.fulfill({ body: 'signing in' }));
  await page.goto(m.url);
  await expect(page.getByRole('heading', { name: 'Welcome to Reef' })).toBeVisible();
  await page.getByRole('textbox', { name: 'What should your Bots call you?' }).fill('Ada');
  await page.getByRole('button', { name: 'Continue' }).click();

  await expect(page.getByRole('heading', { name: /Ada/ })).toBeVisible();
  const claude = page.getByRole('group', { name: 'Claude Code' });
  await expect(claude).toContainText('2.1.306');
  await expect(claude).toContainText(`${m.bin}/claude`);
  await expect(claude).toContainText('Not signed in');
  await expect(page.getByRole('group', { name: 'Codex' })).toContainText('Not found');

  const popup = page.waitForEvent('popup');
  await claude.getByRole('button', { name: 'Sign in' }).click();
  expect((await popup).url()).toBe(loginURL.claude);
  m.release('claude');
  await expect(claude).toContainText('Signed in');
  await expect(claude.getByRole('button', { name: 'Sign in' })).toHaveCount(0);
  await page.getByRole('button', { name: 'Continue' }).click();

  await expect(page.getByRole('heading', { name: 'Meet Chief' })).toBeVisible();
  const asks = page.getByRole('group', { name: 'Example asks' }).getByRole('button');
  await expect(asks).not.toHaveCount(0);
  const ask = ((await asks.first().textContent()) ?? '').trim().replace(/^[“"]|[”"]$/g, '');
  await asks.first().click();

  const log = page.getByRole('log', { name: 'Messages' });
  await expect(log.getByRole('article', { name: 'You' })).toHaveText(ask);
  expect((await api(m, 'GET', '/api/settings')).user_name).toBe('Ada');
  const [thread] = await api(m, 'GET', '/api/bots/chief/threads');
  expect(thread.title).toBe(cut40(ask));
  expect((await api(m, 'GET', '/api/providers'))[0].signed_in).toBe(true);
});

test('first run does not come back once done', async ({ page }) => {
  await page.goto(m.url);
  await expect(page.getByRole('navigation', { name: 'Bots' })).toBeVisible();
  await expect(page.getByRole('heading', { name: 'Welcome to Reef' })).toHaveCount(0);
});
