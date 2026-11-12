// Rich output, later set: diff, checklist, metrics and kv. Contract: e2e/rich-output-contract.md.
import { expect, type Page, test } from '@playwright/test';
import { api, type Machine, newBotAPI, openThreadAPI, startAtoll } from './atoll';

let m: Machine;
test.beforeAll(async () => {
  m = await startAtoll();
});
test.afterAll(() => m?.stop());

async function replyLog(page: Page, name: string, prompt: string) {
  const bot = await newBotAPI(m, name);
  const thread = await openThreadAPI(m, bot.id, prompt);
  await expect
    .poll(async () => (await api(m, 'GET', `/api/threads/${thread.id}`)).run_status, {
      timeout: 15_000,
    })
    .toBe('done');
  await page.goto(`${m.url}/bots/${bot.id}/threads/${thread.id}`);
  const log = page.getByRole('log', { name: 'Messages' });
  await expect(log).toContainText('Rich output');
  return log;
}

const background = (l: import('@playwright/test').Locator) =>
  l.first().evaluate((el) => getComputedStyle(el).backgroundColor);

test('a diff fence is a diff: a header per file with +/- counts, marked lines', async ({
  page,
}, info) => {
  const log = await replyLog(page, 'Diff', 'Rich output: show a diff.');
  const diff = log.locator('[data-component="diff"]');
  await expect(diff).toHaveCount(1);
  const files = diff.locator('[data-kind="file"]');
  await expect(files).toHaveCount(2);
  await expect(files.nth(0)).toContainText('server/a.go');
  await expect(files.nth(0)).toContainText('+2');
  await expect(files.nth(0)).toContainText(/[-−]1/);
  await expect(files.nth(1)).toContainText('web/b.ts');
  await expect(files.nth(1)).toContainText('+0');
  await expect(files.nth(1)).toContainText(/[-−]1/);

  await expect(diff.locator('[data-kind="hunk"]')).toHaveCount(2);
  const added = diff.locator('[data-kind="add"]');
  const removed = diff.locator('[data-kind="del"]');
  const context = diff.locator('[data-kind="ctx"]');
  await expect(added).toHaveCount(2);
  await expect(removed).toHaveCount(2);
  await expect(context).toHaveCount(3);
  await expect(added.nth(1)).toContainText('var burst = 5');
  await expect(removed.nth(0)).toContainText('var limit = 10');
  await expect(context.nth(0)).toContainText('package server');
  const [a, r, c] = [await background(added), await background(removed), await background(context)];
  expect(new Set([a, r, c]).size).toBe(3);
  await info.attach('diff.png', {
    body: await diff.screenshot(),
    contentType: 'image/png',
  });
});

test('a task list is a read-only checklist; a list with a plain item stays a list', async ({
  page,
}, info) => {
  const log = await replyLog(page, 'Checklist', 'Rich output: show a checklist.');
  const lists = log.locator('[data-component="checklist"]');
  await expect(lists).toHaveCount(1);
  const list = lists.first();
  await expect(list.getByRole('checkbox')).toHaveCount(3);
  await expect(list.getByRole('checkbox', { name: 'Tag the release' })).toHaveAttribute(
    'aria-checked',
    'true',
  );
  const open = list.getByRole('checkbox', { name: 'Publish the notes' });
  await expect(open).toHaveAttribute('aria-checked', 'false');
  await expect(open).toHaveAttribute('aria-readonly', 'true');
  await open.click();
  await expect(open).toHaveAttribute('aria-checked', 'false');

  await expect(log.getByText('plain note')).toBeVisible();
  await expect(list.getByText('plain note')).toHaveCount(0);
  await info.attach('checklist.png', {
    body: await list.screenshot(),
    contentType: 'image/png',
  });
});

test('reef-metrics is a row of tiles with label, value and optional delta', async ({
  page,
}, info) => {
  const log = await replyLog(page, 'Metrics', 'Rich output: show metrics.');
  const metrics = log.locator('[data-component="metrics"]');
  await expect(metrics).toHaveCount(1);
  const tiles = metrics.getByRole('listitem');
  await expect(tiles).toHaveCount(3);
  await expect(tiles.nth(0)).toContainText('Open PRs');
  await expect(tiles.nth(0)).toContainText('12');
  await expect(tiles.nth(0)).toContainText('+3');
  await expect(tiles.nth(1)).toContainText('Coverage');
  await expect(tiles.nth(1)).toContainText('87.5');
  await expect(tiles.nth(1)).toContainText('-1.2');
  await expect(tiles.nth(2)).toContainText('Flaky tests');
  await expect(tiles.nth(2)).not.toContainText(/[+−-]\d/);
  await expect(log).not.toContainText('"label"');
  await info.attach('metrics.png', {
    body: await metrics.screenshot(),
    contentType: 'image/png',
  });
});

test('reef-kv is a key-value card; the first ": " splits and blank lines are skipped', async ({
  page,
}, info) => {
  const log = await replyLog(page, 'Details', 'Rich output: show details.');
  const kv = log.locator('[data-component="kv"]');
  await expect(kv).toHaveCount(1);
  await expect(kv.locator('dt')).toHaveText(['Branch', 'Started', 'Checks', 'Owner']);
  await expect(kv.locator('dd')).toHaveText([
    'fix/sidebar-selection',
    '10:42 AM',
    'passing',
    'Ada',
  ]);
  await expect(log).not.toContainText('reef-kv');
  await info.attach('kv.png', {
    body: await kv.screenshot(),
    contentType: 'image/png',
  });
});
