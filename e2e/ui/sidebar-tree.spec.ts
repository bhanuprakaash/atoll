// Threads under their Bot in the sidebar: at most 3 rows with one marker each, an overflow line,
// new threads from the Bot row and closing or renaming from the thread row.
// Contract: e2e/sidebar-tree-contract.md.
import { execFileSync } from 'node:child_process';
import { mkdirSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';
import { expect, type Page, test } from '@playwright/test';
import {
  api,
  type Machine,
  newBotAPI,
  openThreadAPI,
  prompts,
  sidebarRow,
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
    })
    .toBe(status);
}

// visit opens a thread in the page, so it counts as seen.
async function visit(page: Page, botID: string, threadID: string) {
  await page.goto(`${m.url}/bots/${botID}/threads/${threadID}`);
  await expect(
    page.getByRole('log', { name: 'Messages' }).getByRole('article').first(),
  ).toBeVisible();
}

// away shows a Bot of its own, so no thread of the Bots under test is on screen.
async function away(page: Page) {
  const bot = await newBotAPI(m, 'Elsewhere');
  await page.goto(`${m.url}/bots/${bot.id}`);
  await expect(page.getByRole('heading', { name: 'What should Elsewhere work on?' })).toBeVisible();
  return bot;
}

const row = (page: Page, bot: string, title: string) =>
  threadTree(page, bot).getByRole('listitem').filter({ hasText: title });

test('a thread waiting on an approval shows under its Bot as Needs you; answering removes it', async ({
  page,
}) => {
  const bot = await newBotAPI(m, 'Asker');
  await away(page);
  const waiting = await openThreadAPI(m, bot.id, prompts.write);
  await waitStatus(waiting.id, 'waiting');

  const tree = threadTree(page, 'Asker');
  const asking = row(page, 'Asker', threadTitle(prompts.write));
  await expect(asking).toHaveAttribute('data-marker', 'needs-you');
  await expect(asking).toContainText('Needs you');

  await asking.getByRole('link').click();
  await expect(page).toHaveURL(`${m.url}/bots/${bot.id}/threads/${waiting.id}`);
  await page
    .getByRole('log', { name: 'Messages' })
    .getByRole('button', { name: 'Allow', exact: true })
    .click();
  await waitStatus(waiting.id, 'done');
  await expect(tree.locator('[data-marker="needs-you"]')).toHaveCount(0);
  await expect(tree).not.toContainText('Needs you');
});

test('a run ending in an unseen thread shows it as Replied; opening it clears Replied, also after a restart', async ({
  page,
}) => {
  const bot = await newBotAPI(m, 'Newsy');
  const elsewhere = await away(page);
  const seen = await openThreadAPI(m, bot.id, prompts.pong);
  const unseen = await openThreadAPI(m, bot.id, prompts.keep);
  await waitStatus(seen.id, 'done');
  await waitStatus(unseen.id, 'done');

  const tree = threadTree(page, 'Newsy');
  for (const prompt of [prompts.pong, prompts.keep]) {
    const fresh = row(page, 'Newsy', threadTitle(prompt));
    await expect(fresh).toHaveAttribute('data-marker', 'new');
    await expect(fresh).toContainText('Replied');
    const weight = await fresh
      .getByText(threadTitle(prompt), { exact: true })
      .evaluate((el) => Number(getComputedStyle(el).fontWeight));
    expect(weight).toBeGreaterThanOrEqual(600);
  }

  await row(page, 'Newsy', threadTitle(prompts.pong)).getByRole('link').click();
  await expect(page).toHaveURL(`${m.url}/bots/${bot.id}/threads/${seen.id}`);
  await expect(row(page, 'Newsy', threadTitle(prompts.pong))).not.toHaveAttribute(
    'data-marker',
    'new',
  );

  m.kill();
  await m.restart();
  await page.goto(`${m.url}/bots/${elsewhere.id}`);
  await expect(page.getByRole('heading', { name: 'What should Elsewhere work on?' })).toBeVisible();
  const fresh = tree.locator('[data-marker="new"]');
  await expect(fresh).toHaveCount(1);
  await expect(fresh).toContainText(threadTitle(prompts.keep));
});

test('a running thread shows wave dots; no spinner ring on any creature or thread', async ({
  page,
}) => {
  const bot = await newBotAPI(m, 'Drummer', 'claude', { look: 'shark' });
  await away(page);
  const busy = await openThreadAPI(m, bot.id, prompts.hang);
  await waitStatus(busy.id, 'running');

  const working = row(page, 'Drummer', threadTitle(prompts.hang));
  await expect(working).toHaveAttribute('data-marker', 'running');
  await expect(working).not.toContainText('Needs you');
  await expect(working).not.toContainText('Replied');
  await sidebarRow(page, 'Drummer').click();
  await expect(page).toHaveURL(`${m.url}/bots/${bot.id}/threads/${busy.id}`);
  await expect(working).toHaveAttribute('data-marker', 'running');

  const spinning = await page.evaluate(
    () =>
      document
        .getAnimations()
        .filter((a) =>
          (a.effect as KeyframeEffect | null)
            ?.getKeyframes()
            .some((k) =>
              /rotate\(\s*(360deg|1turn)|rotate:\s*(360deg|1turn)/.test(
                `${k.transform ?? ''} rotate: ${k.rotate ?? ''}`,
              ),
            ),
        ).length,
  );
  expect(spinning).toBe(0);
  expect(await page.locator('animateTransform').count()).toBe(0);
  await api(m, 'POST', `/api/runs/${busy.run.id}/stop`);
});

test('the tree rail runs under the 36px creature centre and rows start 14px right of it', async ({
  page,
}) => {
  const bot = await newBotAPI(m, 'Railed');
  await away(page);
  await waitStatus((await openThreadAPI(m, bot.id, prompts.pong)).id, 'done');

  const creature = sidebarRow(page, 'Railed').getByRole('img', { name: /, idle$/ });
  const tree = threadTree(page, 'Railed');
  await expect(tree.getByRole('listitem')).toHaveCount(1);
  const c = (await creature.boundingBox())!;
  expect(Math.abs(c.width - 36)).toBeLessThanOrEqual(1);
  const rail = await tree.evaluate((ul) => {
    const before = getComputedStyle(ul, '::before');
    return (
      ul.getBoundingClientRect().left +
      parseFloat(before.left) +
      parseFloat(before.borderLeftWidth) / 2
    );
  });
  expect(Math.abs(rail - (c.x + c.width / 2))).toBeLessThanOrEqual(1);
  const first = (await tree.getByRole('listitem').first().boundingBox())!;
  expect(Math.abs(first.x - rail - 14)).toBeLessThanOrEqual(1);
});

test('nine idle threads show at most 3 rows and N more, which opens the threads popup', async ({
  page,
}) => {
  const bot = await newBotAPI(m, 'Many');
  const threads = [];
  for (let i = 0; i < 9; i++) threads.push(await openThreadAPI(m, bot.id, prompts.keep));
  for (const t of threads) await waitStatus(t.id, 'done');
  for (const t of threads) await visit(page, bot.id, t.id);

  await page.goto(m.url);
  await sidebarRow(page, 'Many').click();
  const tree = threadTree(page, 'Many');
  const more = tree.getByRole('button', { name: /^\d+ more$/ });
  await expect(more).toBeVisible();
  const rows = await tree.getByRole('link').count();
  expect(rows).toBeGreaterThanOrEqual(1);
  expect(rows).toBeLessThanOrEqual(3);
  const hidden = Number((await more.textContent())!.match(/(\d+) more/)![1]);
  expect(rows + hidden).toBe(9);
  await expect(tree.getByRole('button', { name: /more running/ })).toHaveCount(0);

  await more.click();
  const popup = page.getByRole('dialog', { name: 'Threads with Many' });
  await expect(popup).toBeVisible();
  await expect(popup).toContainText('9 open threads · 0 closed');
  await page.keyboard.press('Escape');
  await expect(popup).toHaveCount(0);
});

test('six running threads show N more running; it unfolds only running threads, rows leave as runs end, switching Bot folds it', async ({
  page,
}) => {
  const bot = await newBotAPI(m, 'Runner');
  const idle = [];
  for (let i = 0; i < 2; i++) idle.push(await openThreadAPI(m, bot.id, prompts.pong));
  for (const t of idle) await waitStatus(t.id, 'done');
  for (const t of idle) await visit(page, bot.id, t.id);
  const busy = [];
  for (let i = 0; i < 6; i++) busy.push(await openThreadAPI(m, bot.id, prompts.hang));
  for (const t of busy) await waitStatus(t.id, 'running');
  await away(page);

  const tree = threadTree(page, 'Runner');
  const running = tree.locator('[data-marker="running"]');
  const more = tree.getByRole('button', { name: '3 more running', exact: true });
  await expect(more).toBeVisible();
  await expect(tree.getByRole('link')).toHaveCount(3);
  await expect(running).toHaveCount(3);

  await more.click();
  await expect(running).toHaveCount(6);
  await expect(tree.getByRole('link')).toHaveCount(6);
  await expect(tree).not.toContainText(threadTitle(prompts.pong));
  await expect(tree.getByRole('button', { name: 'Show less', exact: true })).toBeVisible();

  await api(m, 'POST', `/api/runs/${busy[0].run.id}/stop`);
  await waitStatus(busy[0].id, 'stopped');
  await expect(running).toHaveCount(5);

  await sidebarRow(page, 'Runner').click();
  await expect(tree.getByRole('button', { name: 'Show less', exact: true })).toHaveCount(0);
  await expect(tree.getByRole('button', { name: /^\d+ more running$/ })).toBeVisible();
  expect(await tree.getByRole('link').count()).toBeLessThanOrEqual(3);
  for (const t of busy.slice(1)) await api(m, 'POST', `/api/runs/${t.run.id}/stop`);
});

test('two threads needing approval both show beside three others', async ({ page }) => {
  const bot = await newBotAPI(m, 'Pair');
  const busy = [];
  for (let i = 0; i < 3; i++) busy.push(await openThreadAPI(m, bot.id, prompts.hang));
  const asking = [];
  for (let i = 0; i < 2; i++) asking.push(await openThreadAPI(m, bot.id, prompts.write));
  for (const t of busy) await waitStatus(t.id, 'running');
  for (const t of asking) await waitStatus(t.id, 'waiting');
  await away(page);

  const tree = threadTree(page, 'Pair');
  const needs = tree.locator('[data-marker="needs-you"]');
  await expect(needs).toHaveCount(2);
  for (let i = 0; i < 2; i++) {
    await expect(needs.nth(i)).toBeVisible();
    await expect(needs.nth(i)).toContainText('Needs you');
    await expect(tree.getByRole('listitem').nth(i)).toHaveAttribute('data-marker', 'needs-you');
  }
  for (const t of busy) await api(m, 'POST', `/api/runs/${t.run.id}/stop`);
});

test('clicking a Bot opens its latest thread', async ({ page }) => {
  const bot = await newBotAPI(m, 'Latest');
  const older = await openThreadAPI(m, bot.id, prompts.pong);
  await waitStatus(older.id, 'done');
  const newer = await openThreadAPI(m, bot.id, prompts.keep);
  await waitStatus(newer.id, 'done');
  await away(page);
  await sidebarRow(page, 'Latest').click();
  await expect(page).toHaveURL(`${m.url}/bots/${bot.id}/threads/${newer.id}`);
});

test('+ on Bot hover starts a new thread with that Bot', async ({ page }) => {
  const bot = await newBotAPI(m, 'Hoverer');
  await waitStatus((await openThreadAPI(m, bot.id, prompts.pong)).id, 'done');
  await away(page);
  await sidebarRow(page, 'Hoverer').hover();
  await page
    .getByRole('navigation', { name: 'Bots' })
    .getByRole('button', { name: 'New thread with Hoverer' })
    .click();
  await expect(page.getByRole('heading', { name: 'What should Hoverer work on?' })).toBeVisible();
  await expect(page).toHaveURL(new RegExp(`/bots/${bot.id}/`));
});

test('Bot right-click › New thread starts a new thread with that Bot', async ({ page }) => {
  const bot = await newBotAPI(m, 'Clicker');
  await waitStatus((await openThreadAPI(m, bot.id, prompts.pong)).id, 'done');
  await away(page);
  await sidebarRow(page, 'Clicker').click({ button: 'right' });
  const menu = page.getByRole('menu');
  await expect(menu.getByRole('menuitem')).toHaveText([
    /^New thread/,
    /^Pin to sidebar/,
    /^Move to group/,
    /^Bot settings/,
  ]);
  await menu.getByRole('menuitem', { name: 'New thread' }).click();
  await expect(page.getByRole('heading', { name: 'What should Clicker work on?' })).toBeVisible();
  await expect(page).toHaveURL(new RegExp(`/bots/${bot.id}/`));
});

test('⌘N starts a new thread with the open Bot', async ({ page }) => {
  const bot = await newBotAPI(m, 'Keyer');
  const thread = await openThreadAPI(m, bot.id, prompts.pong);
  await waitStatus(thread.id, 'done');
  await visit(page, bot.id, thread.id);
  await page.keyboard.press('Meta+n');
  await expect(page.getByRole('heading', { name: 'What should Keyer work on?' })).toBeVisible();
  await expect(page).toHaveURL(new RegExp(`/bots/${bot.id}/`));
});

test('thread hover × closes the thread and saves its unfinished work', async ({ page }) => {
  const path = join(m.dir, 'seeded');
  mkdirSync(path);
  writeFileSync(join(path, 'answer.txt'), '41\n');
  const git = (...args: string[]) =>
    execFileSync('git', args, {
      cwd: path,
      encoding: 'utf8',
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
  const bot = await newBotAPI(m, 'Closer', 'claude', { repo_access: [repo.id] });
  const unfinished = 'Edit the seeded answer but leave the work uncommitted.';
  const thread = await api(m, 'POST', `/api/bots/${bot.id}/threads`, {
    text: unfinished,
    repo_id: repo.id,
  });
  await waitStatus(thread.id, 'done');
  await away(page);

  const closing = row(page, 'Closer', threadTitle(unfinished));
  await closing.hover();
  await closing.getByRole('button', { name: 'Close thread' }).click();
  await expect(closing).toHaveCount(0);
  await expect
    .poll(async () => (await api(m, 'GET', `/api/threads/${thread.id}`)).closed)
    .toBe(true);
  const ref = `refs/atoll/unfinished/${thread.id}`;
  expect(git('show', `${ref}:answer.txt`)).toBe('42\n');
  expect(git('show', `${ref}:unfinished.txt`)).toBe('keep this untracked work\n');
});

test('thread right-click › Rename renames it', async ({ page }) => {
  const bot = await newBotAPI(m, 'Namer');
  const thread = await openThreadAPI(m, bot.id, prompts.pong);
  await waitStatus(thread.id, 'done');
  await away(page);

  await row(page, 'Namer', threadTitle(prompts.pong)).click({ button: 'right' });
  const menu = page.getByRole('menu');
  await expect(menu.getByRole('menuitem', { name: 'Close', exact: true })).toBeVisible();
  await menu.getByRole('menuitem', { name: 'Rename' }).click();
  const title = page.getByRole('textbox', { name: 'Thread title' });
  await title.fill('Lantern notes');
  await title.press('Enter');
  await expect(row(page, 'Namer', 'Lantern notes')).toHaveCount(1);
  await page.reload();
  await expect(row(page, 'Namer', 'Lantern notes')).toHaveCount(1);
  await expect(row(page, 'Namer', threadTitle(prompts.pong))).toHaveCount(0);
});
