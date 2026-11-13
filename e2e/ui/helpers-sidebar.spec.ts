// Delegated work in the sidebar: one line under the boss thread, no row for a helper thread under
// its Bot, a breadcrumb from the helper thread back to the boss thread. Contract:
// e2e/helpers-sidebar-contract.md.
import { expect, type Page, test } from '@playwright/test';
import {
  api,
  type Machine,
  newBotAPI,
  openThreadAPI,
  prompts,
  startAtoll,
  threadTree,
} from './atoll';
import {
  bossHandsOff,
  delegate,
  helperThreads,
  makeBoss,
  reportPrompt,
  reportReply,
  runStatus,
  tray,
} from './helpers-fixture';

let m: Machine;
test.beforeEach(async () => {
  m = await startAtoll();
});
test.afterEach(() => m?.stop());

const nav = (page: Page) => page.getByRole('navigation', { name: 'Bots' });
const line = (page: Page, boss = 'Poteto') =>
  threadTree(page, boss).locator('[data-line="helpers"]');
const helperRows = (page: Page, threadID: string) =>
  nav(page).locator(`a[href$="/threads/${threadID}"]`);

// away shows a Bot of its own, so no thread of the Bots under test is on screen.
async function away(page: Page) {
  const bot = await newBotAPI(m, 'Elsewhere');
  await page.goto(`${m.url}/bots/${bot.id}`);
  await expect(page.getByRole('heading', { name: 'What should Elsewhere work on?' })).toBeVisible();
}

test('one helper: one line under the boss thread with its name and step, no row under its Bot', async ({
  page,
}) => {
  const { boss, thread } = await bossHandsOff(m, page, [['Tester', prompts.hang]]);
  await away(page);

  await expect(line(page)).toHaveCount(1);
  await expect(line(page)).toContainText('Tester');
  await expect(line(page)).toContainText('Running make build');
  await expect(line(page)).not.toContainText(/helper/i);
  await expect(helperRows(page, thread('Tester'))).toHaveCount(0);
  await expect(threadTree(page, 'Tester').getByRole('listitem')).toHaveCount(0);

  const bots: { threads?: { id: string; parent_thread_id?: string }[] }[] = await api(
    m,
    'GET',
    '/api/bots',
  );
  const sidebar = bots.flatMap((b) => b.threads ?? []);
  expect(sidebar.find((t) => t.id === thread('Tester'))?.parent_thread_id).toBe(boss);
  expect(sidebar.find((t) => t.id === boss)?.parent_thread_id ?? '').toBe('');
});

test('two helpers: still one line, naming both', async ({ page }) => {
  const { thread } = await bossHandsOff(m, page, [
    ['Tester', prompts.hang],
    ['Reviewer', prompts.hang],
  ]);
  await away(page);

  await expect(line(page)).toHaveCount(1);
  await expect(line(page)).toContainText('Tester');
  await expect(line(page)).toContainText('Reviewer');
  for (const name of ['Tester', 'Reviewer']) {
    await expect(helperRows(page, thread(name))).toHaveCount(0);
    await expect(threadTree(page, name).getByRole('listitem')).toHaveCount(0);
  }
});

test('a helper waiting on approval: Needs you shows once, on the line; Allow in the boss thread clears it', async ({
  page,
}) => {
  await bossHandsOff(m, page, [['Tester', reportPrompt]]);
  const allow = tray(page).getByRole('button', { name: 'Allow', exact: true });
  await expect(allow).toHaveCount(1);
  await away(page);

  await expect(nav(page).getByText('Needs you', { exact: true })).toHaveCount(1);
  await expect(line(page)).toHaveCount(1);
  await expect(line(page)).toContainText('Tester');
  await expect(line(page)).toContainText('Needs you');
  await expect(threadTree(page, 'Poteto').locator('[data-marker="needs-you"]')).toHaveCount(0);
  await expect(threadTree(page, 'Tester').getByRole('listitem')).toHaveCount(0);

  await page.goBack();
  await allow.click();
  await expect(allow).toHaveCount(0);
  await expect(nav(page).getByText(/Needs you/)).toHaveCount(0);
});

test('clicking the line opens the boss thread with the helpers panel in view', async ({ page }) => {
  const { poteto, boss } = await bossHandsOff(m, page, [['Tester', prompts.hang]]);
  await away(page);

  await line(page).click({ timeout: 10_000 });
  await expect(page).toHaveURL(`${m.url}/bots/${poteto.id}/threads/${boss}`);
  await expect(tray(page)).toContainText('Working with Poteto');
  await expect(tray(page)).toBeInViewport();
});

test('the helper thread header is a breadcrumb: the boss thread title, then the helper Bot', async ({
  page,
}) => {
  const { poteto, bots, boss, thread } = await bossHandsOff(m, page, [['Tester', prompts.hang]]);
  const title: string = (await api(m, 'GET', `/api/threads/${boss}`)).title;

  await page.goto(`${m.url}/bots/${bots.Tester.id}/threads/${thread('Tester')}`);
  const crumb = page.locator('header').getByRole('navigation', { name: 'Breadcrumb' });
  await expect(crumb.getByRole('img', { name: /^Whale\b/ })).toBeVisible();
  await expect(crumb).toContainText('›');
  await expect(crumb).toContainText('Tester');
  await crumb.getByRole('link', { name: title }).click({ timeout: 10_000 });
  await expect(page).toHaveURL(`${m.url}/bots/${poteto.id}/threads/${boss}`);
});

test('after the helper reports the line is gone and the helper thread is not in the sidebar but its Bot shows 1 more, also after a reload and a restart; it is in the helper Bot’s threads popup with its history', async ({
  page,
}) => {
  const { thread } = await bossHandsOff(m, page, [['Tester', reportPrompt]]);
  const helper = thread('Tester');
  await tray(page).getByRole('button', { name: 'Allow', exact: true }).click();
  await expect.poll(() => runStatus(m, helper), { timeout: 30_000 }).toBe('done');
  await away(page);

  await expect(line(page)).toHaveCount(0);
  await expect(helperRows(page, helper)).toHaveCount(0);
  await expect(threadTree(page, 'Tester').getByRole('button', { name: /^1 more$/ })).toBeVisible();

  await page.reload();
  await expect(threadTree(page, 'Tester').getByRole('button', { name: /^1 more$/ })).toBeVisible();
  await expect(helperRows(page, helper)).toHaveCount(0);
  m.kill();
  await m.restart();
  await page.goto(m.url);
  await expect(threadTree(page, 'Tester').getByRole('button', { name: /^1 more$/ })).toBeVisible();
  await expect(helperRows(page, helper)).toHaveCount(0);
  await expect(line(page)).toHaveCount(0);

  await threadTree(page, 'Tester')
    .getByRole('button', { name: /^\d+ more$/ })
    .click();
  const popup = page.getByRole('dialog', { name: 'Threads with Tester' });
  await expect(popup.locator(`a[href$="/threads/${helper}"]`)).toHaveCount(1);
  await popup.locator(`a[href$="/threads/${helper}"]`).click();
  await expect(page).toHaveURL(new RegExp(`/threads/${helper}$`));
  const log = page.getByRole('log', { name: 'Messages' });
  await expect(log.getByRole('article', { name: 'Poteto' })).toContainText(reportPrompt);
  await expect(log.getByRole('article', { name: 'Tester' })).toContainText(reportReply);
});

test('with the boss thread folded into N more, that line says a helper is working', async ({
  page,
}) => {
  const { poteto } = await makeBoss(m, ['Tester']);
  for (let i = 0; i < 3; i++) {
    const t = await openThreadAPI(m, poteto.id, prompts.pong);
    await expect.poll(() => runStatus(m, t.id), { timeout: 15_000 }).toBe('done');
    await api(m, 'POST', `/api/threads/${t.id}/pin`);
  }
  const boss = await openThreadAPI(m, poteto.id, delegate([['Tester', prompts.hang]]));
  await helperThreads(m, boss.id, 1);
  await away(page);

  const more = threadTree(page, 'Poteto').getByRole('button', { name: /more/ });
  await expect(more).toHaveCount(1);
  await expect(more).toContainText('Tester working');
});
