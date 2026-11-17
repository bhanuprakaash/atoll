// A run the restart cut short reads `Run failed: Reef restarted.` in its thread, and the old
// reason, `Atoll stopped while the run was going`, is on no screen. Design: docs/design.md, Threads
// and runs and App and UI (Errors).
import { expect, test } from '@playwright/test';
import {
  type Machine,
  newBotAPI,
  openThreadAPI,
  prompts,
  sidebarRow,
  startAtoll,
  waitStatus,
} from './atoll';

let m: Machine;
test.beforeAll(async () => {
  m = await startAtoll();
});
test.afterAll(() => m?.stop());

test('a run cut short by a restart reads Run failed: Reef restarted.', async ({ page }) => {
  const bot = await newBotAPI(m, 'Heron');
  const th = await openThreadAPI(m, bot.id, prompts.hang);
  await waitStatus(m, th.id, 'running');

  m.kill();
  await m.restart();
  await waitStatus(m, th.id, 'failed');

  await page.goto(m.url);
  await sidebarRow(page, 'Heron').click();
  const log = page.getByRole('log', { name: 'Messages' });
  await expect(log.getByText('Run failed: Reef restarted.', { exact: true })).toBeVisible();
  await expect(page.getByText(/Atoll stopped/)).toHaveCount(0);
  await expect(log.getByText(/Atoll stopped/)).toHaveCount(0);
});
