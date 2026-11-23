// A helper waiting on the person counts on Now and lists its boss thread under Needs you: the
// Now button counts it, the row has the light dot and the note Boss · Helper: ask, and answering
// clears the count and the row together.
import { expect, type Page, test } from '@playwright/test';
import { api, lines, type Machine, marks, openNow, startAtoll, waitStatus } from './atoll';
import { bossHandsOff, reportPrompt } from './helpers-fixture';

let m: Machine;
test.beforeEach(async () => {
  m = await startAtoll();
});
test.afterEach(() => m?.stop());

const nowButton = (page: Page) =>
  page.getByRole('navigation', { name: 'Bots' }).getByRole('button', { name: /^Now\b/ });

// onlyHelperWaits hands off to Tester, which asks to run make check, and leaves the page on
// Settings: the boss thread is done, so only the helper waits.
async function onlyHelperWaits(page: Page) {
  const { boss, thread } = await bossHandsOff(m, page, [['Tester', reportPrompt]]);
  const helper = thread('Tester');
  await waitStatus(m, helper, 'waiting');
  await waitStatus(m, boss, 'done');
  await page.goto(`${m.url}/settings`);
  await page.mouse.move(1270, 5);
  return { boss, helper };
}

test('with only a helper waiting on an approval, the Now button reads 1', async ({ page }) => {
  await onlyHelperWaits(page);

  await expect(nowButton(page)).toHaveAccessibleName(/^Now\D*\b1\b/);
  await expect(nowButton(page)).toContainText('1');
});

test('Now lists the boss thread under Needs you with the light dot and the note Boss · Helper: ask', async ({
  page,
}) => {
  const { boss } = await onlyHelperWaits(page);
  const title: string = (await api(m, 'GET', `/api/threads/${boss}`)).title;

  const now = await openNow(page);
  const row = now
    .getByRole('list', { name: 'Needs you', exact: true })
    .getByRole('listitem')
    .filter({ has: page.locator(`a[href$="/threads/${boss}"]`) });
  await expect(row).toHaveCount(1);
  await expect.poll(() => lines(row)).toEqual([title, 'Poteto · Tester: Wants to run make check']);
  await expect.poll(async () => (await marks(row)).map((d) => d.colour)).toEqual(['light']);
});

test('when the person answers, the Now count and the boss row clear together', async ({ page }) => {
  const { boss, helper } = await onlyHelperWaits(page);
  const now = await openNow(page);
  const needsYou = now.getByRole('list', { name: 'Needs you', exact: true });
  const row = needsYou
    .getByRole('listitem')
    .filter({ has: page.locator(`a[href$="/threads/${boss}"]`) });
  await expect(nowButton(page)).toHaveAccessibleName(/^Now\D*\b1\b/);
  await expect(row).toHaveCount(1);

  const { approval } = await api(m, 'GET', `/api/threads/${helper}`);
  await api(m, 'POST', `/api/approvals/${approval.id}`, { approve: true });
  await expect(nowButton(page)).toHaveAccessibleName(/^Now\D*$/);
  await expect(needsYou).toHaveCount(0);
  await expect(row).toHaveCount(0);
});
