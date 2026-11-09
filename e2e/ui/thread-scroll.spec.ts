import { expect, test } from '@playwright/test';
import { api, newBotAPI, prompts, sendMessageAPI, startAtoll } from './atoll';

for (const late of ['image', 'run', 'burst'] as const) {
  test(`long replies stay at the bottom after late ${late} on fresh loads`, async ({ browser }) => {
    test.setTimeout(120_000);
    const m = await startAtoll();
    try {
      const bot = await newBotAPI(m, 'Long reply');
      const thread = await api(m, 'POST', `/api/bots/${bot.id}/threads`, {});
      await sendMessageAPI(m, thread.id, 'Show a long markdown reply.');
      const failures: number[] = [];
      for (let i = 0; i < 10; i++) {
        const page = await browser.newPage();
        let release!: () => void;
        const ready = new Promise<void>((resolve) => (release = resolve));
        try {
          await page.route('**/file?path=late.png', async (route) => {
            if (late === 'image') await ready;
            const height = late === 'image' ? 600 : 1;
            await route.fulfill({
              contentType: 'image/svg+xml',
              body: `<svg xmlns="http://www.w3.org/2000/svg" width="400" height="${height}" />`,
            });
          });
          if (late === 'run') {
            await page.route(/\/api\/runs\/[^/?]+$/, async (route) => {
              const response = await route.fetch();
              const run = await response.json();
              await ready;
              await route.fulfill({
                response,
                json: {
                  ...run,
                  report: {
                    ...run.report,
                    blocked:
                      'Waiting for a detailed review of the changes and the results before this work can continue.',
                  },
                },
              });
            });
          }
          await page.goto(`${m.url}/bots/${bot.id}/threads/${thread.id}`);
          const log = page.getByRole('log', { name: 'Messages' });
          await expect(log.getByText('End of long reply.')).toBeAttached();
          await expect
            .poll(() => log.evaluate((el) => el.scrollHeight - el.scrollTop - el.clientHeight))
            .toBeLessThanOrEqual(1);
          release();
          if (late === 'image') {
            await expect(log.getByRole('img', { name: 'late image' })).toHaveJSProperty(
              'complete',
              true,
            );
            await expect
              .poll(() =>
                log
                  .getByRole('img', { name: 'late image' })
                  .evaluate((el) => el.getBoundingClientRect().height),
              )
              .toBe(600);
          } else if (late === 'run') {
            await expect(log.getByText(/^Blocked: Waiting for a detailed review/)).toBeVisible();
          } else {
            await log.evaluate(async (el) => {
              for (let n = 0; n < 30; n++) {
                const node = document.createElement('div');
                node.style.height = '20px';
                node.style.flexShrink = '0';
                el.firstElementChild!.append(node);
                await new Promise((resolve) => setTimeout(resolve, 3));
              }
            });
          }
          await page.evaluate(
            () =>
              new Promise<void>((resolve) =>
                requestAnimationFrame(() => requestAnimationFrame(() => resolve())),
              ),
          );
          const bottom = await log.evaluate(
            (el) => el.scrollHeight - el.scrollTop - el.clientHeight,
          );
          if (bottom > 1 || (await page.getByRole('button', { name: 'Jump to latest' }).count()))
            failures.push(i + 1);
        } finally {
          release();
          await page.close();
        }
      }
      expect(failures, `loads above the bottom: ${failures.join(', ')}`).toEqual([]);
    } finally {
      m.stop();
    }
  });
}

test('new events do not move a reader away from older messages', async ({ page }) => {
  const m = await startAtoll();
  try {
    const bot = await newBotAPI(m, 'Reader');
    const thread = await api(m, 'POST', `/api/bots/${bot.id}/threads`, {});
    for (let i = 0; i < 10; i++) await sendMessageAPI(m, thread.id, prompts.keep);
    await page.goto(`${m.url}/bots/${bot.id}/threads/${thread.id}`);
    const log = page.getByRole('log', { name: 'Messages' });
    await expect(log.getByTestId('message')).toHaveCount(20);
    await log.evaluate((el) => (el.scrollTop = 100));
    await expect(page.getByRole('button', { name: 'Jump to latest' })).toBeVisible();
    const before = await log.evaluate((el) => el.scrollTop);
    await sendMessageAPI(m, thread.id, prompts.keep);
    await expect(log.getByTestId('message')).toHaveCount(22);
    expect(Math.abs((await log.evaluate((el) => el.scrollTop)) - before)).toBeLessThanOrEqual(2);
    await expect(page.getByRole('button', { name: 'Jump to latest' })).toBeVisible();
  } finally {
    m.stop();
  }
});
