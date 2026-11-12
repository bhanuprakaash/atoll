// The sidebar marks where you are once, on the most specific item: the open thread's row, or the
// Bot row on a Bot's new-thread page. Issue #176; rule in docs/design.md (sidebar, Selected row).
import { expect, type Locator, type Page, test } from '@playwright/test';
import {
  api,
  type Machine,
  newBotAPI,
  openThreadAPI,
  prompts,
  sidebarRow,
  startAtoll,
  threadTree,
} from './atoll';

let m: Machine;
test.beforeEach(async () => {
  m = await startAtoll();
});
test.afterEach(() => m?.stop());

// filled: the row the link sits in (the link up to its list item) has the selected fill.
const filled = (link: Locator) =>
  link.evaluate((el) => {
    const probe = document.createElement('div');
    probe.style.background = 'var(--color-selected)';
    document.body.append(probe);
    const want = getComputedStyle(probe).backgroundColor;
    probe.remove();
    for (let n: Element | null = el; n; n = n.parentElement) {
      if (getComputedStyle(n).backgroundColor === want) return true;
      if (n.tagName === 'LI') return false;
    }
    return false;
  });

async function expectFilled(link: Locator, want: boolean) {
  await expect.poll(() => filled(link)).toBe(want);
}

// away keeps the pointer off every row, so hover cannot stand in for the selected fill.
const away = (page: Page) => page.mouse.move(1270, 5);

const marked = (page: Page) =>
  page
    .locator('[role="navigation"][aria-label="Bots"], nav[aria-label="Bots"]')
    .locator('[aria-current]');

async function waitDone(threadID: string) {
  await expect
    .poll(async () => (await api(m, 'GET', `/api/threads/${threadID}`)).run_status, {
      timeout: 15_000,
    })
    .toBe('done');
}

async function newThreadPage(page: Page, botID: string, name: string) {
  await page.goto(`${m.url}/bots/${botID}/new`);
  await expect(page.getByRole('heading', { name: `What should ${name} work on?` })).toBeVisible();
  await away(page);
}

async function threadPage(page: Page, botID: string, threadID: string) {
  await page.goto(`${m.url}/bots/${botID}/threads/${threadID}`);
  await expect(
    page.getByRole('log', { name: 'Messages' }).getByRole('article').first(),
  ).toBeVisible();
  await away(page);
}

// botRow is a Bot's own link: a thread titled with the Bot's name must not match it.
const botRow = (page: Page, botID: string) =>
  page.getByRole('navigation', { name: 'Bots' }).locator(`a[href="/bots/${botID}"]`);

// expectOnlyThread: the thread's row is the one selected row; its Bot row is not selected.
async function expectOnlyThread(page: Page, bot: string, botID: string, threadID: string) {
  const row = threadTree(page, bot).locator(`a[href="/bots/${botID}/threads/${threadID}"]`);
  await expect(row).toBeVisible();
  await expectFilled(row, true);
  await expect(row).toHaveAttribute('aria-current', 'page');
  await expectFilled(botRow(page, botID), false);
  await expect(botRow(page, botID)).not.toHaveAttribute('aria-current', /./);
  await expect(marked(page)).toHaveCount(1);
}

// expectOnlyBot: the Bot row is the one selected row; no thread row of it is.
async function expectOnlyBot(page: Page, bot: string, botID: string) {
  await expectFilled(botRow(page, botID), true);
  await expect(botRow(page, botID)).toHaveAttribute('aria-current', 'page');
  const rows = threadTree(page, bot).getByRole('link');
  await expect(rows.first()).toBeVisible();
  for (const row of await rows.all()) {
    await expectFilled(row, false);
    await expect(row).not.toHaveAttribute('aria-current', /./);
  }
  await expect(marked(page)).toHaveCount(1);
}

test('with a thread open only its row is selected; on the Bot new-thread page the Bot row is', async ({
  page,
}) => {
  const bot = await newBotAPI(m, 'Pilot');
  const first = await openThreadAPI(m, bot.id, prompts.pong);
  const second = await openThreadAPI(m, bot.id, prompts.keep);
  await waitDone(first.id);
  await waitDone(second.id);

  await threadPage(page, bot.id, first.id);
  await expectOnlyThread(page, 'Pilot', bot.id, first.id);
  await expectFilled(
    threadTree(page, 'Pilot').locator(`a[href="/bots/${bot.id}/threads/${second.id}"]`),
    false,
  );

  await newThreadPage(page, bot.id, 'Pilot');
  await expectOnlyBot(page, 'Pilot', bot.id);
});

test('a pinned Bot and its pinned thread follow the same rule', async ({ page }) => {
  const bot = await newBotAPI(m, 'Anchor');
  const thread = await openThreadAPI(m, bot.id, prompts.pong);
  await waitDone(thread.id);
  await api(m, 'POST', `/api/bots/${bot.id}/pin`);
  await api(m, 'POST', `/api/threads/${thread.id}/pin`);

  await threadPage(page, bot.id, thread.id);
  await expect(threadTree(page, 'Anchor').locator('[data-marker="pinned"]')).toHaveCount(1);
  await expectOnlyThread(page, 'Anchor', bot.id, thread.id);

  await newThreadPage(page, bot.id, 'Anchor');
  await expectOnlyBot(page, 'Anchor', bot.id);
});

test('a helper thread is the one selected row, not its Bot or the boss', async ({ page }) => {
  const poteto = await newBotAPI(m, 'Poteto', 'claude', { look: 'whale' });
  const tester = await newBotAPI(m, 'Tester', 'claude', { look: 'shark' });
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
  await expectOnlyThread(page, 'Tester', tester.id, helper.id);
  await expectFilled(botRow(page, poteto.id), false);
});

test('in the threads popup the open thread is the one selected row, and its Bot row is not', async ({
  page,
}) => {
  const bot = await newBotAPI(m, 'Beacon');
  const names = ['Alpha', 'Delta', 'Echo', 'Foxtrot', 'Golf'];
  const threads = [];
  for (let i = 0; i < names.length; i++) threads.push(await openThreadAPI(m, bot.id, prompts.keep));
  for (const [i, name] of names.entries()) {
    await waitDone(threads[i].id);
    await api(m, 'PATCH', `/api/threads/${threads[i].id}`, {
      title: `${name} notes`,
    });
  }

  await threadPage(page, bot.id, threads[0].id);
  await expectOnlyThread(page, 'Beacon', bot.id, threads[0].id);
  await threadTree(page, 'Beacon')
    .getByRole('button', { name: /^\d+ more$/ })
    .click();
  const popup = page.getByRole('dialog', { name: 'Threads with Beacon' });
  await expect(popup).toBeVisible();
  await away(page);

  const link = (id: string) => popup.locator(`a[href="/bots/${bot.id}/threads/${id}"]`);
  await expectFilled(link(threads[0].id), true);
  await expect(link(threads[0].id)).toHaveAttribute('aria-current', 'page');
  for (const t of threads.slice(1)) {
    await expectFilled(link(t.id), false);
    await expect(link(t.id)).not.toHaveAttribute('aria-current', /./);
  }
  await expectFilled(botRow(page, bot.id), false);
  await expect(marked(page)).toHaveCount(1);
});
