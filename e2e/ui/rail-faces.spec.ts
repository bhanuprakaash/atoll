// The rail of faces only (#572): 64 px, a face is the Bot's creature with no name, the hover label
// names the Bot with its group and state, a red pip for failed, an `N more` chip for faces below the
// fold, the bar alone marks the current Bot or Now, and Now is the count of threads waiting.
import { expect, type Locator, type Page, test } from '@playwright/test';
import {
  api,
  type Machine,
  newBotAPI,
  nowPane,
  openThreadAPI,
  prompts,
  sidebarRow,
  startAtoll,
  threadPane,
  waitStatus,
} from './atoll';

let m: Machine;
test.beforeEach(async () => {
  m = await startAtoll();
});
test.afterEach(() => m?.stop());

const danger = 'rgb(229, 72, 77)';
const accent = 'rgb(236, 236, 236)';

const rail = (page: Page) => page.getByRole('navigation', { name: 'Bots' });
const nowButton = (page: Page) => rail(page).getByRole('button', { name: /^Now\b/ });
const faces = (page: Page) => rail(page).locator('a[href^="/bots/"]');
const chip = (page: Page) => rail(page).getByRole('button', { name: /^\d+ more\b/ });
const box = async (l: Locator) => (await l.boundingBox())!;
const away = (page: Page) => page.mouse.move(1270, 5);

async function grouped(botID: string, groupID: string) {
  await api(m, 'PUT', `/api/bots/${botID}/group`, { group_id: groupID });
}

async function failedBot(name: string, extra: Record<string, unknown> = {}) {
  const bot = await newBotAPI(m, name, 'claude', extra);
  const t = await openThreadAPI(m, bot.id, prompts.none);
  await waitStatus(m, t.id, 'failed');
  return { bot, thread: t };
}

async function askingBot(name: string, extra: Record<string, unknown> = {}) {
  const bot = await newBotAPI(m, name, 'claude', extra);
  const t = await openThreadAPI(m, bot.id, prompts.write);
  await waitStatus(m, t.id, 'waiting');
  return { bot, thread: t };
}

async function approve(threadID: string) {
  const { approval } = await api(m, 'GET', `/api/threads/${threadID}`);
  await api(m, 'POST', `/api/approvals/${approval.id}`, { approve: true });
  await waitStatus(m, threadID, 'done');
}

// labelLines is the label beside the rail, one entry per line.
const labelLines = (page: Page) =>
  page.getByRole('tooltip').evaluate((el) =>
    (el as HTMLElement).innerText
      .split('\n')
      .map((s) => s.trim())
      .filter(Boolean),
  );

// dots lists the small round marks inside el drawn red (danger) or light (accent), with their
// centres.
const dots = (el: Locator) =>
  el.evaluate(
    (root, [red, light]) => {
      const found: { colour: 'red' | 'light'; x: number; y: number }[] = [];
      for (const e of [root, ...root.querySelectorAll('*')]) {
        const r = e.getBoundingClientRect();
        if (!r.width || r.width > 16 || Math.abs(r.width - r.height) > 2) continue;
        const cs = getComputedStyle(e);
        if (cs.display === 'none' || cs.visibility === 'hidden') continue;
        const paints = [cs.backgroundColor, e instanceof SVGElement ? cs.fill : ''];
        const colour = paints.includes(red) ? 'red' : paints.includes(light) ? 'light' : null;
        if (colour) found.push({ colour, x: r.x + r.width / 2, y: r.y + r.height / 2 });
      }
      return found;
    },
    [danger, accent],
  );

// spot is a Bot's one dot of the given colour, relative to its creature's top left.
async function spot(face: Locator, colour: 'red' | 'light') {
  const creature = await box(face.getByRole('img').first());
  const all = await dots(face);
  const mine = all.filter((d) => d.colour === colour);
  expect(mine, JSON.stringify(all)).toHaveLength(1);
  return { x: mine[0].x - creature.x, y: mine[0].y - creature.y };
}

// bars lists the 3 px bars at the rail's left edge, top and bottom.
const bars = (page: Page) =>
  rail(page).evaluate((nav) => {
    const left = nav.getBoundingClientRect().left;
    const found: { top: number; bottom: number }[] = [];
    for (const e of nav.querySelectorAll('*')) {
      const r = e.getBoundingClientRect();
      const bg = getComputedStyle(e).backgroundColor;
      if (Math.abs(r.width - 3) > 0.5 || r.height < 12 || Math.abs(r.left - left) > 0.5) continue;
      if (bg === 'transparent' || bg === 'rgba(0, 0, 0, 0)') continue;
      found.push({ top: r.top, bottom: r.bottom });
    }
    return found;
  });

async function expectBarOn(page: Page, target: Locator) {
  const t = await box(target);
  await expect.poll(() => bars(page)).toHaveLength(1);
  const [bar] = await bars(page);
  const mid = (bar.top + bar.bottom) / 2;
  expect(mid).toBeGreaterThanOrEqual(t.y);
  expect(mid).toBeLessThanOrEqual(t.y + t.height);
}

// hairlines lists the y of every horizontal hairline drawn on the rail: a line up to 1.5 px tall,
// or a top or bottom border.
const hairlines = (page: Page) =>
  rail(page).evaluate((nav) => {
    const clear = (c: string) => c === 'transparent' || c === 'rgba(0, 0, 0, 0)';
    const ys: number[] = [];
    for (const e of nav.querySelectorAll('*')) {
      const r = e.getBoundingClientRect();
      const cs = getComputedStyle(e);
      if (r.width < 12) continue;
      if (r.height > 0 && r.height <= 1.5 && !clear(cs.backgroundColor)) ys.push(r.top);
      if (
        parseFloat(cs.borderTopWidth) > 0 &&
        cs.borderTopStyle !== 'none' &&
        !clear(cs.borderTopColor)
      )
        ys.push(r.top);
      if (
        parseFloat(cs.borderBottomWidth) > 0 &&
        cs.borderBottomStyle !== 'none' &&
        !clear(cs.borderBottomColor)
      )
        ys.push(r.bottom);
    }
    return ys;
  });

// accentText is the text of each element inside el filled with the light accent.
const accentText = (el: Locator) =>
  el.evaluate(
    (root, light) =>
      [root, ...root.querySelectorAll('*')]
        .filter((e) => getComputedStyle(e).backgroundColor === light)
        .map((e) => (e as HTMLElement).innerText?.trim() ?? ''),
    accent,
  );

// fold counts the faces below the rail's fold: wholly below it, and not wholly in view.
const fold = (page: Page) =>
  faces(page).evaluateAll((els) => {
    const scroller = (el: Element) => {
      for (let n = el.parentElement; n; n = n.parentElement)
        if (/(auto|scroll)/.test(getComputedStyle(n).overflowY)) return n;
      return document.documentElement;
    };
    const bottom = scroller(els[0]).getBoundingClientRect().bottom;
    const rects = els.map((el) => el.getBoundingClientRect());
    return {
      below: rects.filter((r) => r.top >= bottom - 0.5).length,
      cut: rects.filter((r) => r.bottom > bottom + 0.5).length,
      hidden: els
        .filter((_, i) => rects[i].bottom > bottom + 0.5)
        .map((el) => el.getAttribute('href')!),
    };
  });

const chipCount = async (page: Page) =>
  Number(
    ((await chip(page).getAttribute('aria-label')) ?? (await chip(page).innerText())).match(
      /\d+/,
    )![0],
  );

test('the rail is 64 px; a face is the 40 px creature with no name under it; no group name shows', async ({
  page,
}) => {
  const bots: Record<string, { id: string }> = {};
  for (const name of ['Ivy', 'Oak', 'Elm']) bots[name] = await newBotAPI(m, name);
  const backend = await api(m, 'POST', '/api/groups', { name: 'Backend' });
  const frontend = await api(m, 'POST', '/api/groups', { name: 'Frontend' });
  await grouped(bots.Ivy.id, backend.id);
  await grouped(bots.Oak.id, frontend.id);

  await page.goto(`${m.url}/settings`);
  await expect(faces(page)).toHaveCount(4);
  expect(Math.abs((await box(rail(page))).width - 64)).toBeLessThanOrEqual(0.5);
  for (const face of await faces(page).all()) {
    const creature = await box(face.getByRole('img').first());
    expect(Math.abs(creature.width - 40)).toBeLessThanOrEqual(1);
    expect(Math.abs(creature.height - 40)).toBeLessThanOrEqual(1);
    expect((await face.innerText()).trim()).toBe('');
  }
  for (const name of ['Chief', 'Ivy', 'Oak', 'Elm'])
    await expect(sidebarRow(page, name)).toHaveCount(1);
  const text = await rail(page).innerText();
  for (const name of ['Chief', 'Ivy', 'Oak', 'Elm', 'Backend', 'Frontend', 'Ungrouped'])
    expect(text).not.toContain(name);
  for (const name of ['Backend', 'Frontend', 'Ungrouped']) {
    const line = rail(page)
      .getByRole('group', { name, exact: true })
      .getByRole('button', { name, exact: true });
    expect((await box(line)).height).toBeLessThanOrEqual(16);
  }
});

test('hover or focus on a face shows its name and, under it, group · state; the state alone with no group; the name alone with neither', async ({
  page,
}) => {
  const frontend = await api(m, 'POST', '/api/groups', { name: 'Frontend' });
  const pebble = await failedBot('Pebble');
  await grouped(pebble.bot.id, frontend.id);
  const asker = await askingBot('Asker');
  await grouped(asker.bot.id, frontend.id);
  const oak = await newBotAPI(m, 'Oak');
  await grouped(oak.id, frontend.id);
  await failedBot('Breaker');
  await newBotAPI(m, 'Calm');

  await page.goto(`${m.url}/settings`);
  for (const [name, lines] of [
    ['Pebble', ['Pebble', 'Frontend · Run failed']],
    ['Asker', ['Asker', 'Frontend · Needs you']],
    ['Oak', ['Oak', 'Frontend']],
    ['Breaker', ['Breaker', 'Run failed']],
    ['Calm', ['Calm']],
  ] as const) {
    await sidebarRow(page, name).hover();
    await expect.poll(() => labelLines(page), { message: name }).toEqual(lines);
    await away(page);
    await expect(page.getByRole('tooltip')).toHaveCount(0);
  }

  await sidebarRow(page, 'Pebble').focus();
  await expect.poll(() => labelLines(page)).toEqual(['Pebble', 'Frontend · Run failed']);
});

test('a failed Bot shows a red pip where Needs you shows its light dot; Needs you wins when a Bot has both', async ({
  page,
}) => {
  await page.emulateMedia({ reducedMotion: 'reduce' });
  await failedBot('Breaker', { look: 'whale' });
  await askingBot('Asker', { look: 'whale' });
  const both = await askingBot('Both', { look: 'whale' });
  const failed = await openThreadAPI(m, both.bot.id, prompts.none);
  await waitStatus(m, failed.id, 'failed');
  expect((await api(m, 'GET', `/api/bots/${both.bot.id}`)).state).toBe('needs_you');

  await page.goto(`${m.url}/settings`);
  await away(page);
  const breaker = sidebarRow(page, 'Breaker');
  await expect(breaker.getByRole('img', { name: 'Whale, failed', exact: true })).toBeVisible();
  await expect(
    sidebarRow(page, 'Both').getByRole('img', { name: 'Whale, needs you', exact: true }),
  ).toBeVisible();

  const light = await spot(sidebarRow(page, 'Asker'), 'light');
  const red = await spot(breaker, 'red');
  expect(Math.abs(red.x - light.x)).toBeLessThanOrEqual(1.5);
  expect(Math.abs(red.y - light.y)).toBeLessThanOrEqual(1.5);
  expect((await dots(breaker)).filter((d) => d.colour === 'light')).toHaveLength(0);

  const asker = sidebarRow(page, 'Asker');
  expect((await dots(asker)).filter((d) => d.colour === 'red')).toHaveLength(0);
  const wins = sidebarRow(page, 'Both');
  expect(await spot(wins, 'light')).toEqual(light);
  expect((await dots(wins)).filter((d) => d.colour === 'red')).toHaveLength(0);
});

test('faces below the fold show an N more chip above +, with the light dot when one needs you; clicking it scrolls to them; with every face in view there is no chip', async ({
  page,
}) => {
  const bots: { id: string }[] = [];
  for (let i = 1; i <= 14; i++) bots.push(await newBotAPI(m, `Face ${i}`));
  await page.setViewportSize({ width: 1280, height: 500 });
  await page.goto(`${m.url}/settings`);
  await expect(faces(page)).toHaveCount(15);
  await away(page);
  expect((await fold(page)).below).toBeGreaterThan(0);

  await expect(chip(page)).toBeVisible();
  const { below, cut } = await fold(page);
  expect([below, cut]).toContain(await chipCount(page));
  await expect(chip(page)).toHaveAccessibleName(`${await chipCount(page)} more`);
  expect((await dots(chip(page))).filter((d) => d.colour === 'light')).toHaveLength(0);
  const c = await box(chip(page));
  const plus = await box(rail(page).getByRole('button', { name: 'New', exact: true }));
  expect(c.y + c.height).toBeLessThanOrEqual(plus.y);
  expect(c.x).toBeGreaterThanOrEqual(0);
  expect(c.x + c.width).toBeLessThanOrEqual(64);

  const last = bots[bots.length - 1];
  const t = await openThreadAPI(m, last.id, prompts.write);
  await waitStatus(m, t.id, 'waiting');
  await expect(sidebarRow(page, 'Face 14')).toHaveAttribute('data-marker', 'needs-you');
  await expect(chip(page)).toHaveAccessibleName(`${await chipCount(page)} more, needs you`);
  await expect
    .poll(async () => (await dots(chip(page))).filter((d) => d.colour === 'light').length)
    .toBe(1);

  const { hidden } = await fold(page);
  await chip(page).click();
  await expect
    .poll(async () => {
      const now = (await fold(page)).hidden;
      return hidden.some((href) => !now.includes(href));
    })
    .toBe(true);

  await page.setViewportSize({ width: 1280, height: 2000 });
  await expect.poll(async () => (await fold(page)).cut).toBe(0);
  await expect(chip(page)).toHaveCount(0);
});

test('the current Bot is marked by the 3 px bar alone, with no fill', async ({ page }) => {
  const pilot = await newBotAPI(m, 'Pilot');
  await newBotAPI(m, 'Other');
  const t = await openThreadAPI(m, pilot.id, prompts.pong);
  await waitStatus(m, t.id, 'done');
  await page.goto(`${m.url}/bots/${pilot.id}/threads/${t.id}`);
  await expect(
    page.getByRole('log', { name: 'Messages' }).getByRole('article').first(),
  ).toBeVisible();
  await away(page);

  const face = sidebarRow(page, 'Pilot');
  await expect(face).toHaveAttribute('data-current', 'true');
  await expectBarOn(page, face);
  const fill = await face.evaluate((el) => getComputedStyle(el).backgroundColor);
  expect(fill).toBe(
    await sidebarRow(page, 'Other').evaluate((el) => getComputedStyle(el).backgroundColor),
  );
  expect(['transparent', 'rgba(0, 0, 0, 0)']).toContain(fill);
  expect((await face.innerText()).trim()).toBe('');
});

test('Now is the count of threads in Needs you or failed on the light accent, with no icon and no badge; with none it is the grey mark; a hairline separates it from Chief', async ({
  page,
}) => {
  const first = await askingBot('First');
  const second = await askingBot('Second');
  const third = await failedBot('Third');
  const waiting = async () =>
    (await api(m, 'GET', '/api/bots'))
      .flatMap((b: { threads?: { state: string }[] }) => b.threads ?? [])
      .filter((t: { state: string }) => t.state === 'needs_you' || t.state === 'failed').length;
  expect(await waiting()).toBe(3);

  await page.goto(`${m.url}/settings`);
  await away(page);
  const now = nowButton(page);
  await expect(now).toHaveText('3');
  await expect(now).toHaveAccessibleName('Now, 3 waiting');
  await expect(now.locator('svg')).toHaveCount(0);
  await expect(now.locator('[data-badge]')).toHaveCount(0);
  expect(await accentText(now)).toContain('3');

  const n = await box(now);
  const chief = await box(sidebarRow(page, 'Chief').getByRole('img').first());
  const between = (await hairlines(page)).filter((y) => y > n.y + n.height / 2 && y < chief.y);
  expect(between.length).toBeGreaterThan(0);

  await approve(first.thread.id);
  await expect(now).toHaveText('2');
  await expect(now).toHaveAccessibleName('Now, 2 waiting');
  await approve(second.thread.id);
  await expect(now).toHaveText('1');

  await page.goto(`${m.url}/bots/${third.bot.id}/threads/${third.thread.id}`);
  await expect(
    page.getByRole('log', { name: 'Messages' }).getByRole('article').first(),
  ).toBeVisible();
  await expect.poll(waiting).toBe(0);
  await expect(now).toHaveText('');
  await expect(now).toHaveAccessibleName('Now');
  expect(await accentText(now)).toEqual([]);
});

test('while Now’s pane shows, the rail bar is on Now, not on a Bot', async ({ page }) => {
  const calm = await newBotAPI(m, 'Calm');
  const seen = await openThreadAPI(m, calm.id, prompts.pong);
  await waitStatus(m, seen.id, 'done');
  const asker = await askingBot('Asker');

  await page.goto(`${m.url}/bots/${calm.id}/threads/${seen.id}`);
  await expect(
    page.getByRole('log', { name: 'Messages' }).getByRole('article').first(),
  ).toBeVisible();
  const pane = await nowPane(page);
  await away(page);
  await expect(nowButton(page)).toHaveAttribute('data-current', 'true');
  await expect(rail(page).locator('[data-current="true"]')).toHaveCount(1);
  await expectBarOn(page, nowButton(page));

  await pane.locator(`a[href$="/threads/${asker.thread.id}"]`).click();
  await expect(page).toHaveURL(`${m.url}/bots/${asker.bot.id}/threads/${asker.thread.id}`);
  await expect(threadPane(page, 'Now')).toBeVisible();
  await away(page);
  await expect(nowButton(page)).toHaveAttribute('data-current', 'true');
  await expect(sidebarRow(page, 'Asker')).not.toHaveAttribute('data-current', 'true');
  await expect(rail(page).locator('[data-current="true"]')).toHaveCount(1);
  await expectBarOn(page, nowButton(page));
});

test('the thread pane and the narrow drawer sit against the 64 px rail with no gap and no overlap', async ({
  page,
}) => {
  const pilot = await newBotAPI(m, 'Pilot');
  const t = await openThreadAPI(m, pilot.id, prompts.pong);
  await waitStatus(m, t.id, 'done');
  const pane = threadPane(page, 'Pilot');
  const edge = async () => {
    const r = await box(rail(page));
    expect(r.x).toBe(0);
    expect(Math.abs(r.width - 64)).toBeLessThanOrEqual(0.5);
    expect(Math.abs((await box(pane)).x - (r.x + r.width))).toBeLessThanOrEqual(0.5);
  };

  await page.setViewportSize({ width: 1280, height: 720 });
  await page.goto(`${m.url}/bots/${pilot.id}/threads/${t.id}`);
  await expect(pane).toBeVisible();
  await edge();

  await page.setViewportSize({ width: 700, height: 500 });
  await expect(pane).toBeHidden();
  const main = await box(page.locator('main'));
  expect(Math.abs(main.x - 64)).toBeLessThanOrEqual(0.5);
  await sidebarRow(page, 'Pilot').click();
  await expect(pane).toBeVisible();
  await edge();
});
