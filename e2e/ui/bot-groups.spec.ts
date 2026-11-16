// Bot groups: rail sections the user creates, fills, renames, drags into order (#246) and deletes;
// groups do not fold on the rail.
import { expect, type Page, test } from '@playwright/test';
import { type Machine, newBotAPI, sidebarRow, startAtoll } from './atoll';

let m: Machine;
test.beforeEach(async () => {
  m = await startAtoll();
});
test.afterEach(() => m?.stop());

const nav = (page: Page) => page.getByRole('navigation', { name: 'Bots' });
const group = (page: Page, name: string) => nav(page).getByRole('group', { name, exact: true });
// line is the group's hairline on the rail.
const line = (page: Page, name: string) =>
  group(page, name).getByRole('button', { name, exact: true });

async function newGroup(page: Page, name: string) {
  await nav(page).getByRole('button', { name: 'New', exact: true }).click();
  await page.getByRole('menuitem', { name: 'New group' }).click();
  const field = page.getByRole('textbox', { name: 'Group name' });
  await field.fill(name);
  await field.press('Enter');
  await expect(group(page, name)).toBeVisible();
}

async function groupAction(page: Page, name: string, action: string) {
  await line(page, name).click({ button: 'right' });
  await page.getByRole('menuitem', { name: action, exact: true }).click();
}

async function moveToGroup(page: Page, bot: string, target: string) {
  await sidebarRow(page, bot).click({ button: 'right' });
  await page.getByRole('menuitem', { name: 'Move to group' }).click();
  await page.getByRole('menuitem', { name: target, exact: true }).click();
}

// order is the names of the groups in the sidebar, top to bottom.
const order = (page: Page) =>
  nav(page)
    .getByRole('group')
    .evaluateAll((els) => els.map((el) => el.getAttribute('aria-label')));

test('New group, Move to group, drop on a line, Rename, drag a group above another persist across restart', async ({
  page,
}) => {
  for (const name of ['Ivy', 'Oak', 'Elm']) await newBotAPI(m, name);
  await page.goto(m.url);

  await nav(page).getByRole('button', { name: 'New', exact: true }).click();
  await expect(page.getByRole('menuitem', { name: 'New Bot' })).toBeVisible();
  await page.getByRole('menuitem', { name: 'New group' }).click();
  const field = page.getByRole('textbox', { name: 'Group name' });
  await field.fill('Nope');
  await field.press('Escape');
  await expect(field).toHaveCount(0);
  await expect(group(page, 'Nope')).toHaveCount(0);

  await newGroup(page, 'Backend');
  await newGroup(page, 'Frontend');

  await sidebarRow(page, 'Ivy').click({ button: 'right' });
  await page.getByRole('menuitem', { name: 'Move to group' }).click();
  for (const name of ['Backend', 'Frontend', 'No group', 'New group…'])
    await expect(page.getByRole('menuitem', { name, exact: true })).toBeVisible();
  await page.getByRole('menuitem', { name: 'Backend', exact: true }).click();
  await expect(group(page, 'Backend').getByRole('link', { name: /\bIvy\b/ })).toBeVisible();

  await sidebarRow(page, 'Oak').dragTo(line(page, 'Frontend'));
  await expect(group(page, 'Frontend').getByRole('link', { name: /\bOak\b/ })).toBeVisible();
  await expect(group(page, 'Ungrouped').getByRole('link', { name: /\bElm\b/ })).toBeVisible();

  await groupAction(page, 'Backend', 'Rename');
  const rename = page.getByRole('textbox', { name: 'Group name' });
  await rename.fill('Platform');
  await rename.press('Enter');
  await expect(group(page, 'Platform').getByRole('link', { name: /\bIvy\b/ })).toBeVisible();
  await expect(group(page, 'Backend')).toHaveCount(0);

  await expect.poll(() => order(page)).toEqual(['Platform', 'Frontend', 'Ungrouped']);
  await line(page, 'Frontend').dragTo(line(page, 'Platform'));
  await expect.poll(() => order(page)).toEqual(['Frontend', 'Platform', 'Ungrouped']);
  await line(page, 'Platform').dragTo(line(page, 'Frontend'));
  await expect.poll(() => order(page)).toEqual(['Platform', 'Frontend', 'Ungrouped']);
  await line(page, 'Frontend').dragTo(line(page, 'Platform'));
  await expect.poll(() => order(page)).toEqual(['Frontend', 'Platform', 'Ungrouped']);

  m.kill();
  await m.restart();
  await page.reload();
  await expect.poll(() => order(page)).toEqual(['Frontend', 'Platform', 'Ungrouped']);
  await expect(group(page, 'Platform').getByRole('link', { name: /\bIvy\b/ })).toBeVisible();
  await expect(line(page, 'Platform')).not.toHaveAttribute('aria-expanded', /./);
  await expect(group(page, 'Frontend').getByRole('link', { name: /\bOak\b/ })).toBeVisible();
});

test('Delete group moves its Bots to Ungrouped', async ({ page }) => {
  for (const name of ['Ivy', 'Oak', 'Elm']) await newBotAPI(m, name);
  await page.goto(m.url);
  await newGroup(page, 'Backend');
  await newGroup(page, 'Frontend');
  await moveToGroup(page, 'Ivy', 'Backend');
  await moveToGroup(page, 'Oak', 'Backend');
  await moveToGroup(page, 'Elm', 'Frontend');
  await expect(group(page, 'Backend').getByRole('link')).toHaveCount(2);

  await groupAction(page, 'Backend', 'Delete group');
  await expect(group(page, 'Backend')).toHaveCount(0);
  for (const name of ['Ivy', 'Oak'])
    await expect(
      group(page, 'Ungrouped').getByRole('link', { name: new RegExp(`\\b${name}\\b`) }),
    ).toBeVisible();
  await expect(group(page, 'Frontend').getByRole('link', { name: /\bElm\b/ })).toBeVisible();
});
