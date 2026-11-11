// Narrow windows: below 900 px the sidebar is a drawer over the thread column, closed by default,
// opened by the header's Sidebar button or Cmd/Ctrl+B and closed by navigation, Esc, a click on
// the scrim or the button. At 900 px and up nothing changes. Design: docs/design.md, "Narrow
// windows".
import { expect, type Locator, type Page, test } from '@playwright/test';
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
let pilot: { id: string };
let other: { id: string };
let first: { id: string };
let second: { id: string };

test.beforeAll(async () => {
  m = await startAtoll();
  pilot = await newBotAPI(m, 'Pilot');
  other = await newBotAPI(m, 'Other');
  first = await openThreadAPI(m, pilot.id, prompts.keep);
  second = await openThreadAPI(m, pilot.id, prompts.pong);
  await openThreadAPI(m, other.id, prompts.pong);
  for (const id of [first.id, second.id])
    await expect
      .poll(async () => (await api(m, 'GET', `/api/threads/${id}`)).run_status, {
        timeout: 15_000,
      })
      .toBe('done');
});
test.afterAll(() => m?.stop());

const sidebar = (page: Page) => page.getByRole('navigation', { name: 'Bots' });
const toggle = (page: Page) => page.getByRole('button', { name: 'Sidebar', exact: true });
const handle = (page: Page) => page.getByRole('separator', { name: 'Resize sidebar' });
const search = (page: Page) => page.getByRole('textbox', { name: 'Search Bots' });
const composer = (page: Page) => page.getByRole('textbox', { name: /^Message/ });
const box = async (l: Locator) => (await l.boundingBox())!;
const pilotThread = (page: Page) => `${m.url}/bots/${pilot.id}/threads/${first.id}`;

async function openAt(page: Page, width: number, height: number, url = pilotThread(page)) {
  await page.setViewportSize({ width, height });
  await page.goto(url);
  await expect(composer(page)).toBeVisible();
}

// A click on the page at the right edge, clear of the drawer, whatever its width.
const clickOutside = (page: Page) => {
  const { width } = page.viewportSize()!;
  return page.mouse.click(width - 10, 300);
};

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

test('at 700x500 and 600x800 the thread column is the full window width and the sidebar is closed', async ({
  page,
}) => {
  for (const [width, height] of [
    [700, 500],
    [600, 800],
  ]) {
    await openAt(page, width, height);
    const main = await box(page.locator('main'));
    expect(main.x).toBe(0);
    expect(main.width).toBe(width);
    await expect(sidebar(page)).toBeHidden();
    await expect(toggle(page)).toBeVisible();
    await expect(toggle(page)).toHaveAttribute('aria-expanded', 'false');
    await expect(handle(page)).toBeHidden();
    const hdr = await box(page.locator('header'));
    expect(hdr.x).toBe(0);
    expect(hdr.width).toBe(width);
  }
});

test('at 420 no page scrolls sideways', async ({ page }) => {
  for (const url of [pilotThread(page), `${m.url}/settings`, `${m.url}/bots/new`]) {
    await page.setViewportSize({ width: 420, height: 800 });
    await page.goto(url);
    await expect(page.locator('main')).toBeVisible();
    await expect(page.getByRole('navigation', { name: 'Bots' })).toBeHidden();
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

test('the Sidebar button and Cmd/Ctrl+B open the drawer over the thread column and close it again', async ({
  page,
}) => {
  await openAt(page, 700, 500);
  const before = await box(page.locator('main'));

  await toggle(page).click();
  await expect(sidebar(page)).toBeVisible();
  await expect(toggle(page)).toHaveAttribute('aria-expanded', 'true');
  const drawer = await box(sidebar(page));
  expect(drawer.x).toBe(0);
  expect(drawer.width).toBe(340);
  expect(drawer.height).toBe(500);
  expect(await box(page.locator('main'))).toEqual(before);
  await expect(handle(page)).toBeHidden();
  await expect(search(page)).toBeFocused();

  await toggle(page).click();
  await expect(sidebar(page)).toBeHidden();
  await expect(toggle(page)).toBeFocused();

  await composer(page).focus();
  await page.keyboard.press('ControlOrMeta+b');
  await expect(sidebar(page)).toBeVisible();
  await expect(toggle(page)).toHaveAttribute('aria-expanded', 'true');
  await page.keyboard.press('ControlOrMeta+b');
  await expect(sidebar(page)).toBeHidden();
  await expect(toggle(page)).toHaveAttribute('aria-expanded', 'false');
});

test('the drawer is never wider than the window minus 48px and keeps the stored width otherwise', async ({
  page,
}) => {
  await openAt(page, 360, 640);
  await toggle(page).click();
  await expect(sidebar(page)).toBeVisible();
  const narrow = await box(sidebar(page));
  expect(narrow.width).toBeLessThanOrEqual(360 - 48);
  expect(narrow.width).toBeGreaterThan(260);

  await page.setViewportSize({ width: 1200, height: 700 });
  await expect(handle(page)).toBeVisible();
  await handle(page).focus();
  await page.keyboard.press('ArrowRight');
  await page.keyboard.press('ArrowRight');
  await expect(handle(page)).toHaveAttribute('aria-valuenow', '372');
  await page.setViewportSize({ width: 800, height: 600 });
  await expect(sidebar(page)).toBeHidden();
  await toggle(page).click();
  await expect(sidebar(page)).toBeVisible();
  expect((await box(sidebar(page))).width).toBe(372);
});

test('selecting a thread closes the drawer and opens that thread', async ({ page }) => {
  await openAt(page, 700, 500);
  await toggle(page).click();
  await expect(sidebar(page)).toBeVisible();
  await threadTree(page, 'Pilot')
    .getByRole('listitem')
    .filter({ hasText: threadTitle(prompts.pong) })
    .getByRole('link')
    .click();
  await expect(page).toHaveURL(`${m.url}/bots/${pilot.id}/threads/${second.id}`);
  await expect(sidebar(page)).toBeHidden();
  await expect(toggle(page)).toHaveAttribute('aria-expanded', 'false');
});

test('selecting a Bot or Settings closes the drawer', async ({ page }) => {
  await openAt(page, 700, 500);
  await toggle(page).click();
  await sidebarRow(page, 'Other').click();
  await expect(page).toHaveURL(new RegExp(`/bots/${other.id}/threads/[^/]+$`));
  await expect(sidebar(page)).toBeHidden();

  await toggle(page).click();
  await expect(sidebar(page)).toBeVisible();
  await sidebar(page).getByRole('link', { name: 'Settings' }).click();
  await expect(page).toHaveURL(`${m.url}/settings`);
  await expect(sidebar(page)).toBeHidden();
});

test('Esc closes the drawer and puts focus back on the button', async ({ page }) => {
  await openAt(page, 700, 500);
  await toggle(page).click();
  await expect(search(page)).toBeFocused();
  await page.keyboard.press('Escape');
  await expect(sidebar(page)).toBeHidden();
  await expect(toggle(page)).toBeFocused();
  await expect(page).toHaveURL(pilotThread(page));
});

test('a click outside closes the drawer, a click inside does not', async ({ page }) => {
  await openAt(page, 700, 500);
  await toggle(page).click();
  await expect(sidebar(page)).toBeVisible();
  await search(page).click();
  await sidebar(page).click({ position: { x: 170, y: 300 } });
  await expect(sidebar(page)).toBeVisible();

  await clickOutside(page);
  await expect(sidebar(page)).toBeHidden();
  await expect(page).toHaveURL(pilotThread(page));
});

test('a closed drawer is not reachable with Tab', async ({ page }) => {
  await openAt(page, 700, 500);
  await expect(sidebar(page)).toBeHidden();
  await toggle(page).focus();
  for (let i = 0; i < 12; i++) {
    await page.keyboard.press('Tab');
    const inside = await page.evaluate(
      () => document.activeElement?.closest('nav[aria-label="Bots"]') !== null,
    );
    expect(inside).toBe(false);
  }
});

test('at 900px and up the sidebar is docked as before: no button, no drawer, Cmd/Ctrl+B does nothing', async ({
  page,
}) => {
  await openAt(page, 900, 600);
  await expect(sidebar(page)).toBeVisible();
  expect((await box(sidebar(page))).width).toBe(340);
  expect((await box(page.locator('main'))).width).toBe(900 - 340);
  await expect(toggle(page)).toHaveCount(0);
  await expect(handle(page)).toBeVisible();

  await composer(page).focus();
  await page.keyboard.press('ControlOrMeta+b');
  await expect(sidebar(page)).toBeVisible();
  expect((await box(page.locator('main'))).width).toBe(900 - 340);

  await openAt(page, 899, 600);
  await expect(sidebar(page)).toBeHidden();
  await expect(toggle(page)).toBeVisible();
  expect((await box(page.locator('main'))).width).toBe(899);
});

test('resizing across 900px: widening docks the sidebar, narrowing closes it, an open drawer does not come back', async ({
  page,
}) => {
  await openAt(page, 800, 600);
  await toggle(page).click();
  await expect(sidebar(page)).toBeVisible();

  await page.setViewportSize({ width: 1000, height: 600 });
  await expect(toggle(page)).toHaveCount(0);
  await expect(sidebar(page)).toBeVisible();
  await expect.poll(async () => (await box(page.locator('main'))).width).toBe(1000 - 340);
  expect((await box(sidebar(page))).width).toBe(340);
  await expect(handle(page)).toBeVisible();
  await expect(composer(page)).toBeVisible();

  await page.setViewportSize({ width: 800, height: 600 });
  await expect(sidebar(page)).toBeHidden();
  await expect(toggle(page)).toHaveAttribute('aria-expanded', 'false');
  await expect.poll(async () => (await box(page.locator('main'))).width).toBe(800);

  await toggle(page).click();
  await expect(sidebar(page)).toBeVisible();
  await page.setViewportSize({ width: 600, height: 600 });
  await expect(sidebar(page)).toBeVisible();
  await expect(toggle(page)).toHaveAttribute('aria-expanded', 'true');
  await page.keyboard.press('Escape');
  await expect(sidebar(page)).toBeHidden();
  await expect(page).toHaveURL(pilotThread(page));
});

test('the island drops the thread title before the helper creatures and the Bot name', async ({
  page,
}) => {
  const boss = await newBotAPI(m, 'Poteto', 'claude', { look: 'whale' });
  const tester = await newBotAPI(m, 'Tester', 'claude', { look: 'shark' });
  const reviewer = await newBotAPI(m, 'Reviewer', 'claude', { look: 'shark' });
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
  const button = await box(toggle(page));
  const settings = await box(header.getByRole('button', { name: 'Bot settings' }));
  expect(island.x).toBeGreaterThanOrEqual(button.x + button.width);
  expect(island.x + island.width).toBeLessThanOrEqual(settings.x);
  expect(await name.evaluate((el) => el.scrollWidth <= el.clientWidth)).toBe(true);
  expect((await sideScroll(page)).page).toBeLessThanOrEqual(0);
});
