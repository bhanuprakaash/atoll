// The island's Earlier list follows reads: a thread opened after its run finished is not listed
// (the sidebar's New rule), a thread shows one row for its latest run, and each row can be
// dismissed on its own. Design: docs/design.md, "The island remembers across a close of the app".
import { expect, type Locator, type Page, test } from '@playwright/test';
import {
  api,
  type Machine,
  newBotAPI,
  openThreadAPI,
  prompts,
  sendMessageAPI,
  startAtoll,
  threadTitle,
  threadTree,
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

async function open(page: Page, botID: string) {
  await page.goto(`${m.url}/bots/${botID}`);
  const island = page.locator('header').getByRole('button', {
    name: /^Notifications/,
  });
  await expect(island).toBeVisible();
  const list = page.getByRole('dialog', { name: 'Notifications' });
  return {
    island,
    list,
    rows: list.getByRole('list', { name: 'Earlier' }).getByRole('listitem'),
  };
}

// readThread makes a thread whose first run is done and marks it read after that run.
async function readThread(botID: string, prompt: string) {
  const t = await openThreadAPI(m, botID, prompt);
  await waitStatus(t.id, 'done');
  await api(m, 'POST', `/api/threads/${t.id}/opened`);
  return t;
}

const finished = (rows: Locator, prompt: string) =>
  rows.filter({ hasText: `finished ${threadTitle(prompt)}` });

const dismiss = (row: Locator) => row.getByRole('button', { name: /^Dismiss/i });

// restart ends the server at once and serves the same data again.
async function restart() {
  m.kill();
  await m.restart();
}

test('a thread opened after its run finished is not under Earlier on connect, one not opened is', async ({
  page,
}) => {
  const pilot = await newBotAPI(m, 'Pilot');
  const home = await newBotAPI(m, 'Home');
  await readThread(pilot.id, prompts.pong);
  const unread = await openThreadAPI(m, pilot.id, prompts.keep);
  await waitStatus(unread.id, 'done');

  const { island, list, rows } = await open(page, home.id);
  await page.reload();
  await island.click();
  await expect(finished(rows, prompts.keep)).toHaveCount(1);
  await expect(finished(rows, prompts.pong)).toHaveCount(0);
  await expect(rows).toHaveCount(1);
  await expect(list).not.toContainText('Nothing new');
});

test('opening a thread removes its run from an open Earlier list as it happens', async ({
  page,
  context,
}) => {
  const pilot = await newBotAPI(m, 'Pilot');
  const home = await newBotAPI(m, 'Home');
  const { island, rows } = await open(page, home.id);
  await island.click();
  const read = await openThreadAPI(m, pilot.id, prompts.pong);
  const unread = await openThreadAPI(m, pilot.id, prompts.keep);
  await expect(finished(rows, prompts.pong)).toHaveCount(1);
  await expect(finished(rows, prompts.keep)).toHaveCount(1);

  const other = await context.newPage();
  const opened = other.waitForResponse((r) => r.url().endsWith(`/api/threads/${read.id}/opened`));
  await other.goto(`${m.url}/bots/${pilot.id}/threads/${read.id}`);
  await opened;
  await expect(finished(rows, prompts.pong)).toHaveCount(0);
  await expect(finished(rows, prompts.keep)).toHaveCount(1);
  expect(unread.id).not.toBe(read.id);
});

test('a run that finishes in a thread after it was opened is under Earlier', async ({ page }) => {
  const pilot = await newBotAPI(m, 'Pilot');
  const home = await newBotAPI(m, 'Home');
  const t = await api(m, 'POST', `/api/bots/${pilot.id}/threads`, {});
  await sendMessageAPI(m, t.id, prompts.keep);
  await api(m, 'POST', `/api/threads/${t.id}/opened`);
  const { island, rows } = await open(page, home.id);
  await island.click();

  await sendMessageAPI(m, t.id, prompts.keep);
  await expect(finished(rows, prompts.keep).first()).toBeVisible();
  await page.reload();
  await island.click();
  await expect(finished(rows, prompts.keep).first()).toBeVisible();
});

test('a thread with several runs has one row, not one per run', async ({ page }) => {
  const pilot = await newBotAPI(m, 'Pilot');
  const home = await newBotAPI(m, 'Home');
  const t = await api(m, 'POST', `/api/bots/${pilot.id}/threads`, {});
  await sendMessageAPI(m, t.id, prompts.keep);
  await sendMessageAPI(m, t.id, prompts.keep);
  const other = await openThreadAPI(m, pilot.id, prompts.pong);
  await waitStatus(other.id, 'done');

  const { island, rows } = await open(page, home.id);
  await island.click();
  await expect(finished(rows, prompts.pong)).toHaveCount(1);
  await expect(finished(rows, prompts.keep)).toHaveCount(1);
  await expect(rows).toHaveCount(2);
});

test('every Earlier row has a dismiss button that shows on hover and on keyboard focus', async ({
  page,
}) => {
  const pilot = await newBotAPI(m, 'Pilot');
  const home = await newBotAPI(m, 'Home');
  for (const prompt of [prompts.pong, prompts.keep]) {
    await waitStatus((await openThreadAPI(m, pilot.id, prompt)).id, 'done');
  }
  const { island, rows } = await open(page, home.id);
  await island.click();
  await expect(rows).toHaveCount(2);

  const pong = finished(rows, prompts.pong);
  await expect(dismiss(pong)).toHaveCount(1);
  await expect(dismiss(finished(rows, prompts.keep))).toHaveCount(1);
  await pong.hover();
  await expect(dismiss(pong)).toBeVisible();
  await expect(dismiss(pong)).toHaveCSS('opacity', '1');

  await page.mouse.move(0, 0);
  const keep = finished(rows, prompts.keep);
  await dismiss(keep).focus();
  await expect(dismiss(keep)).toBeFocused();
  await expect(dismiss(keep)).toBeVisible();
  await expect(dismiss(keep)).toHaveCSS('opacity', '1');
});

test('dismissing a run row removes it at once, marks its thread read, and it stays gone after a reload and a restart', async ({
  page,
}) => {
  const pilot = await newBotAPI(m, 'Pilot');
  const home = await newBotAPI(m, 'Home');
  for (const prompt of [prompts.pong, prompts.keep]) {
    await waitStatus((await openThreadAPI(m, pilot.id, prompt)).id, 'done');
  }
  const { island, rows } = await open(page, home.id);
  const marks = threadTree(page, 'Pilot').locator('[data-marker="new"]');
  await expect(marks).toHaveCount(2);
  await island.click();
  await expect(rows).toHaveCount(2);

  await dismiss(finished(rows, prompts.pong)).click();
  await expect(finished(rows, prompts.pong)).toHaveCount(0);
  await expect(finished(rows, prompts.keep)).toHaveCount(1);
  await expect(marks).toHaveCount(1);

  await page.reload();
  await island.click();
  await expect(finished(rows, prompts.keep)).toHaveCount(1);
  await expect(finished(rows, prompts.pong)).toHaveCount(0);
  await expect(marks).toHaveCount(1);

  await restart();
  await page.reload();
  await island.click();
  await expect(finished(rows, prompts.keep)).toHaveCount(1);
  await expect(finished(rows, prompts.pong)).toHaveCount(0);
  await expect(marks).toHaveCount(1);
});

const mrURL = 'https://gitlab.example/fixtures/seeded/-/merge_requests/7';

// platformRows makes an MR opened by Pilot and a Bot made by Chief, and returns Earlier's rows.
async function platformRows(page: Page) {
  const pilot = await newBotAPI(m, 'Pilot');
  const home = await newBotAPI(m, 'Home');
  const mr = await openThreadAPI(
    m,
    pilot.id,
    `Use MCP tools. [{"server":"atoll","tool":"track_mr","args":{"url":"${mrURL}","title":"Tidy the logs"}}]`,
  );
  await waitStatus(mr.id, 'done');
  const chief = await openThreadAPI(
    m,
    'chief',
    'Create reef-greeting and a Lagoon Bot using claude.',
  );
  await waitStatus(chief.id, 'done');
  return open(page, home.id);
}

test('dismissing an MR-opened row removes it at once, and it stays gone after a reload and a restart', async ({
  page,
}) => {
  const { island, rows } = await platformRows(page);
  await island.click();
  const opened = rows.filter({ hasText: 'Pilot opened !7' });
  const change = rows.filter({ hasText: /Chief created Bot Lagoon/ });
  await expect(opened).toHaveCount(1);
  await expect(change).toHaveCount(1);

  await dismiss(opened).click();
  await expect(opened).toHaveCount(0);
  await expect(change).toHaveCount(1);

  await page.reload();
  await island.click();
  await expect(change).toHaveCount(1);
  await expect(opened).toHaveCount(0);

  await restart();
  await page.reload();
  await island.click();
  await expect(change).toHaveCount(1);
  await expect(opened).toHaveCount(0);
});

test('dismissing a platform-change row removes it at once, and it stays gone after a reload and a restart', async ({
  page,
}) => {
  const { island, rows } = await platformRows(page);
  await island.click();
  const opened = rows.filter({ hasText: 'Pilot opened !7' });
  const change = rows.filter({ hasText: /Chief created Bot Lagoon/ });
  await expect(opened).toHaveCount(1);
  await expect(change).toHaveCount(1);

  await dismiss(change).click();
  await expect(change).toHaveCount(0);
  await expect(opened).toHaveCount(1);

  await page.reload();
  await island.click();
  await expect(opened).toHaveCount(1);
  await expect(change).toHaveCount(0);

  await restart();
  await page.reload();
  await island.click();
  await expect(opened).toHaveCount(1);
  await expect(change).toHaveCount(0);
});

test('opening a thread clears both its sidebar New and its Earlier row', async ({ page }) => {
  const pilot = await newBotAPI(m, 'Pilot');
  const home = await newBotAPI(m, 'Home');
  const [a, b] = [
    await openThreadAPI(m, pilot.id, prompts.pong),
    await openThreadAPI(m, pilot.id, prompts.keep),
  ];
  await waitStatus(a.id, 'done');
  await waitStatus(b.id, 'done');
  const { island, rows } = await open(page, home.id);
  const tree = threadTree(page, 'Pilot');
  const marks = tree.locator('[data-marker="new"]');
  await expect(marks).toHaveCount(2);

  await tree.getByRole('link', { name: threadTitle(prompts.pong) }).click();
  await expect(page).toHaveURL(`${m.url}/bots/${pilot.id}/threads/${a.id}`);
  await expect(marks).toHaveCount(1);
  await island.click();
  await expect(finished(rows, prompts.keep)).toHaveCount(1);
  await expect(finished(rows, prompts.pong)).toHaveCount(0);
});
