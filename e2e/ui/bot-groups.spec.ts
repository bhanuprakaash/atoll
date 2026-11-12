// Bot groups: sidebar sections the user creates, fills, renames, orders, folds and deletes.
// Contract: e2e/sidebar-tree-contract.md.
import { expect, type Page, test } from '@playwright/test';
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
test.beforeEach(async () => {
  m = await startAtoll();
});
test.afterEach(() => m?.stop());

const nav = (page: Page) => page.getByRole('navigation', { name: 'Bots' });
const group = (page: Page, name: string) => nav(page).getByRole('group', { name, exact: true });

async function newGroup(page: Page, name: string) {
  await nav(page).getByRole('button', { name: 'New', exact: true }).click();
  await page.getByRole('menuitem', { name: 'New group' }).click();
  const field = nav(page).getByRole('textbox', { name: 'Group name' });
  await field.fill(name);
  await field.press('Enter');
  await expect(group(page, name)).toBeVisible();
}

async function groupAction(page: Page, name: string, action: string) {
  await group(page, name).getByRole('heading').hover();
  await group(page, name).getByRole('button', { name: 'Group actions' }).click();
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

const fold = (page: Page, name: string) =>
  group(page, name).getByRole('button', { name, exact: true });

test('New group, Move to group, drag, Rename, group drag, and fold persist across restart', async ({
  page,
}) => {
  for (const name of ['Ivy', 'Oak', 'Elm']) await newBotAPI(m, name);
  await page.goto(m.url);

  await nav(page).getByRole('button', { name: 'New', exact: true }).click();
  await expect(page.getByRole('menuitem', { name: 'New Bot' })).toBeVisible();
  await page.getByRole('menuitem', { name: 'New group' }).click();
  const field = nav(page).getByRole('textbox', { name: 'Group name' });
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

  await sidebarRow(page, 'Oak').dragTo(group(page, 'Frontend').getByRole('heading'));
  await expect(group(page, 'Frontend').getByRole('link', { name: /\bOak\b/ })).toBeVisible();
  await expect(group(page, 'Ungrouped').getByRole('link', { name: /\bElm\b/ })).toBeVisible();

  await groupAction(page, 'Backend', 'Rename');
  const rename = nav(page).getByRole('textbox', { name: 'Group name' });
  await rename.fill('Platform');
  await rename.press('Enter');
  await expect(group(page, 'Platform').getByRole('link', { name: /\bIvy\b/ })).toBeVisible();
  await expect(group(page, 'Backend')).toHaveCount(0);

  await expect.poll(() => order(page)).toEqual(['Platform', 'Frontend', 'Ungrouped']);
  await group(page, 'Frontend')
    .getByRole('heading')
    .dragTo(group(page, 'Platform').getByRole('heading'), {
      targetPosition: { x: 20, y: 2 },
    });
  await expect.poll(() => order(page)).toEqual(['Frontend', 'Platform', 'Ungrouped']);
  await group(page, 'Frontend')
    .getByRole('heading')
    .dragTo(group(page, 'Platform').getByRole('heading'), {
      targetPosition: { x: 20, y: 20 },
    });
  await expect.poll(() => order(page)).toEqual(['Platform', 'Frontend', 'Ungrouped']);
  await group(page, 'Frontend')
    .getByRole('heading')
    .dragTo(group(page, 'Platform').getByRole('heading'), {
      targetPosition: { x: 20, y: 2 },
    });
  await expect.poll(() => order(page)).toEqual(['Frontend', 'Platform', 'Ungrouped']);

  await group(page, 'Platform').getByRole('heading').hover();
  await fold(page, 'Platform').click();
  await expect(fold(page, 'Platform')).toHaveAttribute('aria-expanded', 'false');
  await expect(sidebarRow(page, 'Ivy')).toBeHidden();

  m.kill();
  await m.restart();
  await page.reload();
  await expect.poll(() => order(page)).toEqual(['Frontend', 'Platform', 'Ungrouped']);
  await expect(fold(page, 'Platform')).toHaveAttribute('aria-expanded', 'false');
  await expect(sidebarRow(page, 'Ivy')).toBeHidden();
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

test('a folded group shows a red dot only while a Bot inside needs the user', async ({ page }) => {
  const pager = await newBotAPI(m, 'Pager');
  await newBotAPI(m, 'Calm');
  await page.goto(m.url);
  await newGroup(page, 'Ops');
  await newGroup(page, 'Quiet');
  await moveToGroup(page, 'Pager', 'Ops');
  await moveToGroup(page, 'Calm', 'Quiet');
  for (const name of ['Ops', 'Quiet']) {
    await group(page, name).getByRole('heading').hover();
    await fold(page, name).click();
    await expect(fold(page, name)).toHaveAttribute('aria-expanded', 'false');
  }
  await expect(group(page, 'Ops')).not.toHaveAttribute('data-marker', /./);

  const waiting = await openThreadAPI(m, pager.id, prompts.write);
  await expect
    .poll(async () => (await api(m, 'GET', `/api/threads/${waiting.id}`)).approval?.id)
    .toBeTruthy();
  await expect(group(page, 'Ops')).toHaveAttribute('data-marker', 'needs-you');
  await expect(group(page, 'Quiet')).not.toHaveAttribute('data-marker', /./);

  const { approval } = await api(m, 'GET', `/api/threads/${waiting.id}`);
  await api(m, 'POST', `/api/approvals/${approval.id}`, { approve: true });
  await expect(group(page, 'Ops')).not.toHaveAttribute('data-marker', /./);
});
