// No header island: a thread page's header holds only the thread tabs and the Thread details and
// Bot settings buttons.
// What waits on the person shows on its Bot row, its tab and in Now, and is answered in the thread;
// the open thread's run state is its tab's marker, or for a failed run the chat's own line.
import { expect, type Locator, type Page, test } from '@playwright/test';
import {
  api,
  lines,
  type Machine,
  marks,
  newBotAPI,
  openNow,
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

const away = (page: Page) => page.mouse.move(1270, 5);
const colours = async (el: Locator) => (await marks(el)).map((d) => d.colour);
const editAsk = 'Wants to edit hello.txt';

// watchHeader collects every text the header shows from now on.
async function watchHeader(page: Page) {
  await page.locator('header').evaluate((el) => {
    const w = window as unknown as { __header: string[] };
    w.__header = [(el as HTMLElement).innerText];
    new MutationObserver(() => w.__header.push((el as HTMLElement).innerText)).observe(el, {
      subtree: true,
      childList: true,
      characterData: true,
    });
  });
}
const headerSeen = (page: Page) =>
  page.evaluate(() => (window as unknown as { __header: string[] }).__header.join('\n'));

// bareHeader: the header holds the Threads tablist and its N more and New thread buttons, then
// Thread details and Bot settings, and nothing of the island.
async function bareHeader(page: Page) {
  const header = page.locator('header');
  await expect(header.getByRole('button', { name: 'Bot settings', exact: true })).toBeVisible();
  await expect(header.getByRole('tablist', { name: 'Threads' })).toBeVisible();
  const buttons = await header
    .getByRole('button')
    .evaluateAll((els) =>
      els
        .filter((el) => !el.closest('[role="tablist"]'))
        .map((el) => el.getAttribute('aria-label') ?? el.textContent),
    );
  expect(buttons.filter((b) => !/^(\d+ more|Closed)$/.test(b ?? ''))).toEqual([
    'New thread',
    'Thread details',
    'Bot settings',
  ]);
  expect(buttons.length).toBeLessThanOrEqual(4);
  await expect(header).not.toContainText(editAsk);
  await expect(page.getByRole('button', { name: /^Notifications/ })).toHaveCount(0);
  await expect(page.getByRole('dialog', { name: 'Notifications' })).toHaveCount(0);
  await expect(page.locator('[data-island]')).toHaveCount(0);
  await expect(header.getByRole('heading')).toHaveCount(0);
  await expect(header.getByRole('navigation')).toHaveCount(0);
  await expect(header.getByRole('link')).toHaveCount(0);
  await expect(header.getByRole('alert')).toHaveCount(0);
  await expect(header.getByRole('status')).toHaveCount(0);
  await expect(header.getByRole('button', { name: /^(Allow|Deny)$/ })).toHaveCount(0);
}

test('a thread page has no island while another Bot waits on approval; the approval is on its Bot row, its tab and in Now, and Allow in the thread answers it', async ({
  page,
}) => {
  const pilot = await newBotAPI(m, 'Pilot');
  const asker = await newBotAPI(m, 'Asker');
  const mine = await openThreadAPI(m, pilot.id, prompts.keep);
  const other = await openThreadAPI(m, asker.id, prompts.pong);
  await waitStatus(m, mine.id, 'done');
  await waitStatus(m, other.id, 'done');

  await openThread(page, m, pilot.id, mine.id);
  await watchHeader(page);
  const waiting = await openThreadAPI(m, asker.id, prompts.write);
  await waitStatus(m, waiting.id, 'waiting');
  await away(page);

  const row = sidebarRow(page, 'Asker');
  await expect.poll(() => lines(row)).toEqual(['Asker', editAsk]);
  await expect.poll(() => colours(row)).toContain('light');
  await bareHeader(page);
  const seen = await headerSeen(page);
  for (const word of ['Asker', 'hello.txt', 'Needs you', 'Allow', 'waiting'])
    expect(seen, `the header never shows "${word}"`).not.toContain(word);

  const now = await openNow(page);
  await away(page);
  const needs = now.getByRole('list', { name: 'Needs you', exact: true }).getByRole('listitem');
  await expect(needs).toHaveCount(1);
  await expect.poll(() => lines(needs)).toEqual([threadTitle(prompts.write), `Asker · ${editAsk}`]);

  await openThread(page, m, asker.id, other.id);
  await away(page);
  const tab = threadTab(page, threadTitle(prompts.write));
  await expect.poll(() => colours(tab)).toEqual(['light']);
  await bareHeader(page);

  await tab.click();
  await expect(page).toHaveURL(`${m.url}/bots/${asker.id}/threads/${waiting.id}`);
  const card = page.getByRole('log', { name: 'Messages' }).getByRole('group', { name: 'Approval' });
  await expect(card.getByRole('button', { name: 'Deny', exact: true })).toBeVisible();
  await card.getByRole('button', { name: 'Allow', exact: true }).click();
  await waitStatus(m, waiting.id, 'done');
  await expect(card).toHaveCount(0);
  await expect(row).not.toContainText('Wants to');
  await bareHeader(page);
});

test("the open thread's run state is its tab's marker or the chat's failed line, never words in the header", async ({
  page,
}) => {
  const bot = await newBotAPI(m, 'Pilot');
  const busy = await openThreadAPI(m, bot.id, prompts.hang);
  const waiting = await openThreadAPI(m, bot.id, prompts.write);
  const failed = await openThreadAPI(m, bot.id, prompts.none);
  try {
    await waitStatus(m, busy.id, 'running');
    await waitStatus(m, waiting.id, 'waiting');
    await waitStatus(m, failed.id, 'failed');
    const open = () => threadTabs(page).locator('[aria-selected="true"]');
    const header = page.locator('header');

    await page.goto(`${m.url}/bots/${bot.id}/threads/${busy.id}`);
    await away(page);
    await expect(open().getByRole('img', { name: 'Working', exact: true })).toBeVisible();
    await expect(header).not.toContainText('Thinking');
    await bareHeader(page);

    await page.goto(`${m.url}/bots/${bot.id}/threads/${waiting.id}`);
    await away(page);
    await expect.poll(() => colours(open())).toEqual(['light']);
    await expect(header).not.toContainText('Needs you');
    await bareHeader(page);

    await page.goto(`${m.url}/bots/${bot.id}/threads/${failed.id}`);
    await away(page);
    await expect(
      page.getByRole('log', { name: 'Messages' }).getByText(/^(Run failed|Didn't run)/),
    ).toBeVisible();
    await expect(header).not.toContainText('Run failed');
    await bareHeader(page);
  } finally {
    await api(m, 'POST', `/api/runs/${busy.run.id}/stop`);
  }
});

test('a run that finishes in another Bot’s thread shows nothing in the header', async ({
  page,
}) => {
  const pilot = await newBotAPI(m, 'Pilot');
  const reviewer = await newBotAPI(m, 'Reviewer');
  const mine = await openThreadAPI(m, pilot.id, prompts.keep);
  await waitStatus(m, mine.id, 'done');
  await openThread(page, m, pilot.id, mine.id);
  await watchHeader(page);

  const theirs = await openThreadAPI(m, reviewer.id, prompts.pong);
  await waitStatus(m, theirs.id, 'done');
  // The island widened for about 4 s after a run ended; 2 s is well inside that.
  await page.waitForTimeout(2_000);
  await bareHeader(page);
  const seen = await headerSeen(page);
  for (const word of ['Reviewer', 'finished', threadTitle(prompts.pong)])
    expect(seen, `the header never shows "${word}"`).not.toContain(word);
});
