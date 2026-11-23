// A Bot's threads as tabs above its chat: one marker each, new threads from the Bot row's menu or
// ⌘N, closing or renaming from the tab.
import { execFileSync } from 'node:child_process';
import { mkdirSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';
import { expect, type Locator, test } from '@playwright/test';
import {
  api,
  type Machine,
  marks,
  newBotAPI,
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

const colours = async (el: Locator) => (await marks(el)).map((d) => d.colour);

// base makes a done thread of the Bot titled title, to open so its tabs show without reading the
// thread under test.
async function base(botID: string, title = 'Base notes') {
  const t = await openThreadAPI(m, botID, prompts.pong);
  await waitStatus(m, t.id, 'done');
  await api(m, 'PATCH', `/api/threads/${t.id}`, { title });
  return t;
}

test('a thread waiting on an approval gets a tab with the light dot; answering clears it', async ({
  page,
}) => {
  const bot = await newBotAPI(m, 'Asker');
  const open = await base(bot.id);
  await openThread(page, m, bot.id, open.id);
  const waiting = await openThreadAPI(m, bot.id, prompts.write);
  await waitStatus(m, waiting.id, 'waiting');

  const asking = threadTab(page, threadTitle(prompts.write));
  await expect.poll(() => colours(asking)).toEqual(['light']);
  await expect(asking).not.toContainText('Needs you');

  await asking.click();
  await expect(page).toHaveURL(`${m.url}/bots/${bot.id}/threads/${waiting.id}`);
  await expect(asking).toHaveAttribute('aria-selected', 'true');
  await page
    .getByRole('log', { name: 'Messages' })
    .getByRole('button', { name: 'Allow', exact: true })
    .click();
  await waitStatus(m, waiting.id, 'done');
  await expect.poll(() => colours(threadTabs(page))).toEqual([]);
  await expect(threadTabs(page)).not.toContainText('Needs you');
});

test('a pinned thread that replied while unseen shows the blue dot and a bold title on its tab; opening it clears Replied, also after a restart', async ({
  page,
}) => {
  const bot = await newBotAPI(m, 'Newsy');
  const open = await base(bot.id);
  const seen = await openThreadAPI(m, bot.id, prompts.pong);
  const unseen = await openThreadAPI(m, bot.id, prompts.keep);
  await waitStatus(m, seen.id, 'done');
  await waitStatus(m, unseen.id, 'done');
  for (const t of [seen, unseen]) await api(m, 'POST', `/api/threads/${t.id}/pin`);

  await openThread(page, m, bot.id, open.id);
  for (const prompt of [prompts.pong, prompts.keep]) {
    const fresh = threadTab(page, threadTitle(prompt));
    await expect.poll(() => colours(fresh)).toEqual(['blue']);
    await expect(fresh.getByRole('img', { name: 'Pinned', exact: true })).toBeVisible();
    const weight = await fresh
      .getByText(threadTitle(prompt), { exact: true })
      .evaluate((el) => Number(getComputedStyle(el).fontWeight));
    expect(weight).toBeGreaterThanOrEqual(600);
  }

  await threadTab(page, threadTitle(prompts.pong)).click();
  await expect(page).toHaveURL(`${m.url}/bots/${bot.id}/threads/${seen.id}`);
  await expect.poll(() => colours(threadTab(page, threadTitle(prompts.pong)))).toEqual([]);

  m.kill();
  await m.restart();
  await openThread(page, m, bot.id, open.id);
  await expect.poll(() => colours(threadTab(page, threadTitle(prompts.keep)))).toEqual(['blue']);
  await expect.poll(() => colours(threadTab(page, threadTitle(prompts.pong)))).toEqual([]);
});

test('a running thread shows wave dots on its tab; no spinner ring on any creature or thread', async ({
  page,
}) => {
  const bot = await newBotAPI(m, 'Drummer', 'claude', { look: 'whale' });
  const open = await base(bot.id);
  await openThread(page, m, bot.id, open.id);
  const busy = await openThreadAPI(m, bot.id, prompts.hang);
  await waitStatus(m, busy.id, 'running');

  const working = threadTab(page, threadTitle(prompts.hang));
  await expect(working.getByRole('img', { name: 'Working', exact: true })).toBeVisible();
  await expect(working).not.toContainText('Needs you');
  await expect(working).not.toContainText('Replied');
  await sidebarRow(page, 'Drummer').click();
  await expect(page).toHaveURL(`${m.url}/bots/${bot.id}/threads/${busy.id}`);
  await expect(working.getByRole('img', { name: 'Working', exact: true })).toBeVisible();

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

test('two threads needing approval and three running each get a tab with their marker', async ({
  page,
}) => {
  const bot = await newBotAPI(m, 'Pair');
  const open = await base(bot.id);
  const busy = [];
  for (let i = 0; i < 3; i++) busy.push(await openThreadAPI(m, bot.id, prompts.hang));
  const asking = [];
  for (let i = 0; i < 2; i++) asking.push(await openThreadAPI(m, bot.id, prompts.write));
  try {
    for (const t of busy) await waitStatus(m, t.id, 'running');
    for (const t of asking) await waitStatus(m, t.id, 'waiting');
    for (const [i, t] of busy.entries())
      await api(m, 'PATCH', `/api/threads/${t.id}`, { title: `Run ${i + 1}` });
    for (const [i, t] of asking.entries())
      await api(m, 'PATCH', `/api/threads/${t.id}`, { title: `Ask ${i + 1}` });
    await openThread(page, m, bot.id, open.id);

    await expect(threadTabs(page).getByRole('tab')).toHaveCount(6);
    for (const title of ['Ask 1', 'Ask 2']) {
      await expect
        .poll(() => colours(threadTab(page, title)), { message: title })
        .toEqual(['light']);
      await expect(threadTab(page, title)).not.toContainText('Needs you');
    }
    for (const title of ['Run 1', 'Run 2', 'Run 3'])
      await expect(
        threadTab(page, title).getByRole('img', { name: 'Working', exact: true }),
      ).toBeVisible();
  } finally {
    for (const t of busy) await api(m, 'POST', `/api/runs/${t.run.id}/stop`);
  }
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
  await openThread(page, m, bot.id, thread.id);
  await page.keyboard.press('Meta+n');
  await expect(page.getByRole('heading', { name: 'What should Keyer work on?' })).toBeVisible();
  await expect(page).toHaveURL(new RegExp(`/bots/${bot.id}/`));
});

test('Close thread on a hovered tab closes the thread and saves its unfinished work', async ({
  page,
}) => {
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
  await openThread(page, m, bot.id, thread.id);

  const closing = threadTab(page, threadTitle(unfinished));
  await closing.hover();
  await page.getByRole('button', { name: 'Close thread' }).click();
  await expect(closing).toHaveCount(0);
  await expect
    .poll(async () => (await api(m, 'GET', `/api/threads/${thread.id}`)).closed)
    .toBe(true);
  const ref = `refs/atoll/unfinished/${thread.id}`;
  expect(git('show', `${ref}:answer.txt`)).toBe('42\n');
  expect(git('show', `${ref}:unfinished.txt`)).toBe('keep this untracked work\n');
});

test('tab right-click › Rename renames the thread', async ({ page }) => {
  const bot = await newBotAPI(m, 'Namer');
  const thread = await openThreadAPI(m, bot.id, prompts.pong);
  await waitStatus(m, thread.id, 'done');
  await openThread(page, m, bot.id, thread.id);

  await threadTab(page, threadTitle(prompts.pong)).click({ button: 'right' });
  const menu = page.getByRole('menu');
  await expect(menu.getByRole('menuitem', { name: 'Close', exact: true })).toBeVisible();
  await menu.getByRole('menuitem', { name: 'Rename' }).click();
  const title = page.getByRole('textbox', { name: 'Thread title' });
  await title.fill('Lantern notes');
  await title.press('Enter');
  await expect(threadTab(page, 'Lantern notes')).toHaveCount(1);
  await page.reload();
  await expect(threadTab(page, 'Lantern notes')).toHaveCount(1);
  await expect(threadTab(page, threadTitle(prompts.pong))).toHaveCount(0);
  expect((await api(m, 'GET', `/api/threads/${thread.id}`)).title).toBe('Lantern notes');
});
