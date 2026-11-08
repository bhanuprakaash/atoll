// The threads popup from a Bot's `N more` line: search over title, branch and MR, and pins that
// put a thread in the sidebar tree. Contract: e2e/sidebar-tree-contract.md.
import { execFileSync } from 'node:child_process';
import { mkdirSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';
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

async function waitStatus(threadID: string, status: string) {
  await expect
    .poll(async () => (await api(m, 'GET', `/api/threads/${threadID}`)).run_status, {
      timeout: 15_000,
    })
    .toBe(status);
}

// titled waits for a thread's run, names it and opens it in the page, so it counts as seen.
async function titled(page: Page, botID: string, threadID: string, title: string) {
  await waitStatus(threadID, 'done');
  await api(m, 'PATCH', `/api/threads/${threadID}`, { title });
  await page.goto(`${m.url}/bots/${botID}/threads/${threadID}`);
  await expect(
    page.getByRole('log', { name: 'Messages' }).getByRole('article').first(),
  ).toBeVisible();
}

async function openPopup(page: Page, bot: string) {
  await page.goto(m.url);
  await sidebarRow(page, bot).click();
  await threadTree(page, bot)
    .getByRole('button', { name: /^\d+ more$/ })
    .click();
  const popup = page.getByRole('dialog', { name: `Threads with ${bot}` });
  await expect(popup).toBeVisible();
  return popup;
}

const item = (popup: Locator, title: string) =>
  popup.getByRole('listitem').filter({ hasText: title });

test('threads popup search finds a thread by title, branch and MR', async ({ page }) => {
  const path = join(m.dir, 'seeded');
  mkdirSync(path);
  writeFileSync(join(path, 'answer.txt'), '41\n');
  const git = (...args: string[]) =>
    execFileSync('git', args, {
      cwd: path,
      env: {
        ...process.env,
        GIT_AUTHOR_NAME: 'Fixture',
        GIT_AUTHOR_EMAIL: 'fixture@example.test',
        GIT_COMMITTER_NAME: 'Fixture',
        GIT_COMMITTER_EMAIL: 'fixture@example.test',
        GIT_CONFIG_NOSYSTEM: '1',
      },
    });
  git('init', '-b', 'main');
  git('add', 'answer.txt');
  git('commit', '-m', 'seed bug');
  const repo = await api(m, 'POST', '/api/repos', { name: 'seeded', path });
  const bot = await newBotAPI(m, 'Lantern', 'claude', { repo_access: [repo.id] });

  const branch = await api(m, 'POST', `/api/bots/${bot.id}/threads`, {
    text: 'Edit the seeded answer but leave the work uncommitted.',
    repo_id: repo.id,
  });
  const mrURL = 'https://gitlab.example/fixtures/seeded/-/merge_requests/7';
  const mr = await openThreadAPI(
    m,
    bot.id,
    `Use MCP tools. [{"server":"atoll","tool":"track_mr","args":{"url":"${mrURL}","title":"Tidy the logs"}}]`,
  );
  const plain = [];
  for (let i = 0; i < 4; i++) plain.push(await openThreadAPI(m, bot.id, prompts.keep));
  await titled(page, bot.id, branch.id, 'Bravo notes');
  await titled(page, bot.id, mr.id, 'Hotel notes');
  for (const [i, name] of ['Alpha', 'Gamma', 'Delta', 'Echo'].entries())
    await titled(page, bot.id, plain[i].id, `${name} notes`);
  expect((await api(m, 'GET', `/api/threads/${branch.id}`)).branch).toBe('fix/seeded-bug');
  expect((await api(m, 'GET', `/api/threads/${mr.id}`)).mr_url).toBe(mrURL);

  const popup = await openPopup(page, 'Lantern');
  await expect(popup).toContainText('6 open threads · 0 closed');
  await expect(popup.getByRole('button', { name: 'New thread' })).toBeVisible();
  const search = popup.getByRole('searchbox', { name: 'Search threads' });
  const items = popup.getByRole('listitem');
  for (const [query, title] of [
    ['Gamma', 'Gamma notes'],
    ['seeded-bug', 'Bravo notes'],
    ['!7', 'Hotel notes'],
  ]) {
    await search.fill(query);
    await expect(items).toHaveCount(1);
    await expect(items).toContainText(title);
  }
  await page.keyboard.press('Escape');
  await expect(popup).toHaveCount(0);
});

test('Pin to sidebar puts a thread in the tree with a pin; a 4th pin asks which to replace; pins survive restart', async ({
  page,
}) => {
  const bot = await newBotAPI(m, 'Beacon');
  const names = ['Alpha', 'Delta', 'Echo', 'Foxtrot', 'Golf', 'Hotel', 'India'];
  const threads = [];
  for (let i = 0; i < names.length; i++) threads.push(await openThreadAPI(m, bot.id, prompts.keep));
  for (const [i, name] of names.entries())
    await titled(page, bot.id, threads[i].id, `${name} notes`);

  const popup = await openPopup(page, 'Beacon');
  const pin = async (title: string) => {
    await item(popup, title).hover();
    await item(popup, title).getByRole('button', { name: 'Pin to sidebar' }).click();
  };
  for (const name of ['Delta', 'Echo', 'Foxtrot']) await pin(`${name} notes`);
  await item(popup, 'Delta notes').hover();
  await expect(item(popup, 'Delta notes').getByRole('button', { name: 'Unpin' })).toBeVisible();
  await expect(popup.getByRole('list', { name: 'In the sidebar' })).toContainText('Delta notes');

  await pin('Golf notes');
  const replace = page.getByRole('dialog', { name: 'Replace which pin?' });
  await expect(replace.getByRole('button')).toContainText([
    'Delta notes',
    'Echo notes',
    'Foxtrot notes',
  ]);
  await replace.getByRole('button', { name: 'Delta notes' }).click();
  await expect(replace).toHaveCount(0);
  await page.keyboard.press('Escape');
  await expect(popup).toHaveCount(0);

  const pinned = threadTree(page, 'Beacon').locator('[data-marker="pinned"]');
  const expectPins = async () => {
    await expect(pinned).toHaveCount(3);
    for (const name of ['Echo', 'Foxtrot', 'Golf'])
      await expect(pinned.filter({ hasText: `${name} notes` })).toHaveCount(1);
    await expect(pinned.filter({ hasText: 'Delta notes' })).toHaveCount(0);
  };
  await expectPins();

  m.kill();
  await m.restart();
  await page.reload();
  await expectPins();
});
