// Slice 6 sidebar (#469): one row per Bot, Chief first, its creature showing the Bot's
// state; reduced motion stops every animation.
import { expect, type Locator, test } from '@playwright/test';
import {
  api,
  type Machine,
  newBotAPI,
  openThreadAPI,
  prompts,
  running,
  sidebarRow,
  startAtoll,
  waitStatus,
} from './atoll';

let m: Machine;
test.beforeAll(async () => {
  // codex is installed but not signed in: its Bots are asleep.
  m = await startAtoll({ fakes: ['claude', 'codex'] });
  await newBotAPI(m, 'Idle', 'claude', { look: 'seal' });
  await newBotAPI(m, 'Sleeper', 'codex', { look: 'turtle' });
  for (const [name, prompt, status] of [
    ['Worker', prompts.hang, 'running'],
    ['Asker', prompts.write, 'waiting'],
    ['Breaker', prompts.none, 'failed'],
  ]) {
    const b = await newBotAPI(m, name, 'claude', { look: 'whale' });
    const th = await openThreadAPI(m, b.id, prompt);
    await expect
      .poll(async () => (await api(m, 'GET', `/api/threads/${th.id}`)).run_status, {
        timeout: 15_000,
      })
      .toBe(status);
  }
});
test.afterAll(() => m?.stop());

test('Bot rows show each state on the creature, Chief first', async ({ page }) => {
  // Landing on / would open the newest thread, Breaker's failed one, and reading it clears its pip.
  await page.goto(`${m.url}/bots/chief/new`);
  const nav = page.getByRole('navigation', { name: 'Bots' });
  await expect(nav.getByRole('link').first()).toContainText('Chief');
  await expect(nav.getByRole('button', { name: 'New', exact: true })).toBeVisible();
  for (const [name, img] of [
    ['Chief', 'Jellyfish, idle'],
    ['Idle', 'Seal, idle'],
    ['Sleeper', 'Turtle, asleep'],
    ['Worker', 'Whale, working'],
    ['Asker', 'Whale, needs you'],
    ['Breaker', 'Whale, failed'],
  ]) {
    await expect(sidebarRow(page, name).getByRole('img', { name: img, exact: true })).toBeVisible();
  }
  await expect(sidebarRow(page, 'Asker')).toContainText('Wants to edit hello.txt');
});

test('reduced motion stops the animations', async ({ page }) => {
  await page.emulateMedia({ reducedMotion: 'no-preference' });
  await page.goto(m.url);
  await sidebarRow(page, 'Worker').click();
  await expect(page.getByRole('log', { name: 'Messages' }).getByRole('status')).toBeVisible();
  await expect.poll(() => running(page)).toBeGreaterThan(0);

  await page.emulateMedia({ reducedMotion: 'reduce' });
  await expect.poll(() => running(page)).toBe(0);
  await page.reload();
  await expect(page.getByRole('log', { name: 'Messages' }).getByRole('status')).toBeVisible();
  await expect.poll(() => running(page)).toBe(0);
});

// waves counts the wave-dot animations playing inside el.
const waves = (el: Locator) =>
  el.evaluate(
    (node) =>
      node
        .getAnimations({ subtree: true })
        .filter((a) => (a as CSSAnimation).animationName === 'wave').length,
  );

test('the creature follows the runs; the row shows the last reply, Stopped after a stop, the ask when waiting, never the words Needs you', async ({
  page,
}) => {
  await page.emulateMedia({ reducedMotion: 'no-preference' });
  const bot = await newBotAPI(m, 'Calm', 'claude', { look: 'whale' });
  await waitStatus(m, (await openThreadAPI(m, bot.id, prompts.keep)).id, 'done');
  const replied = (await api(m, 'GET', `/api/bots/${bot.id}`)).activity;
  expect(replied).toBe('ok');

  await page.goto(m.url);
  const row = sidebarRow(page, 'Calm');
  const idle = row.getByRole('img', { name: 'Whale, idle', exact: true });
  await expect(idle).toBeVisible();
  await expect(row).toContainText(replied);
  expect(await waves(row)).toBe(0);

  const busy = await openThreadAPI(m, bot.id, prompts.hang);
  await waitStatus(m, busy.id, 'running');
  await expect(row.getByRole('img', { name: 'Whale, working', exact: true })).toBeVisible();
  await expect.poll(() => waves(row)).toBeGreaterThan(0);
  await api(m, 'POST', `/api/runs/${busy.run.id}/stop`);
  await waitStatus(m, busy.id, 'stopped');
  await expect(idle).toBeVisible();
  await expect(row).toContainText('Stopped');

  const asking = await openThreadAPI(m, bot.id, prompts.write);
  await waitStatus(m, asking.id, 'waiting');
  await expect(row.getByRole('img', { name: 'Whale, needs you', exact: true })).toBeVisible();
  await expect(row).toContainText('Wants to edit hello.txt');
  await expect(row).not.toContainText('Needs you');
  const { approval } = await api(m, 'GET', `/api/threads/${asking.id}`);
  await api(m, 'POST', `/api/approvals/${approval.id}`, { approve: true });
  await waitStatus(m, asking.id, 'done');
  await expect(idle).toBeVisible();
  await expect(row).toContainText((await api(m, 'GET', `/api/bots/${bot.id}`)).activity);
  await expect(row).not.toContainText('Wants to edit');
});

test('a running older thread keeps the Bot working when a newer thread has finished', async ({
  page,
}) => {
  await page.emulateMedia({ reducedMotion: 'no-preference' });
  const bot = await newBotAPI(m, 'Older worker');
  const busy = await openThreadAPI(m, bot.id, prompts.hang);
  try {
    await waitStatus(m, busy.id, 'running');
    const newer = await openThreadAPI(m, bot.id, prompts.keep);
    await waitStatus(m, newer.id, 'done');

    await page.goto(m.url);
    const row = sidebarRow(page, 'Older worker');
    const summary = (await api(m, 'GET', '/api/bots')).find((b: { id: string }) => b.id === bot.id);
    expect(summary.state).toBe('working');
    expect(summary.activity).toBe('Running Run the build');
    await expect(row.getByRole('img', { name: /, working$/ })).toBeVisible();
  } finally {
    await api(m, 'POST', `/api/runs/${busy.run.id}/stop`);
    await waitStatus(m, busy.id, 'stopped');
  }
});

test('an older thread awaiting approval takes priority over a newer finished thread', async ({
  page,
}) => {
  const bot = await newBotAPI(m, 'Older approval');
  const asking = await openThreadAPI(m, bot.id, prompts.write);
  await waitStatus(m, asking.id, 'waiting');
  const newer = await openThreadAPI(m, bot.id, prompts.keep);
  await waitStatus(m, newer.id, 'done');

  await page.goto(m.url);
  const row = sidebarRow(page, 'Older approval');
  await expect(row.getByRole('img', { name: /, needs you$/ })).toBeVisible();
  await expect(row).toContainText('Wants to edit hello.txt');
});
