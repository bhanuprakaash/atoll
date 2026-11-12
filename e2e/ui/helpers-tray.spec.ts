// The helpers tray above the composer: one calm line, a popover for the list, an approval line
// only when a helper needs you, reported helpers until their report was seen. Issue #175;
// contract: e2e/helpers-ui-contract.md. Fake Claude only.
import { expect, type Locator, type Page, test } from '@playwright/test';
import { api, type Machine, prompts, recordText, seenText, startAtoll } from './atoll';
import {
  box,
  bossHandsOff,
  delegate,
  type Handoff,
  helperThreads,
  makeBoss,
  popover,
  reportPrompt,
  reportReply,
  runStatus,
  send,
  startThread,
  tray,
} from './helpers-fixture';

let m: Machine;
test.beforeEach(async () => {
  m = await startAtoll();
});
test.afterEach(() => m?.stop());

// The helper plays testdata/claude/helper-loop.jsonl: a shell loop that never ends.
const loopPrompt = 'Check each open pull request.';

// Briefs that start with the task and end with the prompt of the recording the helper plays; the
// helper thread's title is the first six words of the brief.
const hang = (task: string): Handoff[1] => `${task}. ${prompts.hang}`;

const composerCard = (page: Page) =>
  page.getByRole('textbox', { name: /^Message/ }).locator('xpath=ancestor::form');

async function pendingApproval(threadID: string) {
  let id = '';
  await expect
    .poll(async () => (id = (await api(m, 'GET', `/api/threads/${threadID}`)).approval?.id ?? ''))
    .not.toBe('');
  return id;
}

const inView = (l: Locator) =>
  l.evaluate((el) => {
    const r = el.getBoundingClientRect();
    const log = el.closest('[role="log"]')!.getBoundingClientRect();
    return r.bottom > log.top && r.top < log.bottom;
  });

test('three helpers running: the tray is one line of 36px at most, attached to the composer, and the chat keeps its height', async ({
  page,
}) => {
  await makeBoss(m, ['Tester', 'Reviewer']);
  await page.goto(m.url);
  const boss = await startThread(page, 'Poteto', prompts.keep);
  await expect.poll(() => runStatus(m, boss)).toBe('done');
  const log = page.getByRole('log', { name: 'Messages' });
  await expect(tray(page)).toHaveCount(0);
  const before = await box(log);

  await send(
    page,
    delegate([
      ['Tester', hang('Poll cost flake')],
      ['Tester', hang('Header island width')],
      ['Reviewer', hang('Sidebar rail')],
    ]),
  );
  const region = tray(page);
  await expect(region).toBeVisible();
  await expect(region).toContainText('3 working');
  await expect(region).not.toContainText('reported');
  await expect(region.getByLabel('Tester: working', { exact: true })).toHaveCount(2);
  await expect(region.getByLabel('Reviewer: working', { exact: true })).toHaveCount(1);
  await expect(region.getByRole('button', { name: 'Show', exact: true })).toBeVisible();
  await expect(region.getByRole('link', { name: 'Open' })).toHaveCount(0);
  await expect(region.getByRole('button', { name: /^(Allow|Deny)$/ })).toHaveCount(0);

  const line = await box(region);
  expect(line.height).toBeLessThanOrEqual(36);
  const card = await box(composerCard(page));
  const inside = line.y >= card.y - 1 && line.y + line.height <= card.y + card.height + 1;
  const touching = Math.abs(line.y + line.height - card.y) <= 2;
  expect(inside || touching, 'the tray is part of the composer card, not a block above it').toBe(
    true,
  );
  expect((await box(log)).height).toBeGreaterThanOrEqual(before.height - 36);
});

test('two helpers of one Bot are two creatures and, in the popover, two rows with different task titles', async ({
  page,
}) => {
  const { bots, helpers } = await bossHandsOff(m, page, [
    ['Tester', hang('Poll cost flake')],
    ['Tester', hang('Header island width')],
    ['Reviewer', hang('Sidebar rail')],
  ]);
  const region = tray(page);
  await expect(region.getByLabel('Tester: working', { exact: true })).toHaveCount(2);
  await region.getByRole('button', { name: 'Show', exact: true }).click();

  const rows = popover(page).getByRole('listitem');
  await expect(rows).toHaveCount(3);
  const testers = helpers.filter((h) => h.bot_id === bots.Tester.id);
  expect(testers).toHaveLength(2);
  expect(testers[0].title).not.toBe(testers[1].title);
  for (const t of testers) {
    const row = rows.filter({ hasText: t.title });
    await expect(row).toHaveCount(1);
    await expect(row).toContainText('Tester');
    await expect(row.getByText('Running make build')).toBeVisible();
    await expect(row).toContainText(/\b\d+\s?(s|m|h)\b/);
    await expect(row.getByRole('link', { name: 'Open' })).toHaveAttribute(
      'href',
      `/bots/${bots.Tester.id}/threads/${t.id}`,
    );
  }
  const reviewer = rows.filter({ hasText: 'Reviewer' });
  await expect(reviewer).toHaveCount(1);
  await expect(reviewer.getByRole('img', { name: 'Reviewer: working' })).toBeVisible();
});

test('Show opens a popover over the chat that does not move it; Esc and an outside click close it', async ({
  page,
}) => {
  const { boss } = await bossHandsOff(m, page, [
    ['Tester', hang('Poll cost flake')],
    ['Reviewer', hang('Sidebar rail')],
  ]);
  const log = page.getByRole('log', { name: 'Messages' });
  await expect.poll(() => runStatus(m, boss)).toBe('done');
  await expect(log.getByRole('status')).toHaveCount(0);
  const region = tray(page);
  const show = region.getByRole('button', { name: 'Show', exact: true });
  const [logBefore, trayBefore] = [await box(log), await box(region)];
  const scrollBefore = await log.evaluate((el) => el.scrollTop);

  await show.click();
  const pop = popover(page);
  await expect(pop).toBeVisible();
  await expect(pop.getByRole('listitem')).toHaveCount(2);
  const over = await box(pop);
  expect(over.y).toBeGreaterThanOrEqual(logBefore.y);
  expect(over.y + over.height).toBeLessThanOrEqual(trayBefore.y + 1);
  expect(await box(log)).toEqual(logBefore);
  expect(await box(region)).toEqual(trayBefore);
  expect(await log.evaluate((el) => el.scrollTop)).toBe(scrollBefore);

  await page.keyboard.press('Escape');
  await expect(pop).toHaveCount(0);
  await expect(show).toBeFocused();

  await show.click();
  await expect(pop).toBeVisible();
  const view = page.viewportSize()!;
  await page.mouse.click(view.width - 20, view.height / 2);
  await expect(pop).toHaveCount(0);
  expect(await box(log)).toEqual(logBefore);
  expect(await log.evaluate((el) => el.scrollTop)).toBe(scrollBefore);
});

test('the popover has a shortcut, arrow keys and Enter, and Stop on hover', async ({ page }) => {
  const { bots, helpers } = await bossHandsOff(m, page, [
    ['Tester', hang('Poll cost flake')],
    ['Reviewer', hang('Sidebar rail')],
  ]);
  const pop = popover(page);
  await page.getByRole('textbox', { name: /^Message/ }).focus();
  await page.keyboard.press('ControlOrMeta+.');
  await expect(pop).toBeVisible();
  await page.keyboard.press('ControlOrMeta+.');
  await expect(pop).toHaveCount(0);

  await page.keyboard.press('ControlOrMeta+.');
  await expect(pop).toBeVisible();
  const rows = pop.getByRole('listitem');
  const focused = () =>
    page.evaluate(() => {
      const li = document.activeElement?.closest('li');
      return li ? [...li.parentElement!.children].indexOf(li) : -1;
    });
  await page.keyboard.press('ArrowDown');
  const first = await focused();
  expect(first).toBeGreaterThanOrEqual(0);
  await page.keyboard.press('ArrowDown');
  const second = await focused();
  expect(second).not.toBe(first);
  const href = await rows.nth(second).getByRole('link', { name: 'Open' }).getAttribute('href');
  await page.keyboard.press('Enter');
  await expect(page).toHaveURL(`${m.url}${href}`);

  await page.goBack();
  await tray(page).getByRole('button', { name: 'Show', exact: true }).click();
  const tester = helpers.find((h) => h.bot_id === bots.Tester.id)!;
  const row = rows.filter({ hasText: tester.title });
  await row.hover();
  await row.getByRole('button', { name: 'Stop' }).click();
  await expect.poll(() => runStatus(m, tester.id)).toBe('stopped');
  const reviewer = helpers.find((h) => h.bot_id === bots.Reviewer.id)!;
  expect(await runStatus(m, reviewer.id)).toBe('running');
});

test('a helper approval is one line under the summary within 2s; two waiting show the first and 1 more; Allow and Deny answer it in both threads', async ({
  page,
  context,
}) => {
  const { bots, helpers } = await bossHandsOff(m, page, [
    ['Tester', reportPrompt],
    ['Reviewer', reportPrompt],
  ]);
  const region = tray(page);
  const ids = helpers.map((h) => h.id);
  for (const id of ids) await pendingApproval(id);
  const allow = region.getByRole('button', { name: 'Allow', exact: true });
  const deny = region.getByRole('button', { name: 'Deny', exact: true });
  await expect(allow).toHaveCount(1, { timeout: 3_000 });
  await expect(deny).toHaveCount(1);
  await expect(region).toContainText('make check');
  await expect(region).toContainText('1 more');
  await expect(region).toContainText('2 working');

  const summary = await box(region.getByRole('button', { name: 'Show', exact: true }));
  expect((await box(allow)).y).toBeGreaterThanOrEqual(summary.y + summary.height);

  const waitingName = /Tester\s*wants/i.test((await region.textContent())!) ? 'Tester' : 'Reviewer';
  const otherName = waitingName === 'Tester' ? 'Reviewer' : 'Tester';
  const first = helpers.find((h) => h.bot_id === bots[waitingName].id)!;
  const second = helpers.find((h) => h.bot_id === bots[otherName].id)!;

  const helperPage = await context.newPage();
  await helperPage.goto(`${m.url}/bots/${bots[waitingName].id}/threads/${first.id}`);
  const inHelper = helperPage
    .getByRole('log', { name: 'Messages' })
    .getByRole('button', { name: 'Allow', exact: true });
  await expect(inHelper).toHaveCount(1);

  await allow.click();
  await expect(inHelper).toHaveCount(0);
  await expect.poll(() => runStatus(m, first.id), { timeout: 30_000 }).toBe('done');
  await expect(region).not.toContainText('1 more');
  await expect(region).toContainText(otherName);
  await expect(allow).toHaveCount(1);

  // Each page holds two event streams; a third tab would use up Chromium's six per host.
  await helperPage.close();
  const otherPage = await context.newPage();
  await otherPage.goto(`${m.url}/bots/${bots[otherName].id}/threads/${second.id}`);
  const inOther = otherPage
    .getByRole('log', { name: 'Messages' })
    .getByRole('button', { name: 'Allow', exact: true });
  await expect(inOther).toHaveCount(1);
  await deny.click();
  await expect(deny).toHaveCount(0);
  await expect(inOther).toHaveCount(0);
  await expect
    .poll(async () => (await api(m, 'GET', `/api/threads/${second.id}`)).approval ?? null)
    .toBeNull();
});

test('a helper that reported stays in the tray until its report was on screen, then leaves', async ({
  page,
}) => {
  await makeBoss(m, ['Tester']);
  await page.setViewportSize({ width: 1280, height: 520 });
  await page.goto(m.url);
  const context = 'Some context for the helpers. '.repeat(60);
  const boss = await startThread(page, 'Poteto', delegate([['Tester', reportPrompt]], context));
  const [helper] = await helperThreads(m, boss, 1);
  const log = page.getByRole('log', { name: 'Messages' });
  const region = tray(page);
  await expect(region).toContainText('1 working');
  await expect(log.getByText('Handed off to Tester', { exact: true })).toBeVisible();
  expect(await log.evaluate((el) => el.scrollHeight > el.clientHeight + 100)).toBe(true);
  await log.evaluate((el) => (el.scrollTop = 0));

  await api(m, 'POST', `/api/approvals/${await pendingApproval(helper.id)}`, { approve: true });
  await expect.poll(() => runStatus(m, helper.id), { timeout: 30_000 }).toBe('done');
  const report = log.getByRole('article', { name: 'Tester' });
  await expect(report).toContainText(reportReply);
  await expect.poll(() => runStatus(m, boss), { timeout: 30_000 }).toBe('done');
  expect(await inView(report), 'the report is below what the person is looking at').toBe(false);

  await expect(region).toContainText('1 reported');
  await expect(region).not.toContainText('working');
  await region.getByRole('button', { name: 'Show', exact: true }).click();
  const row = popover(page).getByRole('listitem').filter({ hasText: helper.title });
  await expect(row).toContainText('Reported');
  await expect(row.getByRole('link', { name: 'Open' })).toBeVisible();
  await page.keyboard.press('Escape');

  await log.evaluate((el) => (el.scrollTop = el.scrollHeight));
  await expect(report).toBeInViewport();
  await expect(region).toHaveCount(0);
  const reloaded = page.waitForResponse((r) => r.url().endsWith(`/api/threads/${boss}`));
  await page.reload();
  await reloaded;
  await expect(log.getByRole('article', { name: 'Tester' })).toContainText(reportReply);
  await page.waitForTimeout(1_000);
  await expect(tray(page)).toHaveCount(0);
});

test('a helper whose report folded into the ledger leaves the tray once its row was on screen', async ({
  page,
}) => {
  await makeBoss(m, ['Tester']);
  await page.setViewportSize({ width: 1280, height: 520 });
  await page.goto(m.url);
  const context = 'Some context for the helpers. '.repeat(60);
  // The helper plays testdata/claude/final-answer.jsonl: a done report with nothing blocked.
  const brief = 'Check the build, then say what you found.';
  const boss = await startThread(page, 'Poteto', delegate([['Tester', brief]], context));
  const [helper] = await helperThreads(m, boss, 1);
  const log = page.getByRole('log', { name: 'Messages' });
  const region = tray(page);
  await expect(log.getByText('Handed off to Tester', { exact: true })).toBeVisible();
  expect(await log.evaluate((el) => el.scrollHeight > el.clientHeight + 100)).toBe(true);
  await log.evaluate((el) => (el.scrollTop = 0));

  await expect.poll(() => runStatus(m, helper.id), { timeout: 30_000 }).toBe('done');
  const row = log
    .getByRole('list', { name: 'Updates' })
    .getByRole('listitem')
    .filter({ hasText: 'Checked the build: it is green.' });
  await expect(row).toHaveCount(1);
  await expect.poll(() => runStatus(m, boss), { timeout: 30_000 }).toBe('done');
  expect(await inView(row), 'the row is below what the person is looking at').toBe(false);
  await expect(region).toContainText('1 reported');

  await log.evaluate((el) => (el.scrollTop = el.scrollHeight));
  await expect(row).toBeInViewport();
  await expect(region).toHaveCount(0);
});

test('a step never shows a raw shell fragment: a loop shows its first command', async ({
  page,
}) => {
  await makeBoss(m, ['Tester']);
  await page.goto(m.url);
  await recordText(page);
  const boss = await startThread(page, 'Poteto', delegate([['Tester', loopPrompt]]));
  const [helper] = await helperThreads(m, boss, 1);
  await expect
    .poll(async () => (await api(m, 'GET', `/api/threads/${helper.id}`)).step, { timeout: 15_000 })
    .not.toBe('');

  await tray(page).getByRole('button', { name: 'Show', exact: true }).click();
  const row = popover(page).getByRole('listitem').filter({ hasText: helper.title });
  await expect(row.getByText('Running gh pr', { exact: true })).toBeVisible();
  const seen = await seenText(page);
  expect(seen).not.toMatch(/Running for\b|for n in|\bdo gh\b|; done/);
});

test('with no helper running or unread nothing shows above the composer', async ({ page }) => {
  await makeBoss(m, ['Tester']);
  await page.goto(m.url);
  const boss = await startThread(page, 'Poteto', prompts.keep);
  await expect.poll(() => runStatus(m, boss)).toBe('done');
  await expect(page.getByRole('textbox', { name: /^Message/ })).toBeVisible();
  await expect(tray(page)).toHaveCount(0);
  await expect(page.getByRole('button', { name: 'Show', exact: true })).toHaveCount(0);
  await expect(popover(page)).toHaveCount(0);
});
