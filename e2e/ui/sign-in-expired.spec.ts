// A run that fails because its provider's sign-in was revoked says "<Provider> sign-in expired"
// with Sign in again, never the provider's raw error. Sign in again runs the provider's own
// sign-in; after it, Retry sends the same message and the run completes. The Providers page
// shows the provider signed out after the failed run. Contract: sign-in-expired-contract.md.
import { expect, type Page, test } from '@playwright/test';
import {
  api,
  loginURL,
  type Machine,
  newBotAPI,
  prompts,
  recordText,
  seenText,
  sidebarRow,
  startAtoll,
} from './atoll';

let m: Machine;
test.beforeAll(async () => {
  m = await startAtoll({ fakes: ['claude', 'codex'], signedIn: ['claude', 'codex'] });
});
test.afterAll(() => m?.stop());

const cases = [
  {
    provider: 'claude',
    label: 'Claude Code',
    line: 'Claude sign-in expired',
    bot: 'Kelp',
    raw: /Failed to authenticate|OAuth token revoked/,
    // Claude's sign-in asks for the code its page shows.
    finish: async (page: Page) => {
      await page.getByRole('textbox', { name: 'Code', exact: true }).fill('reef-test-code');
      await page.getByRole('button', { name: 'Submit', exact: true }).click();
    },
  },
  {
    provider: 'codex',
    label: 'Codex',
    line: 'Codex sign-in expired',
    bot: 'Coral',
    raw: /access token could not be refreshed|refresh token was revoked/,
    finish: async () => m.release('codex'),
  },
] as const;

const signedIn = async (provider: string) =>
  (await api(m, 'GET', '/api/providers')).find((p: { name: string }) => p.name === provider)
    .signed_in;

for (const c of cases) {
  test(`${c.line}: Sign in again, then Retry completes the run`, async ({ page, context }) => {
    await context.route('https://login.reef.test/**', (r) => r.fulfill({ body: 'signing in' }));
    const bot = await newBotAPI(m, c.bot, c.provider);
    await page.goto(m.url);
    await sidebarRow(page, c.bot).click();
    await expect(
      page.getByRole('heading', { name: `What should ${c.bot} work on?` }),
    ).toBeVisible();
    await recordText(page);

    m.revoke(c.provider);
    await page.getByRole('textbox', { name: 'Message' }).fill(prompts.pong);
    await page.getByRole('button', { name: 'Send' }).click();

    const log = page.getByRole('log', { name: 'Messages' });
    await expect(log.getByText(c.line)).toBeVisible();
    await expect(log.getByRole('button', { name: 'Sign in again' })).toBeVisible();
    expect(await seenText(page)).not.toMatch(c.raw);
    const [thread] = await api(m, 'GET', `/api/bots/${bot.id}/threads`);
    expect(thread.run_status).toBe('failed');
    expect((await api(m, 'GET', `/api/runs/${thread.run_id}`)).error_kind).toBe('signed_out');

    await page
      .getByRole('navigation', { name: 'Bots' })
      .getByRole('link', { name: /Settings/ })
      .click();
    await page.getByRole('link', { name: 'Providers' }).click();
    await expect(page.getByRole('group', { name: c.label })).toContainText('Not signed in');
    expect(await signedIn(c.provider)).toBe(false);

    // The line is kept on the run, so it is still there when the thread is opened again.
    await page.goto(`${m.url}/bots/${bot.id}/threads/${thread.id}`);
    await expect(log.getByText(c.line)).toBeVisible();
    const popup = page.waitForEvent('popup');
    await log.getByRole('button', { name: 'Sign in again' }).click();
    expect((await popup).url()).toBe(loginURL[c.provider]);
    await c.finish(page);
    await expect.poll(() => signedIn(c.provider)).toBe(true);

    await log.getByRole('button', { name: 'Retry' }).click();
    // The fake replies pong only to a prompt that holds the same message.
    await expect(log.getByText('pong', { exact: true })).toBeVisible();
    await expect
      .poll(async () => (await api(m, 'GET', `/api/threads/${thread.id}`)).run_status)
      .toBe('done');
  });
}

test('the sign-in expired line names the provider of the failed run', async ({ page }) => {
  const bot = await newBotAPI(m, 'Wrasse', 'claude');
  await page.goto(m.url);
  await sidebarRow(page, 'Wrasse').click();
  m.revoke('claude');
  await page.getByRole('textbox', { name: 'Message' }).fill(prompts.pong);
  await page.getByRole('button', { name: 'Send' }).click();
  const log = page.getByRole('log', { name: 'Messages' });
  await expect(log.getByText('Claude sign-in expired')).toBeVisible();

  await api(m, 'PUT', `/api/bots/${bot.id}`, { provider: 'codex' });
  const [thread] = await api(m, 'GET', `/api/bots/${bot.id}/threads`);
  await page.goto(`${m.url}/bots/${bot.id}/threads/${thread.id}`);
  await expect(log.getByText('Claude sign-in expired')).toBeVisible();
  await expect(log.getByText('Codex sign-in expired')).toHaveCount(0);
});
