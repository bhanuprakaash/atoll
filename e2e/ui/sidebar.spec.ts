// Slice 6 sidebar: one row per Bot, Chief first, its creature showing the Bot's state; reduced
// motion stops every animation.
import { expect, test } from '@playwright/test';
import {
  api,
  type Machine,
  newBotAPI,
  openThreadAPI,
  prompts,
  running,
  sidebarRow,
  startAtoll,
} from './atoll';

let m: Machine;
test.beforeAll(async () => {
  // codex is installed but not signed in: its Bots are asleep.
  m = await startAtoll({ fakes: ['claude', 'codex'] });
  await newBotAPI(m, 'Idle', 'claude', { look: 'penguin' });
  await newBotAPI(m, 'Sleeper', 'codex', { look: 'turtle' });
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
  await expect(nav.getByRole('button', { name: 'New Bot' })).toBeVisible();
  for (const [name, img] of [
    ['Chief', 'Jellyfish, idle'],
    ['Idle', 'Penguin, idle'],
    ['Sleeper', 'Turtle, asleep'],
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
