// The header island as a notification surface for work in other threads: approvals drop it open,
// finished runs widen it for a moment, a click opens the notification list.
// Contract: e2e/sidebar-tree-contract.md.
import { expect, type Page, test } from '@playwright/test';
import {
  api,
  type Machine,
  newBotAPI,
  openThreadAPI,
  prompts,
  startAtoll,
  threadTitle,
} from './atoll';

let m: Machine;
test.beforeEach(async () => {
  m = await startAtoll();
});
test.afterEach(() => m?.stop());

async function waitStatus(threadID: string, status: string) {
  await expect
    .poll(async () => (await api(m, 'GET', `/api/threads/${threadID}`)).run_status, {
      timeout: 15_000,
      intervals: [100],
    })
    .toBe(status);
}

// viewA opens Pilot's done thread A and returns the Bot and A.
async function viewA(page: Page) {
  const pilot = await newBotAPI(m, 'Pilot');
  const a = await openThreadAPI(m, pilot.id, prompts.keep);
  await waitStatus(a.id, 'done');
  const url = `${m.url}/bots/${pilot.id}/threads/${a.id}`;
  await page.goto(url);
  const header = page.locator('header');
  await expect(header.getByRole('heading', { name: threadTitle(prompts.keep) })).toBeVisible();
  return { pilot, a, url, header };
}

test('the island has no thread list; clicking it opens the notification list', async ({ page }) => {
  const { header } = await viewA(page);
  const scout = await newBotAPI(m, 'Scout');
  const finished = await openThreadAPI(m, scout.id, prompts.pong);
  await waitStatus(finished.id, 'done');
  const waiting = await openThreadAPI(m, scout.id, prompts.write);
  await waitStatus(waiting.id, 'waiting');
  await expect(header.getByRole('button', { name: 'Allow', exact: true })).toBeVisible();
  await page.keyboard.press('Escape');
  await expect(header.getByRole('button', { name: 'Allow', exact: true })).toHaveCount(0);

  await expect(page.getByRole('button', { name: /^Threads/ })).toHaveCount(0);
  const island = header.getByRole('button', { name: /^Notifications/ });
  await island.click();
  const list = page.getByRole('dialog', { name: 'Notifications' });
  await expect(list).toBeVisible();
  await expect(page.getByRole('dialog', { name: /^Threads with / })).toHaveCount(0);
  await expect(page.getByRole('searchbox', { name: 'Search threads' })).toHaveCount(0);
  await expect(header.getByRole('button', { name: 'New thread' })).toHaveCount(0);
  await expect(header.getByRole('button', { name: 'Close thread' })).toHaveCount(0);

  const needs = list.getByRole('list', { name: 'Needs you' }).getByRole('listitem');
  await expect(needs).toHaveCount(1);
  await expect(needs).toContainText(threadTitle(prompts.write));
  await expect(needs).toContainText('hello.txt');
  await expect(needs.getByRole('button', { name: 'Deny', exact: true })).toBeVisible();
  await expect(list.getByRole('list', { name: 'Earlier' })).toContainText(
    threadTitle(prompts.pong),
  );

  await needs.getByRole('button', { name: 'Allow', exact: true }).click();
  await waitStatus(waiting.id, 'done');
  await expect(list.getByRole('list', { name: 'Needs you' }).getByRole('listitem')).toHaveCount(0);
  await expect(island).toHaveAccessibleName('Notifications');
});

test('an approval in another thread of the same Bot drops the island open; Allow continues it without leaving A', async ({
  page,
}) => {
  const { pilot, url, header } = await viewA(page);
  const b = await openThreadAPI(m, pilot.id, prompts.write);
  await waitStatus(b.id, 'waiting');

  await expect(header).toContainText(threadTitle(prompts.write));
  await expect(header).toContainText('hello.txt');
  await expect(header.getByRole('link', { name: 'Open thread' })).toHaveAttribute(
    'href',
    `/bots/${pilot.id}/threads/${b.id}`,
  );
  await expect(header.getByRole('button', { name: 'Deny', exact: true })).toBeVisible();
  await header.getByRole('button', { name: 'Allow', exact: true }).click();

  await waitStatus(b.id, 'done');
  expect(page.url()).toBe(url);
  await expect(header.getByRole('button', { name: 'Allow', exact: true })).toHaveCount(0);
});

test("an approval in another Bot's thread drops the island open; Esc leaves a red count", async ({
  page,
}) => {
  const { url, header } = await viewA(page);
  const scout = await newBotAPI(m, 'Scout');
  const b = await openThreadAPI(m, scout.id, prompts.write);
  await waitStatus(b.id, 'waiting');

  await expect(header).toContainText('Scout');
  await expect(header).toContainText(threadTitle(prompts.write));
  await expect(header).toContainText('hello.txt');
  await expect(header.getByRole('button', { name: 'Deny', exact: true })).toBeVisible();
  await expect(header.getByRole('button', { name: 'Allow', exact: true })).toBeVisible();

  await page.keyboard.press('Escape');
  await expect(header.getByRole('button', { name: 'Allow', exact: true })).toHaveCount(0);
  const island = header.getByRole('button', {
    name: 'Notifications, 1 waiting',
    exact: true,
  });
  await expect(island).toBeVisible();
  await expect(header).toContainText('1');
  expect(page.url()).toBe(url);
  expect((await api(m, 'GET', `/api/threads/${b.id}`)).run_status).toBe('waiting');
});

test('a run ending in another thread widens the island for about 4 s, then it returns to rest', async ({
  page,
}) => {
  const { header } = await viewA(page);
  const reviewer = await newBotAPI(m, 'Reviewer');
  const b = await openThreadAPI(m, reviewer.id, prompts.pong);
  await waitStatus(b.id, 'done');

  const finished = header.getByText(
    new RegExp(`Reviewer finished ${threadTitle(prompts.pong)}`, 'i'),
  );
  await expect(finished).toBeVisible({ timeout: 3_000 });
  await expect(header).toContainText('View');
  await expect(header.getByRole('button', { name: /^(Allow|Deny)$/ })).toHaveCount(0);
  // The line is about time: the notice stays a few seconds, then leaves on its own.
  await page.waitForTimeout(2_000);
  await expect(finished).toBeVisible();
  await expect(finished).toHaveCount(0, { timeout: 6_000 });
  await expect(header.getByRole('heading', { level: 1, name: 'Pilot' })).toBeVisible();
  await expect(header.getByRole('heading', { name: threadTitle(prompts.keep) })).toBeVisible();
});

// islandWidth is the width of the open island once its opening motion has settled.
async function islandWidth(page: Page) {
  const island = page.locator('header [data-island]');
  await page.waitForTimeout(900);
  return (await island.boundingBox())!.width;
}

// approvalFrom gives a Bot with this name a thread that waits on an approval.
async function approvalFrom(name: string) {
  const bot = await newBotAPI(m, name);
  const t = await openThreadAPI(m, bot.id, prompts.write);
  await waitStatus(t.id, 'waiting');
  return t;
}

test('the open island keeps one width whatever its rows say, and long lines end truncated', async ({
  page,
}) => {
  const { header } = await viewA(page);
  const short = await approvalFrom('Scout');
  await expect(header.getByRole('button', { name: 'Allow', exact: true })).toBeVisible();
  const narrow = await islandWidth(page);
  await header.getByRole('button', { name: 'Allow', exact: true }).click();
  await waitStatus(short.id, 'done');
  await expect(header.getByRole('button', { name: 'Allow', exact: true })).toHaveCount(0);

  const longName = Array.from({ length: 60 }, () => 'wordy')
    .join(' ')
    .slice(0, 300);
  expect(longName).toHaveLength(300);
  await approvalFrom(longName);
  await expect(header.getByRole('button', { name: 'Allow', exact: true })).toBeVisible();
  const wide = await islandWidth(page);
  expect(wide).toBe(narrow);

  const line = header.getByText(longName.slice(0, 40)).first();
  const cut = await line.evaluate((el) => {
    const s = getComputedStyle(el);
    return {
      overflow: s.textOverflow,
      clipped: el.scrollWidth > el.clientWidth,
    };
  });
  expect(cut).toEqual({ overflow: 'ellipsis', clipped: true });
});

test('at 420 px wide the page does not scroll sideways with the island open', async ({ page }) => {
  await page.setViewportSize({ width: 420, height: 800 });
  const pilot = await newBotAPI(m, 'Pilot');
  await page.goto(`${m.url}/bots/${pilot.id}`);
  const longName = Array.from({ length: 60 }, () => 'wordy')
    .join(' ')
    .slice(0, 300);
  await approvalFrom(longName);
  const header = page.locator('header');
  await expect(header.getByRole('button', { name: 'Allow', exact: true })).toBeVisible();
  await page.waitForTimeout(900);
  const sideways = await page.evaluate(
    () => document.documentElement.scrollWidth - document.documentElement.clientWidth,
  );
  expect(sideways).toBeLessThanOrEqual(0);
});
