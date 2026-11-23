// A helper thread has no tab and no row of its own, so while one is open the sidebar stays with its
// boss: the boss Bot's row is selected and the boss thread's tab is, running or finished. No line
// under a row names the helpers; the header island shows them. Rule in docs/design.md (Selected
// row).
import { expect, type Page, test } from '@playwright/test';
import {
  api,
  type Machine,
  prompts,
  sidebarRow,
  startAtoll,
  threadTab,
  threadTabs,
  waitStatus,
} from './atoll';
import { bossHandsOff, details, openDetails, reportPrompt, runStatus } from './helpers-fixture';

let m: Machine;
test.beforeEach(async () => {
  m = await startAtoll();
});
test.afterEach(() => m?.stop());

const sidebar = (page: Page) => page.getByRole('navigation', { name: 'Bots' });
const away = (page: Page) => page.mouse.move(1270, 5);
const title = async (id: string): Promise<string> =>
  (await api(m, 'GET', `/api/threads/${id}`)).title;

// expectWithBoss: the boss Bot's row is the one current row, the boss thread's tab the one
// selected tab, the helper thread has no tab, and no helper line shows.
async function expectWithBoss(page: Page, boss: string, helper: string) {
  await away(page);
  await expect(sidebarRow(page, 'Poteto')).toHaveAttribute('aria-current', 'page');
  await expect(sidebarRow(page, 'Tester')).not.toHaveAttribute('aria-current', /./);
  await expect(sidebar(page).locator('[aria-current]')).toHaveCount(1);
  await expect(threadTab(page, await title(boss))).toHaveAttribute('aria-selected', 'true');
  await expect(threadTabs(page).locator('[aria-selected="true"]')).toHaveCount(1);
  await expect(threadTab(page, await title(helper))).toHaveCount(0);
  await expect(page.locator('[data-line="helpers"]')).toHaveCount(0);
}

const helperURL = (bot: string, thread: string) => `/bots/${bot}/threads/${thread}`;

test('opening a running helper thread from the handoff line selects the boss row and the boss thread tab; the island shows the helper', async ({
  page,
}) => {
  const { bots, boss, thread } = await bossHandsOff(m, page, [['Tester', prompts.hang]]);
  await expect(page.locator('header').getByLabel('Tester: working', { exact: true })).toBeVisible();
  await expect(page.locator('[data-line="helpers"]')).toHaveCount(0);

  await page
    .getByRole('log', { name: 'Messages' })
    .getByRole('link', { name: 'Tester', exact: true })
    .click();
  await expect(page).toHaveURL(`${m.url}${helperURL(bots.Tester.id, thread('Tester'))}`);
  await expectWithBoss(page, boss, thread('Tester'));
});

test('opening a running helper thread from the thread panel Helpers section gives the same selection', async ({
  page,
}) => {
  const { bots, boss, thread } = await bossHandsOff(m, page, [['Tester', prompts.hang]]);

  const region = await openDetails(page);
  await region.locator(`a[href$="/threads/${thread('Tester')}"]`).click();
  await expect(page).toHaveURL(`${m.url}${helperURL(bots.Tester.id, thread('Tester'))}`);
  await expect(details(page)).toBeHidden();
  await expectWithBoss(page, boss, thread('Tester'));
});

test('a finished helper thread that is open keeps the boss row and the boss thread tab selected', async ({
  page,
}) => {
  const { bots, boss, thread } = await bossHandsOff(m, page, [['Tester', reportPrompt]]);
  const helper = thread('Tester');
  await waitStatus(m, helper, 'waiting');

  await page
    .getByRole('log', { name: 'Messages' })
    .getByRole('link', { name: 'Tester', exact: true })
    .click();
  await expect(page).toHaveURL(`${m.url}${helperURL(bots.Tester.id, helper)}`);
  const { approval } = await api(m, 'GET', `/api/threads/${helper}`);
  await api(m, 'POST', `/api/approvals/${approval.id}`, { approve: true });
  await expect.poll(() => runStatus(m, helper), { timeout: 30_000 }).toBe('done');
  await expectWithBoss(page, boss, helper);
});

test('reloading on a running helper thread URL shows the same selection', async ({ page }) => {
  const { bots, boss, thread } = await bossHandsOff(m, page, [['Tester', prompts.hang]]);

  await page.goto(`${m.url}${helperURL(bots.Tester.id, thread('Tester'))}`);
  await page.reload();
  await expect(page.getByRole('log', { name: 'Messages' })).toBeVisible();
  await expectWithBoss(page, boss, thread('Tester'));
});
