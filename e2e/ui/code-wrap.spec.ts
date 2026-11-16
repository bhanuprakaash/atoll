// The Wrap toggle of a code block is a viewer preference kept in the web app's localStorage: on in
// one block, every block in every thread is wrapped, after a reload and after the server restarts.
import { expect, type Locator, type Page, test } from '@playwright/test';
import { api, type Machine, newBotAPI, openThreadAPI, startAtoll } from './atoll';

let m: Machine;
let threads: string[];
test.beforeAll(async () => {
  m = await startAtoll();
  threads = [];
  for (const name of ['Wrap one', 'Wrap two']) {
    const bot = await newBotAPI(m, name);
    const th = await openThreadAPI(m, bot.id, 'Rich output: show code.');
    await expect
      .poll(async () => (await api(m, 'GET', `/api/threads/${th.id}`)).run_status, {
        timeout: 15_000,
      })
      .toBe('done');
    threads.push(`${m.url}/bots/${bot.id}/threads/${th.id}`);
  }
});
test.afterAll(() => m?.stop());

async function blocks(page: Page, thread: number) {
  await page.goto(threads[thread]);
  const log = page.getByRole('log', { name: 'Messages' });
  const code = log.locator('[data-component="code"]');
  await expect(code).toHaveCount(2);
  return code;
}

async function expectWrapped(code: Locator, wrapped: boolean) {
  for (const i of [0, 1]) {
    const block = code.nth(i);
    await expect(block.getByRole('button', { name: 'Wrap' })).toHaveAttribute(
      'aria-pressed',
      String(wrapped),
    );
    await expect(block.locator('pre')).toHaveCSS(
      'white-space',
      wrapped ? /^pre-wrap$|^break-spaces$/ : /^(?!pre-wrap|break-spaces)/,
    );
  }
}

test('code blocks open scrolling when nothing is saved', async ({ page }) => {
  const code = await blocks(page, 0);
  await expectWrapped(code, false);
  expect(
    await page.evaluate(() => Object.keys(localStorage).filter((k) => /wrap/i.test(k))),
  ).toEqual([]);
});

test('wrap in one block wraps every code block in every thread', async ({ page }) => {
  const first = await blocks(page, 0);
  await first.nth(0).getByRole('button', { name: 'Wrap' }).click();
  await expectWrapped(first, true);

  const second = await blocks(page, 1);
  await expectWrapped(second, true);

  await second.nth(1).getByRole('button', { name: 'Wrap' }).click();
  await expectWrapped(second, false);
  await expectWrapped(await blocks(page, 0), false);
});

test('wrap survives a reload and a restart; off survives them too', async ({ page }) => {
  const code = await blocks(page, 0);
  await code.nth(1).getByRole('button', { name: 'Wrap' }).click();
  await expectWrapped(code, true);

  await page.reload();
  await expectWrapped(
    page.getByRole('log', { name: 'Messages' }).locator('[data-component="code"]'),
    true,
  );

  m.kill();
  await m.restart();
  await expectWrapped(await blocks(page, 1), true);

  const again = await blocks(page, 1);
  await again.nth(0).getByRole('button', { name: 'Wrap' }).click();
  await expectWrapped(again, false);

  await page.reload();
  await expectWrapped(
    page.getByRole('log', { name: 'Messages' }).locator('[data-component="code"]'),
    false,
  );
  m.kill();
  await m.restart();
  await expectWrapped(await blocks(page, 0), false);
});
