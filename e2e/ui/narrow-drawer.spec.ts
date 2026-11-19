// Narrow windows (#472): below 900 px the 64 px rail stays docked and the thread pane floats over
// the thread column, closed by default; the whole-sidebar drawer and its Sidebar button are gone.
// Design: docs/design.md, "Narrow windows". The floating
// pane's opening and closing is in sidebar-rail.spec.ts.
import { expect, type Locator, type Page, test } from '@playwright/test';
import {
  api,
  type Machine,
  newBotAPI,
  openThreadAPI,
  prompts,
  sidebarRow,
  startAtoll,
  threadPane,
} from './atoll';

let m: Machine;
let pilot: { id: string };
let first: { id: string };

test.beforeAll(async () => {
  m = await startAtoll();
  pilot = await newBotAPI(m, 'Pilot');
  first = await openThreadAPI(m, pilot.id, prompts.keep);
  await expect
    .poll(async () => (await api(m, 'GET', `/api/threads/${first.id}`)).run_status, {
      timeout: 15_000,
    })
    .toBe('done');
});
test.afterAll(() => m?.stop());

const rail = (page: Page) => page.getByRole('navigation', { name: 'Bots' });
const pane = (page: Page) => threadPane(page, 'Pilot');
const toggle = (page: Page) => page.getByRole('button', { name: 'Sidebar', exact: true });
const handle = (page: Page) => page.getByRole('separator', { name: 'Resize thread pane' });
const box = async (l: Locator) => (await l.boundingBox())!;
const pilotThread = () => `${m.url}/bots/${pilot.id}/threads/${first.id}`;

async function openAt(page: Page, width: number, height: number, url = pilotThread()) {
  await page.setViewportSize({ width, height });
  await page.goto(url);
  await expect(page.getByRole('textbox', { name: /^Message/ })).toBeVisible();
}

async function sideScroll(page: Page) {
  return page.evaluate(() => {
    const root = document.documentElement;
    const main = document.querySelector('main')!;
    return {
      page: Math.max(root.scrollWidth, document.body.scrollWidth) - window.innerWidth,
      main: main.scrollWidth - main.clientWidth,
    };
  });
}

test('at 700x500 and 600x800 the rail stays docked, the thread column fills the rest, the pane is closed and there is no Sidebar button', async ({
  page,
}) => {
  for (const [width, height] of [
    [700, 500],
    [600, 800],
  ]) {
    await openAt(page, width, height);
    const r = await box(rail(page));
    expect(r.x).toBe(0);
    expect(Math.abs(r.width - 64)).toBeLessThanOrEqual(1);
    const main = await box(page.locator('main'));
    expect(Math.abs(main.x - 64)).toBeLessThanOrEqual(1);
    expect(Math.abs(main.width - (width - 64))).toBeLessThanOrEqual(1);
    await expect(pane(page)).toBeHidden();
    await expect(toggle(page)).toHaveCount(0);
    await expect(handle(page)).toBeHidden();
  }
});

test('at 420 no page scrolls sideways', async ({ page }) => {
  for (const url of [pilotThread(), `${m.url}/settings`, `${m.url}/bots/new`]) {
    await page.setViewportSize({ width: 420, height: 800 });
    await page.goto(url);
    await expect(page.locator('main')).toBeVisible();
    await expect(rail(page)).toBeVisible();
    const over = await sideScroll(page);
    expect(over.page, url).toBeLessThanOrEqual(0);
    expect(over.main, url).toBeLessThanOrEqual(0);
  }
});

test('while a run works the composer placeholder is Working… and fits at 420', async ({ page }) => {
  const bot = await newBotAPI(m, 'Hanger');
  const thread = await openThreadAPI(m, bot.id, prompts.hang);
  await page.setViewportSize({ width: 420, height: 800 });
  await page.goto(`${m.url}/bots/${bot.id}/threads/${thread.id}`);
  const input = page.getByRole('textbox', { name: /run in progress/ });
  await expect(input).toHaveAttribute('placeholder', 'Working…');
  const fit = await input.evaluate(
    (el) => (el as HTMLTextAreaElement).scrollHeight <= el.clientHeight,
  );
  expect(fit).toBe(true);
  expect((await sideScroll(page)).page).toBeLessThanOrEqual(0);
  await api(m, 'POST', `/api/runs/${thread.run.id}/stop`);
});

test('a closed pane is not reachable with Tab', async ({ page }) => {
  await openAt(page, 700, 500);
  await expect(pane(page)).toBeHidden();
  await sidebarRow(page, 'Pilot').focus();
  for (let i = 0; i < 12; i++) {
    await page.keyboard.press('Tab');
    const inside = await page.evaluate(
      () =>
        document.activeElement?.closest(
          '[role="region"][aria-label="Pilot"], section[aria-label="Pilot"]',
        ) != null,
    );
    expect(inside).toBe(false);
  }
});

test('resizing across 900px: widening docks the pane at its stored width, narrowing closes it', async ({
  page,
}) => {
  await openAt(page, 800, 600);
  await sidebarRow(page, 'Pilot').click();
  await expect(pane(page)).toBeVisible();

  await page.setViewportSize({ width: 1000, height: 600 });
  await expect(pane(page)).toBeVisible();
  await expect(handle(page)).toBeVisible();
  await expect.poll(async () => Math.round((await box(pane(page))).width)).toBe(284);
  await expect
    .poll(async () => Math.round((await box(page.locator('main'))).width))
    .toBe(1000 - 64 - 284);

  await page.setViewportSize({ width: 800, height: 600 });
  await expect(pane(page)).toBeHidden();
  await expect.poll(async () => Math.round((await box(page.locator('main'))).width)).toBe(800 - 64);
});

test('the island drops the thread title before the helper creatures and the Bot name', async ({
  page,
}) => {
  const boss = await newBotAPI(m, 'Poteto', 'claude', { look: 'whale' });
  const tester = await newBotAPI(m, 'Tester', 'claude', { look: 'octopus' });
  const reviewer = await newBotAPI(m, 'Reviewer', 'claude', { look: 'octopus' });
  await api(m, 'PUT', `/api/bots/${boss.id}`, {
    delegates: [tester.id, reviewer.id],
  });
  // The brief is \u-escaped so the boss's own prompt does not hold the helper's recorded prompt.
  const escaped = Array.from(
    prompts.hang,
    (c) => `\\u${c.charCodeAt(0).toString(16).padStart(4, '0')}`,
  ).join('');
  const brief = (name: string) =>
    `{"server":"atoll","tool":"delegate","args":{"bot":"${name}","brief":"${escaped}"}}`;
  const thread = await openThreadAPI(
    m,
    boss.id,
    `Use MCP tools. [${brief('Tester')},${brief('Reviewer')}]`,
  );

  const header = page.locator('header');
  const title = header.getByRole('heading', { level: 2 });
  const name = header.getByRole('heading', { level: 1, name: 'Poteto' });
  await page.setViewportSize({ width: 1280, height: 720 });
  await page.goto(`${m.url}/bots/${boss.id}/threads/${thread.id}`);
  await expect(header.getByLabel('Tester: working', { exact: true })).toBeVisible();
  await expect(header.getByLabel('Reviewer: working', { exact: true })).toBeVisible();
  await expect(title).toBeVisible();

  await page.setViewportSize({ width: 420, height: 800 });
  await expect(title).toBeHidden();
  await expect(name).toBeVisible();
  for (const label of ['Tester: working', 'Reviewer: working']) {
    const creature = header.getByLabel(label, { exact: true });
    await expect(creature).toBeVisible();
    const c = await box(creature);
    expect(c.x).toBeGreaterThanOrEqual(0);
    expect(c.x + c.width).toBeLessThanOrEqual(420);
  }
  const island = await box(page.locator('[data-island]'));
  const r = await box(rail(page));
  const settings = await box(header.getByRole('button', { name: 'Bot settings' }));
  expect(island.x).toBeGreaterThanOrEqual(r.x + r.width);
  expect(island.x + island.width).toBeLessThanOrEqual(settings.x);
  expect(await name.evaluate((el) => el.scrollWidth <= el.clientWidth)).toBe(true);
  expect((await sideScroll(page)).page).toBeLessThanOrEqual(0);
});
