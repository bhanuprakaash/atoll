// While the person's Stop holds a lead's helper reports, the composer line says how many helpers
// still run and how many reports wait. Issue #499; Fake Claude only.
import { expect, test } from '@playwright/test';
import { api, type Machine, prompts, startAtoll } from './atoll';
import { bossHandsOff, runStatus, send, composerArea } from './helpers-fixture';

let m: Machine;
test.beforeEach(async () => {
  m = await startAtoll();
});
test.afterEach(() => m?.stop());

async function stopRun(threadID: string) {
  const { run_id } = await api(m, 'GET', `/api/threads/${threadID}`);
  await api(m, 'POST', `/api/runs/${run_id}/stop`);
}

test('after a Stop on the lead the composer line counts helpers still running and reports waiting, a part at 0 left out, and clears with the next message', async ({
  page,
}) => {
  const { boss, helpers } = await bossHandsOff(m, page, [
    ['Tester', `Poll cost flake. ${prompts.hang}`],
    ['Tester', `Header island width. ${prompts.hang}`],
  ]);
  await expect.poll(() => runStatus(m, boss)).toBe('done');
  const region = composerArea(page);
  expect(helpers).toHaveLength(2);

  await send(page, prompts.chatPolishHang);
  await expect.poll(() => runStatus(m, boss)).toBe('running');
  await page.getByRole('button', { name: 'Stop', exact: true }).click();
  await expect.poll(() => runStatus(m, boss)).toBe('stopped');
  await expect(region.getByText('2 helpers still running', { exact: true })).toBeVisible();
  await expect(region).not.toContainText('working');

  await stopRun(helpers[0].id);
  await expect.poll(() => runStatus(m, helpers[0].id)).toBe('stopped');
  await expect(
    region.getByText('1 helper still running · 1 report waiting', {
      exact: true,
    }),
  ).toBeVisible();
  expect(await runStatus(m, boss)).toBe('stopped');

  await stopRun(helpers[1].id);
  await expect.poll(() => runStatus(m, helpers[1].id)).toBe('stopped');
  await expect(region.getByText('2 reports waiting', { exact: true })).toBeVisible();
  await expect(region).not.toContainText('still running');
  expect(await runStatus(m, boss)).toBe('stopped');

  await send(page, 'Tester, any news?');
  await expect.poll(() => runStatus(m, boss)).toBe('done');
  await expect(composerArea(page)).not.toContainText(/helpers? still running|reports? waiting/);
});
