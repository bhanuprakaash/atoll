// Drag to reorder Bots and groups on the rail (#246): native HTML5 drag and drop. A face dropped on
// the top half of another face lands before it, on the bottom half after it; a face dropped on a
// group's line goes last in that group; a group's line dropped on another group's line lands before
// that group. Selectors: e2e/sidebar-rail-contract.md. The API and the upgrade are in
// e2e/sidebar_place_test.go.
import { expect, type Locator, type Page, test } from '@playwright/test';
import { api, type Machine, newBotAPI, showPane, sidebarRow, startAtoll } from './atoll';

let m: Machine;
const ids: Record<string, string> = {};
let platform = '';
let frontend = '';
test.beforeEach(async () => {
  m = await startAtoll();
  for (const name of ['Ivy', 'Oak', 'Pine', 'Elm', 'Fir', 'Ash', 'Yew'])
    ids[name] = (await newBotAPI(m, name)).id;
  platform = (await api(m, 'POST', '/api/groups', { name: 'Platform' })).id;
  frontend = (await api(m, 'POST', '/api/groups', { name: 'Frontend' })).id;
  for (const name of ['Ivy', 'Oak', 'Pine'])
    await api(m, 'PUT', `/api/bots/${ids[name]}/group`, { group_id: platform });
  await api(m, 'PUT', `/api/bots/${ids.Yew}/group`, { group_id: frontend });
  for (const name of ['Fir', 'Ash']) await api(m, 'POST', `/api/bots/${ids[name]}/pin`);
});
test.afterEach(() => m?.stop());

const rail = (page: Page) => page.getByRole('navigation', { name: 'Bots' });
const group = (page: Page, name: string) => rail(page).getByRole('group', { name, exact: true });
const groupLine = (page: Page, name: string) =>
  group(page, name).getByRole('button', { name, exact: true });

// drop drags source onto target: on its top edge (before), bottom edge (after) or middle.
async function drop(source: Locator, target: Locator, where: 'before' | 'after' | 'on' = 'on') {
  const b = (await target.boundingBox())!;
  const y = where === 'before' ? 2 : where === 'after' ? b.height - 2 : b.height / 2;
  await source.dragTo(target, { targetPosition: { x: b.width / 2, y } });
}

// names is the Bot ids of the faces in a group, top to bottom.
const names = (l: Locator) =>
  l
    .locator('a[href^="/bots/"]')
    .evaluateAll((els) => els.map((el) => el.getAttribute('href')!.split('/').pop()!));
const byID = (want: string[]) => want.map((n) => ids[n]);

// pinned is the ids of the faces between Chief and the first group, top to bottom.
const pinned = (page: Page) =>
  rail(page)
    .locator('a[href^="/bots/"]')
    .evaluateAll((els) =>
      els
        .filter((el) => !el.closest('[role="group"]'))
        .map((el) => el.getAttribute('href')!.split('/').pop()!)
        .filter((id) => id !== 'chief'),
    );

const groupOrder = (page: Page) =>
  rail(page)
    .getByRole('group')
    .evaluateAll((els) => els.map((el) => el.getAttribute('aria-label')));

async function restart(page: Page) {
  m.kill();
  await m.restart();
  await page.reload();
}

test("dragging group Frontend's line above Platform's shows Frontend, Platform, Ungrouped, also after a restart", async ({
  page,
}) => {
  await page.goto(`${m.url}/settings`);
  await expect.poll(() => groupOrder(page)).toEqual(['Platform', 'Frontend', 'Ungrouped']);
  await drop(groupLine(page, 'Frontend'), groupLine(page, 'Platform'));
  await expect.poll(() => groupOrder(page)).toEqual(['Frontend', 'Platform', 'Ungrouped']);
  await expect(group(page, 'Platform').locator('a[href^="/bots/"]')).toHaveCount(3);
  await restart(page);
  await expect.poll(() => groupOrder(page)).toEqual(['Frontend', 'Platform', 'Ungrouped']);
  await expect.poll(() => names(group(page, 'Frontend'))).toEqual(byID(['Yew']));
});

test('the group menu has no Move up or Move down', async ({ page }) => {
  await page.goto(`${m.url}/settings`);
  await groupLine(page, 'Platform').click({ button: 'right' });
  const menu = page.getByRole('menu');
  await expect(menu.getByRole('menuitem')).toHaveText([
    /^Rename/,
    /^New Bot in this group/,
    /^Delete group/,
  ]);
  await expect(menu.getByRole('menuitem', { name: /Move (up|down)/ })).toHaveCount(0);
});

test('dragging Bot Elm between two Bots of a group puts it there, also after a restart', async ({
  page,
}) => {
  await page.goto(`${m.url}/settings`);
  await expect.poll(() => names(group(page, 'Platform'))).toEqual(byID(['Ivy', 'Oak', 'Pine']));
  await drop(sidebarRow(page, 'Elm'), sidebarRow(page, 'Oak'), 'before');
  const want = byID(['Ivy', 'Elm', 'Oak', 'Pine']);
  await expect.poll(() => names(group(page, 'Platform'))).toEqual(want);
  expect((await api(m, 'GET', `/api/bots/${ids.Elm}`)).group_id).toBe(platform);
  await restart(page);
  await expect.poll(() => names(group(page, 'Platform'))).toEqual(want);
});

test("dropping a face on a group's line puts the Bot last in that group", async ({ page }) => {
  await page.goto(`${m.url}/settings`);
  await drop(sidebarRow(page, 'Elm'), groupLine(page, 'Platform'));
  await expect
    .poll(() => names(group(page, 'Platform')))
    .toEqual(byID(['Ivy', 'Oak', 'Pine', 'Elm']));
  await drop(sidebarRow(page, 'Ivy'), groupLine(page, 'Frontend'));
  await expect.poll(() => names(group(page, 'Frontend'))).toEqual(byID(['Yew', 'Ivy']));
});

test('dragging a Bot into Pinned pins it at the drop place; dragging it out to a group unpins it and puts it there', async ({
  page,
}) => {
  await page.goto(`${m.url}/settings`);
  await expect.poll(() => pinned(page)).toEqual(byID(['Fir', 'Ash']));

  await drop(sidebarRow(page, 'Elm'), sidebarRow(page, 'Ash'), 'before');
  await expect.poll(() => pinned(page)).toEqual(byID(['Fir', 'Elm', 'Ash']));
  expect((await api(m, 'GET', `/api/bots/${ids.Elm}`)).pinned).toBe(true);

  await drop(sidebarRow(page, 'Elm'), sidebarRow(page, 'Oak'), 'before');
  await expect.poll(() => pinned(page)).toEqual(byID(['Fir', 'Ash']));
  await expect
    .poll(() => names(group(page, 'Platform')))
    .toEqual(byID(['Ivy', 'Elm', 'Oak', 'Pine']));
  const elm = await api(m, 'GET', `/api/bots/${ids.Elm}`);
  expect(elm.pinned).toBe(false);
  expect(elm.group_id).toBe(platform);
});

test('Chief has no drag and nothing lands above it; Ungrouped stays last and does not drag', async ({
  page,
}) => {
  await page.goto(`${m.url}/settings`);
  const chief = sidebarRow(page, 'Chief');
  await expect(chief).toHaveAttribute('draggable', 'false');
  await expect(sidebarRow(page, 'Elm')).toHaveAttribute('draggable', 'true');
  await expect(groupLine(page, 'Platform')).toHaveAttribute('draggable', 'true');
  await expect(groupLine(page, 'Ungrouped')).not.toHaveAttribute('draggable', 'true');

  const first = () => rail(page).locator('a[href^="/bots/"]').first().getAttribute('href');
  await drop(sidebarRow(page, 'Elm'), chief, 'before');
  await expect.poll(first).toBe('/bots/chief');
  await drop(sidebarRow(page, 'Fir'), chief, 'before');
  await expect.poll(first).toBe('/bots/chief');

  await drop(chief, sidebarRow(page, 'Oak'), 'before');
  await expect.poll(() => names(group(page, 'Platform'))).toEqual(byID(['Ivy', 'Oak', 'Pine']));
  await expect.poll(first).toBe('/bots/chief');

  await drop(groupLine(page, 'Ungrouped'), groupLine(page, 'Platform'));
  await drop(groupLine(page, 'Platform'), groupLine(page, 'Ungrouped'));
  await expect.poll(() => groupOrder(page)).toEqual(['Platform', 'Frontend', 'Ungrouped']);
});

test('thread rows are not draggable', async ({ page }) => {
  for (let i = 0; i < 2; i++) await api(m, 'POST', `/api/bots/${ids.Elm}/threads`, {});
  const pane = await showPane(page, m, ids.Elm, 'Elm');
  const links = pane.getByRole('listitem').getByRole('link');
  await expect(links).toHaveCount(2);
  for (const link of await links.all()) await expect(link).toHaveAttribute('draggable', 'false');
  const before = await pane.getByRole('listitem').allTextContents();
  await drop(links.nth(1), links.nth(0), 'before');
  expect(await pane.getByRole('listitem').allTextContents()).toEqual(before);
});
