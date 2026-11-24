// The sidebar as one list of Bots: Now, Chief, pinned Bots, groups and Ungrouped, one row per Bot
// with a second line always (last reply, live verb, ask, Run failed, Stopped); Now opens its list
// in the main area. The resize is in sidebar-resize.spec.ts, narrow windows in narrow-drawer.spec.ts, drag in
// sidebar-drag.spec.ts.
import { expect, type Locator, type Page, test } from '@playwright/test';
import {
  api,
  lines,
  type Machine,
  marks,
  newBotAPI,
  nowView,
  openNow,
  openThread,
  openThreadAPI,
  prompts,
  sidebarRow,
  startAtoll,
  threadTab,
  threadTitle,
  waitStatus,
} from './atoll';

let m: Machine;
test.beforeEach(async () => {
  m = await startAtoll();
});
test.afterEach(() => m?.stop());

// What the Bot asks for testdata/claude/run-approve.jsonl (Write /work/hello.txt).
const editAsk = 'Wants to edit hello.txt';
// A time today: the locale's clock, `6:21 AM` or `18:21`.
const time = String.raw`\d{1,2}:\d{2}(\s?[AP]M)?`;
const clock = new RegExp(`^${time}$`);

const sidebar = (page: Page) => page.getByRole('navigation', { name: 'Bots' });
const nowButton = (page: Page) => sidebar(page).getByRole('button', { name: /^Now\b/ });
const group = (page: Page, name: string) => sidebar(page).getByRole('group', { name, exact: true });
const box = async (l: Locator) => (await l.boundingBox())!;
const away = (page: Page) => page.mouse.move(1270, 5);
const stop = (t: { run: { id: string } }) => api(m, 'POST', `/api/runs/${t.run.id}/stop`);

// rowOrder is the href of every Bot row in the sidebar, top to bottom.
const rowOrder = (page: Page) =>
  sidebar(page)
    .locator('a[href^="/bots/"]')
    .evaluateAll((els) => els.map((el) => el.getAttribute('href')));

const groupOrder = (page: Page) =>
  sidebar(page)
    .getByRole('group')
    .evaluateAll((els) => els.map((el) => el.getAttribute('aria-label')));

// filled: the row or one of its ancestors inside the sidebar has the selected fill.
const filled = (row: Locator) =>
  row.evaluate((el) => {
    const probe = document.createElement('div');
    probe.style.background = 'var(--color-selected)';
    document.body.append(probe);
    const want = getComputedStyle(probe).backgroundColor;
    probe.remove();
    for (let n: Element | null = el; n && n.tagName !== 'NAV'; n = n.parentElement)
      if (getComputedStyle(n).backgroundColor === want) return true;
    return false;
  });

// hairlines lists the y of every horizontal hairline drawn in the sidebar: a line up to 1.5 px
// tall, or a top or bottom border.
const hairlines = (page: Page) =>
  sidebar(page).evaluate((nav) => {
    const clear = (c: string) => c === 'transparent' || c === 'rgba(0, 0, 0, 0)';
    const ys: number[] = [];
    for (const e of nav.querySelectorAll('*')) {
      const r = e.getBoundingClientRect();
      const cs = getComputedStyle(e);
      if (r.width < 12) continue;
      if (r.height > 0 && r.height <= 1.5 && !clear(cs.backgroundColor)) ys.push(r.top);
      if (parseFloat(cs.borderTopWidth) > 0 && cs.borderTopStyle !== 'none')
        if (!clear(cs.borderTopColor)) ys.push(r.top);
      if (parseFloat(cs.borderBottomWidth) > 0 && cs.borderBottomStyle !== 'none')
        if (!clear(cs.borderBottomColor)) ys.push(r.bottom);
    }
    return ys;
  });

// waves counts the wave-dot animations playing inside el.
const waves = (el: Locator) =>
  el.evaluate(
    (node) =>
      node
        .getAnimations({ subtree: true })
        .filter((a) => (a as CSSAnimation).animationName === 'wave').length,
  );

const colours = async (el: Locator) => (await marks(el)).map((d) => d.colour);

// paintOf is the text colour token el has: muted (--color-muted), ink (--color-ink) or neither.
const paintOf = (el: Locator) =>
  el.evaluate((node) => {
    const same = (token: string) => {
      const probe = document.createElement('div');
      probe.style.color = `var(--color-${token})`;
      document.body.append(probe);
      const want = getComputedStyle(probe).color;
      probe.remove();
      return getComputedStyle(node).color === want;
    };
    return same('muted') ? 'muted' : same('ink') ? 'ink' : 'other';
  });

async function approve(threadID: string) {
  const { approval } = await api(m, 'GET', `/api/threads/${threadID}`);
  await api(m, 'POST', `/api/approvals/${approval.id}`, { approve: true });
  await waitStatus(m, threadID, 'done');
}

test.describe('the Bot list', () => {
  test('one column: Now, a hairline, Chief, pinned Bots, the groups in order, Ungrouped, then + and Settings; no rail, thread pane or N more', async ({
    page,
  }) => {
    const bots: Record<string, { id: string }> = {};
    for (const name of ['Ivy', 'Oak', 'Elm', 'Fir']) bots[name] = await newBotAPI(m, name);
    for (let i = 1; i <= 12; i++) await newBotAPI(m, `Spare ${i}`);
    const backend = await api(m, 'POST', '/api/groups', { name: 'Backend' });
    const frontend = await api(m, 'POST', '/api/groups', { name: 'Frontend' });
    await api(m, 'PUT', `/api/bots/${bots.Oak.id}/group`, { group_id: frontend.id });
    await api(m, 'PUT', `/api/bots/${bots.Ivy.id}/group`, { group_id: backend.id });
    await api(m, 'POST', `/api/bots/${bots.Fir.id}/pin`);

    await page.setViewportSize({ width: 1280, height: 1600 });
    await page.goto(`${m.url}/settings`);
    await away(page);
    await expect(nowButton(page)).toBeVisible();
    await expect
      .poll(async () => (await rowOrder(page)).slice(0, 4))
      .toEqual([
        '/bots/chief',
        `/bots/${bots.Fir.id}`,
        `/bots/${bots.Ivy.id}`,
        `/bots/${bots.Oak.id}`,
      ]);
    await expect.poll(() => groupOrder(page)).toEqual(['Backend', 'Frontend', 'Ungrouped']);
    await expect(group(page, 'Ungrouped').getByRole('link', { name: /^Elm\b/ })).toHaveCount(1);
    for (const name of ['Backend', 'Frontend'])
      await expect(group(page, name).getByRole('button', { name, exact: true })).toHaveText(name);

    // One column: every Bot row starts at the same x, as wide as the sidebar allows.
    const nav = await box(sidebar(page));
    expect(Math.abs(nav.width - 260)).toBeLessThanOrEqual(1);
    for (const name of ['Chief', 'Fir', 'Ivy', 'Oak', 'Elm']) {
      const r = await box(sidebarRow(page, name));
      expect(Math.abs(r.x - (await box(sidebarRow(page, 'Chief'))).x), name).toBeLessThanOrEqual(1);
      expect(r.width, name).toBeGreaterThan(nav.width / 2);
    }
    const now = await box(nowButton(page));
    const chief = await box(sidebarRow(page, 'Chief'));
    expect(now.y + now.height).toBeLessThanOrEqual(chief.y + 1);
    const between = (await hairlines(page)).filter(
      (y) => y >= now.y + now.height / 2 && y <= chief.y,
    );
    expect(between.length).toBeGreaterThan(0);

    const plus = sidebar(page).getByRole('button', { name: 'New', exact: true });
    const settings = sidebar(page).getByRole('link', { name: /^Settings\b/ });
    const lastRow = sidebar(page).locator('a[href^="/bots/"]').last();
    const last = await box(lastRow);
    expect((await box(plus)).y).toBeGreaterThanOrEqual(last.y + last.height - 1);
    const s = await box(settings);
    const p = await box(plus);
    expect(s.y + s.height).toBeGreaterThanOrEqual(p.y + p.height);
    await plus.click();
    await expect(page.getByRole('menu').getByRole('menuitem')).toHaveText([
      /^New Bot/,
      /^New group/,
    ]);
    await page.keyboard.press('Escape');

    // In a short window the list scrolls: no rail, no thread pane beside it, no `N more` chip.
    await expect(page.getByRole('region', { name: 'Chief', exact: true })).toHaveCount(0);
    await expect(sidebar(page).getByRole('tablist')).toHaveCount(0);
    await page.setViewportSize({ width: 1280, height: 500 });
    await expect(lastRow).not.toBeInViewport();
    await expect(sidebar(page).getByRole('button', { name: /\bmore\b/ })).toHaveCount(0);
    await lastRow.scrollIntoViewIfNeeded();
    await expect(lastRow).toBeInViewport();
  });

  test('a Bot row is its 36 px creature, name, time and an always-there second line: the last reply, the live verb, the ask, Run failed, Stopped or No messages yet; Needs you wins over failed', async ({
    page,
  }) => {
    await page.emulateMedia({ reducedMotion: 'no-preference' });
    const calm = await newBotAPI(m, 'Calm', 'claude', { look: 'whale' });
    const quiet = await openThreadAPI(m, calm.id, prompts.pong);
    await waitStatus(m, quiet.id, 'done');
    await api(m, 'POST', `/api/threads/${quiet.id}/opened`);
    const fresh = await newBotAPI(m, 'Fresh', 'claude', { look: 'whale' });
    const unread = await openThreadAPI(m, fresh.id, prompts.pong);
    await waitStatus(m, unread.id, 'done');
    await newBotAPI(m, 'Idle', 'claude', { look: 'whale' });
    const worker = await newBotAPI(m, 'Worker', 'claude', { look: 'whale' });
    const busy = await openThreadAPI(m, worker.id, prompts.hang);
    const asker = await newBotAPI(m, 'Asker', 'claude', { look: 'whale' });
    const waiting = await openThreadAPI(m, asker.id, prompts.write);
    const breaker = await newBotAPI(m, 'Breaker', 'claude', { look: 'whale' });
    const failed = await openThreadAPI(m, breaker.id, prompts.none);
    const both = await newBotAPI(m, 'Both', 'claude', { look: 'whale' });
    const bothAsks = await openThreadAPI(m, both.id, prompts.write);
    const bothFails = await openThreadAPI(m, both.id, prompts.none);
    try {
      await waitStatus(m, busy.id, 'running');
      await waitStatus(m, waiting.id, 'waiting');
      await waitStatus(m, failed.id, 'failed');
      await waitStatus(m, bothAsks.id, 'waiting');
      await waitStatus(m, bothFails.id, 'failed');

      await page.goto(`${m.url}/settings`);
      await away(page);

      const activity = async (id: string): Promise<string> =>
        (await api(m, 'GET', '/api/bots')).find((b: { id: string }) => b.id === id).activity;

      const row = sidebarRow(page, 'Calm');
      const creature = row.getByRole('img', { name: 'Whale, idle', exact: true });
      await expect(creature).toBeVisible();
      const c = await box(creature);
      expect(Math.abs(c.width - 36)).toBeLessThanOrEqual(1);
      expect(Math.abs(c.height - 36)).toBeLessThanOrEqual(1);
      const name = await box(row.getByText('Calm', { exact: true }));
      expect(name.x).toBeGreaterThanOrEqual(c.x + c.width - 1);
      expect(name.y + name.height / 2).toBeGreaterThan(c.y);
      expect(name.y + name.height / 2).toBeLessThan(c.y + c.height);

      // The time is on the right of the name, on the name's line.
      const pong = await activity(calm.id);
      expect(pong).toBeTruthy();
      await expect.poll(() => lines(row)).toEqual(['Calm', expect.stringMatching(clock), pong]);
      const [, stamp] = await lines(row);
      const at = await box(row.getByText(stamp, { exact: true }));
      expect(at.x).toBeGreaterThanOrEqual(name.x + name.width - 1);
      expect(Math.abs(at.y + at.height / 2 - (name.y + name.height / 2))).toBeLessThanOrEqual(4);
      expect(await marks(row)).toEqual([]);
      expect(await waves(row)).toBe(0);

      // An idle Bot's reply is muted once its thread was opened and ink while one is unopened.
      expect(await paintOf(row.getByText(pong, { exact: true }))).toBe('muted');
      const unopened = sidebarRow(page, 'Fresh');
      await expect
        .poll(() => lines(unopened))
        .toEqual(['Fresh', expect.stringMatching(clock), pong]);
      expect(await paintOf(unopened.getByText(pong, { exact: true }))).toBe('ink');
      expect(await marks(unopened)).toEqual([]);

      // A Bot that never ran has no time.
      const never = sidebarRow(page, 'Idle');
      await expect.poll(() => lines(never)).toEqual(['Idle', 'No messages yet']);
      expect(await paintOf(never.getByText('No messages yet', { exact: true }))).toBe('muted');

      const working = sidebarRow(page, 'Worker');
      const verb = await activity(worker.id);
      expect(verb).toBeTruthy();
      await expect
        .poll(() => lines(working))
        .toEqual(['Worker', expect.stringMatching(clock), verb]);
      await expect.poll(() => waves(working)).toBeGreaterThan(0);
      await expect(working.getByRole('img', { name: 'Whale, working', exact: true })).toBeVisible();

      const asking = sidebarRow(page, 'Asker');
      await expect
        .poll(() => lines(asking))
        .toEqual(['Asker', expect.stringMatching(clock), editAsk]);
      await expect.poll(() => colours(asking)).toContain('light');
      expect(await colours(asking)).not.toContain('red');
      await expect(
        asking.getByRole('img', { name: 'Whale, needs you', exact: true }),
      ).toBeVisible();

      const failing = sidebarRow(page, 'Breaker');
      await expect
        .poll(() => lines(failing))
        .toEqual(['Breaker', expect.stringMatching(clock), 'Run failed']);
      await expect.poll(() => colours(failing)).toContain('red');
      expect(await colours(failing)).not.toContain('light');

      const wins = sidebarRow(page, 'Both');
      await expect.poll(() => lines(wins)).toEqual(['Both', expect.stringMatching(clock), editAsk]);
      await expect.poll(() => colours(wins)).toContain('light');
      expect(await colours(wins)).not.toContain('red');

      // The ask answered, the row goes back to the Bot's last reply, with no dot.
      await approve(waiting.id);
      const reply = await activity(asker.id);
      expect(reply).toBeTruthy();
      expect(reply).not.toBe(editAsk);
      await expect
        .poll(() => lines(asking))
        .toEqual(['Asker', expect.stringMatching(clock), reply]);
      expect(await colours(asking)).not.toContain('light');

      await stop(busy);
      await waitStatus(m, busy.id, 'stopped');
      await expect
        .poll(() => lines(working))
        .toEqual(['Worker', expect.stringMatching(clock), 'Stopped']);
      expect(await paintOf(working.getByText('Stopped', { exact: true }))).toBe('muted');
      expect(await colours(working)).toEqual([]);
      await expect.poll(() => waves(working)).toBe(0);
    } finally {
      await stop(busy).catch(() => {});
    }
  });

  test('clicking a Bot row opens its latest thread; only that row has the selected fill and aria-current', async ({
    page,
  }) => {
    const pilot = await newBotAPI(m, 'Pilot');
    const older = await openThreadAPI(m, pilot.id, prompts.pong);
    await waitStatus(m, older.id, 'done');
    const newer = await openThreadAPI(m, pilot.id, prompts.keep);
    await waitStatus(m, newer.id, 'done');
    const other = await newBotAPI(m, 'Other');
    const theirs = await openThreadAPI(m, other.id, prompts.pong);
    await waitStatus(m, theirs.id, 'done');

    await page.goto(`${m.url}/settings`);
    await sidebarRow(page, 'Pilot').click();
    await expect(page).toHaveURL(`${m.url}/bots/${pilot.id}/threads/${newer.id}`);
    await expect(threadTab(page, threadTitle(prompts.keep))).toHaveAttribute(
      'aria-selected',
      'true',
    );
    await away(page);
    await expect(sidebarRow(page, 'Pilot')).toHaveAttribute('aria-current', 'page');
    await expect(sidebar(page).locator('[aria-current]')).toHaveCount(1);
    await expect.poll(() => filled(sidebarRow(page, 'Pilot'))).toBe(true);
    expect(await filled(sidebarRow(page, 'Other'))).toBe(false);

    await sidebarRow(page, 'Other').click();
    await expect(page).toHaveURL(`${m.url}/bots/${other.id}/threads/${theirs.id}`);
    await away(page);
    await expect(sidebarRow(page, 'Other')).toHaveAttribute('aria-current', 'page');
    await expect(sidebarRow(page, 'Pilot')).not.toHaveAttribute('aria-current', /./);
    await expect(sidebar(page).locator('[aria-current]')).toHaveCount(1);
    await expect.poll(() => filled(sidebarRow(page, 'Pilot'))).toBe(false);
    expect(await filled(sidebarRow(page, 'Other'))).toBe(true);
  });

  test('right-click on a Bot row opens the Bot menu; Pin to sidebar puts the Bot under Chief, before the groups', async ({
    page,
  }) => {
    const bots: Record<string, { id: string }> = {};
    for (const name of ['Ivy', 'Elm', 'Fir']) bots[name] = await newBotAPI(m, name);
    const ops = await api(m, 'POST', '/api/groups', { name: 'Ops' });
    await api(m, 'PUT', `/api/bots/${bots.Ivy.id}/group`, { group_id: ops.id });
    await api(m, 'POST', `/api/bots/${bots.Fir.id}/pin`);
    await page.goto(`${m.url}/settings`);
    await expect
      .poll(async () => (await rowOrder(page)).slice(0, 3))
      .toEqual(['/bots/chief', `/bots/${bots.Fir.id}`, `/bots/${bots.Ivy.id}`]);

    await sidebarRow(page, 'Elm').click({ button: 'right' });
    await expect(page.getByRole('menu').getByRole('menuitem')).toHaveText([
      /^New thread/,
      /^Pin to sidebar/,
      /^Move to group/,
      /^Bot settings/,
    ]);
    await page.getByRole('menuitem', { name: 'Pin to sidebar' }).click();
    await expect
      .poll(async () => (await rowOrder(page)).slice(0, 4))
      .toEqual([
        '/bots/chief',
        `/bots/${bots.Fir.id}`,
        `/bots/${bots.Elm.id}`,
        `/bots/${bots.Ivy.id}`,
      ]);
    expect((await api(m, 'GET', `/api/bots/${bots.Elm.id}`)).pinned).toBe(true);
  });

  test('⌘B hides and shows the sidebar', async ({ page }) => {
    const bot = await newBotAPI(m, 'Pilot');
    const t = await openThreadAPI(m, bot.id, prompts.pong);
    await waitStatus(m, t.id, 'done');
    await page.setViewportSize({ width: 1280, height: 720 });
    await openThread(page, m, bot.id, t.id);
    await expect(sidebar(page)).toBeVisible();
    const wide = (await box(page.locator('main'))).width;

    await page.getByRole('textbox', { name: /^Message/ }).focus();
    await page.keyboard.press('ControlOrMeta+b');
    await expect(sidebar(page)).toBeHidden();
    await expect.poll(async () => (await box(page.locator('main'))).width).toBeGreaterThan(wide);

    await page.keyboard.press('ControlOrMeta+b');
    await expect(sidebar(page)).toBeVisible();
    await expect.poll(async () => (await box(page.locator('main'))).width).toBe(wide);
  });
});

test.describe('Now', () => {
  test('Now is the first row with the Needs you count; it clears as nothing waits', async ({
    page,
  }) => {
    const first = await newBotAPI(m, 'First');
    const second = await newBotAPI(m, 'Second');
    const third = await newBotAPI(m, 'Third');
    const a = await openThreadAPI(m, first.id, prompts.write);
    const b = await openThreadAPI(m, second.id, prompts.write);
    const c = await openThreadAPI(m, third.id, prompts.none);
    await waitStatus(m, a.id, 'waiting');
    await waitStatus(m, b.id, 'waiting');
    await waitStatus(m, c.id, 'failed');

    await page.goto(`${m.url}/settings`);
    const now = nowButton(page);
    await expect(now).toBeVisible();
    expect(
      await now.evaluate(
        (el, nav) =>
          [...(nav as Element).querySelectorAll('a, button')].find((x) => x.checkVisibility()) ===
          el,
        await sidebar(page).elementHandle(),
      ),
    ).toBe(true);
    await expect(now).toContainText('3');

    await approve(a.id);
    await expect(now).toContainText('2');
    await expect(now).not.toContainText('3');
    await approve(b.id);
    await expect(now).toContainText('1');
    await expect(now).not.toContainText('2');

    await openThread(page, m, third.id, c.id);
    await expect(now).not.toContainText(/\d/);
  });

  test('Now opens its list in the main area: Needs you, Running and Replied across every Bot, each row with the creature and Bot · note', async ({
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
      await openThread(page, m, calm.id, seen.id);

      const now = await openNow(page);
      expect(
        await page
          .locator('main')
          .evaluate((main, n) => main.contains(n as Node), await now.elementHandle()),
      ).toBe(true);
      await expect(page.getByRole('log', { name: 'Messages' })).toHaveCount(0);
      await expect(sidebar(page).locator('a[aria-current]')).toHaveCount(0);
      await expect
        .poll(() =>
          now
            .getByRole('list')
            .evaluateAll((els) => els.map((el) => el.getAttribute('aria-label') ?? '')),
        )
        .toEqual(['Needs you', 'Running', 'Replied']);
      for (const [name, id, bot, img] of [
        ['Needs you', waiting.id, 'Asker', /^Octopus\b/],
        ['Running', busy.id, 'Worker', /^Seal\b/],
        ['Replied', replied.id, 'Newsy', /^Whale\b/],
      ] as const) {
        const row = now
          .getByRole('list', { name, exact: true })
          .getByRole('listitem')
          .filter({ has: page.locator(`a[href$="/threads/${id}"]`) });
        await expect(row).toHaveCount(1);
        await expect(row.getByRole('img', { name: img })).toBeVisible();
        await expect(row).toContainText(new RegExp(`${bot} · \\S`));
      }
      await expect(now.locator(`a[href$="/threads/${seen.id}"]`)).toHaveCount(0);
      await expect(now.getByRole('listitem')).toHaveCount(3);

      await now.locator(`a[href$="/threads/${waiting.id}"]`).click();
      await expect(page).toHaveURL(`${m.url}/bots/${asker.id}/threads/${waiting.id}`);
      await expect(nowView(page)).toHaveCount(0);
      await expect(threadTab(page, threadTitle(prompts.write))).toHaveAttribute(
        'aria-selected',
        'true',
      );
      await expect(sidebarRow(page, 'Asker')).toHaveAttribute('aria-current', 'page');
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
    await openThread(page, m, bot.id, t.id);
    const now = await openNow(page);
    await expect(now).toContainText('Nothing needs you');
    await expect(now.getByRole('listitem')).toHaveCount(0);
    await expect(nowButton(page)).not.toContainText(/\d/);
  });
});
