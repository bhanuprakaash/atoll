// Narrow windows: below 900 px the sidebar floats over the chat with a scrim, closed on launch,
// opened by the header's Sidebar button or Cmd/Ctrl+B and closed by a Bot row, Esc, the scrim or
// Cmd/Ctrl+B. At 900 px and up it is docked. Design: docs/design.md, "Narrow windows".
import { expect, type Locator, type Page, test } from '@playwright/test';
import {
  api,
  type Machine,
  newBotAPI,
  openThreadAPI,
  prompts,
  sidebarRow,
  startAtoll,
} from './atoll';

let m: Machine;
let pilot: { id: string };
let other: { id: string };
let first: { id: string };
let theirs: { id: string };

test.beforeAll(async () => {
  m = await startAtoll();
  pilot = await newBotAPI(m, 'Pilot');
  other = await newBotAPI(m, 'Other');
  first = await openThreadAPI(m, pilot.id, prompts.keep);
  theirs = await openThreadAPI(m, other.id, prompts.pong);
  for (const id of [first.id, theirs.id])
    await expect
      .poll(async () => (await api(m, 'GET', `/api/threads/${id}`)).run_status, {
        timeout: 15_000,
      })
      .toBe('done');
});
test.afterAll(() => m?.stop());

const sidebar = (page: Page) => page.getByRole('navigation', { name: 'Bots' });
const toggle = (page: Page) => page.getByRole('button', { name: 'Sidebar', exact: true });
const composer = (page: Page) => page.getByRole('textbox', { name: /^Message/ });
const box = async (l: Locator) => (await l.boundingBox())!;
const pilotThread = () => `${m.url}/bots/${pilot.id}/threads/${first.id}`;

async function openAt(page: Page, width: number, height: number, url = pilotThread()) {
  await page.setViewportSize({ width, height });
  await page.goto(url);
  await expect(composer(page)).toBeVisible();
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

test('at 700x500 and 600x800 the sidebar is closed on launch and the chat fills the window', async ({
  page,
}) => {
  for (const [width, height] of [
    [700, 500],
    [600, 800],
  ]) {
    await openAt(page, width, height);
    await expect(sidebar(page)).toBeHidden();
    await expect(toggle(page)).toBeVisible();
    await expect(toggle(page)).toHaveAttribute('aria-expanded', 'false');
    const main = await box(page.locator('main'));
    expect(main.x).toBe(0);
    expect(main.width).toBe(width);
  }
});

test('the Sidebar button and Cmd/Ctrl+B open the sidebar over the chat with a scrim; nothing shifts', async ({
  page,
}) => {
  await openAt(page, 700, 500);
  const before = await box(page.locator('main'));

  await toggle(page).click();
  await expect(sidebar(page)).toBeVisible();
  await expect(toggle(page)).toHaveAttribute('aria-expanded', 'true');
  const floating = await box(sidebar(page));
  expect(floating.x).toBe(0);
  expect(floating.width).toBeLessThan(700);
  expect(await box(page.locator('main'))).toEqual(before);

  await composer(page).focus();
  await page.keyboard.press('ControlOrMeta+b');
  await expect(sidebar(page)).toBeHidden();
  await expect(toggle(page)).toHaveAttribute('aria-expanded', 'false');
  await page.keyboard.press('ControlOrMeta+b');
  await expect(sidebar(page)).toBeVisible();
  await expect(toggle(page)).toHaveAttribute('aria-expanded', 'true');
  await page.keyboard.press('ControlOrMeta+b');
  await expect(sidebar(page)).toBeHidden();
});

test('picking a Bot row opens its thread and closes the sidebar', async ({ page }) => {
  await openAt(page, 700, 500);
  await toggle(page).click();
  await sidebarRow(page, 'Other').click();
  await expect(page).toHaveURL(`${m.url}/bots/${other.id}/threads/${theirs.id}`);
  await expect(sidebar(page)).toBeHidden();
  await expect(toggle(page)).toHaveAttribute('aria-expanded', 'false');
});

test('Esc closes the sidebar and puts focus back on the Sidebar button', async ({ page }) => {
  await openAt(page, 700, 500);
  await toggle(page).click();
  await expect(sidebar(page)).toBeVisible();
  await page.keyboard.press('Escape');
  await expect(sidebar(page)).toBeHidden();
  await expect(toggle(page)).toBeFocused();
  await expect(page).toHaveURL(pilotThread());
});

test('a click on the scrim closes the sidebar, a click inside it does not', async ({ page }) => {
  await openAt(page, 700, 500);
  await toggle(page).click();
  await expect(sidebar(page)).toBeVisible();
  await sidebar(page).click({ position: { x: 4, y: 200 } });
  await expect(sidebar(page)).toBeVisible();

  await page.mouse.click(690, 300);
  await expect(sidebar(page)).toBeHidden();
  await expect(page).toHaveURL(pilotThread());
});

test('a closed sidebar is not reachable with Tab', async ({ page }) => {
  await openAt(page, 700, 500);
  await expect(sidebar(page)).toBeHidden();
  await toggle(page).focus();
  for (let i = 0; i < 12; i++) {
    await page.keyboard.press('Tab');
    const inside = await page.evaluate(
      () => document.activeElement?.closest('nav[aria-label="Bots"]') != null,
    );
    expect(inside).toBe(false);
  }
});

test('at 900px and up the sidebar is docked: no Sidebar button', async ({ page }) => {
  await openAt(page, 900, 600);
  await expect(sidebar(page)).toBeVisible();
  await expect(toggle(page)).toHaveCount(0);
  const nav = await box(sidebar(page));
  expect(Math.abs((await box(page.locator('main'))).x - (nav.x + nav.width))).toBeLessThanOrEqual(
    1,
  );

  await openAt(page, 899, 600);
  await expect(sidebar(page)).toBeHidden();
  await expect(toggle(page)).toBeVisible();
});

test('resizing across 900px: widening docks the sidebar, narrowing closes it', async ({ page }) => {
  await openAt(page, 800, 600);
  await toggle(page).click();
  await expect(sidebar(page)).toBeVisible();

  await page.setViewportSize({ width: 1000, height: 600 });
  await expect(toggle(page)).toHaveCount(0);
  await expect(sidebar(page)).toBeVisible();
  await expect
    .poll(async () => Math.round((await box(page.locator('main'))).width))
    .toBe(1000 - 260);

  await page.setViewportSize({ width: 800, height: 600 });
  await expect(sidebar(page)).toBeHidden();
  await expect(toggle(page)).toHaveAttribute('aria-expanded', 'false');
  await expect.poll(async () => (await box(page.locator('main'))).width).toBe(800);
});

test('at 420 and up no page scrolls sideways', async ({ page }) => {
  for (const width of [420, 700, 900]) {
    for (const url of [pilotThread(), `${m.url}/settings`, `${m.url}/bots/new`, `${m.url}/now`]) {
      await page.setViewportSize({ width, height: 800 });
      await page.goto(url);
      await expect(page.locator('main')).toBeVisible();
      const over = await sideScroll(page);
      expect(over.page, `${url} at ${width}`).toBeLessThanOrEqual(0);
      expect(over.main, `${url} at ${width}`).toBeLessThanOrEqual(0);
    }
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
  const button = await box(toggle(page));
  const settings = await box(header.getByRole('button', { name: 'Bot settings' }));
  expect(island.x).toBeGreaterThanOrEqual(button.x + button.width);
  expect(island.x + island.width).toBeLessThanOrEqual(settings.x);
  expect(await name.evaluate((el) => el.scrollWidth <= el.clientWidth)).toBe(true);
  expect((await sideScroll(page)).page).toBeLessThanOrEqual(0);
});
