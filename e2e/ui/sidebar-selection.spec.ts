// The sidebar marks where you are once: the open thread's Bot row has the selected fill and
// aria-current, and the open thread's tab is the one selected tab; on a Bot's New thread screen the
// Bot row is selected and no tab is. Issues #176, #469, #470; rule in docs/design.md (Selected row).
import { expect, type Locator, type Page, test } from '@playwright/test';
import {
  api,
  type Machine,
  newBotAPI,
  openThread,
  openThreadAPI,
  prompts,
  sidebarRow,
  startAtoll,
  threadTab,
  threadTabs,
  threadTitle,
  waitStatus,
} from './atoll';

let m: Machine;
test.beforeEach(async () => {
  m = await startAtoll();
});
test.afterEach(() => m?.stop());

// filled: the row or one of its ancestors inside the sidebar has the selected fill.
const filled = (row: Locator) =>
  row.evaluate((el) => {
    const probe = document.createElement('div');
    probe.style.background = 'var(--color-selected)';
    document.body.append(probe);
    const want = getComputedStyle(probe).backgroundColor;
    probe.remove();
    for (let n: Element | null = el; n && n.tagName !== 'NAV'; n = n.parentElement)
      if (getComputedStyle(n).backgroundColor === want) return true;
    return false;
  });

// away keeps the pointer off every row, so hover cannot stand in for the selected fill.
const away = (page: Page) => page.mouse.move(1270, 5);

const current = (page: Page) =>
  page.getByRole('navigation', { name: 'Bots' }).locator('[aria-current]');
const selectedTabs = (page: Page) => threadTabs(page).locator('[aria-selected="true"]');

// botRow is a Bot's row by id: a thread titled with the Bot's name must not match it.
const botRow = (page: Page, botID: string) =>
  page.getByRole('navigation', { name: 'Bots' }).locator(`a[href="/bots/${botID}"]`);

// expectOnlyThread: the Bot row is the one current row with the fill, and the thread's tab is the
// one selected tab.
async function expectOnlyThread(page: Page, botID: string, title: string) {
  await away(page);
  await expect(botRow(page, botID)).toHaveAttribute('aria-current', 'page');
  await expect.poll(() => filled(botRow(page, botID))).toBe(true);
  await expect(current(page)).toHaveCount(1);
  await expect(threadTab(page, title)).toHaveAttribute('aria-selected', 'true');
  await expect(selectedTabs(page)).toHaveCount(1);
}

// expectOnlyBot: on the New thread screen the Bot row is selected and no tab is.
async function expectOnlyBot(page: Page, botID: string, name: string) {
  await page.goto(`${m.url}/bots/${botID}/new`);
  await expect(page.getByRole('heading', { name: `What should ${name} work on?` })).toBeVisible();
  await away(page);
  await expect(botRow(page, botID)).toHaveAttribute('aria-current', 'page');
  await expect(current(page)).toHaveCount(1);
  await expect(selectedTabs(page)).toHaveCount(0);
}

test('with a thread open its Bot row and its tab are the selected ones; on the New thread screen no tab is', async ({
  page,
}) => {
  const bot = await newBotAPI(m, 'Pilot');
  const other = await newBotAPI(m, 'Other');
  const first = await openThreadAPI(m, bot.id, prompts.pong);
  const second = await openThreadAPI(m, bot.id, prompts.keep);
  await waitStatus(m, first.id, 'done');
  await waitStatus(m, second.id, 'done');
  await api(m, 'POST', `/api/threads/${second.id}/pin`);

  await openThread(page, m, bot.id, first.id);
  await expectOnlyThread(page, bot.id, threadTitle(prompts.pong));
  await expect(threadTab(page, threadTitle(prompts.keep))).toHaveAttribute(
    'aria-selected',
    'false',
  );
  expect(await filled(botRow(page, other.id))).toBe(false);

  await expectOnlyBot(page, bot.id, 'Pilot');
});

test('a pinned Bot and its pinned thread follow the same rule', async ({ page }) => {
  const bot = await newBotAPI(m, 'Anchor');
  const thread = await openThreadAPI(m, bot.id, prompts.pong);
  await waitStatus(m, thread.id, 'done');
  await api(m, 'POST', `/api/bots/${bot.id}/pin`);
  await api(m, 'POST', `/api/threads/${thread.id}/pin`);

  await openThread(page, m, bot.id, thread.id);
  await expect(
    threadTab(page, threadTitle(prompts.pong)).getByRole('img', { name: 'Pinned', exact: true }),
  ).toBeVisible();
  await expectOnlyThread(page, bot.id, threadTitle(prompts.pong));

  await expectOnlyBot(page, bot.id, 'Anchor');
});

test('a running helper thread has no tab; its boss Bot row is selected, not the helper Bot', async ({
  page,
}) => {
  const poteto = await newBotAPI(m, 'Poteto', 'claude', { look: 'whale' });
  const tester = await newBotAPI(m, 'Tester', 'claude', { look: 'octopus' });
  await api(m, 'PUT', `/api/bots/${poteto.id}`, { delegates: [tester.id] });
  const brief = 'Run the checks, then report back.';
  const escaped = `"${Array.from(brief, (c) => `\\u${c.charCodeAt(0).toString(16).padStart(4, '0')}`).join('')}"`;
  await page.goto(m.url);
  await sidebarRow(page, 'Poteto').click();
  await page
    .getByRole('textbox', { name: 'Message' })
    .fill(
      `Use MCP tools. [{"server":"atoll","tool":"delegate","args":{"bot":"Tester","brief":${escaped}}}]`,
    );
  await page.getByRole('button', { name: 'Send' }).click();
  await expect(page).toHaveURL(new RegExp(`/bots/${poteto.id}/threads/[^/]+$`));
  await expect
    .poll(async () => (await api(m, 'GET', `/api/bots/${tester.id}/threads`)).length)
    .toBe(1);
  const [helper] = await api(m, 'GET', `/api/bots/${tester.id}/threads`);

  await page.goto(`${m.url}/bots/${tester.id}/threads/${helper.id}`);
  await away(page);
  await expect(botRow(page, poteto.id)).toHaveAttribute('aria-current', 'page');
  await expect(botRow(page, tester.id)).not.toHaveAttribute('aria-current', /./);
  await expect(current(page)).toHaveCount(1);
  await expect(threadTab(page, helper.title)).toHaveCount(0);
  await expect(selectedTabs(page)).toHaveCount(1);
  expect(await filled(botRow(page, tester.id))).toBe(false);
});
