// Delegated work in the thread pane: one line under the boss thread, the helper thread a row of its
// own Bot's pane only once it is finished (under Recent), a breadcrumb from the helper thread
// back to the boss thread.
import { expect, type Page, test } from '@playwright/test';
import { api, type Machine, prompts, showPane, startAtoll, threadPane } from './atoll';
import {
  bossHandsOff,
  reportPrompt,
  reportReply,
  runStatus,
  composerArea,
  details,
} from './helpers-fixture';

let m: Machine;
test.beforeEach(async () => {
  m = await startAtoll();
});
test.afterEach(() => m?.stop());

const line = (page: Page, boss = 'Poteto') =>
  threadPane(page, boss).locator('[data-line="helpers"]');
const rowOf = (page: Page, bot: string, threadID: string) =>
  threadPane(page, bot)
    .getByRole('listitem')
    .filter({ has: page.locator(`a[href$="/threads/${threadID}"]`) });

test('one helper: one line under the boss thread with its name and step; in its own Bot pane no row while it runs', async ({
  page,
}) => {
  const { poteto, bots, boss, thread } = await bossHandsOff(m, page, [['Tester', prompts.hang]]);
  await showPane(page, m, poteto.id, 'Poteto');

  await expect(line(page)).toHaveCount(1);
  await expect(line(page)).toContainText('Tester');
  await expect(line(page)).toContainText('Running make build');
  await expect(line(page)).not.toContainText(/helper/i);
  await expect(rowOf(page, 'Poteto', thread('Tester'))).toHaveCount(0);

  await showPane(page, m, bots.Tester.id, 'Tester');
  await expect(threadPane(page, 'Tester')).toBeVisible();
  await expect(rowOf(page, 'Tester', thread('Tester'))).toHaveCount(0);

  const summaries: { threads?: { id: string; parent_thread_id?: string }[] }[] = await api(
    m,
    'GET',
    '/api/bots',
  );
  const sidebar = summaries.flatMap((b) => b.threads ?? []);
  expect(sidebar.find((t) => t.id === thread('Tester'))?.parent_thread_id).toBe(boss);
  expect(sidebar.find((t) => t.id === boss)?.parent_thread_id ?? '').toBe('');
});

test('two helpers: still one line, naming both', async ({ page }) => {
  const { poteto, bots, thread } = await bossHandsOff(m, page, [
    ['Tester', prompts.hang],
    ['Reviewer', prompts.hang],
  ]);
  await showPane(page, m, poteto.id, 'Poteto');

  await expect(line(page)).toHaveCount(1);
  await expect(line(page)).toContainText('Tester');
  await expect(line(page)).toContainText('Reviewer');
  for (const name of ['Tester', 'Reviewer']) {
    await expect(rowOf(page, 'Poteto', thread(name))).toHaveCount(0);
    await showPane(page, m, bots[name].id, name);
    await expect(rowOf(page, name, thread(name))).toHaveCount(0);
    await showPane(page, m, poteto.id, 'Poteto');
  }
});

test('a helper waiting on approval: Needs you shows once, on the line; Allow in the boss thread clears it', async ({
  page,
}) => {
  const { poteto } = await bossHandsOff(m, page, [['Tester', reportPrompt]]);
  const allow = composerArea(page).getByRole('button', {
    name: 'Allow',
    exact: true,
  });
  await expect(allow).toHaveCount(1);
  const pane = await showPane(page, m, poteto.id, 'Poteto');

  await expect(pane.getByText('Needs you', { exact: true })).toHaveCount(1);
  await expect(line(page)).toHaveCount(1);
  await expect(line(page)).toContainText('Tester');
  await expect(line(page)).toContainText('Needs you');
  await expect(pane.locator('[data-marker="needs-you"]')).toHaveCount(0);

  await page.goBack();
  await allow.click();
  await expect(allow).toHaveCount(0);
  await expect(pane.getByText(/Needs you/)).toHaveCount(0);
});

test('clicking the line opens the boss thread with the helpers panel in view', async ({ page }) => {
  const { poteto, boss } = await bossHandsOff(m, page, [['Tester', prompts.hang]]);
  await showPane(page, m, poteto.id, 'Poteto');

  await line(page).click({ timeout: 10_000 });
  await expect(page).toHaveURL(`${m.url}/bots/${poteto.id}/threads/${boss}`);
  await expect(details(page)).toContainText('Tester');
  await expect(page.getByText(/Working with/)).toHaveCount(0);
  await expect(details(page)).toBeInViewport();
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

test('after the helper reports the line is gone and the helper thread is a row under Recent in its Bot pane, also after a reload and a restart; it opens with its history', async ({
  page,
}) => {
  const { poteto, bots, thread } = await bossHandsOff(m, page, [['Tester', reportPrompt]]);
  const helper = thread('Tester');
  await composerArea(page).getByRole('button', { name: 'Allow', exact: true }).click();
  await expect.poll(() => runStatus(m, helper), { timeout: 30_000 }).toBe('done');
  await showPane(page, m, poteto.id, 'Poteto');
  await expect(line(page)).toHaveCount(0);
  await expect(rowOf(page, 'Poteto', helper)).toHaveCount(0);

  const recent = (page: Page) =>
    threadPane(page, 'Tester').getByRole('list', {
      name: 'Recent',
      exact: true,
    });
  await showPane(page, m, bots.Tester.id, 'Tester');
  await expect(recent(page).locator(`a[href$="/threads/${helper}"]`)).toHaveCount(1);
  await page.reload();
  await expect(recent(page).locator(`a[href$="/threads/${helper}"]`)).toHaveCount(1);
  m.kill();
  await m.restart();
  await showPane(page, m, bots.Tester.id, 'Tester');
  await expect(recent(page).locator(`a[href$="/threads/${helper}"]`)).toHaveCount(1);

  await recent(page).locator(`a[href$="/threads/${helper}"]`).click();
  await expect(page).toHaveURL(new RegExp(`/threads/${helper}$`));
  const log = page.getByRole('log', { name: 'Messages' });
  await expect(log.getByRole('article', { name: 'Poteto' })).toContainText(reportPrompt);
  await expect(log).toContainText(reportReply);
});
