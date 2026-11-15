// Thread labels and quiet background runs: a thread shows one label (Needs you, Working, Failed,
// Out until <time>, Replied), a run only Reef's notices started adds no chat message and shows its
// final message nowhere in the thread, and nothing waits on the person as a card.
import { expect, type Page, test } from '@playwright/test';
import {
  api,
  type Machine,
  newBotAPI,
  openThreadAPI,
  prompts,
  showPane,
  startAtoll,
  threadPane,
  threadTitle,
  waitStatus,
} from './atoll';
import {
  delegate,
  helperThreads,
  makeBoss,
  reportPrompt,
  runStatus,
  startThread,
} from './helpers-fixture';

let m: Machine;
test.beforeEach(async () => {
  m = await startAtoll();
});
test.afterEach(() => m?.stop());

// quietRunEnded waits for the thread's newest run to be a different one from before and to have
// ended.
async function quietRunEnded(threadID: string, before: string) {
  await expect
    .poll(
      async () => {
        const thread = await api(m, 'GET', `/api/threads/${threadID}`);
        return thread.run_id !== before && thread.run_status !== 'running';
      },
      { timeout: 30_000 },
    )
    .toBe(true);
}

async function hasStatusLine(threadID: string) {
  return 'status_line' in (await api(m, 'GET', `/api/threads/${threadID}`));
}

const row = (page: Page, bot: string, title: string) =>
  threadPane(page, bot).getByRole('listitem').filter({ hasText: title });

test('a failed run shows Failed on its row until the thread is opened', async ({ page }) => {
  const bot = await newBotAPI(m, 'Failer');
  await showPane(page, m, bot.id, 'Failer');
  const failed = await openThreadAPI(m, bot.id, prompts.none);
  await waitStatus(m, failed.id, 'failed');

  const failing = row(page, 'Failer', threadTitle(prompts.none));
  await expect(failing).toContainText('Failed');
  await expect(failing).not.toContainText('Replied');

  await failing.getByRole('link').click();
  await expect(page).toHaveURL(`${m.url}/bots/${bot.id}/threads/${failed.id}`);
  await showPane(page, m, bot.id, 'Failer');
  await expect(row(page, 'Failer', threadTitle(prompts.none))).not.toContainText('Failed');
});

test('a thread of a provider at its usage limit reads Out until the reset time', async ({ page }) => {
  const bot = await newBotAPI(m, 'Limited');
  m.limit('claude', new Date(Date.now() + 2 * 60 * 60 * 1000));
  const limited = await openThreadAPI(m, bot.id, prompts.keep);
  await waitStatus(m, limited.id, 'failed');
  await page.goto(`${m.url}/bots/${bot.id}/threads/${limited.id}`);
  await expect(
    page.getByRole('log', { name: 'Messages' }).getByRole('article').first(),
  ).toBeVisible();
  await showPane(page, m, bot.id, 'Limited');

  const limitedRow = row(page, 'Limited', threadTitle(prompts.keep));
  await expect(limitedRow).toContainText(/Out until \d/);
  await expect(limitedRow).not.toContainText('Failed');
  await expect(limitedRow).not.toContainText('Replied');
});

// The boss hands its work to Tester, which answers at once; the boss's run for Reef's notice calls
// no tell_user, so what it writes last is shown nowhere in the thread.
test('a background run adds no message and its final message shows nowhere in the thread', async ({
  page,
}) => {
  await makeBoss(m, ['Tester']);
  await page.goto(m.url);
  const boss = await startThread(page, 'Poteto', delegate([['Tester', reportPrompt]]));
  const [helper] = await helperThreads(m, boss, 1);
  await expect
    .poll(async () => (await api(m, 'GET', `/api/threads/${helper.id}`)).approval?.id ?? '', {
      timeout: 15_000,
    })
    .not.toBe('');
  await expect.poll(() => runStatus(m, boss), { timeout: 15_000 }).not.toBe('running');
  const before = (await api(m, 'GET', `/api/threads/${boss}`)).run_id;
  const approval = (await api(m, 'GET', `/api/threads/${helper.id}`)).approval.id;
  await api(m, 'POST', `/api/approvals/${approval}`, { approve: true });
  await quietRunEnded(boss, before);
  await page.reload();

  const log = page.getByRole('log', { name: 'Messages' });
  await expect(log.getByRole('article').first()).toBeVisible();
  await expect(log.getByRole('article', { name: 'Poteto' })).toHaveCount(0);
  await expect(log.getByRole('article', { name: 'Tester' })).toHaveCount(0);
  await expect(page.getByText('Thanks, Tester.')).toHaveCount(0);

  // Nothing waits on the person as a card, a Blocked line, a ready PR card or a ledger.
  await expect(page.getByText('Waiting on you')).toHaveCount(0);
  await expect(page.getByText(/^Blocked:/)).toHaveCount(0);
  await expect(log.getByRole('list', { name: 'Updates' })).toHaveCount(0);
  await expect(page.getByRole('region', { name: /Waiting on you|Ready/ })).toHaveCount(0);
  expect(await hasStatusLine(boss), 'the API has no status_line').toBe(false);
});

// tell_user is the way a background run reaches the person: one Bot message for each call, and
// the final message shows nowhere.
test('tell_user adds one Bot message per call and the final message shows nowhere', async ({
  page,
}) => {
  await makeBoss(m, ['Tester']);
  await page.goto(m.url);
  const boss = await startThread(
    page,
    'Poteto',
    delegate([['Tester', 'Run the checks, then report the blocker.']]),
  );
  await helperThreads(m, boss, 1);

  const log = page.getByRole('log', { name: 'Messages' });
  await expect(log.getByText('Tester is blocked: it cannot read the release notes.')).toHaveCount(1, {
    timeout: 30_000,
  });
  await expect(
    log.getByText('I will hold the changelog until you say how to get it access.'),
  ).toHaveCount(1);
  await expect.poll(() => runStatus(m, boss), { timeout: 30_000 }).not.toBe('running');
  await page.reload();
  await expect(log.getByText('Tester is blocked: it cannot read the release notes.')).toHaveCount(1);
  await expect(page.getByText('Told the user about the blocker.')).toHaveCount(0);
  await expect(page.getByText('Thanks, Tester.')).toHaveCount(0);
  expect(await hasStatusLine(boss), 'the API has no status_line').toBe(false);
});
