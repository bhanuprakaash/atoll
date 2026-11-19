// Helpers in the boss's thread: Thread details, the header island, and a read-only
// helper thread.
import { expect, test } from '@playwright/test';
import { api, type Machine, prompts, sidebarRow, startAtoll, threadPane } from './atoll';
import {
  box,
  openDetails,
  bossHandsOff,
  details,
  reportPrompt,
  reportReply,
  runStatus,
  composerArea,
} from './helpers-fixture';

let m: Machine;
test.beforeEach(async () => {
  m = await startAtoll();
});
test.afterEach(() => m?.stop());

test('two helpers: one handoff line, a row each in Thread details and a creature in the island; Stop in the helper thread', async ({
  page,
}) => {
  const { bots, thread } = await bossHandsOff(m, page, [
    ['Tester', prompts.hang],
    ['Reviewer', prompts.hang],
  ]);
  const log = page.getByRole('log', { name: 'Messages' });
  const handedOff = log.getByText('Handed off to Tester and Reviewer', {
    exact: true,
  });
  await expect(handedOff).toHaveCount(1);
  await expect(log.getByText('Handed off to Tester', { exact: true })).toHaveCount(0);

  const region = await openDetails(page);
  await expect(region.getByLabel('Tester: working', { exact: true })).toHaveCount(1);
  await expect(region.getByLabel('Reviewer: working', { exact: true })).toHaveCount(1);
  const rows = details(page).getByRole('listitem');
  await expect(rows).toHaveCount(2);
  await expect(rows.filter({ hasText: 'Tester' }).getByText('Running make build')).toHaveCount(1);
  await expect(rows.filter({ hasText: 'Reviewer' }).getByText('Running make build')).toHaveCount(1);
  await expect(details(page)).not.toContainText('Needs you');
  const open = details(page).getByRole('link').filter({ hasNotText: /^[#!]/ });
  await expect(open).toHaveCount(2);
  await expect(page.getByText(/Working with/)).toHaveCount(0);

  const header = page.locator('header');
  const name = await box(header.getByRole('heading', { level: 1, name: 'Poteto' }));
  for (const label of ['Tester: working', 'Reviewer: working']) {
    const creature = header.getByLabel(label, { exact: true });
    await expect(creature).toBeVisible();
    expect((await box(creature)).x).toBeGreaterThanOrEqual(name.x + name.width);
  }

  await open.and(page.locator(`[href$="/threads/${thread('Tester')}"]`)).click();
  await expect(page).toHaveURL(`${m.url}/bots/${bots.Tester.id}/threads/${thread('Tester')}`);
  await expect(page.getByRole('textbox', { name: /^Message/ })).toHaveCount(0);
  await expect(
    page.getByText('Poteto is handling this thread. Talk to Poteto to change it.'),
  ).toBeVisible();
  await page.getByRole('button', { name: 'Stop', exact: true }).click();
  await expect.poll(() => runStatus(m, thread('Tester'))).toBe('stopped');
  await expect.poll(() => runStatus(m, thread('Reviewer'))).toBe('running');
});

test('a helper that hands off reads "Handing off" on its line, never "Setting up"', async ({
  page,
}) => {
  await bossHandsOff(m, page, [['Tester', prompts.handOff]]);
  await openDetails(page);
  const line = details(page).getByRole('listitem').filter({ hasText: 'Tester' });
  await expect(line.getByText('Handing off', { exact: true })).toHaveCount(1);
  await expect(line).not.toContainText('Setting up');
});

test('a helper approval shows under its line and Allow works there; the helper’s reply stays out of the boss chat', async ({
  page,
  context,
}) => {
  const { bots, thread } = await bossHandsOff(m, page, [['Tester', reportPrompt]]);
  const log = page.getByRole('log', { name: 'Messages' });
  await expect(log.getByText('Handed off to Tester', { exact: true })).toHaveCount(1);

  const region = composerArea(page);
  await expect(region).toContainText('Tester');
  await expect(region).toContainText('make check');
  const allow = region.getByRole('button', { name: 'Allow', exact: true });
  await expect(allow).toHaveCount(1);
  await expect(region.getByRole('button', { name: 'Deny', exact: true })).toHaveCount(1);

  const header = page.locator('header');
  await expect(header.getByLabel('Tester: needs you', { exact: true })).toBeVisible();
  await expect(page.getByRole('button', { name: /^Notifications/ })).toHaveAccessibleName(
    /^Notifications\b.*\b1\b/,
  );
  await expect(sidebarRow(page, 'Poteto')).toHaveAttribute('data-marker', 'needs-you');

  const helperPage = await context.newPage();
  await helperPage.goto(`${m.url}/bots/${bots.Tester.id}/threads/${thread('Tester')}`);
  const helperAllow = helperPage
    .getByRole('log', { name: 'Messages' })
    .getByRole('button', { name: 'Allow', exact: true });
  await expect(helperAllow).toHaveCount(1);

  await allow.click();
  await expect(allow).toHaveCount(0);
  await expect(helperAllow).toHaveCount(0);
  await expect.poll(() => runStatus(m, thread('Tester')), { timeout: 30_000 }).toBe('done');

  // The helper's reply and Reef's notice to the boss are not messages of the boss chat.
  await expect(log.getByRole('article', { name: 'Tester' })).toHaveCount(0);
  await expect(
    log.getByRole('article', { name: 'You' }).filter({ hasText: reportReply }),
  ).toHaveCount(0);
  await expect(log).not.toContainText(reportReply);
  await expect(log).not.toContainText('finished thread');
  await expect(log).not.toContainText('Done:');
  await helperPage.reload();
  await expect(helperPage.getByRole('log', { name: 'Messages' })).toContainText(reportReply);
  await expect(header.getByLabel('Tester: needs you', { exact: true })).toHaveCount(0);
  await expect(page.getByRole('button', { name: /^Notifications/ })).toHaveAccessibleName(
    'Notifications',
  );
});

test('the helper thread is not a row of the boss pane and is read-only; Allow there leaves the boss thread', async ({
  page,
  context,
}) => {
  const { poteto, bots, boss, thread } = await bossHandsOff(m, page, [['Tester', reportPrompt]]);
  const bossAllow = composerArea(page).getByRole('button', {
    name: 'Allow',
    exact: true,
  });
  await expect(bossAllow).toHaveCount(1);

  const helperPage = await context.newPage();
  await helperPage.goto(`${m.url}/bots/${poteto.id}/threads/${boss}`);
  await expect(
    sidebarRow(helperPage, 'Tester').getByRole('img', { name: /^Octopus\b/ }),
  ).toBeVisible();
  await expect(threadPane(helperPage, 'Poteto')).toBeVisible();
  await expect(
    threadPane(helperPage, 'Poteto').locator(`a[href$="/threads/${thread('Tester')}"]`),
  ).toHaveCount(0);
  await helperPage.goto(`${m.url}/bots/${bots.Tester.id}/threads/${thread('Tester')}`);
  const helperLog = helperPage.getByRole('log', { name: 'Messages' });
  await expect(helperLog.getByRole('article', { name: 'Poteto' })).toContainText(reportPrompt);
  await expect(helperLog.getByRole('article', { name: 'You' })).toHaveCount(0);

  await expect(helperPage.getByRole('textbox', { name: /^Message/ })).toHaveCount(0);
  await expect(
    helperPage.getByText('Poteto is handling this thread. Talk to Poteto to change it.'),
  ).toBeVisible();
  const stop = helperPage.getByRole('button', { name: 'Stop', exact: true });
  await expect(stop).toBeVisible();

  await helperPage
    .getByRole('log', { name: 'Messages' })
    .getByRole('button', { name: 'Allow', exact: true })
    .click();
  await expect(bossAllow).toHaveCount(0);
  await expect.poll(() => runStatus(m, thread('Tester')), { timeout: 30_000 }).toBe('done');
  await expect(stop).toHaveCount(0);

  const back = "Open Poteto's thread";
  await helperPage
    .getByRole('link', { name: back })
    .or(helperPage.getByRole('button', { name: back }))
    .click();
  await expect(helperPage).toHaveURL(`${m.url}/bots/${poteto.id}/threads/${boss}`);
});
