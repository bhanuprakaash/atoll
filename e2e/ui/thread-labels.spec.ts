// Thread labels and quiet background runs: a thread shows one label (Needs you, Working, Failed,
// Out until <time>, Replied), a run only Reef's notices started adds no chat message and shows its
// final message as one status line above the composer, and nothing waits on the person as a card.
// Contract: e2e/no-report-contract.md.
import { expect, type Page, test } from '@playwright/test';
import { api, type Machine, newBotAPI, openThreadAPI, prompts, startAtoll, threadTitle, threadTree } from './atoll';
import { delegate, helperThreads, makeBoss, reportPrompt, startThread } from './helpers-fixture';

let m: Machine;
test.beforeEach(async () => {
  m = await startAtoll();
});
test.afterEach(() => m?.stop());

async function waitStatus(threadID: string, status: string) {
  await expect
    .poll(async () => (await api(m, 'GET', `/api/threads/${threadID}`)).run_status, {
      timeout: 15_000,
    })
    .toBe(status);
}

// away shows a Bot of its own, so no thread of the Bots under test is on screen.
async function away(page: Page) {
  const bot = await newBotAPI(m, 'Elsewhere');
  await page.goto(`${m.url}/bots/${bot.id}`);
  await expect(page.getByRole('heading', { name: 'What should Elsewhere work on?' })).toBeVisible();
}

const row = (page: Page, bot: string, title: string) =>
  threadTree(page, bot).getByRole('listitem').filter({ hasText: title });

test('a failed run shows Failed on its row until the thread is opened', async ({ page }) => {
  const bot = await newBotAPI(m, 'Failer');
  await away(page);
  const failed = await openThreadAPI(m, bot.id, prompts.none);
  await waitStatus(failed.id, 'failed');

  const failing = row(page, 'Failer', threadTitle(prompts.none));
  await expect(failing).toContainText('Failed');
  await expect(failing).not.toContainText('Replied');

  await failing.getByRole('link').click();
  await expect(page).toHaveURL(`${m.url}/bots/${bot.id}/threads/${failed.id}`);
  await away(page);
  await expect(row(page, 'Failer', threadTitle(prompts.none))).not.toContainText('Failed');
});

test('a thread of a provider at its usage limit reads Out until the reset time', async ({ page }) => {
  const bot = await newBotAPI(m, 'Limited');
  m.limit('claude', new Date(Date.now() + 2 * 60 * 60 * 1000));
  const limited = await openThreadAPI(m, bot.id, prompts.keep);
  await waitStatus(limited.id, 'failed');
  await page.goto(`${m.url}/bots/${bot.id}/threads/${limited.id}`);
  await expect(page.getByRole('log', { name: 'Messages' }).getByRole('article').first()).toBeVisible();
  await away(page);

  const limitedRow = row(page, 'Limited', threadTitle(prompts.keep));
  await expect(limitedRow).toContainText(/Out until \d/);
  await expect(limitedRow).not.toContainText('Failed');
  await expect(limitedRow).not.toContainText('Replied');
});

// The boss hands its work to Tester, which answers at once; the boss's run for Reef's notice calls
// no tell_user, so what it writes last is a status line, not a message.
test('a background run adds no message: its final message is one muted status line above the composer', async ({
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
  const approval = (await api(m, 'GET', `/api/threads/${helper.id}`)).approval.id;
  await api(m, 'POST', `/api/approvals/${approval}`, { approve: true });

  const log = page.getByRole('log', { name: 'Messages' });
  const status = page.getByText('Thanks, Tester.', { exact: true });
  await expect(status).toHaveCount(1, { timeout: 30_000 });
  await expect(log.getByText('Thanks, Tester.')).toHaveCount(0);
  await expect(log.getByRole('article', { name: 'Poteto' })).toHaveCount(0);
  await expect(log.getByRole('article', { name: 'Tester' })).toHaveCount(0);

  const composer = await page.getByRole('textbox', { name: /^Message/ }).boundingBox();
  const line = await status.boundingBox();
  const chat = await log.boundingBox();
  expect(line!.y, 'above the composer').toBeLessThan(composer!.y);
  expect(line!.y, 'below the chat').toBeGreaterThanOrEqual(chat!.y + chat!.height - 2);
  expect(await status.evaluate((el) => getComputedStyle(el).color)).toBe('rgb(155, 155, 155)');

  // Nothing waits on the person as a card, a Blocked line, a ready PR card or a ledger.
  await expect(page.getByText('Waiting on you')).toHaveCount(0);
  await expect(page.getByText(/^Blocked:/)).toHaveCount(0);
  await expect(log.getByRole('list', { name: 'Updates' })).toHaveCount(0);
  await expect(page.getByRole('region', { name: /Waiting on you|Ready/ })).toHaveCount(0);
});

// tell_user is the way a background run reaches the person: one Bot message for each call, and the
// final message stays a status line, the newest one only.
test('tell_user adds one Bot message per call and the final message replaces the status line', async ({
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
  await expect(page.getByText('Told the user about the blocker.', { exact: true })).toHaveCount(1);
  await expect(log.getByText('Told the user about the blocker.')).toHaveCount(0);
  await expect(page.getByText('Thanks, Tester.')).toHaveCount(0);
});
