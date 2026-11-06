// Claude sign-in with a pasted code: once claude prints its "Paste code here if prompted >"
// prompt, the provider row (Settings and the first-run card) shows a Code field and Submit, which
// send the code to claude's stdin. The fake claude accepts only reef-test-code and prints
// "Invalid code" for anything else, as the real one does. Codex never shows the field.
import { expect, type Page, test } from '@playwright/test';
import { api, loginURL, type Machine, startAtoll } from './atoll';

const code = 'reef-test-code';

let m: Machine | undefined;
test.afterEach(() => m?.stop());

async function openProviders(page: Page) {
  await page.goto(m!.url);
  await page
    .getByRole('navigation', { name: 'Bots' })
    .getByRole('link', { name: /Settings/ })
    .click();
  await page.getByRole('link', { name: 'Providers' }).click();
}

test.beforeEach(async ({ context }) => {
  await context.route('https://login.reef.test/**', (r) => r.fulfill({ body: 'signing in' }));
});

test('Settings: the right code signs Claude in', async ({ page }) => {
  m = await startAtoll({ fakes: ['claude'], signedIn: [] });
  await openProviders(page);
  const claude = page.getByRole('group', { name: 'Claude Code' });
  await expect(claude).toContainText('Not signed in');
  await expect(claude.getByRole('textbox', { name: 'Code', exact: true })).toHaveCount(0);

  await claude.getByRole('button', { name: 'Sign in' }).click();
  const field = claude.getByRole('textbox', { name: 'Code', exact: true });
  await expect(field).toBeVisible();
  await field.fill(code);
  await claude.getByRole('button', { name: 'Submit' }).click();

  await expect(claude).toContainText('Signed in');
  await expect(claude).not.toContainText(/JSON|pattern|error/i);
  await expect(field).toHaveCount(0);
  await expect(claude.getByRole('button', { name: 'Sign in' })).toHaveCount(0);
  expect((await api(m, 'GET', '/api/providers'))[0].signed_in).toBe(true);
});

test('Settings: a wrong code shows the error and Sign in works again', async ({ page }) => {
  m = await startAtoll({ fakes: ['claude'], signedIn: [] });
  await openProviders(page);
  const claude = page.getByRole('group', { name: 'Claude Code' });
  const field = claude.getByRole('textbox', { name: 'Code', exact: true });

  await claude.getByRole('button', { name: 'Sign in' }).click();
  await expect(field).toBeVisible();
  await field.fill('not-the-code');
  await claude.getByRole('button', { name: 'Submit' }).click();

  await expect(claude).toContainText('Invalid code');
  await expect(claude).toContainText('Not signed in');
  await expect(claude.getByRole('button', { name: 'Sign in' })).toBeEnabled();
  expect((await api(m, 'GET', '/api/providers'))[0].signed_in).toBe(false);

  await claude.getByRole('button', { name: 'Sign in' }).click();
  await expect(field).toBeVisible();
  await field.fill(code);
  await claude.getByRole('button', { name: 'Submit' }).click();
  await expect(claude).toContainText('Signed in');
  await expect(field).toHaveCount(0);
  expect((await api(m, 'GET', '/api/providers'))[0].signed_in).toBe(true);
});

test('first run: the provider card takes the code', async ({ page }) => {
  m = await startAtoll({ fakes: ['claude'], signedIn: [], userName: null });
  await page.goto(m.url);
  await page.getByRole('textbox', { name: 'What should your Bots call you?' }).fill('Ada');
  await page.getByRole('button', { name: 'Continue' }).click();

  const claude = page.getByRole('group', { name: 'Claude Code' });
  await expect(claude).toContainText('Not signed in');
  await claude.getByRole('button', { name: 'Sign in' }).click();
  const field = claude.getByRole('textbox', { name: 'Code', exact: true });
  await expect(field).toBeVisible();
  await expect(claude.getByRole('button', { name: 'Submit' })).toBeVisible();
  await field.fill(code);
  await claude.getByRole('button', { name: 'Submit' }).click();

  await expect(claude).toContainText('Signed in');
  await expect(claude).not.toContainText(/JSON|pattern|error/i);
  await expect(field).toHaveCount(0);
  expect((await api(m, 'GET', '/api/providers'))[0].signed_in).toBe(true);
});

test('Codex sign-in shows no Code field', async ({ page }) => {
  m = await startAtoll({ fakes: ['claude', 'codex'], signedIn: ['claude'] });
  await openProviders(page);
  const codex = page.getByRole('group', { name: 'Codex' });
  await expect(codex).toContainText('Not signed in');

  const popup = page.waitForEvent('popup');
  await codex.getByRole('button', { name: 'Sign in' }).click();
  expect((await popup).url()).toBe(loginURL.codex);
  await expect(codex.getByRole('textbox')).toHaveCount(0);
  await expect(codex.getByRole('button', { name: 'Submit' })).toHaveCount(0);

  m.release('codex');
  await expect(codex).toContainText('Signed in');
  await expect(codex.getByRole('textbox')).toHaveCount(0);
});
