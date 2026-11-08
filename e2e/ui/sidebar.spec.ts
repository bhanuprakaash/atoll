// Slice 6 sidebar: one row per Bot, Chief first, its creature showing the Bot's state; reduced
// motion stops every animation.
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
  threadTree,
} from './atoll';

let m: Machine;
test.beforeAll(async () => {
  // codex is installed but not signed in: its Bots are asleep.
  m = await startAtoll({ fakes: ['claude', 'codex'] });
  await newBotAPI(m, 'Idle', 'claude', { look: 'penguin' });
  await newBotAPI(m, 'Sleeper', 'codex', { look: 'narwhal' });
  for (const [name, prompt, status] of [
    ['Worker', prompts.hang, 'running'],
    ['Asker', prompts.write, 'waiting'],
    ['Breaker', prompts.none, 'failed'],
  ]) {
    const b = await newBotAPI(m, name, 'claude', { look: 'shark' });
    const th = await openThreadAPI(m, b.id, prompt);
    await expect
      .poll(async () => (await api(m, 'GET', `/api/threads/${th.id}`)).run_status, {
        timeout: 15_000,
      })
      .toBe(status);
  }
});
test.afterAll(() => m?.stop());

test('sidebar rows show each state on the creature, Chief first', async ({ page }) => {
  await page.goto(m.url);
  const nav = page.getByRole('navigation', { name: 'Bots' });
  await expect(nav.getByRole('link').first()).toContainText('Chief');
  await expect(nav.getByRole('button', { name: 'New', exact: true })).toBeVisible();
  for (const [name, img] of [
    ['Chief', 'Jellyfish, idle'],
    ['Idle', 'Penguin, idle'],
    ['Sleeper', 'Narwhal, asleep'],
    ['Worker', 'Shark, working'],
    ['Asker', 'Shark, needs you'],
    ['Breaker', 'Shark, failed'],
  ]) {
    await expect(sidebarRow(page, name).getByRole('img', { name: img, exact: true })).toBeVisible();
  }
  await expect(sidebarRow(page, 'Asker')).toContainText(/hello\.txt/);
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

async function waitStatus(threadID: string, status: string) {
  await expect
    .poll(async () => (await api(m, 'GET', `/api/threads/${threadID}`)).run_status, {
      timeout: 15_000,
    })
    .toBe(status);
}

// waves counts the wave-dot animations playing inside el.
const waves = (el: Locator) =>
  el.evaluate(
    (node) =>
      node
        .getAnimations({ subtree: true })
        .filter((a) => (a as CSSAnimation).animationName === 'wave').length,
  );

test('an idle Bot row is one line with a 36px creature; the activity line shows only while a run works or an approval waits', async ({
  page,
}) => {
  await page.emulateMedia({ reducedMotion: 'no-preference' });
  const bot = await newBotAPI(m, 'Calm', 'claude', { look: 'whale' });
  await waitStatus((await openThreadAPI(m, bot.id, prompts.keep)).id, 'done');
  const replied = (await api(m, 'GET', `/api/bots/${bot.id}`)).activity;
  expect(replied).toBe('ok');

  await page.goto(m.url);
  const row = sidebarRow(page, 'Calm');
  const idle = row.getByRole('img', { name: 'Whale, idle', exact: true });
  await expect(idle).toBeVisible();
  expect(Math.abs((await idle.boundingBox())!.width - 36)).toBeLessThanOrEqual(1);
  await expect(row).not.toContainText(replied);
  expect(await waves(row)).toBe(0);

  const busy = await openThreadAPI(m, bot.id, prompts.hang);
  await waitStatus(busy.id, 'running');
  await expect(row.getByRole('img', { name: 'Whale, working', exact: true })).toBeVisible();
  await expect(row).toContainText('Running Run the build');
  await expect.poll(() => waves(row)).toBe(3);

  await api(m, 'POST', `/api/runs/${busy.run.id}/stop`);
  await waitStatus(busy.id, 'stopped');
  await expect(idle).toBeVisible();
  expect((await api(m, 'GET', `/api/bots/${bot.id}`)).activity).toBe('Stopped');
  await expect(row).not.toContainText('Running');
  await expect(row).not.toContainText('Stopped');
  await expect.poll(() => waves(row)).toBe(0);

  const asking = await openThreadAPI(m, bot.id, prompts.write);
  await waitStatus(asking.id, 'waiting');
  const needs = row.getByText(/^Needs you: /);
  await expect(needs).toHaveText('Needs you: Write hello.txt');
  const needsInk = await threadTree(page, 'Calm')
    .getByText('Needs you', { exact: true })
    .evaluate((el) => getComputedStyle(el).color);
  await expect(needs).toHaveCSS('color', needsInk);

  const { approval } = await api(m, 'GET', `/api/threads/${asking.id}`);
  await api(m, 'POST', `/api/approvals/${approval.id}`, { approve: true });
  await waitStatus(asking.id, 'done');
  await expect(idle).toBeVisible();
  await expect(row).not.toContainText('Needs you');
  const done = (await api(m, 'GET', `/api/bots/${bot.id}`)).activity;
  expect(done).not.toBe('');
  await expect(row).not.toContainText(done);
  expect(await waves(row)).toBe(0);
});

test('a running older thread supplies the Bot activity when a newer thread has finished', async ({ page }) => {
  await page.emulateMedia({ reducedMotion: 'no-preference' });
  const bot = await newBotAPI(m, 'Older worker');
  const busy = await openThreadAPI(m, bot.id, prompts.hang);
  await waitStatus(busy.id, 'running');
  const newer = await openThreadAPI(m, bot.id, prompts.keep);
  await waitStatus(newer.id, 'done');

  await page.goto(m.url);
  const row = sidebarRow(page, 'Older worker');
  const summary = (await api(m, 'GET', '/api/bots')).find((b: { id: string }) => b.id === bot.id);
  expect(summary.state).toBe('working');
  expect(summary.activity).toBe('Running Run the build');
  await expect(row).toContainText('Running Run the build');
  await expect.poll(() => waves(row)).toBe(3);

  await api(m, 'POST', `/api/runs/${busy.run.id}/stop`);
  await waitStatus(busy.id, 'stopped');
});
