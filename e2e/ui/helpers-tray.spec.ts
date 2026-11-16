// Thread details holds helper status; approvals stay beside the composer. Fake Claude only.
import { expect, type Page, test } from '@playwright/test';
import { api, type Machine, prompts, recordText, seenText, startAtoll } from './atoll';
import {
  box,
  bossHandsOff,
  delegate,
  type Handoff,
  helperThreads,
  makeBoss,
  details,
  openDetails,
  reportPrompt,
  reportReply,
  runStatus,
  send,
  startThread,
  composerArea,
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

test('three running helpers live in Thread details and leave the composer and chat height alone', async ({
  page,
}) => {
  await makeBoss(m, ['Tester', 'Reviewer']);
  await page.goto(m.url);
  const boss = await startThread(page, 'Poteto', prompts.keep);
  await expect.poll(() => runStatus(m, boss)).toBe('done');
  const log = page.getByRole('log', { name: 'Messages' });
  const before = await box(log);

  await send(
    page,
    delegate([
      ['Tester', hang('Poll cost flake')],
      ['Tester', hang('Header island width')],
      ['Reviewer', hang('Sidebar rail')],
    ]),
  );
  await helperThreads(m, boss, 3);
  await expect.poll(() => runStatus(m, boss)).toBe('done');
  await expect(page.getByText(/Working with/)).toHaveCount(0);
  await expect(composerArea(page)).not.toContainText(/Tester|Reviewer|working|reported/);
  expect((await box(log)).height).toBe(before.height);
  const panel = await openDetails(page);
  await expect(panel.getByRole('listitem')).toHaveCount(3);
  await expect(panel.getByLabel('Tester: working', { exact: true })).toHaveCount(2);
  await expect(panel.getByLabel('Reviewer: working', { exact: true })).toHaveCount(1);
  expect((await box(log)).height).toBe(before.height);
});

test('two helpers of one Bot have two creatures and two rows with different task titles', async ({
  page,
}) => {
  const { bots, helpers } = await bossHandsOff(m, page, [
    ['Tester', hang('Poll cost flake')],
    ['Tester', hang('Header island width')],
    ['Reviewer', hang('Sidebar rail')],
  ]);
  const region = await openDetails(page);
  await expect(region.getByLabel('Tester: working', { exact: true })).toHaveCount(2);

  const rows = details(page).getByRole('listitem');
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
    await expect(row.getByRole('link').filter({ hasNotText: /^[#!]/ })).toHaveAttribute(
      'href',
      `/bots/${bots.Tester.id}/threads/${t.id}`,
    );
  }
  const reviewer = rows.filter({ hasText: 'Reviewer' });
  await expect(reviewer).toHaveCount(1);
  await expect(reviewer.getByRole('img', { name: 'Reviewer: working' })).toBeVisible();
});

test('Thread details overlays the chat without moving it; Esc, outside click and the icon close it', async ({
  page,
}) => {
  const { boss } = await bossHandsOff(m, page, [
    ['Tester', hang('Poll cost flake')],
    ['Reviewer', hang('Sidebar rail')],
  ]);
  const log = page.getByRole('log', { name: 'Messages' });
  await expect.poll(() => runStatus(m, boss)).toBe('done');
  await expect(log.getByRole('status')).toHaveCount(0);
  const show = page.getByRole('button', {
    name: 'Thread details',
    exact: true,
  });
  const logBefore = await box(log);
  await log.evaluate((el) => {
    el.scrollTop = 0;
  });
  const scrollBefore = await log.evaluate((el) => el.scrollTop);

  await expect(show).toBeVisible();
  await show.click();
  const pop = details(page);
  await expect(pop).toBeVisible();
  await expect(pop.getByRole('listitem')).toHaveCount(2);
  const over = await box(pop);
  expect(over.y).toBeGreaterThanOrEqual(logBefore.y);
  expect(over.y + over.height).toBeLessThanOrEqual((await box(composerCard(page))).y);
  expect(over.width).toBe(340);
  expect(await box(log)).toEqual(logBefore);
  expect(await log.evaluate((el) => el.scrollTop)).toBe(scrollBefore);

  await page.keyboard.press('Escape');
  await expect(pop).toHaveCount(0);
  await expect(show).toBeFocused();

  await show.click();
  await expect(pop).toBeVisible();
  await page.getByRole('textbox', { name: /^Message/ }).click();
  await expect(pop).toHaveCount(0);
  expect(await box(log)).toEqual(logBefore);
  expect(await log.evaluate((el) => el.scrollTop)).toBe(scrollBefore);
  await show.click();
  await expect(pop).toBeVisible();
  await show.click();
  await expect(pop).toHaveCount(0);
});

test('the panel has a shortcut, arrow keys and Enter, and Stop on hover', async ({ page }) => {
  const { bots, helpers } = await bossHandsOff(m, page, [
    ['Tester', hang('Poll cost flake')],
    ['Reviewer', hang('Sidebar rail')],
  ]);
  const pop = details(page);
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
  const href = await rows
    .nth(second)
    .getByRole('link')
    .filter({ hasNotText: /^[#!]/ })
    .getAttribute('href');
  await page.keyboard.press('Enter');
  await expect(page).toHaveURL(`${m.url}${href}`);

  await page.goBack();
  await openDetails(page);
  const stops: string[] = [];
  page.on('request', (r) => {
    if (r.url().endsWith('/stop')) stops.push(new URL(r.url()).pathname);
  });
  const tester = helpers.find((h) => h.bot_id === bots.Tester.id)!;
  const row = rows.filter({ hasText: tester.title });
  await row.hover();
  await row.getByRole('button', { name: 'Stop' }).click();
  await expect.poll(() => runStatus(m, tester.id)).toBe('stopped');
  const stopped = (await api(m, 'GET', `/api/threads/${tester.id}`)).run_id;
  expect(stops).toEqual([`/api/runs/${stopped}/stop`]);
  const reviewer = helpers.find((h) => h.bot_id === bots.Reviewer.id)!;
  expect(await runStatus(m, reviewer.id)).toBe('running');
});

test('a helper approval stays above the composer within 2s; two waiting show the first and 1 more; Allow and Deny answer it in both threads', async ({
  page,
  context,
}) => {
  const { bots, helpers } = await bossHandsOff(m, page, [
    ['Tester', reportPrompt],
    ['Reviewer', reportPrompt],
  ]);
  const region = composerArea(page);
  const ids = helpers.map((h) => h.id);
  for (const id of ids) await pendingApproval(id);
  const allow = region.getByRole('button', { name: 'Allow', exact: true });
  const deny = region.getByRole('button', { name: 'Deny', exact: true });
  await expect(allow).toHaveCount(1, { timeout: 3_000 });
  await expect(deny).toHaveCount(1);
  await expect(region).toContainText('make check');
  await expect(region).toContainText('1 more');
  await expect(region.getByRole('img', { name: /^(Tester|Reviewer): needs you$/ }).first()).toBeVisible();
  expect((await box(allow)).y).toBeLessThan(
    (await box(page.getByRole('textbox', { name: /^Message/ }))).y,
  );
  await expect(details(page)).toHaveCount(0);

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

test('a step never shows a raw shell fragment: a loop shows its first command', async ({
  page,
}) => {
  await makeBoss(m, ['Tester']);
  await page.goto(m.url);
  await recordText(page);
  const boss = await startThread(page, 'Poteto', delegate([['Tester', loopPrompt]]));
  const [helper] = await helperThreads(m, boss, 1);
  await expect
    .poll(async () => (await api(m, 'GET', `/api/threads/${helper.id}`)).step, {
      timeout: 15_000,
    })
    .not.toBe('');

  await openDetails(page);
  const row = details(page).getByRole('listitem').filter({ hasText: helper.title });
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
  await expect(composerArea(page)).not.toContainText(/working|reported|Working with/);
  await expect(page.getByRole('button', { name: 'Show', exact: true })).toHaveCount(0);
  await expect(details(page)).toHaveCount(0);
});

test('finished helpers stay under Done after running helpers, also after reopening the thread', async ({
  page,
}) => {
  const { poteto, bots, boss, thread } = await bossHandsOff(m, page, [
    ['Tester', reportPrompt],
    ['Reviewer', prompts.hang],
  ]);
  await composerArea(page).getByRole('button', { name: 'Allow', exact: true }).click();
  await expect.poll(() => runStatus(m, thread('Tester')), { timeout: 30_000 }).toBe('done');
  for (const reload of [false, true]) {
    if (reload) await page.reload();
    const panel = await openDetails(page);
    await expect(panel.getByText('Helpers', { exact: true })).toBeVisible();
    const rows = panel.getByRole('listitem');
    await expect(rows).toHaveCount(2);
    await expect(rows.nth(0)).toContainText('Reviewer');
    await expect(rows.nth(1)).toContainText('Tester');
    const finished = rows.nth(1);
    await expect(finished.getByText('Done', { exact: true })).toBeVisible();
    await expect(panel.getByText('Done', { exact: true })).toHaveCount(2);
    await expect(finished.getByRole('button', { name: 'Stop', exact: true })).toHaveCount(0);
    const name = await box(rows.nth(0).getByText('Reviewer', { exact: true }));
    const step = await box(rows.nth(0).getByText('Running make build', { exact: true }));
    expect(step.y).toBeGreaterThan(name.y);
    await page.keyboard.press('Escape');
  }
  await openDetails(page);
  await details(page).getByRole('listitem').filter({ hasText: 'Tester' }).click();
  await expect(page).toHaveURL(`${m.url}/bots/${bots.Tester.id}/threads/${thread('Tester')}`);
  await expect(page.getByRole('log', { name: 'Messages' })).toContainText(reportReply);
  await page.goto(`${m.url}/bots/${poteto.id}/threads/${boss}`);
  await expect(page.getByText(/Working with/)).toHaveCount(0);
});
