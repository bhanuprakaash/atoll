// Drag to reorder the sidebar: groups, Bots in a group, and the Pinned rows under Chief.
import { expect, type Page, test } from '@playwright/test';
import { api, type Machine, newBotAPI, sidebarRow, startAtoll } from './atoll';

let m: Machine;
test.beforeEach(async () => {
  m = await startAtoll();
});
test.afterEach(() => m?.stop());

const nav = (page: Page) => page.getByRole('navigation', { name: 'Bots' });
const group = (page: Page, name: string) => nav(page).getByRole('group', { name, exact: true });
const heading = (page: Page, name: string) => group(page, name).getByRole('heading');
const top = { targetPosition: { x: 40, y: 2 } };
const bottom = (height: number) => ({ targetPosition: { x: 40, y: height - 2 } });

const botNames = (links: Locator) =>
  links.evaluateAll((els) =>
    els
      .filter((el) => /^\/bots\/[^/?]+$/.test(el.getAttribute('href') ?? ''))
      .map((el) => (el as HTMLElement).innerText.split('\n')[0]),
  );
type Locator = ReturnType<Page['locator']>;

// order is the names of the groups in the sidebar, top to bottom.
const order = (page: Page) =>
  nav(page)
    .getByRole('group')
    .evaluateAll((els) => els.map((el) => el.getAttribute('aria-label')));
// members is the Bots in a group, top to bottom.
const members = (page: Page, name: string) => botNames(group(page, name).getByRole('link'));
// pinned is the Bots in the rows above the first group, Chief first.
const pinned = (page: Page) =>
  nav(page).evaluate((el) => {
    const out: string[] = [];
    for (const a of el.querySelectorAll('a[href^="/bots/"]')) {
      if (a.closest('[role="group"]')) break;
      if (/^\/bots\/[^/?]+$/.test(a.getAttribute('href') ?? ''))
        out.push((a as HTMLElement).innerText.split('\n')[0]);
    }
    return out;
  });

async function newGroup(page: Page, name: string) {
  await nav(page).getByRole('button', { name: 'New', exact: true }).click();
  await page.getByRole('menuitem', { name: 'New group' }).click();
  const field = nav(page).getByRole('textbox', { name: 'Group name' });
  await field.fill(name);
  await field.press('Enter');
  await expect(group(page, name)).toBeVisible();
}

async function moveToGroup(page: Page, bot: string, target: string) {
  await sidebarRow(page, bot).click({ button: 'right' });
  await page.getByRole('menuitem', { name: 'Move to group' }).click();
  await page.getByRole('menuitem', { name: target, exact: true }).click();
  await expect(
    group(page, target).getByRole('link', { name: new RegExp(`\\b${bot}\\b`) }),
  ).toBeVisible();
}

async function restart(page: Page) {
  m.kill();
  await m.restart();
  await page.reload();
}

async function height(loc: Locator) {
  return (await loc.boundingBox())!.height;
}

test('dragging a group heading reorders the groups and survives restart', async ({ page }) => {
  await page.goto(m.url);
  await newGroup(page, 'Platform');
  await newGroup(page, 'Frontend');
  await expect.poll(() => order(page)).toEqual(['Platform', 'Frontend', 'Ungrouped']);

  await heading(page, 'Frontend').dragTo(heading(page, 'Platform'), top);
  await expect.poll(() => order(page)).toEqual(['Frontend', 'Platform', 'Ungrouped']);

  await restart(page);
  await expect.poll(() => order(page)).toEqual(['Frontend', 'Platform', 'Ungrouped']);
});

test("a group's menu has no Move up or Move down", async ({ page }) => {
  await page.goto(m.url);
  await newGroup(page, 'Platform');
  await newGroup(page, 'Frontend');
  for (const name of ['Platform', 'Frontend']) {
    await heading(page, name).hover();
    await group(page, name).getByRole('button', { name: 'Group actions' }).click();
    await expect(page.getByRole('menuitem', { name: 'Rename', exact: true })).toBeVisible();
    await expect(page.getByRole('menuitem', { name: /^Move (up|down)$/ })).toHaveCount(0);
    await page.keyboard.press('Escape');
  }
});

test('dragging a Bot between two Bots of a group puts it there and survives restart', async ({
  page,
}) => {
  for (const name of ['Ivy', 'Oak', 'Elm', 'Fig']) await newBotAPI(m, name);
  await page.goto(m.url);
  await newGroup(page, 'Platform');
  for (const name of ['Ivy', 'Oak', 'Fig']) await moveToGroup(page, name, 'Platform');
  await expect.poll(() => members(page, 'Platform')).toEqual(['Ivy', 'Oak', 'Fig']);

  const oak = sidebarRow(page, 'Oak');
  await sidebarRow(page, 'Elm').dragTo(oak, bottom(await height(oak)));
  await expect.poll(() => members(page, 'Platform')).toEqual(['Ivy', 'Oak', 'Elm', 'Fig']);

  await sidebarRow(page, 'Fig').dragTo(sidebarRow(page, 'Ivy'), top);
  await expect.poll(() => members(page, 'Platform')).toEqual(['Fig', 'Ivy', 'Oak', 'Elm']);

  await restart(page);
  await expect.poll(() => members(page, 'Platform')).toEqual(['Fig', 'Ivy', 'Oak', 'Elm']);
});

test('dragging a Bot into Pinned pins it at the drop place; dragging it out unpins it', async ({
  page,
}) => {
  for (const name of ['Ivy', 'Oak', 'Elm']) await newBotAPI(m, name);
  await page.goto(m.url);
  await newGroup(page, 'Platform');
  await moveToGroup(page, 'Elm', 'Platform');
  for (const name of ['Ivy', 'Oak']) await api(m, 'POST', `/api/bots/${await botID(name)}/pin`);
  await page.reload();
  await expect.poll(() => pinned(page)).toEqual(['Chief', 'Ivy', 'Oak']);

  const ivy = sidebarRow(page, 'Ivy');
  await sidebarRow(page, 'Elm').dragTo(ivy, bottom(await height(ivy)));
  await expect.poll(() => pinned(page)).toEqual(['Chief', 'Ivy', 'Elm', 'Oak']);
  await expect(sidebarRow(page, 'Elm')).toHaveCount(1);
  await expect(group(page, 'Platform').getByRole('link', { name: /\bElm\b/ })).toHaveCount(0);

  await restart(page);
  await expect.poll(() => pinned(page)).toEqual(['Chief', 'Ivy', 'Elm', 'Oak']);

  await sidebarRow(page, 'Elm').dragTo(heading(page, 'Platform'));
  await expect(group(page, 'Platform').getByRole('link', { name: /\bElm\b/ })).toBeVisible();
  await expect.poll(() => pinned(page)).toEqual(['Chief', 'Ivy', 'Oak']);
  const bots = await api(m, 'GET', '/api/bots');
  expect(bots.find((b: { name: string }) => b.name === 'Elm').pinned).toBe(false);
});

async function botID(name: string): Promise<string> {
  const bots = await api(m, 'GET', '/api/bots');
  return bots.find((b: { name: string }) => b.name === name).id;
}

test('Chief cannot be dragged, nothing lands above it, and Ungrouped stays last', async ({
  page,
}) => {
  await newBotAPI(m, 'Ivy');
  await page.goto(m.url);
  await newGroup(page, 'Platform');

  const chief = sidebarRow(page, 'Chief');
  await expect(chief.locator('xpath=..')).not.toHaveAttribute('draggable', 'true');
  await expect(sidebarRow(page, 'Ivy').locator('xpath=..')).toHaveAttribute('draggable', 'true');

  await sidebarRow(page, 'Ivy').dragTo(chief, top);
  await expect.poll(() => botNames(nav(page).getByRole('link'))).toContain('Ivy');
  expect((await botNames(nav(page).getByRole('link')))[0]).toBe('Chief');
  await expect.poll(() => pinned(page)).toEqual(['Chief']);

  await expect(heading(page, 'Ungrouped')).not.toHaveAttribute('draggable', 'true');
  await heading(page, 'Ungrouped').dragTo(heading(page, 'Platform'), top);
  await expect.poll(() => order(page)).toEqual(['Platform', 'Ungrouped']);
  await heading(page, 'Platform').dragTo(heading(page, 'Ungrouped'), top);
  await expect.poll(() => order(page)).toEqual(['Platform', 'Ungrouped']);
});

test('with text in the search field, Bot rows and group headings are not draggable', async ({
  page,
}) => {
  await newBotAPI(m, 'Ivy');
  await newBotAPI(m, 'Ivory');
  await page.goto(m.url);
  await newGroup(page, 'Platform');
  await moveToGroup(page, 'Ivy', 'Platform');
  await expect(sidebarRow(page, 'Ivy').locator('xpath=..')).toHaveAttribute('draggable', 'true');
  await expect(heading(page, 'Platform')).toHaveAttribute('draggable', 'true');

  await page.getByRole('textbox', { name: 'Search Bots' }).fill('iv');
  await expect(sidebarRow(page, 'Ivory')).toBeVisible();
  for (const name of ['Ivy', 'Ivory'])
    await expect(sidebarRow(page, name).locator('xpath=..')).not.toHaveAttribute(
      'draggable',
      'true',
    );
  await expect(heading(page, 'Platform')).not.toHaveAttribute('draggable', 'true');
});

// The e2e harness cannot seed an older database, so this checks the order the store gives Bots
// created in a known order, which is what the upgrade migration must reproduce.
test('Bots keep creation order in a group and pin order in Pinned after restart', async ({
  page,
}) => {
  for (const name of ['Ivy', 'Oak', 'Elm', 'Fig']) await newBotAPI(m, name);
  await page.goto(m.url);
  await newGroup(page, 'Platform');
  for (const name of ['Ivy', 'Oak', 'Elm']) await moveToGroup(page, name, 'Platform');
  for (const name of ['Fig']) await api(m, 'POST', `/api/bots/${await botID(name)}/pin`);

  await restart(page);
  await expect.poll(() => members(page, 'Platform')).toEqual(['Ivy', 'Oak', 'Elm']);
  await expect.poll(() => pinned(page)).toEqual(['Chief', 'Fig']);
});
