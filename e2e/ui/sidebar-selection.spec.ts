// The sidebar marks where you are once: with a thread open only its row in the pane is selected;
// on a Bot's New thread screen no row is, and the face's bar alone marks the Bot. Issues #176, #469,
// #470; rule in docs/design.md (Selected row).
import { expect, type Locator, type Page, test } from '@playwright/test';
import {
  api,
  type Machine,
  newBotAPI,
  openThreadAPI,
  prompts,
  sidebarRow,
  startAtoll,
  threadPane,
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

// marked is every element of the rail and the pane that says it is current.
const marked = (page: Page, bot: string) =>
  page
    .getByRole('navigation', { name: 'Bots' })
    .locator('[aria-current]')
    .or(threadPane(page, bot).locator('[aria-current]'));

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

// botRow is a Bot's face: a thread titled with the Bot's name must not match it.
const botRow = (page: Page, botID: string) =>
  page.getByRole('navigation', { name: 'Bots' }).locator(`a[href="/bots/${botID}"]`);

// expectOnlyThread: the thread's row is the one selected row; the face has the bar, not the fill.
async function expectOnlyThread(page: Page, bot: string, botID: string, threadID: string) {
  const row = threadPane(page, bot).locator(`a[href="/bots/${botID}/threads/${threadID}"]`);
  await expect(row).toBeVisible();
  await expectFilled(row, true);
  await expect(row).toHaveAttribute('aria-current', 'page');
  await expect(botRow(page, botID)).toHaveAttribute('data-current', 'true');
  await expectFilled(botRow(page, botID), false);
  await expect(botRow(page, botID)).not.toHaveAttribute('aria-current', /./);
  await expect(marked(page, bot)).toHaveCount(1);
}

// expectOnlyBot: on the New thread screen no row is selected; the face's bar alone marks the Bot.
async function expectOnlyBot(page: Page, bot: string, botID: string) {
  await expect(botRow(page, botID)).toHaveAttribute('data-current', 'true');
  await expect(botRow(page, botID)).not.toHaveAttribute('aria-current', /./);
  const rows = threadPane(page, bot).getByRole('link');
  await expect(rows.first()).toBeVisible();
  for (const row of await rows.all()) {
    await expectFilled(row, false);
    await expect(row).not.toHaveAttribute('aria-current', /./);
  }
  await expect(marked(page, bot)).toHaveCount(0);
}

test('with a thread open only its row is selected; on the New thread screen no row is and the face has the bar', async ({
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
    threadPane(page, 'Pilot').locator(`a[href="/bots/${bot.id}/threads/${second.id}"]`),
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
  await expect(threadPane(page, 'Anchor').locator('[data-marker="pinned"]')).toHaveCount(1);
  await expectOnlyThread(page, 'Anchor', bot.id, thread.id);

  await newThreadPage(page, bot.id, 'Anchor');
  await expectOnlyBot(page, 'Anchor', bot.id);
});

test('a running helper thread has no row; its boss Bot has the bar and the boss row is not selected', async ({
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
  await expect(botRow(page, poteto.id)).toHaveAttribute('data-current', 'true');
  await expect(threadPane(page, 'Tester').locator(`a[href$="/threads/${helper.id}"]`)).toHaveCount(0);
  await expect(marked(page, 'Tester')).toHaveCount(0);
  await expectFilled(botRow(page, poteto.id), false);
});
