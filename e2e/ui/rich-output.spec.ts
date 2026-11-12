// Rich output, first set: ordinary code, tables and the fallback to a code block. Contract:
// e2e/rich-output-contract.md.
import { expect, type Page, test } from '@playwright/test';
import { api, type Machine, newBotAPI, openThreadAPI, startAtoll } from './atoll';

let m: Machine;
test.beforeAll(async () => {
  m = await startAtoll();
});
test.afterAll(() => m?.stop());

// replyLog has a new Bot answer prompt in a new thread and returns the page's message log.
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

const goSource = 'func Add(a, b int) int {\n\t// sum\n\treturn a + b\n}';
const steps = Array.from({ length: 30 }, (_, i) => `step_${i + 1}: run`);

test('fenced code is a boxed, highlighted block with its language and Copy', async ({
  page,
  context,
}, info) => {
  await context.grantPermissions(['clipboard-read', 'clipboard-write']);
  const log = await replyLog(page, 'Code box', 'Rich output: show code.');
  const blocks = log.locator('[data-component="code"]');
  await expect(blocks).toHaveCount(2);
  const go = blocks.nth(0);
  await expect(go).toContainText('func Add');
  await expect(go.getByText('go', { exact: true })).toBeVisible();

  const look = await go.evaluate((el) => {
    const pre = el.querySelector('pre')!;
    const range = document.createRange();
    range.selectNodeContents(pre);
    const bg = getComputedStyle(el).backgroundColor;
    return {
      bg,
      pageBg: getComputedStyle(document.body).backgroundColor,
      inset: range.getBoundingClientRect().left - el.getBoundingClientRect().left,
      colours: new Set([...pre.querySelectorAll('span')].map((s) => getComputedStyle(s).color))
        .size,
    };
  });
  expect(look.bg).not.toBe('rgba(0, 0, 0, 0)');
  expect(look.bg).not.toBe(look.pageBg);
  expect(look.inset).toBeGreaterThanOrEqual(8);
  expect(look.colours).toBeGreaterThan(1);

  await go.getByRole('button', { name: 'Copy' }).click();
  await expect.poll(() => page.evaluate(() => navigator.clipboard.readText())).toBe(goSource);
  await expect(go.getByRole('button', { name: 'Show all' })).toHaveCount(0);
  await info.attach('code.png', {
    body: await go.screenshot(),
    contentType: 'image/png',
  });
});

test('code past 20 lines shows 20 with Show all; Copy takes all of it', async ({
  page,
  context,
}) => {
  await context.grantPermissions(['clipboard-read', 'clipboard-write']);
  const log = await replyLog(page, 'Code long', 'Rich output: show code.');
  const long = log.locator('[data-component="code"]').nth(1);
  await expect(long).toContainText('step_20: run');
  await expect(long).not.toContainText('step_21: run');

  await long.getByRole('button', { name: 'Copy' }).click();
  await expect
    .poll(() => page.evaluate(() => navigator.clipboard.readText()))
    .toBe(steps.join('\n'));

  await long.getByRole('button', { name: 'Show all' }).click();
  await expect(long).toContainText('step_30: run');
  await long.getByRole('button', { name: 'Show less' }).click();
  await expect(long).not.toContainText('step_21: run');
  await expect(long.getByRole('button', { name: 'Show all' })).toBeVisible();
});

test('inline code stays inline', async ({ page }) => {
  const log = await replyLog(page, 'Code inline', 'Rich output: show code.');
  await expect(log.locator('[data-component="code"]')).toHaveCount(2);
  await expect(log.locator('code', { hasText: /^Add$/ })).toBeVisible();
});

test('a GFM table is a Reef table: sticky header, sort, numbers right-aligned', async ({
  page,
}, info) => {
  const log = await replyLog(page, 'Table', 'Rich output: show a table.');
  const component = log.locator('[data-component="table"]');
  await expect(component).toHaveCount(1);
  const table = component.getByRole('table');
  await expect(table.getByRole('columnheader')).toHaveText(['PR', 'State', 'Checks', 'Lines']);
  const prs = () => table.locator('tbody tr td:first-child').allTextContents();
  expect(await prs()).toEqual(['#12', '#7', '#31']);

  const th = (name: string) => table.getByRole('columnheader', { name });
  expect(await th('PR').evaluate((el) => getComputedStyle(el).position)).toBe('sticky');

  await th('Lines').getByRole('button', { name: 'Lines' }).click();
  await expect(th('Lines')).toHaveAttribute('aria-sort', 'ascending');
  expect(await prs()).toEqual(['#12', '#31', '#7']);
  await th('Lines').getByRole('button', { name: 'Lines' }).click();
  await expect(th('Lines')).toHaveAttribute('aria-sort', 'descending');
  expect(await prs()).toEqual(['#7', '#31', '#12']);
  await th('Lines').getByRole('button', { name: 'Lines' }).click();
  await expect(th('Lines')).toHaveAttribute('aria-sort', 'none');
  expect(await prs()).toEqual(['#12', '#7', '#31']);

  await th('PR').getByRole('button', { name: 'PR' }).click();
  await expect(th('PR')).toHaveAttribute('aria-sort', 'ascending');
  await expect(th('Lines')).toHaveAttribute('aria-sort', 'none');
  expect(await prs()).toEqual(['#12', '#31', '#7']);

  const align = (sel: string) =>
    table.locator(sel).evaluateAll((els) => els.map((el) => getComputedStyle(el).textAlign));
  expect(await align('tbody td:nth-child(4)')).toEqual(['right', 'right', 'right']);
  expect((await align('tbody td:nth-child(2)')).every((a) => /^(left|start)$/.test(a))).toBe(true);
  await info.attach('table.png', {
    body: await component.screenshot(),
    contentType: 'image/png',
  });
});

test('a reef- fence that does not fit its form shows its body as a code block', async ({
  page,
}) => {
  const log = await replyLog(page, 'Malformed', 'Rich output: show broken blocks.');
  const blocks = log.locator('[data-component="code"]');
  await expect(blocks).toHaveCount(4);
  await expect(blocks.nth(0)).toContainText('[{"label":"Open PRs","value":');
  await expect(blocks.nth(1)).toContainText('"label":"E"');
  await expect(blocks.nth(2)).toContainText('this line has no separator');
  await expect(blocks.nth(3)).toContainText('{"bars":[1,2,3]}');
  for (let i = 0; i < 4; i++) await expect(blocks.nth(i)).not.toContainText('reef-');
  await expect(log.locator('[data-component="metrics"], [data-component="kv"]')).toHaveCount(0);
  await expect(log).toContainText('First, a cut-off one:');
  await expect(log).toContainText('End of the broken blocks.');
  await expect(log).not.toContainText(/undefined|\[object Object\]|SyntaxError|Unexpected token/);
});

test('a reef- fence still open when the run ends is a code block, not a placeholder', async ({
  page,
}) => {
  const log = await replyLog(page, 'Unclosed', 'Rich output: show an unfinished block.');
  const blocks = log.locator('[data-component="code"]');
  await expect(blocks).toHaveCount(1);
  await expect(blocks.first()).toContainText('[{"label":"Open PRs","value":"12"},');
  await expect(log).not.toContainText('Metrics…');
});

// escaped is s as a JSON string with every character \u-escaped, so the boss's prompt does not
// hold the prompt of the recording the helper plays.
const escaped = (s: string) =>
  `"${Array.from(s, (c) => `\\u${c.charCodeAt(0).toString(16).padStart(4, '0')}`).join('')}"`;

test("a helper's report in the boss thread renders the same table", async ({ page }) => {
  const poteto = await newBotAPI(m, 'Poteto relay', 'claude', {
    look: 'whale',
  });
  const tester = await newBotAPI(m, 'Tester relay', 'claude', {
    look: 'shark',
  });
  await api(m, 'PUT', `/api/bots/${poteto.id}`, { delegates: [tester.id] });
  const brief = 'Rich output: report the status table.';
  const boss = await openThreadAPI(
    m,
    poteto.id,
    `Use MCP tools. [{"server":"atoll","tool":"delegate","args":{"bot":"Tester relay","brief":${escaped(brief)}}}]`,
  );
  await page.goto(`${m.url}/bots/${poteto.id}/threads/${boss.id}`);
  const ledger = page.getByRole('log', { name: 'Messages' }).getByRole('list', { name: 'Updates' });
  await ledger.getByRole('button', { name: /Tester relay/ }).click();
  const report = ledger.getByRole('listitem');
  const table = report.locator('[data-component="table"]');
  await expect(table).toHaveCount(1);
  await expect(table.getByRole('columnheader')).toHaveText(['Check', 'Result']);
  await expect(table.getByRole('cell', { name: 'lint' })).toBeVisible();
});
