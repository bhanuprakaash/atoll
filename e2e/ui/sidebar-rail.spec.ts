// The sidebar as a rail of Bot faces and a thread pane for the selected Bot or Now: issues
// #469–#472. The pane's resize is in sidebar-resize.spec.ts.
import { execFileSync } from 'node:child_process';
import { mkdirSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';
import { expect, type Locator, type Page, test } from '@playwright/test';
import {
  api,
  type Machine,
  newBotAPI,
  nowPane,
  openThreadAPI,
  prompts,
  showPane,
  sidebarRow,
  startAtoll,
  threadPane,
  threadTitle,
  waitStatus,
} from './atoll';
import { bossHandsOff, details } from './helpers-fixture';

let m: Machine;
test.beforeEach(async () => {
  m = await startAtoll();
});
test.afterEach(() => m?.stop());

const rail = (page: Page) => page.getByRole('navigation', { name: 'Bots' });
const nowButton = (page: Page) => rail(page).getByRole('button', { name: /^Now\b/ });
const group = (page: Page, name: string) => rail(page).getByRole('group', { name, exact: true });
const groupLine = (page: Page, name: string) =>
  group(page, name).getByRole('button', { name, exact: true });
const section = (pane: Locator, name: string) => pane.getByRole('list', { name, exact: true });
const rowOf = (pane: Locator, id: string) =>
  pane.getByRole('listitem').filter({ has: pane.page().locator(`a[href$="/threads/${id}"]`) });
const linkOf = (pane: Locator, id: string) => pane.locator(`a[href$="/threads/${id}"]`);
const box = async (l: Locator) => (await l.boundingBox())!;

const stop = (t: { run: { id: string } }) => api(m, 'POST', `/api/runs/${t.run.id}/stop`);

// visit opens a thread in the page, so it counts as seen.
async function visit(page: Page, botID: string, threadID: string) {
  await page.goto(`${m.url}/bots/${botID}/threads/${threadID}`);
  await expect(
    page.getByRole('log', { name: 'Messages' }).getByRole('article').first(),
  ).toBeVisible();
}

// faceOrder is the href of every Bot face on the rail, top to bottom.
const faceOrder = (page: Page) =>
  rail(page)
    .locator('a[href^="/bots/"]')
    .evaluateAll((els) => els.map((el) => el.getAttribute('href')));

// precedes: a comes before b in the document.
const precedes = async (a: Locator, b: Locator) =>
  a.evaluate(
    (x, y) => !!(x.compareDocumentPosition(y as Node) & Node.DOCUMENT_POSITION_FOLLOWING),
    await b.elementHandle(),
  );

// The rows of the pane's sections, as their list names, top to bottom.
const sections = (pane: Locator) =>
  pane.getByRole('list').evaluateAll((els) => els.map((el) => el.getAttribute('aria-label') ?? ''));

// animating counts the animations playing inside el.
const animating = (el: Locator) =>
  el.evaluate(
    (node) => node.getAnimations({ subtree: true }).filter((a) => a.playState === 'running').length,
  );

test.describe('#469 rail and the Bot pane', () => {
  test('the rail lists Now, Chief, groups in order, Ungrouped last; a long name ends in … and shows in full on hover and focus', async ({
    page,
  }) => {
    const long = 'Bartholomew Longname Reviewer';
    const bots: Record<string, { id: string }> = {};
    for (const name of ['Ivy', 'Oak', 'Elm', long]) bots[name] = await newBotAPI(m, name);
    const backend = await api(m, 'POST', '/api/groups', { name: 'Backend' });
    const frontend = await api(m, 'POST', '/api/groups', { name: 'Frontend' });
    await api(m, 'PUT', `/api/bots/${bots.Oak.id}/group`, {
      group_id: frontend.id,
    });
    await api(m, 'PUT', `/api/bots/${bots.Ivy.id}/group`, {
      group_id: backend.id,
    });

    await page.goto(`${m.url}/settings`);
    await expect(rail(page)).toBeVisible();
    expect(Math.abs((await box(rail(page))).width - 76)).toBeLessThanOrEqual(1);
    await expect(nowButton(page)).toBeVisible();
    expect(await precedes(nowButton(page), sidebarRow(page, 'Chief'))).toBe(true);

    const order = await faceOrder(page);
    expect(order.slice(0, 3)).toEqual([
      '/bots/chief',
      `/bots/${bots.Ivy.id}`,
      `/bots/${bots.Oak.id}`,
    ]);
    expect(order.slice(3).sort()).toEqual(
      [`/bots/${bots.Elm.id}`, `/bots/${bots[long].id}`].sort(),
    );
    await expect
      .poll(() =>
        rail(page)
          .getByRole('group')
          .evaluateAll((els) => els.map((el) => el.getAttribute('aria-label'))),
      )
      .toEqual(['Backend', 'Frontend', 'Ungrouped']);
    await expect(group(page, 'Ungrouped').getByRole('link', { name: /\bElm\b/ })).toBeVisible();

    const face = sidebarRow(page, 'Bartholomew');
    const name = face.getByText(long, { exact: true });
    await expect(name).toBeVisible();
    const creature = await box(face.getByRole('img').first());
    expect((await box(name)).y).toBeGreaterThanOrEqual(creature.y + creature.height - 1);
    const cut = await name.evaluate((el) => ({
      overflow: getComputedStyle(el).textOverflow,
      cut: el.scrollWidth > el.clientWidth,
      width: el.getBoundingClientRect().width,
    }));
    expect(cut.overflow).toBe('ellipsis');
    expect(cut.cut).toBe(true);
    expect(cut.width).toBeLessThanOrEqual(65);

    await face.hover();
    await expect(page.getByRole('tooltip')).toContainText(long);
    await page.mouse.move(1270, 700);
    await expect(page.getByRole('tooltip')).toHaveCount(0);
    await face.focus();
    await expect(page.getByRole('tooltip')).toContainText(long);
  });

  test('clicking a face opens the latest thread and its pane; the face gets the bar and only the open row is current', async ({
    page,
  }) => {
    const bot = await newBotAPI(m, 'Pilot');
    const older = await openThreadAPI(m, bot.id, prompts.pong);
    await waitStatus(m, older.id, 'done');
    const newer = await openThreadAPI(m, bot.id, prompts.keep);
    await waitStatus(m, newer.id, 'done');

    await page.goto(`${m.url}/settings`);
    await sidebarRow(page, 'Pilot').click();
    await expect(page).toHaveURL(`${m.url}/bots/${bot.id}/threads/${newer.id}`);
    const pane = threadPane(page, 'Pilot');
    await expect(pane).toBeVisible();
    expect(
      await rail(page).evaluate((nav, p) => !nav.contains(p as Node), await pane.elementHandle()),
    ).toBe(true);

    await expect(sidebarRow(page, 'Pilot')).toHaveAttribute('data-current', 'true');
    await expect(rail(page).locator('[data-current="true"]')).toHaveCount(1);
    await expect(linkOf(pane, newer.id)).toHaveAttribute('aria-current', 'page');
    await expect(linkOf(pane, older.id)).not.toHaveAttribute('aria-current', /./);
    await expect(pane.locator('[aria-current]')).toHaveCount(1);
    await expect(rail(page).locator('[aria-current]')).toHaveCount(0);

    await linkOf(pane, older.id).click();
    await expect(page).toHaveURL(`${m.url}/bots/${bot.id}/threads/${older.id}`);
    await expect(linkOf(pane, older.id)).toHaveAttribute('aria-current', 'page');
    await expect(pane.locator('[aria-current]')).toHaveCount(1);
  });

  test('a Bot that is not selected shows the Needs you dot and its working motion on its face', async ({
    page,
  }) => {
    await page.emulateMedia({ reducedMotion: 'no-preference' });
    const asker = await newBotAPI(m, 'Asker', 'claude', { look: 'whale' });
    const worker = await newBotAPI(m, 'Worker', 'claude', { look: 'whale' });
    const waiting = await openThreadAPI(m, asker.id, prompts.write);
    const busy = await openThreadAPI(m, worker.id, prompts.hang);
    try {
      await waitStatus(m, waiting.id, 'waiting');
      await waitStatus(m, busy.id, 'running');
      await showPane(page, m, 'chief', 'Chief');

      const asking = sidebarRow(page, 'Asker');
      await expect(asking).not.toHaveAttribute('data-current', 'true');
      await expect(asking).toHaveAttribute('data-marker', 'needs-you');
      await expect(
        asking.getByRole('img', { name: 'Whale, needs you', exact: true }),
      ).toBeVisible();
      await expect.poll(() => animating(asking)).toBeGreaterThan(0);

      const working = sidebarRow(page, 'Worker');
      await expect(working).not.toHaveAttribute('data-current', 'true');
      await expect(working.getByRole('img', { name: 'Whale, working', exact: true })).toBeVisible();
      await expect.poll(() => animating(working)).toBeGreaterThan(0);
      await expect(working).not.toHaveAttribute('data-marker', 'needs-you');
    } finally {
      await stop(busy);
    }
  });

  test('the pane lists every open thread under Needs you, Running, Pinned and Recent with their markers; no cap, no N more', async ({
    page,
  }) => {
    const bot = await newBotAPI(m, 'Many');
    const asking = [];
    for (let i = 0; i < 2; i++) asking.push(await openThreadAPI(m, bot.id, prompts.write));
    const busy = [];
    for (let i = 0; i < 2; i++) busy.push(await openThreadAPI(m, bot.id, prompts.hang));
    const pinned = await openThreadAPI(m, bot.id, prompts.pong);
    const recent = [];
    for (let i = 0; i < 5; i++) recent.push(await openThreadAPI(m, bot.id, prompts.keep));
    try {
      for (const t of asking) await waitStatus(m, t.id, 'waiting');
      for (const t of busy) await waitStatus(m, t.id, 'running');
      for (const t of [pinned, ...recent]) await waitStatus(m, t.id, 'done');
      await api(m, 'POST', `/api/threads/${pinned.id}/pin`);
      await visit(page, bot.id, pinned.id);

      const pane = await showPane(page, m, bot.id, 'Many');
      await expect.poll(() => sections(pane)).toEqual(['Needs you', 'Running', 'Pinned', 'Recent']);
      await expect(pane.getByRole('listitem')).toHaveCount(10);
      const marked = async (name: string, marker: string, ids: string[]) => {
        await expect(section(pane, name).getByRole('listitem')).toHaveCount(ids.length);
        for (const id of ids)
          await expect(rowOf(section(pane, name), id)).toHaveAttribute('data-marker', marker);
      };
      await marked(
        'Needs you',
        'needs-you',
        asking.map((t) => t.id),
      );
      await marked(
        'Running',
        'running',
        busy.map((t) => t.id),
      );
      await marked('Pinned', 'pinned', [pinned.id]);
      await marked(
        'Recent',
        'new',
        recent.map((t) => t.id),
      );
      await expect(section(pane, 'Needs you').getByRole('listitem').first()).toContainText(
        'Needs you',
      );
      await expect(section(pane, 'Recent').getByRole('listitem').first()).toContainText('Replied');
      await expect(pane.getByRole('button', { name: /\bmore\b/ })).toHaveCount(0);
      await expect(pane.getByRole('button', { name: 'Show less' })).toHaveCount(0);
    } finally {
      for (const t of busy) await stop(t);
    }
  });

  test('a running helper shows its line under the boss thread; clicking it opens the helpers panel', async ({
    page,
  }) => {
    const { poteto, boss } = await bossHandsOff(m, page, [['Tester', prompts.hang]]);
    const pane = await showPane(page, m, poteto.id, 'Poteto');

    const line = pane.locator('[data-line="helpers"]');
    await expect(line).toHaveCount(1);
    await expect(line).toContainText('Tester');
    expect(await line.evaluate((el) => el.tagName)).not.toBe('LI');
    expect(await precedes(rowOf(pane, boss), line)).toBe(true);
    const bossRow = await box(rowOf(pane, boss));
    expect((await box(line)).y).toBeGreaterThanOrEqual(bossRow.y + bossRow.height - 1);

    await line.click();
    await expect(page).toHaveURL(`${m.url}/bots/${poteto.id}/threads/${boss}`);
    await expect(details(page)).toContainText('Tester');
    await expect(page.getByText(/Working with/)).toHaveCount(0);
    await expect(details(page)).toBeInViewport();
  });

  test('a group line shows the group name and Bot count on hover and focus, and opens the group menu on right-click', async ({
    page,
  }) => {
    const bots: Record<string, { id: string }> = {};
    for (const name of ['Ivy', 'Oak', 'Elm']) bots[name] = await newBotAPI(m, name);
    const ops = await api(m, 'POST', '/api/groups', { name: 'Ops' });
    const quiet = await api(m, 'POST', '/api/groups', { name: 'Quiet' });
    await api(m, 'PUT', `/api/bots/${bots.Ivy.id}/group`, { group_id: ops.id });
    await api(m, 'PUT', `/api/bots/${bots.Oak.id}/group`, {
      group_id: quiet.id,
    });
    await page.goto(`${m.url}/settings`);

    await groupLine(page, 'Ops').hover();
    const tip = page.getByRole('tooltip');
    await expect(tip).toContainText('Ops');
    await expect(tip).toContainText(/\b1\b/);
    await page.mouse.move(1270, 700);
    await expect(tip).toHaveCount(0);
    await groupLine(page, 'Quiet').focus();
    await expect(tip).toContainText('Quiet');

    await groupLine(page, 'Ops').click({ button: 'right' });
    await expect(page.getByRole('menu').getByRole('menuitem')).toHaveText([
      /^Rename/,
      /^New Bot in this group/,
      /^Delete group/,
    ]);
    await page.keyboard.press('Escape');
    await expect(page.getByRole('menu')).toHaveCount(0);

    await api(m, 'PUT', `/api/bots/${bots.Elm.id}/group`, { group_id: ops.id });
    await groupLine(page, 'Ops').hover();
    await expect(tip).toContainText(/\b2\b/);
  });

  test('pane search filters this Bot’s threads by title, branch and MR reference', async ({
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
    const bot = await newBotAPI(m, 'Lantern', 'claude', {
      repo_access: [repo.id],
    });
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
      'India notes': await openThreadAPI(
        m,
        bot.id,
        track('https://github.com/acme/widgets/pull/92'),
      ),
      'Gamma notes': await openThreadAPI(m, bot.id, prompts.keep),
      'Delta notes': await openThreadAPI(m, bot.id, prompts.keep),
    };
    const elsewhere = await openThreadAPI(m, other.id, prompts.keep);
    for (const [title, t] of Object.entries(threads)) {
      await waitStatus(m, t.id, 'done');
      await api(m, 'PATCH', `/api/threads/${t.id}`, { title });
    }
    await waitStatus(m, elsewhere.id, 'done');
    await api(m, 'PATCH', `/api/threads/${elsewhere.id}`, {
      title: 'Gamma elsewhere',
    });
    expect((await api(m, 'GET', `/api/threads/${threads['Bravo notes'].id}`)).branch).toBe(
      'fix/seeded-bug',
    );

    const pane = await showPane(page, m, bot.id, 'Lantern');
    const rows = pane.getByRole('listitem');
    await expect(rows).toHaveCount(5);
    const search = pane.getByRole('textbox', { name: 'Search threads' });
    for (const [query, title] of [
      ['gamma', 'Gamma notes'],
      ['seeded-bug', 'Bravo notes'],
      ['!7', 'Hotel notes'],
      ['#92', 'India notes'],
    ]) {
      await search.fill(query);
      await expect(rows).toHaveCount(1);
      await expect(rows).toContainText(title);
    }
    await search.fill('');
    await expect(rows).toHaveCount(5);
  });

  test('pinned Bots sit under Chief, before the groups', async ({ page }) => {
    const bots: Record<string, { id: string }> = {};
    for (const name of ['Ivy', 'Elm', 'Fir']) bots[name] = await newBotAPI(m, name);
    const ops = await api(m, 'POST', '/api/groups', { name: 'Ops' });
    await api(m, 'PUT', `/api/bots/${bots.Ivy.id}/group`, { group_id: ops.id });
    await api(m, 'POST', `/api/bots/${bots.Fir.id}/pin`);
    await page.goto(`${m.url}/settings`);
    await expect(nowButton(page)).toBeVisible();
    await expect
      .poll(async () => (await faceOrder(page)).slice(0, 3))
      .toEqual(['/bots/chief', `/bots/${bots.Fir.id}`, `/bots/${bots.Ivy.id}`]);

    await sidebarRow(page, 'Elm').click({ button: 'right' });
    await page.getByRole('menuitem', { name: 'Pin to sidebar' }).click();
    await expect
      .poll(async () => (await faceOrder(page)).slice(0, 4))
      .toEqual([
        '/bots/chief',
        `/bots/${bots.Fir.id}`,
        `/bots/${bots.Elm.id}`,
        `/bots/${bots.Ivy.id}`,
      ]);
    expect(await precedes(sidebarRow(page, 'Elm'), group(page, 'Ops'))).toBe(true);
    expect((await api(m, 'GET', `/api/bots/${bots.Elm.id}`)).pinned).toBe(true);
  });

  test('⌘B hides and shows the pane; the rail stays', async ({ page }) => {
    const bot = await newBotAPI(m, 'Pilot');
    const t = await openThreadAPI(m, bot.id, prompts.pong);
    await waitStatus(m, t.id, 'done');
    await page.setViewportSize({ width: 1280, height: 720 });
    await visit(page, bot.id, t.id);
    const pane = threadPane(page, 'Pilot');
    await expect(pane).toBeVisible();
    const wide = (await box(page.locator('main'))).width;

    await page.getByRole('textbox', { name: /^Message/ }).focus();
    await page.keyboard.press('ControlOrMeta+b');
    await expect(pane).toBeHidden();
    await expect(rail(page)).toBeVisible();
    expect(Math.abs((await box(rail(page))).width - 76)).toBeLessThanOrEqual(1);
    await expect.poll(async () => (await box(page.locator('main'))).width).toBeGreaterThan(wide);

    await page.keyboard.press('ControlOrMeta+b');
    await expect(pane).toBeVisible();
    await expect(rail(page)).toBeVisible();
    await expect.poll(async () => (await box(page.locator('main'))).width).toBe(wide);
  });
});

test.describe('#470 row actions, Closed and New thread', () => {
  test('hover or focus on a row shows Pin/Unpin and Close in place of its marker; a 4th pin asks which to replace; pins survive a restart', async ({
    page,
  }) => {
    const bot = await newBotAPI(m, 'Beacon');
    const asking = await openThreadAPI(m, bot.id, prompts.write);
    const names = ['Delta', 'Echo', 'Foxtrot', 'Golf'];
    const threads: Record<string, { id: string }> = {};
    for (const name of names) threads[name] = await openThreadAPI(m, bot.id, prompts.keep);
    await waitStatus(m, asking.id, 'waiting');
    for (const name of names) {
      await waitStatus(m, threads[name].id, 'done');
      await api(m, 'PATCH', `/api/threads/${threads[name].id}`, {
        title: `${name} notes`,
      });
    }

    const pane = await showPane(page, m, bot.id, 'Beacon');
    const needs = rowOf(pane, asking.id);
    const marker = needs.getByText('Needs you', { exact: true });
    await expect(marker).toBeVisible();
    await expect(needs.getByRole('button', { name: 'Close thread' })).toBeHidden();
    await needs.hover();
    await expect(needs.getByRole('button', { name: 'Pin', exact: true })).toBeVisible();
    await expect(needs.getByRole('button', { name: 'Close thread' })).toBeVisible();
    await expect(marker).toBeHidden();
    await page.mouse.move(1270, 700);
    await expect(marker).toBeVisible();

    const row = (name: string) => rowOf(pane, threads[name].id);
    const pin = async (name: string) => {
      await row(name).hover();
      await row(name).getByRole('button', { name: 'Pin', exact: true }).click();
    };
    for (const name of ['Delta', 'Echo', 'Foxtrot']) await pin(name);
    await expect(section(pane, 'Pinned').getByRole('listitem')).toHaveCount(3);

    await page.mouse.move(1270, 700);
    await row('Delta').getByRole('link').focus();
    await expect(row('Delta').getByRole('button', { name: 'Unpin', exact: true })).toBeVisible();
    await expect(row('Delta').getByRole('button', { name: 'Close thread' })).toBeVisible();

    await pin('Golf');
    const replace = page.getByRole('dialog', { name: 'Replace which pin?' });
    await expect(replace.getByRole('button')).toContainText([
      'Delta notes',
      'Echo notes',
      'Foxtrot notes',
    ]);
    await replace.getByRole('button', { name: 'Delta notes' }).click();
    await expect(replace).toHaveCount(0);

    const expectPins = async () => {
      const pinned = section(pane, 'Pinned');
      await expect(pinned.getByRole('listitem')).toHaveCount(3);
      for (const name of ['Echo', 'Foxtrot', 'Golf'])
        await expect(pinned.getByRole('listitem').filter({ hasText: `${name} notes` })).toHaveCount(
          1,
        );
      await expect(pinned).not.toContainText('Delta notes');
    };
    await expectPins();
    m.kill();
    await m.restart();
    await page.reload();
    await expectPins();

    await row('Echo').hover();
    await row('Echo').getByRole('button', { name: 'Unpin', exact: true }).click();
    await expect(section(pane, 'Pinned').getByRole('listitem')).toHaveCount(2);
    expect((await api(m, 'GET', `/api/threads/${threads.Echo.id}`)).pinned).toBe(false);

    await row('Delta').hover();
    await row('Delta').getByRole('button', { name: 'Close thread' }).click();
    await expect(row('Delta')).toHaveCount(0);
    await expect
      .poll(async () => (await api(m, 'GET', `/api/threads/${threads.Delta.id}`)).closed)
      .toBe(true);
  });

  test('Closed folds open in the pane with the latest closed threads and Search closed threads; the threads popup is gone', async ({
    page,
  }) => {
    const bot = await newBotAPI(m, 'Archivist');
    const open = await openThreadAPI(m, bot.id, prompts.pong);
    await waitStatus(m, open.id, 'done');
    const closed: Record<string, { id: string }> = {};
    for (const name of ['Alpha', 'Bravo', 'Charlie']) {
      const t = await openThreadAPI(m, bot.id, prompts.keep);
      await waitStatus(m, t.id, 'done');
      await api(m, 'PATCH', `/api/threads/${t.id}`, {
        title: `${name} closed`,
      });
      await api(m, 'POST', `/api/threads/${t.id}/close`);
      closed[name] = t;
    }

    const pane = await showPane(page, m, bot.id, 'Archivist');
    const fold = pane.getByRole('button', { name: 'Closed · 3', exact: true });
    await expect(fold).toHaveAttribute('aria-expanded', 'false');
    await expect(section(pane, 'Closed')).toBeHidden();
    await expect(pane.getByRole('listitem')).toHaveCount(1);
    await expect(pane.getByRole('button', { name: /^\d+ more$/ })).toHaveCount(0);

    await fold.click();
    await expect(fold).toHaveAttribute('aria-expanded', 'true');
    const list = section(pane, 'Closed');
    await expect(list.getByRole('listitem').first()).toContainText('Charlie closed');
    const search = pane.getByRole('textbox', { name: 'Search closed threads' });
    await search.fill('alpha');
    await expect(list.getByRole('listitem')).toHaveCount(1);
    await expect(list.getByRole('listitem')).toContainText('Alpha closed');
    await expect(page.getByRole('dialog', { name: /^Threads with / })).toHaveCount(0);

    await linkOf(list, closed.Alpha.id).click();
    await expect(page).toHaveURL(`${m.url}/bots/${bot.id}/threads/${closed.Alpha.id}`);
    await expect(page.getByRole('dialog', { name: /^Threads with / })).toHaveCount(0);
  });

  test('pane + and ⌘N open New thread for the selected Bot; no thread row is selected then', async ({
    page,
  }) => {
    const bot = await newBotAPI(m, 'Keyer');
    const t = await openThreadAPI(m, bot.id, prompts.pong);
    await waitStatus(m, t.id, 'done');
    const heading = page.getByRole('heading', {
      name: 'What should Keyer work on?',
    });
    const expectNewThread = async () => {
      await expect(heading).toBeVisible();
      await expect(page).toHaveURL(new RegExp(`/bots/${bot.id}/`));
      const pane = threadPane(page, 'Keyer');
      await expect(linkOf(pane, t.id)).toBeVisible();
      await expect(pane.locator('[aria-current]')).toHaveCount(0);
      await expect(rail(page).locator('[aria-current]')).toHaveCount(0);
      await expect(sidebarRow(page, 'Keyer')).toHaveAttribute('data-current', 'true');
    };

    await visit(page, bot.id, t.id);
    await threadPane(page, 'Keyer').getByRole('button', { name: 'New thread' }).click();
    await expectNewThread();

    await visit(page, bot.id, t.id);
    await expect(linkOf(threadPane(page, 'Keyer'), t.id)).toHaveAttribute('aria-current', 'page');
    await page.keyboard.press('Meta+n');
    await expectNewThread();
  });
});

test.describe('#471 Now', () => {
  test('Now lists Needs you, Running and Replied threads across every Bot, each with its creature and Bot · note', async ({
    page,
  }) => {
    const asker = await newBotAPI(m, 'Asker', 'claude', { look: 'octopus' });
    const worker = await newBotAPI(m, 'Worker', 'claude', { look: 'seal' });
    const newsy = await newBotAPI(m, 'Newsy', 'claude', { look: 'whale' });
    const calm = await newBotAPI(m, 'Calm');
    const waiting = await openThreadAPI(m, asker.id, prompts.write);
    const busy = await openThreadAPI(m, worker.id, prompts.hang);
    const replied = await openThreadAPI(m, newsy.id, prompts.keep);
    const seen = await openThreadAPI(m, calm.id, prompts.pong);
    try {
      await waitStatus(m, waiting.id, 'waiting');
      await waitStatus(m, busy.id, 'running');
      await waitStatus(m, replied.id, 'done');
      await waitStatus(m, seen.id, 'done');
      await visit(page, calm.id, seen.id);
      await showPane(page, m, 'chief', 'Chief');

      const now = await nowPane(page);
      await expect.poll(() => sections(now)).toEqual(['Needs you', 'Running', 'Replied']);
      for (const [name, id, bot, img] of [
        ['Needs you', waiting.id, 'Asker', /^Octopus\b/],
        ['Running', busy.id, 'Worker', /^Seal\b/],
        ['Replied', replied.id, 'Newsy', /^Whale\b/],
      ] as const) {
        const row = rowOf(section(now, name), id);
        await expect(row).toHaveCount(1);
        await expect(row.getByRole('img', { name: img })).toBeVisible();
        await expect(row).toContainText(new RegExp(`${bot} · \\S`));
      }
      await expect(linkOf(now, seen.id)).toHaveCount(0);
      await expect(now.getByRole('listitem')).toHaveCount(3);
    } finally {
      await stop(busy);
    }
  });

  test('the Now count equals the Needs you count and clears when nothing waits', async ({
    page,
  }) => {
    const first = await newBotAPI(m, 'First');
    const second = await newBotAPI(m, 'Second');
    const a = await openThreadAPI(m, first.id, prompts.write);
    const b = await openThreadAPI(m, second.id, prompts.write);
    await waitStatus(m, a.id, 'waiting');
    await waitStatus(m, b.id, 'waiting');
    await page.goto(`${m.url}/settings`);
    await expect(nowButton(page)).toBeVisible();
    await expect(nowButton(page)).toHaveText('2');

    for (const [t, left] of [
      [a, '1'],
      [b, ''],
    ] as const) {
      const { approval } = await api(m, 'GET', `/api/threads/${t.id}`);
      await api(m, 'POST', `/api/approvals/${approval.id}`, { approve: true });
      await waitStatus(m, t.id, 'done');
      await expect(nowButton(page)).toHaveText(left);
    }
    await expect(nowButton(page)).toBeVisible();
  });

  test('opening a thread from Now keeps the pane on Now and selects that row', async ({ page }) => {
    const worker = await newBotAPI(m, 'Worker');
    const asker = await newBotAPI(m, 'Asker');
    const busy = await openThreadAPI(m, worker.id, prompts.hang);
    const waiting = await openThreadAPI(m, asker.id, prompts.write);
    try {
      await waitStatus(m, busy.id, 'running');
      await waitStatus(m, waiting.id, 'waiting');
      await showPane(page, m, 'chief', 'Chief');
      const now = await nowPane(page);

      for (const [bot, t] of [
        [worker, busy],
        [asker, waiting],
      ] as const) {
        await linkOf(now, t.id).click();
        await expect(page).toHaveURL(`${m.url}/bots/${bot.id}/threads/${t.id}`);
        await expect(now).toBeVisible();
        await expect(threadPane(page, bot === worker ? 'Worker' : 'Asker')).toHaveCount(0);
        await expect(linkOf(now, t.id)).toHaveAttribute('aria-current', 'page');
        await expect(now.locator('[aria-current]')).toHaveCount(1);
        await expect(rail(page).locator('[aria-current]')).toHaveCount(0);
      }
    } finally {
      await stop(busy);
    }
  });

  test('with nothing waiting, running or replied, Now reads Nothing needs you', async ({
    page,
  }) => {
    const bot = await newBotAPI(m, 'Calm');
    const t = await openThreadAPI(m, bot.id, prompts.pong);
    await waitStatus(m, t.id, 'done');
    await visit(page, bot.id, t.id);
    const now = await nowPane(page);
    await expect(now).toContainText('Nothing needs you');
    await expect(now.getByRole('listitem')).toHaveCount(0);
    await expect(nowButton(page)).toHaveText('');
  });
});

test.describe('#472 narrow window', () => {
  test('below 900 px the rail stays docked; a face opens the pane over the thread with a scrim; a thread, the scrim or Esc closes it', async ({
    page,
  }) => {
    const pilot = await newBotAPI(m, 'Pilot');
    const first = await openThreadAPI(m, pilot.id, prompts.keep);
    await waitStatus(m, first.id, 'done');
    const second = await openThreadAPI(m, pilot.id, prompts.pong);
    await waitStatus(m, second.id, 'done');

    await page.setViewportSize({ width: 700, height: 500 });
    await visit(page, pilot.id, first.id);
    const pane = threadPane(page, 'Pilot');
    const face = sidebarRow(page, 'Pilot');
    await expect(rail(page)).toBeVisible();
    const r = await box(rail(page));
    expect(r.x).toBe(0);
    expect(Math.abs(r.width - 76)).toBeLessThanOrEqual(1);
    await expect(pane).toBeHidden();
    const main = await box(page.locator('main'));
    expect(Math.abs(main.x - 76)).toBeLessThanOrEqual(1);
    expect(Math.abs(main.width - (700 - 76))).toBeLessThanOrEqual(1);

    // Esc closes it and focus goes back to the face.
    await face.click();
    await expect(pane).toBeVisible();
    const p = await box(pane);
    expect(Math.abs(p.x - 76)).toBeLessThanOrEqual(1);
    expect(await box(page.locator('main'))).toEqual(main);
    await expect(page.getByRole('separator', { name: 'Resize thread pane' })).toBeHidden();
    await page.keyboard.press('Escape');
    await expect(pane).toBeHidden();
    await expect(face).toBeFocused();

    // A click on the scrim, clear of the pane, closes it.
    await face.click();
    await expect(pane).toBeVisible();
    await page.mouse.click(690, 250);
    await expect(pane).toBeHidden();
    await expect(face).toBeFocused();

    // Picking a thread opens it and closes the pane. The face opened the latest thread, second.
    await face.click();
    await expect(pane).toBeVisible();
    await expect(page).toHaveURL(`${m.url}/bots/${pilot.id}/threads/${second.id}`);
    await linkOf(pane, first.id).click();
    await expect(page).toHaveURL(`${m.url}/bots/${pilot.id}/threads/${first.id}`);
    await expect(pane).toBeHidden();
    await expect(rail(page)).toBeVisible();

    // At 360 the pane leaves the scrim showing.
    await page.setViewportSize({ width: 360, height: 640 });
    await face.click();
    await expect(pane).toBeVisible();
    expect((await box(pane)).width).toBeLessThanOrEqual(360 - 76 - 48);
  });

  test('no page scrolls sideways at 420 px and up', async ({ page }) => {
    const bot = await newBotAPI(m, 'Pilot');
    const t = await openThreadAPI(m, bot.id, prompts.pong);
    await waitStatus(m, t.id, 'done');
    for (const width of [420, 700, 900]) {
      for (const url of [
        `${m.url}/bots/${bot.id}/threads/${t.id}`,
        `${m.url}/settings`,
        `${m.url}/bots/new`,
      ]) {
        await page.setViewportSize({ width, height: 800 });
        await page.goto(url);
        await expect(page.locator('main')).toBeVisible();
        await expect(rail(page)).toBeVisible();
        const over = await page.evaluate(() => {
          const root = document.documentElement;
          const main = document.querySelector('main')!;
          return {
            page: Math.max(root.scrollWidth, document.body.scrollWidth) - window.innerWidth,
            main: main.scrollWidth - main.clientWidth,
          };
        });
        expect(over.page, `${url} at ${width}`).toBeLessThanOrEqual(0);
        expect(over.main, `${url} at ${width}`).toBeLessThanOrEqual(0);
      }
    }
  });
});
