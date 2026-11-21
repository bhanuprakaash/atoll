// Settings › Usage: what Bots used over a range, read from GET /api/usage. Design:
// docs/design.md, "Settings > Usage". Seeded with the run-cost recordings: Costly (Claude, $0.19
// then $0.05), Cheap (Claude, $0.19) and Codexer (Codex, no price). Tokens are input + output +
// cache read + cache write: Costly 126,901, Cheap 85,828, Codexer 46,900.
import { expect, type Locator, type Page, test } from '@playwright/test';
import {
  type Machine,
  newBotAPI,
  openThreadAPI,
  prompts,
  sendMessageAPI,
  sidebarRow,
  startAtoll,
  waitStatus,
} from './atoll';

const first = 'Cost check: look around the folder six times, then answer.';
const second = 'Cost check, second round: look again and answer.';
const months = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec'];

const settingsNav = (page: Page) => page.getByRole('navigation', { name: 'Settings' });
const totals = (page: Page) => page.getByRole('group', { name: 'Totals' });
const range = (page: Page) => page.getByRole('radiogroup', { name: 'Range' });
const perDay = (page: Page) => page.getByRole('list', { name: /^Per day/ });
const byBot = (page: Page) => page.getByRole('table', { name: 'By Bot' });
const byModel = (page: Page) => page.getByRole('table', { name: 'By model' });

// usageRequest waits for the page's GET /api/usage with the range and the zone offset in minutes
// east of UTC; the browser runs in this process's zone.
function usageRequest(page: Page, want: string) {
  const offset = String(-new Date().getTimezoneOffset());
  return page.waitForResponse(
    (r) => {
      const u = new URL(r.url());
      return (
        u.pathname === '/api/usage' &&
        u.searchParams.get('range') === want &&
        u.searchParams.get('tz_offset') === offset
      );
    },
    { timeout: 15_000 },
  );
}

// bodyRows is the text of each cell of every row under the header.
const bodyRows = (table: Locator) =>
  table.evaluate((t) =>
    [...t.querySelectorAll('tbody tr')].map((tr) =>
      [...tr.querySelectorAll('td, th')].map((c) => (c as HTMLElement).innerText.trim()),
    ),
  );

// days are the bar labels the range draws, oldest first: "21 Nov" for today.
const days = (page: Page, n: number) =>
  page.evaluate(
    ([n, months]) =>
      Array.from({ length: n }, (_, i) => {
        const d = new Date();
        d.setDate(d.getDate() - (n - 1 - i));
        return `${d.getDate()} ${months[d.getMonth()]}`;
      }),
    [n, months] as const,
  );

const barLabels = (page: Page) =>
  perDay(page)
    .getByRole('listitem')
    .evaluateAll((items) => items.map((i) => i.getAttribute('aria-label')));

// scrollBox is the nearest ancestor of el that scrolls on axis.
const scrollBox = (el: Locator, axis: 'x' | 'y') =>
  el.evaluate((node, axis) => {
    for (let e = node.parentElement; e; e = e.parentElement) {
      const o = getComputedStyle(e)[axis === 'x' ? 'overflowX' : 'overflowY'];
      if (o === 'auto' || o === 'scroll') {
        const r = e.getBoundingClientRect();
        return {
          left: r.left,
          right: r.right,
          ownBox: !e.querySelector('h2') && e.querySelectorAll('table').length === 1,
        };
      }
    }
    return null;
  }, axis);

test.describe('with runs', () => {
  let m: Machine;
  let costly: { id: string };
  let costlyThread: { id: string };

  test.beforeAll(async () => {
    m = await startAtoll({
      fakes: ['claude', 'codex'],
      signedIn: ['claude', 'codex'],
    });
    costly = await newBotAPI(m, 'Costly', 'claude', { colour: 'teal' });
    const cheap = await newBotAPI(m, 'Cheap', 'claude', { colour: 'pink' });
    const codexer = await newBotAPI(m, 'Codexer', 'codex', { colour: 'sky' });
    await newBotAPI(m, 'Resting', 'claude');
    costlyThread = await openThreadAPI(m, costly.id, first);
    const cheapThread = await openThreadAPI(m, cheap.id, first);
    const codexThread = await openThreadAPI(m, codexer.id, first);
    for (const t of [costlyThread, cheapThread, codexThread]) await waitStatus(m, t.id, 'done');
    await sendMessageAPI(m, costlyThread.id, second);
    await sendMessageAPI(m, codexThread.id, second);
  });
  test.afterAll(() => m?.stop());

  test('Usage is the last Settings entry and opens /settings/usage', async ({ page }) => {
    await page.goto(`${m.url}/settings/general`);
    const links = settingsNav(page).getByRole('link');
    await expect(links.last()).toHaveAccessibleName('Usage');
    await links.last().click();
    await expect(page).toHaveURL(`${m.url}/settings/usage`);
    await expect(page.getByRole('heading', { name: 'Usage', exact: true })).toBeVisible();
  });

  test('the range starts on 7 days, and each choice asks for its own range', async ({ page }) => {
    let res = usageRequest(page, '7d');
    await page.goto(`${m.url}/settings/usage`);
    let body = await (await res).json();
    const radios = ['24h', '7 days', '30 days', 'All'];
    for (const name of radios)
      await expect(range(page).getByRole('radio', { name, exact: true })).toBeVisible();
    await expect(range(page).getByRole('radio', { name: '7 days' })).toHaveAttribute(
      'aria-checked',
      'true',
    );
    expect(body.total.runs).toBe(5);
    await expect(totals(page).getByText('5', { exact: true })).toBeVisible();

    for (const [name, param] of [
      ['30 days', '30d'],
      ['All', 'all'],
      ['24h', '24h'],
    ]) {
      res = usageRequest(page, param);
      await range(page).getByRole('radio', { name, exact: true }).click();
      body = await (await res).json();
      await expect(range(page).getByRole('radio', { name, exact: true })).toHaveAttribute(
        'aria-checked',
        'true',
      );
      for (const other of radios.filter((r) => r !== name))
        await expect(range(page).getByRole('radio', { name: other, exact: true })).toHaveAttribute(
          'aria-checked',
          'false',
        );
      expect(body.total.runs).toBe(5);
      await expect(totals(page).getByText('$0.43', { exact: true })).toBeVisible();
      await expect(totals(page).getByText('5', { exact: true })).toBeVisible();
    }
  });

  test('totals show cost at API prices for Claude only, tokens and runs, and a bar per day', async ({
    page,
  }) => {
    await page.goto(`${m.url}/settings/usage`);
    const used = totals(page);
    await expect(used.getByText('$0.43', { exact: true })).toBeVisible();
    await expect(used.getByText('at API prices · Claude only', { exact: true })).toBeVisible();
    await expect(used.getByText('259.6k', { exact: true })).toBeVisible();
    await expect(used.getByText(/^tokens/)).toBeVisible();
    await expect(used.getByText('5', { exact: true })).toBeVisible();
    await expect(used.getByText('runs', { exact: true })).toBeVisible();

    const week = await days(page, 7);
    await expect.poll(() => barLabels(page)).toHaveLength(7);
    expect(await barLabels(page)).toEqual([
      ...week.slice(0, 6).map((d) => `${d}, no runs`),
      `${week[6]}, $0.43`,
    ]);

    await range(page).getByRole('radio', { name: '30 days' }).click();
    const month = await days(page, 30);
    await expect
      .poll(() => barLabels(page))
      .toEqual([...month.slice(0, 29).map((d) => `${d}, no runs`), `${month[29]}, $0.43`]);
  });

  test('By Bot lists each Bot with runs, highest cost first, with its colour dot', async ({
    page,
  }) => {
    await page.goto(`${m.url}/settings/usage`);
    const table = byBot(page);
    await expect(table).toBeVisible();
    for (const name of ['Bot', 'Runs', 'Tokens', 'Cost'])
      await expect(table.getByRole('columnheader', { name, exact: true })).toBeVisible();
    const rows = await bodyRows(table);
    expect(rows.map((r) => r.slice(0, 3))).toEqual([
      ['Costly', '2', '126.9k'],
      ['Cheap', '1', '85.8k'],
      ['Codexer', '2', '46.9k'],
    ]);
    expect(rows[0][3]).toBe('$0.24');
    expect(rows[1][3]).toBe('$0.19');

    for (const [name, token] of [
      ['Costly', 'bot-teal'],
      ['Cheap', 'bot-pink'],
      ['Codexer', 'bot-sky'],
    ]) {
      const row = table.getByRole('row').filter({ hasText: name });
      const dots = await row.evaluate((tr, token) => {
        const probe = document.createElement('div');
        probe.style.background = `var(--color-${token})`;
        document.body.append(probe);
        const want = getComputedStyle(probe).backgroundColor;
        probe.remove();
        return [...tr.querySelectorAll('*')].filter((e) => {
          const r = e.getBoundingClientRect();
          return (
            r.width > 0 &&
            r.width <= 12 &&
            Math.abs(r.width - r.height) <= 1 &&
            getComputedStyle(e).backgroundColor === want
          );
        }).length;
      }, token);
      expect(dots, name).toBe(1);
    }
  });

  test('By model names a Claude id as on the run footer, and a Codex model has no cost', async ({
    page,
  }) => {
    await page.goto(`${m.url}/settings/usage`);
    const table = byModel(page);
    await expect(table).toBeVisible();
    for (const name of ['Model', 'Tokens', 'Cost'])
      await expect(table.getByRole('columnheader', { name, exact: true })).toBeVisible();
    expect(await bodyRows(table)).toEqual([
      ['Sonnet 5.5', '212.7k', '$0.43'],
      ['gpt-5.6-sol', '46.9k', '—'],
    ]);
  });

  test('at 375 px the nav is a row on top, the tables stack and scroll in their box, the page never sideways', async ({
    page,
  }) => {
    await page.setViewportSize({ width: 375, height: 800 });
    await page.goto(`${m.url}/settings/usage`);
    await expect(byBot(page)).toBeVisible();
    await expect(byModel(page)).toBeVisible();

    const nav = (await settingsNav(page).boundingBox())!;
    const heading = page.getByRole('heading', { name: 'Usage', exact: true });
    expect(nav.y + nav.height).toBeLessThanOrEqual((await heading.boundingBox())!.y + 1);
    const links = settingsNav(page).getByRole('link');
    const firstLink = (await links.first().boundingBox())!;
    const lastLink = (await links.last().boundingBox())!;
    expect(Math.abs(firstLink.y - lastLink.y)).toBeLessThanOrEqual(2);
    expect(lastLink.x).toBeGreaterThan(firstLink.x);

    const bot = (await byBot(page).boundingBox())!;
    const model = (await byModel(page).boundingBox())!;
    expect(bot.y + bot.height).toBeLessThanOrEqual(model.y);

    const width = await page.evaluate(() => window.innerWidth);
    for (const table of [byBot(page), byModel(page)]) {
      const box = await scrollBox(table, 'x');
      expect(box).not.toBeNull();
      expect(box!.ownBox).toBe(true);
      expect(box!.left).toBeGreaterThanOrEqual(0);
      expect(box!.right).toBeLessThanOrEqual(width);
    }

    expect(
      await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth),
    ).toBe(true);
    const sideways = await heading.evaluate((h) => {
      const over: string[] = [];
      for (let e = h.parentElement; e; e = e.parentElement) {
        if (e.scrollWidth > e.clientWidth + 1) over.push(e.tagName);
        if (e.tagName === 'MAIN') break;
      }
      return over;
    });
    expect(sideways).toEqual([]);
  });

  test('the run footer and the sidebar still show no cost', async ({ page }) => {
    await page.goto(`${m.url}/bots/${costly.id}/threads/${costlyThread.id}`);
    const log = page.getByRole('log', { name: 'Messages' });
    await expect(log.getByText(/Sonnet 5\.5, 2 calls$/)).toHaveCount(1);
    expect(await log.innerText()).not.toContain('$');
    for (const name of ['Costly', 'Cheap', 'Codexer', 'Resting'])
      await expect(sidebarRow(page, name)).not.toContainText('$');
  });
});

test.describe('with many Bots', () => {
  let m: Machine;

  test.beforeAll(async () => {
    m = await startAtoll();
    const threads = await Promise.all(
      Array.from({ length: 24 }, async (_, i) => {
        const bot = await newBotAPI(m, `Pinger ${i + 1}`);
        return openThreadAPI(m, bot.id, prompts.pong);
      }),
    );
    for (const t of threads) await waitStatus(m, t.id, 'done');
  });
  test.afterAll(() => m?.stop());

  test('a long By Bot list scrolls in its own box and its header stays', async ({ page }) => {
    await page.goto(`${m.url}/settings/usage`);
    const table = byBot(page);
    await expect(table).toBeVisible();
    expect(await bodyRows(table)).toHaveLength(24);
    expect((await scrollBox(table, 'y'))?.ownBox).toBe(true);

    const header = table.getByRole('columnheader', {
      name: 'Bot',
      exact: true,
    });
    expect(await header.evaluate((th) => getComputedStyle(th).position)).toBe('sticky');
    const moved = await table.evaluate((t) => {
      let box = t.parentElement!;
      while (!['auto', 'scroll'].includes(getComputedStyle(box).overflowY))
        box = box.parentElement!;
      box.scrollIntoView({ block: 'end' });
      const scrolls = box.scrollHeight > box.clientHeight;
      box.scrollTop = box.scrollHeight;
      return {
        scrolls,
        top: box.getBoundingClientRect().top,
        scrolled: box.scrollTop > 0,
      };
    });
    expect(moved.scrolls).toBe(true);
    expect(moved.scrolled).toBe(true);
    await expect
      .poll(async () => Math.abs((await header.boundingBox())!.y - moved.top))
      .toBeLessThanOrEqual(2);
    await expect(table.getByRole('row').last()).toBeInViewport();
  });
});

test.describe('with no runs', () => {
  let m: Machine;

  test.beforeAll(async () => {
    m = await startAtoll();
    await newBotAPI(m, 'Idle');
  });
  test.afterAll(() => m?.stop());

  test('a range with no runs shows one plain line, not empty tables', async ({ page }) => {
    const res = usageRequest(page, '7d');
    await page.goto(`${m.url}/settings/usage`);
    await res;
    await expect(page.getByText('No runs in this range.', { exact: true })).toBeVisible();
    await expect(range(page)).toBeVisible();
    await expect(page.getByRole('table')).toHaveCount(0);
    await expect(perDay(page)).toHaveCount(0);
    await expect(totals(page)).toHaveCount(0);
  });
});
