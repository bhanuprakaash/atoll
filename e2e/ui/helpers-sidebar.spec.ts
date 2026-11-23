// Delegated work in the sidebar and the tabs: Thread details shows the helpers, the boss's Bot
// row carries a helper's ask, the helper thread is never a tab or one of the other threads of its
// own Bot (running or finished), and the boss thread's tab, selected on the helper thread, leads
// back to the boss thread.
import { expect, type Page, test } from '@playwright/test';
import {
  api,
  type Machine,
  moreButton,
  openNow,
  openThread,
  openThreadAPI,
  prompts,
  sidebarRow,
  startAtoll,
  threadTab,
  waitStatus,
} from './atoll';
import {
  bossHandsOff,
  reportPrompt,
  reportReply,
  runStatus,
  composerArea,
  details,
  openDetails,
} from './helpers-fixture';

let m: Machine;
test.beforeEach(async () => {
  m = await startAtoll();
});
test.afterEach(() => m?.stop());

const titleOf = async (id: string): Promise<string> =>
  (await api(m, 'GET', `/api/threads/${id}`)).title;

// ownThread gives the helper Bot a finished thread of its own, so a page of that Bot shows its tabs.
async function ownThread(botID: string) {
  const t = await openThreadAPI(m, botID, prompts.pong);
  await waitStatus(m, t.id, 'done');
  return t.id as string;
}

// notListed: on a page of the helper Bot, the helper thread is no tab and there are no other
// threads under N more.
async function notListed(page: Page, botID: string, own: string, helperTitle: string) {
  await openThread(page, m, botID, own);
  await expect(threadTab(page, helperTitle)).toHaveCount(0);
  await expect(moreButton(page)).toHaveCount(0);
}

test('one helper: Thread details shows it; its thread is no tab of the boss, nor of its own Bot while it runs', async ({
  page,
}) => {
  const { poteto, bots, boss, thread } = await bossHandsOff(m, page, [['Tester', prompts.hang]]);
  const helperTitle = await titleOf(thread('Tester'));

  await expect(page).toHaveURL(`${m.url}/bots/${poteto.id}/threads/${boss}`);
  await expect(
    (await openDetails(page)).getByLabel('Tester: working', { exact: true }),
  ).toBeVisible();
  await page.keyboard.press('Escape');
  await expect(threadTab(page, await titleOf(boss))).toHaveAttribute('aria-selected', 'true');
  await expect(threadTab(page, helperTitle)).toHaveCount(0);

  await notListed(page, bots.Tester.id, await ownThread(bots.Tester.id), helperTitle);

  const summaries: { threads?: { id: string; parent_thread_id?: string }[] }[] = await api(
    m,
    'GET',
    '/api/bots',
  );
  const sidebar = summaries.flatMap((b) => b.threads ?? []);
  expect(sidebar.find((t) => t.id === thread('Tester'))?.parent_thread_id).toBe(boss);
  expect(sidebar.find((t) => t.id === boss)?.parent_thread_id ?? '').toBe('');
});

test('two helpers: Thread details shows both; neither thread is a tab', async ({ page }) => {
  const { bots, thread } = await bossHandsOff(m, page, [
    ['Tester', prompts.hang],
    ['Reviewer', prompts.hang],
  ]);
  const region = await openDetails(page);
  for (const name of ['Tester', 'Reviewer']) {
    await expect(region.getByLabel(`${name}: working`, { exact: true })).toBeVisible();
    await expect(threadTab(page, await titleOf(thread(name)))).toHaveCount(0);
  }
  for (const name of ['Tester', 'Reviewer'])
    await notListed(
      page,
      bots[name].id,
      await ownThread(bots[name].id),
      await titleOf(thread(name)),
    );
});

test('a helper waiting on approval: the boss Bot row says what it asks; Allow in the boss thread clears it', async ({
  page,
}) => {
  await bossHandsOff(m, page, [['Tester', reportPrompt]]);
  const allow = composerArea(page).getByRole('button', {
    name: 'Allow',
    exact: true,
  });
  await expect(allow).toHaveCount(1);
  const row = sidebarRow(page, 'Poteto');
  await expect(row).toContainText('Wants to run make check');
  await expect(row).not.toContainText('Needs you');

  await allow.click();
  await expect(allow).toHaveCount(0);
  await expect(row).not.toContainText('Wants to');
});

test('on the helper thread the boss thread tab is selected and leads back to the boss thread', async ({
  page,
}) => {
  const { poteto, bots, boss, thread } = await bossHandsOff(m, page, [['Tester', prompts.hang]]);
  const title: string = (await api(m, 'GET', `/api/threads/${boss}`)).title;

  await page.goto(`${m.url}/bots/${bots.Tester.id}/threads/${thread('Tester')}`);
  const tab = threadTab(page, title);
  await expect(tab).toHaveAttribute('aria-selected', 'true');
  await tab.click({ timeout: 10_000 });
  await expect(page).toHaveURL(`${m.url}/bots/${poteto.id}/threads/${boss}`);
});

test('after the helper reports the helper thread is no tab of the boss nor of its own Bot, also after a reload and a restart; the API still lists it', async ({
  page,
}) => {
  const { poteto, bots, boss, thread } = await bossHandsOff(m, page, [['Tester', reportPrompt]]);
  const helper = thread('Tester');
  const helperTitle = await titleOf(helper);
  await composerArea(page).getByRole('button', { name: 'Allow', exact: true }).click();
  await expect.poll(() => runStatus(m, helper), { timeout: 30_000 }).toBe('done');
  await openThread(page, m, poteto.id, boss);
  await expect(threadTab(page, helperTitle)).toHaveCount(0);

  const own = await ownThread(bots.Tester.id);
  await notListed(page, bots.Tester.id, own, helperTitle);
  await page.reload();
  await expect(threadTab(page, await titleOf(own))).toBeVisible();
  await expect(threadTab(page, helperTitle)).toHaveCount(0);
  await expect(moreButton(page)).toHaveCount(0);
  m.kill();
  await m.restart();
  await notListed(page, bots.Tester.id, own, helperTitle);

  const summaries: { threads?: { id: string; parent_thread_id?: string }[] }[] = await api(
    m,
    'GET',
    '/api/bots',
  );
  const listed = summaries.flatMap((b) => b.threads ?? []).find((t) => t.id === helper);
  expect(listed?.parent_thread_id).toBe(boss);
});

test('a finished helper thread is no row in Now; it still opens from Thread details with its history and the boss thread tab leads back to the boss thread', async ({
  page,
}) => {
  const { poteto, bots, boss, thread } = await bossHandsOff(m, page, [['Tester', reportPrompt]]);
  const helper = thread('Tester');
  const title: string = (await api(m, 'GET', `/api/threads/${boss}`)).title;
  await composerArea(page).getByRole('button', { name: 'Allow', exact: true }).click();
  await expect.poll(() => runStatus(m, helper), { timeout: 30_000 }).toBe('done');

  const now = await openNow(page);
  await expect(now.locator(`a[href$="/threads/${helper}"]`)).toHaveCount(0);

  await page.goto(`${m.url}/bots/${poteto.id}/threads/${boss}`);
  await openDetails(page);
  await details(page).getByRole('listitem').filter({ hasText: 'Tester' }).click();
  await expect(page).toHaveURL(`${m.url}/bots/${bots.Tester.id}/threads/${helper}`);
  await expect(page.getByRole('log', { name: 'Messages' })).toContainText(reportReply);

  const tab = threadTab(page, title);
  await expect(tab).toHaveAttribute('aria-selected', 'true');
  await tab.click({ timeout: 10_000 });
  await expect(page).toHaveURL(`${m.url}/bots/${poteto.id}/threads/${boss}`);
});
