import { expect, type Page, test } from '@playwright/test';
import { api, type Machine, newBotAPI, startAtoll } from './atoll';

let m: Machine;
test.beforeAll(async () => {
  m = await startAtoll();
});
test.afterAll(() => m?.stop());

// watchConsole collects every console error and page error of the page from now on.
function watchConsole(page: Page) {
  const errors: string[] = [];
  page.on('console', (msg) => msg.type() === 'error' && errors.push(msg.text()));
  page.on('pageerror', (err) => errors.push(err.message));
  return errors;
}

// onlyNotFound is true when every console error is the browser's line for a 404 GET.
const onlyNotFound = (errors: string[]) =>
  errors.every((e) => /Failed to load resource.*\b404\b/.test(e));

test('a missing Bot says it is gone, with one link home', async ({ page }) => {
  const errors = watchConsole(page);
  await page.goto(`${m.url}/bots/nonexistent`);

  const main = page.getByRole('main');
  await expect(main.getByText('This Bot is gone')).toBeVisible();
  await expect(main.getByRole('link')).toHaveCount(1);
  await expect(main.getByText('Reconnecting')).toHaveCount(0);
  expect(onlyNotFound(errors), errors.join('\n')).toBe(true);
  expect(page.url()).toBe(`${m.url}/bots/nonexistent`);

  await main.getByRole('link', { name: 'Go home' }).click();
  await expect(main.getByText('This Bot is gone')).toHaveCount(0);
  await expect(page.getByRole('heading', { name: 'Chief', exact: true })).toBeVisible();
});

test('a deleted Bot, and a thread under it, say the Bot is gone', async ({ page }) => {
  const bot = await newBotAPI(m, 'Wren');
  const thread = await api(m, 'POST', `/api/bots/${bot.id}/threads`, {});
  await api(m, 'DELETE', `/api/bots/${bot.id}`);
  const errors = watchConsole(page);

  for (const path of [`/bots/${bot.id}`, `/bots/${bot.id}/threads/${thread.id}`]) {
    await page.goto(m.url + path);
    const main = page.getByRole('main');
    await expect(main.getByText('This Bot is gone')).toBeVisible();
    await expect(main.getByText('This thread is gone')).toHaveCount(0);
    await expect(main.getByRole('link')).toHaveCount(1);
    await expect(main.getByText('Reconnecting')).toHaveCount(0);
  }
  expect(onlyNotFound(errors), errors.join('\n')).toBe(true);
});

test('a missing thread says it is gone, with one link to the Bot newest thread', async ({
  page,
}) => {
  const bot = await newBotAPI(m, 'Mako');
  await api(m, 'POST', `/api/bots/${bot.id}/threads`, {});
  const newest = await api(m, 'POST', `/api/bots/${bot.id}/threads`, {});
  const errors = watchConsole(page);
  const gone = `/bots/${bot.id}/threads/zzzz`;
  await page.goto(m.url + gone);

  const main = page.getByRole('main');
  await expect(main.getByText('This thread is gone')).toBeVisible();
  await expect(main.getByRole('link')).toHaveCount(1);
  await expect(main.getByText('Reconnecting')).toHaveCount(0);
  await expect(main.getByRole('textbox')).toHaveCount(0);
  expect(onlyNotFound(errors), errors.join('\n')).toBe(true);
  expect(page.url()).toBe(m.url + gone);

  await main.getByRole('link', { name: 'Open the newest thread' }).click();
  await expect(page).toHaveURL(`${m.url}/bots/${bot.id}/threads/${newest.id}`);
  await expect(main.getByText('This thread is gone')).toHaveCount(0);
});

test('Chief, whose Bot exists, still shows a missing thread as gone', async ({ page }) => {
  await page.goto(`${m.url}/bots/chief/threads/zzzz`);
  const main = page.getByRole('main');
  await expect(main.getByText('This thread is gone')).toBeVisible();
  await expect(main.getByText('Reconnecting')).toHaveCount(0);
});
