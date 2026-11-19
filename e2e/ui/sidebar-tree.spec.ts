// A Bot's threads in its thread pane: one marker each, new threads from the face's menu or ⌘N,
// closing or renaming from the thread row.
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
  showPane,
  sidebarRow,
  startAtoll,
  threadPane,
  threadTitle,
  waitStatus,
} from './atoll';

let m: Machine;
test.beforeEach(async () => {
  m = await startAtoll();
});
test.afterEach(() => m?.stop());

// visit opens a thread in the page, so it counts as seen.
async function visit(page: Page, botID: string, threadID: string) {
  await page.goto(`${m.url}/bots/${botID}/threads/${threadID}`);
  await expect(
    page.getByRole('log', { name: 'Messages' }).getByRole('article').first(),
  ).toBeVisible();
}

const row = (page: Page, bot: string, title: string) =>
  threadPane(page, bot).getByRole('listitem').filter({ hasText: title });

test('a thread waiting on an approval shows in its Bot pane as Needs you; answering removes it', async ({
  page,
}) => {
  const bot = await newBotAPI(m, 'Asker');
  const tree = await showPane(page, m, bot.id, 'Asker');
  const waiting = await openThreadAPI(m, bot.id, prompts.write);
  await waitStatus(m, waiting.id, 'waiting');

  const asking = row(page, 'Asker', threadTitle(prompts.write));
  await expect(asking).toHaveAttribute('data-marker', 'needs-you');
  await expect(asking).not.toContainText('Needs you');

  await asking.getByRole('link').click();
  await expect(page).toHaveURL(`${m.url}/bots/${bot.id}/threads/${waiting.id}`);
  await page
    .getByRole('log', { name: 'Messages' })
    .getByRole('button', { name: 'Allow', exact: true })
    .click();
  await waitStatus(m, waiting.id, 'done');
  await expect(tree.locator('[data-marker="needs-you"]')).toHaveCount(0);
  await expect(tree).not.toContainText('Needs you');
});

test('a run ending in an unseen thread shows it as Replied; opening it clears Replied, also after a restart', async ({
  page,
}) => {
  const bot = await newBotAPI(m, 'Newsy');
  const seen = await openThreadAPI(m, bot.id, prompts.pong);
  const unseen = await openThreadAPI(m, bot.id, prompts.keep);
  await waitStatus(m, seen.id, 'done');
  await waitStatus(m, unseen.id, 'done');

  const tree = await showPane(page, m, bot.id, 'Newsy');
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
  await showPane(page, m, bot.id, 'Newsy');
  const fresh = tree.locator('[data-marker="new"]');
  await expect(fresh).toHaveCount(1);
  await expect(fresh).toContainText(threadTitle(prompts.keep));
});

test('a running thread shows wave dots; no spinner ring on any creature or thread', async ({
  page,
}) => {
  const bot = await newBotAPI(m, 'Drummer', 'claude', { look: 'whale' });
  await showPane(page, m, bot.id, 'Drummer');
  const busy = await openThreadAPI(m, bot.id, prompts.hang);
  await waitStatus(m, busy.id, 'running');

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

test('two threads needing approval both show first, beside three running', async ({ page }) => {
  const bot = await newBotAPI(m, 'Pair');
  const busy = [];
  for (let i = 0; i < 3; i++) busy.push(await openThreadAPI(m, bot.id, prompts.hang));
  const asking = [];
  for (let i = 0; i < 2; i++) asking.push(await openThreadAPI(m, bot.id, prompts.write));
  for (const t of busy) await waitStatus(m, t.id, 'running');
  for (const t of asking) await waitStatus(m, t.id, 'waiting');
  const tree = await showPane(page, m, bot.id, 'Pair');
  const needs = tree.locator('[data-marker="needs-you"]');
  await expect(needs).toHaveCount(2);
  for (let i = 0; i < 2; i++) {
    await expect(needs.nth(i)).toBeVisible();
    await expect(needs.nth(i)).not.toContainText('Needs you');
    await expect(tree.getByRole('listitem').nth(i)).toHaveAttribute('data-marker', 'needs-you');
  }
  for (const t of busy) await api(m, 'POST', `/api/runs/${t.run.id}/stop`);
});

test('clicking a Bot opens its latest thread', async ({ page }) => {
  const bot = await newBotAPI(m, 'Latest');
  const older = await openThreadAPI(m, bot.id, prompts.pong);
  await waitStatus(m, older.id, 'done');
  const newer = await openThreadAPI(m, bot.id, prompts.keep);
  await waitStatus(m, newer.id, 'done');
  await page.goto(`${m.url}/settings`);
  await sidebarRow(page, 'Latest').click();
  await expect(page).toHaveURL(`${m.url}/bots/${bot.id}/threads/${newer.id}`);
});

test('Bot right-click › New thread starts a new thread with that Bot', async ({ page }) => {
  const bot = await newBotAPI(m, 'Clicker');
  await waitStatus(m, (await openThreadAPI(m, bot.id, prompts.pong)).id, 'done');
  await page.goto(`${m.url}/settings`);
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
  await waitStatus(m, thread.id, 'done');
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
  await waitStatus(m, thread.id, 'done');
  await showPane(page, m, bot.id, 'Closer');

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
  await waitStatus(m, thread.id, 'done');
  await showPane(page, m, bot.id, 'Namer');

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
