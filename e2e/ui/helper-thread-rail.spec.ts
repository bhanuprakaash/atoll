// An open helper thread has no row in any pane, so the sidebar stays with its boss: the rail bar on
// the boss Bot, the boss's pane, and the selected fill and aria-current on the boss thread's helper
// line while the helper runs, on the boss thread's row once it is finished. Issues #555, #569; rule
// in docs/design.md (Selected row).
import { expect, type Locator, type Page, test } from '@playwright/test';
import { api, type Machine, nowPane, prompts, startAtoll, threadPane, waitStatus } from './atoll';
import { bossHandsOff, details, openDetails, reportPrompt, runStatus } from './helpers-fixture';

let m: Machine;
test.beforeEach(async () => {
  m = await startAtoll();
});
test.afterEach(() => m?.stop());

const rail = (page: Page) => page.getByRole('navigation', { name: 'Bots' });
const face = (page: Page, botID: string) => rail(page).locator(`a[href="/bots/${botID}"]`);
const line = (page: Page) => threadPane(page, 'Poteto').locator('[data-line="helpers"]');
const away = (page: Page) => page.mouse.move(1270, 5);

// filled: the link or one of its ancestors up to its list has the selected fill.
const filled = (link: Locator) =>
  link.evaluate((el) => {
    const probe = document.createElement('div');
    probe.style.background = 'var(--color-selected)';
    document.body.append(probe);
    const want = getComputedStyle(probe).backgroundColor;
    probe.remove();
    for (let n: Element | null = el; n && n.getAttribute('role') !== 'list'; n = n.parentElement)
      if (getComputedStyle(n).backgroundColor === want) return true;
    return false;
  });

// expectWithBoss: the rail bar is on the boss Bot, its pane shows its threads, and only the boss
// thread's helper line is selected, not the boss thread's row.
async function expectWithBoss(page: Page, bossBot: string, helperBot: string, boss: string) {
  await away(page);
  await expect(face(page, bossBot)).toHaveAttribute('data-current', 'true');
  await expect(face(page, helperBot)).not.toHaveAttribute('data-current', 'true');
  await expect(rail(page).locator('[aria-current]')).toHaveCount(0);

  const pane = threadPane(page, 'Poteto');
  await expect(pane).toBeVisible();
  await expect(threadPane(page, 'Tester')).toHaveCount(0);
  const bossRow = pane.locator(`a[href="/bots/${bossBot}/threads/${boss}"]:not([data-line])`);
  await expect(bossRow).toBeVisible();
  await expect(bossRow).not.toHaveAttribute('aria-current', /./);
  expect(await filled(bossRow)).toBe(false);

  await expect(line(page)).toHaveCount(1);
  await expect(line(page)).toHaveAttribute('aria-current', 'page');
  await expect.poll(() => filled(line(page))).toBe(true);
  await expect(pane.locator('[aria-current]')).toHaveCount(1);
}

const helperURL = (bot: string, thread: string) => `/bots/${bot}/threads/${thread}`;

test('opening a running helper thread from the handoff line keeps the rail and pane on the boss and selects the helper line', async ({
  page,
}) => {
  const { poteto, bots, boss, thread } = await bossHandsOff(m, page, [['Tester', prompts.hang]]);

  await page
    .getByRole('log', { name: 'Messages' })
    .getByRole('link', { name: 'Tester', exact: true })
    .click();
  await expect(page).toHaveURL(`${m.url}${helperURL(bots.Tester.id, thread('Tester'))}`);
  await expectWithBoss(page, poteto.id, bots.Tester.id, boss);
});

test('opening a running helper thread from the thread panel Helpers section gives the same rail, pane and selection', async ({
  page,
}) => {
  const { poteto, bots, boss, thread } = await bossHandsOff(m, page, [['Tester', prompts.hang]]);

  const region = await openDetails(page);
  await region.locator(`a[href$="/threads/${thread('Tester')}"]`).click();
  await expect(page).toHaveURL(`${m.url}${helperURL(bots.Tester.id, thread('Tester'))}`);
  await expect(details(page)).toBeHidden();
  await expectWithBoss(page, poteto.id, bots.Tester.id, boss);
});

test('a finished helper thread that is open keeps the rail and pane on the boss and selects the boss thread row', async ({
  page,
}) => {
  const { poteto, bots, boss, thread } = await bossHandsOff(m, page, [['Tester', reportPrompt]]);
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
  await away(page);

  await expect(face(page, poteto.id)).toHaveAttribute('data-current', 'true');
  await expect(face(page, bots.Tester.id)).not.toHaveAttribute('data-current', 'true');
  await expect(rail(page).locator('[aria-current]')).toHaveCount(0);
  const pane = threadPane(page, 'Poteto');
  await expect(pane).toBeVisible();
  await expect(threadPane(page, 'Tester')).toHaveCount(0);
  await expect(line(page)).toHaveCount(0);
  const row = pane.locator(`a[href="${helperURL(poteto.id, boss)}"]:not([data-line])`);
  await expect(row).toHaveCount(1);
  await expect(row).toHaveAttribute('aria-current', 'page');
  await expect.poll(() => filled(row)).toBe(true);
  await expect(pane.locator('[aria-current]')).toHaveCount(1);
});

test('opening a running helper thread from Now keeps the pane on Now', async ({ page }) => {
  const { poteto, bots, thread } = await bossHandsOff(m, page, [['Tester', prompts.hang]]);
  const now = await nowPane(page);

  await page
    .getByRole('log', { name: 'Messages' })
    .getByRole('link', { name: 'Tester', exact: true })
    .click();
  await expect(page).toHaveURL(`${m.url}${helperURL(bots.Tester.id, thread('Tester'))}`);
  await away(page);
  await expect(now).toBeVisible();
  await expect(threadPane(page, 'Poteto')).toHaveCount(0);
  await expect(threadPane(page, 'Tester')).toHaveCount(0);
  await expect(face(page, poteto.id)).not.toHaveAttribute('data-current', 'true');
  await expect(face(page, bots.Tester.id)).not.toHaveAttribute('data-current', 'true');
});

test('reloading on a running helper thread URL shows the same rail, pane and selection', async ({
  page,
}) => {
  const { poteto, bots, boss, thread } = await bossHandsOff(m, page, [['Tester', prompts.hang]]);

  await page.goto(`${m.url}${helperURL(bots.Tester.id, thread('Tester'))}`);
  await page.reload();
  await expect(page.getByRole('log', { name: 'Messages' })).toBeVisible();
  await expectWithBoss(page, poteto.id, bots.Tester.id, boss);
});
