// A Bot's threads as tabs above its chat: the open thread and the threads that need you, run or are
// pinned, in a fixed order, each with its marker; the rest of its open threads, a search and its
// closed threads under `N more`. A helper thread's tabs are in helper-thread-tabs.spec.ts.
import { execFileSync } from 'node:child_process';
import { mkdirSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';
import { expect, type Locator, type Page, test } from '@playwright/test';
import {
  api,
  type Machine,
  marks,
  moreButton,
  newBotAPI,
  openThread,
  openThreadAPI,
  otherThreads,
  prompts,
  startAtoll,
  threadTab,
  threadTabs,
  waitStatus,
} from './atoll';

let m: Machine;
test.beforeEach(async () => {
  m = await startAtoll();
});
test.afterEach(() => m?.stop());

const box = async (l: Locator) => (await l.boundingBox())!;
const away = (page: Page) => page.mouse.move(1270, 5);
const stop = (t: { run: { id: string } }) => api(m, 'POST', `/api/runs/${t.run.id}/stop`);
const colours = async (el: Locator) => (await marks(el)).map((d) => d.colour);
const rename = (id: string, title: string) => api(m, 'PATCH', `/api/threads/${id}`, { title });
const thread = async (id: string) => api(m, 'GET', `/api/threads/${id}`);

// tabTitles is the title of every tab, left to right, out of the titles the test gave.
const tabTitles = async (page: Page, known: string[]) =>
  (await threadTabs(page).getByRole('tab').allTextContents()).map(
    (text) => known.find((t) => text.includes(t)) ?? text,
  );

// waves counts the wave-dot animations playing inside el.
const waves = (el: Locator) =>
  el.evaluate(
    (node) =>
      node
        .getAnimations({ subtree: true })
        .filter((a) => (a as CSSAnimation).animationName === 'wave').length,
  );

// closeOn is the Close thread button shown on a tab: a visible one over the tab or right after it
// that nothing makes see-through; null when none is.
async function closeOn(page: Page, tab: Locator) {
  const t = await box(tab);
  const all = page.getByRole('button', { name: 'Close thread' });
  for (let i = 0; i < (await all.count()); i++) {
    const b = all.nth(i);
    if (!(await b.isVisible())) continue;
    const r = await box(b);
    const over =
      r.x < t.x + t.width + 24 &&
      r.x + r.width > t.x &&
      r.y < t.y + t.height &&
      r.y + r.height > t.y;
    const shown = await b.evaluate((el) => {
      for (let n: Element | null = el; n; n = n.parentElement)
        if (Number(getComputedStyle(n).opacity) === 0) return false;
      return true;
    });
    if (over && shown) return b;
  }
  return null;
}

test('the tab bar shows the open thread and the threads that need you, run or are pinned, each with its marker; other threads get no tab', async ({
  page,
}) => {
  await page.emulateMedia({ reducedMotion: 'no-preference' });
  const bot = await newBotAPI(m, 'Tabs');
  const asks = await openThreadAPI(m, bot.id, prompts.write);
  const runs = await openThreadAPI(m, bot.id, prompts.hang);
  const pinned = await openThreadAPI(m, bot.id, prompts.keep);
  const replied = await openThreadAPI(m, bot.id, prompts.keep);
  const failed = await openThreadAPI(m, bot.id, prompts.none);
  const plain = await openThreadAPI(m, bot.id, prompts.keep);
  const open = await openThreadAPI(m, bot.id, prompts.pong);
  try {
    await waitStatus(m, asks.id, 'waiting');
    await waitStatus(m, runs.id, 'running');
    await waitStatus(m, failed.id, 'failed');
    for (const t of [pinned, replied, plain, open]) await waitStatus(m, t.id, 'done');
    const titles: [{ id: string }, string][] = [
      [asks, 'Asks notes'],
      [runs, 'Runs notes'],
      [pinned, 'Pinned notes'],
      [replied, 'Replied notes'],
      [failed, 'Failed notes'],
      [plain, 'Plain notes'],
      [open, 'Open notes'],
    ];
    for (const [t, title] of titles) await rename(t.id, title);
    for (const t of [pinned, plain]) await api(m, 'POST', `/api/threads/${t.id}/opened`);
    for (const t of [pinned, replied, failed]) await api(m, 'POST', `/api/threads/${t.id}/pin`);

    await openThread(page, m, bot.id, open.id);
    await away(page);
    const known = titles.map(([, title]) => title);
    await expect
      .poll(async () => (await tabTitles(page, known)).sort())
      .toEqual(
        [
          'Open notes',
          'Asks notes',
          'Runs notes',
          'Pinned notes',
          'Replied notes',
          'Failed notes',
        ].sort(),
      );
    await expect(threadTab(page, 'Plain notes')).toHaveCount(0);
    await expect(moreButton(page)).toHaveAccessibleName('1 more');

    const tab = (title: string) => threadTab(page, title);
    await expect(tab('Open notes')).toHaveAttribute('aria-selected', 'true');
    await expect(threadTabs(page).locator('[aria-selected="true"]')).toHaveCount(1);
    expect(await colours(tab('Open notes'))).toEqual([]);
    expect(await waves(tab('Open notes'))).toBe(0);

    await expect.poll(() => colours(tab('Asks notes'))).toEqual(['light']);
    await expect.poll(() => waves(tab('Runs notes'))).toBeGreaterThan(0);
    expect(await colours(tab('Runs notes'))).toEqual([]);
    await expect(
      tab('Pinned notes').getByRole('img', { name: 'Pinned', exact: true }),
    ).toBeVisible();
    expect(await colours(tab('Pinned notes'))).toEqual([]);
    await expect.poll(() => colours(tab('Replied notes'))).toEqual(['blue']);
    await expect(
      tab('Replied notes').getByRole('img', { name: 'Pinned', exact: true }),
    ).toBeVisible();
    const weight = await tab('Replied notes')
      .getByText('Replied notes', { exact: true })
      .evaluate((el) => Number(getComputedStyle(el).fontWeight));
    expect(weight).toBeGreaterThanOrEqual(600);
    await expect.poll(() => colours(tab('Failed notes'))).toEqual(['red']);
  } finally {
    await stop(runs);
  }
});

test('tabs keep a fixed order: a thread that gets a tab goes on the right; a tab does not move when its state changes or it is opened', async ({
  page,
}) => {
  const bot = await newBotAPI(m, 'Order');
  const alpha = await openThreadAPI(m, bot.id, prompts.keep);
  const bravo = await openThreadAPI(m, bot.id, prompts.keep);
  const open = await openThreadAPI(m, bot.id, prompts.pong);
  for (const [t, title] of [
    [alpha, 'Alpha notes'],
    [bravo, 'Bravo notes'],
    [open, 'Open notes'],
  ] as const) {
    await waitStatus(m, t.id, 'done');
    await rename(t.id, title);
    await api(m, 'POST', `/api/threads/${t.id}/opened`);
  }
  for (const t of [alpha, bravo]) await api(m, 'POST', `/api/threads/${t.id}/pin`);
  const known = ['Alpha notes', 'Bravo notes', 'Open notes', 'Waits notes'];

  await openThread(page, m, bot.id, open.id);
  await away(page);
  await expect.poll(async () => (await tabTitles(page, known)).length).toBe(3);
  const before = await tabTitles(page, known);

  const waits = await openThreadAPI(m, bot.id, prompts.write);
  await waitStatus(m, waits.id, 'waiting');
  await rename(waits.id, 'Waits notes');
  await expect.poll(() => tabTitles(page, known)).toEqual([...before, 'Waits notes']);

  await api(m, 'POST', `/api/threads/${waits.id}/pin`);
  const { approval } = await thread(waits.id);
  await api(m, 'POST', `/api/approvals/${approval.id}`, { approve: true });
  await waitStatus(m, waits.id, 'done');
  await expect.poll(() => colours(threadTab(page, 'Waits notes'))).toEqual(['blue']);
  expect(await tabTitles(page, known)).toEqual([...before, 'Waits notes']);

  await threadTab(page, 'Alpha notes').click();
  await expect(page).toHaveURL(`${m.url}/bots/${bot.id}/threads/${alpha.id}`);
  await expect(threadTab(page, 'Alpha notes')).toHaveAttribute('aria-selected', 'true');
  await expect
    .poll(async () => (await tabTitles(page, known)).filter((t) => t !== 'Open notes'))
    .toEqual([...before.filter((t) => t !== 'Open notes'), 'Waits notes']);
});

test('when the tabs do not fit, the tab bar scrolls sideways and the page does not', async ({
  page,
}) => {
  const bot = await newBotAPI(m, 'Crowd');
  const waiting = [];
  for (let i = 1; i <= 9; i++) waiting.push(await openThreadAPI(m, bot.id, prompts.write));
  const open = await openThreadAPI(m, bot.id, prompts.pong);
  await waitStatus(m, open.id, 'done');
  for (const [i, t] of waiting.entries()) {
    await waitStatus(m, t.id, 'waiting');
    await rename(t.id, `A rather long waiting thread title ${i + 1}`);
  }

  await page.setViewportSize({ width: 1000, height: 700 });
  await openThread(page, m, bot.id, open.id);
  const tabs = threadTabs(page).getByRole('tab');
  await expect(tabs).toHaveCount(10);
  const ys = await tabs.evaluateAll((els) => els.map((el) => el.getBoundingClientRect().top));
  for (const y of ys) expect(Math.abs(y - ys[0])).toBeLessThanOrEqual(1);

  const scroller = await threadTabs(page).evaluate((bar) => {
    for (let n: HTMLElement | null = bar as HTMLElement; n; n = n.parentElement) {
      if (n.tagName === 'MAIN' || n === document.body) break;
      if (/(auto|scroll)/.test(getComputedStyle(n).overflowX) && n.scrollWidth > n.clientWidth) {
        n.scrollLeft = n.scrollWidth;
        return { found: true, left: n.scrollLeft };
      }
    }
    return { found: false, left: 0 };
  });
  expect(scroller.found).toBe(true);
  expect(scroller.left).toBeGreaterThan(0);
  await expect(threadTab(page, 'A rather long waiting thread title 9')).toBeInViewport();

  const over = await page.evaluate(() => {
    const root = document.documentElement;
    const main = document.querySelector('main')!;
    return {
      page: Math.max(root.scrollWidth, document.body.scrollWidth) - window.innerWidth,
      main: main.scrollWidth - main.clientWidth,
    };
  });
  expect(over.page).toBeLessThanOrEqual(0);
  expect(over.main).toBeLessThanOrEqual(0);
});

test('at 1280x800 the tabs share the 56 px header row with Thread details and Bot settings; many tabs scroll and never push the icons', async ({
  page,
}) => {
  const bot = await newBotAPI(m, 'Row');
  const open = await openThreadAPI(m, bot.id, prompts.pong);
  await waitStatus(m, open.id, 'done');

  await page.setViewportSize({ width: 1280, height: 800 });
  await openThread(page, m, bot.id, open.id);
  const header = page.locator('header');
  const details = header.getByRole('button', { name: 'Thread details', exact: true });
  const settings = header.getByRole('button', { name: 'Bot settings', exact: true });
  await expect(header.getByRole('tablist', { name: 'Threads' })).toBeVisible();
  await expect(settings).toBeVisible();
  const mid = (b: { y: number; height: number }) => b.y + b.height / 2;
  const row = async () => {
    const [h, tab, d, s] = [
      await box(header),
      await box(threadTabs(page).getByRole('tab').first()),
      await box(details),
      await box(settings),
    ];
    expect(h.height).toBeCloseTo(56, 0);
    for (const b of [d, s]) expect(Math.abs(mid(b) - mid(tab))).toBeLessThanOrEqual(2);
    expect(d.x + d.width).toBeLessThanOrEqual(s.x);
    expect(s.x + s.width).toBeLessThanOrEqual(1280 - 16);
    return { d, s };
  };
  const alone = await row();

  for (let i = 1; i <= 9; i++) {
    const t = await openThreadAPI(m, bot.id, prompts.write);
    await waitStatus(m, t.id, 'waiting');
    await rename(t.id, `A rather long waiting thread title ${i}`);
  }
  await expect(threadTabs(page).getByRole('tab')).toHaveCount(10);
  const crowded = await row();
  expect(crowded.d).toEqual(alone.d);
  expect(crowded.s).toEqual(alone.s);
  const list = await box(threadTabs(page));
  expect(list.x + list.width).toBeLessThanOrEqual(crowded.d.x);
  expect(
    await threadTabs(page).evaluate((el) => el.scrollWidth > el.clientWidth),
    'the tablist scrolls sideways',
  ).toBe(true);
});

test('clicking a tab opens its thread; hover or focus shows Close thread; right-click has Rename, Pin or Unpin and Close; a 4th pin asks which pin to replace', async ({
  page,
}) => {
  const bot = await newBotAPI(m, 'Beacon');
  const asks = await openThreadAPI(m, bot.id, prompts.write);
  await waitStatus(m, asks.id, 'waiting');
  await rename(asks.id, 'Asks notes');
  const pins: Record<string, { id: string }> = {};
  for (const name of ['Delta', 'Echo', 'Foxtrot']) {
    const t = await openThreadAPI(m, bot.id, prompts.keep);
    await waitStatus(m, t.id, 'done');
    await rename(t.id, `${name} notes`);
    await api(m, 'POST', `/api/threads/${t.id}/opened`);
    await api(m, 'POST', `/api/threads/${t.id}/pin`);
    pins[name] = t;
  }
  const open = await openThreadAPI(m, bot.id, prompts.pong);
  await waitStatus(m, open.id, 'done');
  await rename(open.id, 'Open notes');

  await openThread(page, m, bot.id, open.id);
  const tab = (title: string) => threadTab(page, title);
  await tab('Delta notes').click();
  await expect(page).toHaveURL(`${m.url}/bots/${bot.id}/threads/${pins.Delta.id}`);
  await expect(tab('Delta notes')).toHaveAttribute('aria-selected', 'true');
  await expect(threadTabs(page).locator('[aria-selected="true"]')).toHaveCount(1);

  await away(page);
  await expect.poll(async () => (await closeOn(page, tab('Asks notes'))) === null).toBe(true);
  await tab('Asks notes').hover();
  await expect.poll(async () => (await closeOn(page, tab('Asks notes'))) !== null).toBe(true);
  await away(page);
  await tab('Foxtrot notes').focus();
  await expect.poll(async () => (await closeOn(page, tab('Foxtrot notes'))) !== null).toBe(true);

  const menu = page.getByRole('menu');
  await tab('Delta notes').click({ button: 'right' });
  await expect(menu.getByRole('menuitem')).toHaveText([/^Rename/, /^Unpin/, /^Close/]);
  await page.keyboard.press('Escape');
  await expect(menu).toHaveCount(0);

  await tab('Asks notes').click({ button: 'right' });
  await expect(menu.getByRole('menuitem')).toHaveText([/^Rename/, /^Pin/, /^Close/]);
  await menu.getByRole('menuitem', { name: 'Pin', exact: true }).click();
  const replace = page.getByRole('dialog', { name: 'Replace which pin?' });
  await expect(replace.getByRole('button')).toContainText([
    'Delta notes',
    'Echo notes',
    'Foxtrot notes',
  ]);
  await replace.getByRole('button', { name: 'Echo notes' }).click();
  await expect(replace).toHaveCount(0);
  await expect.poll(async () => (await thread(asks.id)).pinned).toBe(true);
  expect((await thread(pins.Echo.id)).pinned).toBe(false);

  m.kill();
  await m.restart();
  await page.reload();
  await expect(tab('Delta notes')).toHaveAttribute('aria-selected', 'true');
  for (const name of ['Delta', 'Foxtrot'])
    await expect(
      tab(`${name} notes`).getByRole('img', { name: 'Pinned', exact: true }),
    ).toBeVisible();
  await expect(tab('Echo notes')).toHaveCount(0);

  await tab('Foxtrot notes').click({ button: 'right' });
  await menu.getByRole('menuitem', { name: 'Rename' }).click();
  const title = page.getByRole('textbox', { name: 'Thread title' });
  await title.fill('Lantern notes');
  await title.press('Enter');
  await expect(tab('Lantern notes')).toHaveCount(1);
  expect((await thread(pins.Foxtrot.id)).title).toBe('Lantern notes');

  await tab('Lantern notes').click({ button: 'right' });
  await menu.getByRole('menuitem', { name: 'Unpin', exact: true }).click();
  await expect.poll(async () => (await thread(pins.Foxtrot.id)).pinned).toBe(false);

  await tab('Asks notes').hover();
  await expect.poll(async () => (await closeOn(page, tab('Asks notes'))) !== null).toBe(true);
  await (await closeOn(page, tab('Asks notes')))!.click();
  await expect.poll(async () => (await thread(asks.id)).closed).toBe(true);
  await expect(tab('Asks notes')).toHaveCount(0);

  await tab('Delta notes').click({ button: 'right' });
  await menu.getByRole('menuitem', { name: 'Close', exact: true }).click();
  await expect.poll(async () => (await thread(pins.Delta.id)).closed).toBe(true);
  await expect(tab('Delta notes')).toHaveCount(0);
});

test('N more lists the Bot’s other open threads under a search (title, branch, MR reference) with Closed · N at the bottom; New thread starts one', async ({
  page,
}) => {
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
  const other = await newBotAPI(m, 'Other');
  const track = (url: string) =>
    `Use MCP tools. [{"server":"atoll","tool":"track_mr","args":{"url":"${url}","title":"Tidy the logs"}}]`;

  const threads: Record<string, { id: string }> = {
    'Bravo notes': await api(m, 'POST', `/api/bots/${bot.id}/threads`, {
      text: 'Edit the seeded answer but leave the work uncommitted.',
      repo_id: repo.id,
    }),
    'Hotel notes': await openThreadAPI(
      m,
      bot.id,
      track('https://gitlab.example/fixtures/seeded/-/merge_requests/7'),
    ),
    'India notes': await openThreadAPI(m, bot.id, track('https://github.com/acme/widgets/pull/92')),
    'Gamma notes': await openThreadAPI(m, bot.id, prompts.keep),
    'Delta notes': await openThreadAPI(m, bot.id, prompts.keep),
    'Alpha closed': await openThreadAPI(m, bot.id, prompts.keep),
    'Charlie closed': await openThreadAPI(m, bot.id, prompts.keep),
    'Open notes': await openThreadAPI(m, bot.id, prompts.pong),
  };
  const elsewhere = await openThreadAPI(m, other.id, prompts.keep);
  for (const [title, t] of Object.entries(threads)) {
    await waitStatus(m, t.id, 'done');
    await rename(t.id, title);
    await api(m, 'POST', `/api/threads/${t.id}/opened`);
  }
  for (const title of ['Alpha closed', 'Charlie closed'])
    await api(m, 'POST', `/api/threads/${threads[title].id}/close`);
  await waitStatus(m, elsewhere.id, 'done');
  await rename(elsewhere.id, 'Gamma elsewhere');
  expect((await thread(threads['Bravo notes'].id)).branch).toBe('fix/seeded-bug');

  await openThread(page, m, bot.id, threads['Open notes'].id);
  await expect(threadTabs(page).getByRole('tab')).toHaveCount(1);
  await expect(moreButton(page)).toHaveAccessibleName('5 more');
  const tabs = await box(threadTabs(page));
  expect((await box(moreButton(page))).x).toBeGreaterThanOrEqual(tabs.x + tabs.width - 1);
  const newThread = page.getByRole('button', { name: 'New thread', exact: true });
  await expect(newThread).toBeVisible();

  const list = await otherThreads(page);
  const links = list.getByRole('link');
  await expect(links).toHaveCount(5);
  const search = list.getByRole('searchbox', { name: 'Search threads' });
  const closed = list.getByRole('button', { name: 'Closed · 2', exact: true });
  expect((await box(search)).y).toBeLessThan((await box(links.first())).y);
  expect((await box(closed)).y).toBeGreaterThan((await box(links.last())).y);
  for (const [query, title] of [
    ['gamma', 'Gamma notes'],
    ['seeded-bug', 'Bravo notes'],
    ['!7', 'Hotel notes'],
    ['#92', 'India notes'],
  ]) {
    await search.fill(query);
    await expect(links).toHaveCount(1);
    await expect(links).toHaveAccessibleName(new RegExp(`^${title}`));
  }
  await search.fill('');
  await expect(links).toHaveCount(5);

  await closed.click();
  for (const title of ['Alpha closed', 'Charlie closed'])
    await expect(list.getByRole('link', { name: new RegExp(`^${title}`) })).toBeVisible();
  await page.keyboard.press('Escape');
  await expect(list).toHaveCount(0);

  await otherThreads(page);
  await list.getByRole('link', { name: /^Gamma notes/ }).click();
  await expect(page).toHaveURL(`${m.url}/bots/${bot.id}/threads/${threads['Gamma notes'].id}`);
  await expect(list).toHaveCount(0);
  await expect(threadTab(page, 'Gamma notes')).toHaveAttribute('aria-selected', 'true');
  await expect(threadTabs(page).getByRole('tab').last()).toHaveAccessibleName(/^Gamma notes/);

  await newThread.click();
  await expect(page.getByRole('heading', { name: 'What should Lantern work on?' })).toBeVisible();
  await expect(page).toHaveURL(new RegExp(`/bots/${bot.id}/new$`));
});

test('with no other open threads there is no N more', async ({ page }) => {
  const bot = await newBotAPI(m, 'Single');
  const only = await openThreadAPI(m, bot.id, prompts.pong);
  await waitStatus(m, only.id, 'done');
  await openThread(page, m, bot.id, only.id);
  await expect(threadTabs(page).getByRole('tab')).toHaveCount(1);
  await expect(page.getByRole('button', { name: 'New thread', exact: true })).toBeVisible();
  await expect(moreButton(page)).toHaveCount(0);
});
