// What a run cost: the usage segment of the run footer and each Bot's last 24 h in the sidebar.
// Contract: e2e/run-cost-contract.md and issue #193.
import { expect, test } from '@playwright/test';
import {
  type Machine,
  newBotAPI,
  openThreadAPI,
  sendMessageAPI,
  sidebarRow,
  startAtoll,
} from './atoll';

const first = 'Cost check: look around the folder six times, then answer.';
const second = 'Cost check, second round: look again and answer.';

let m: Machine;
test.beforeAll(async () => {
  m = await startAtoll({
    fakes: ['claude', 'codex'],
    signedIn: ['claude', 'codex'],
  });
});
test.afterAll(() => m?.stop());

// twoRuns gives a Bot a thread with the first run and a resumed second one, both done.
async function twoRuns(name: string, provider: string) {
  const bot = await newBotAPI(m, name, provider);
  const thread = await openThreadAPI(m, bot.id, first);
  await expect
    .poll(
      async () => (await (await fetch(`${m.url}/api/threads/${thread.id}`)).json()).run_status,
      {
        timeout: 15_000,
      },
    )
    .toBe('done');
  await sendMessageAPI(m, thread.id, second);
  return { bot, thread };
}

test('a Claude run ends with its model, calls and cost, counting only itself', async ({ page }) => {
  const { bot, thread } = await twoRuns('Costly', 'claude');
  await page.goto(`${m.url}/bots/${bot.id}/threads/${thread.id}`);
  const log = page.getByRole('log', { name: 'Messages' });
  const one = log.getByText(/Sonnet 5\.5, 7 calls, \$0\.19$/);
  const two = log.getByText(/Sonnet 5\.5, 3 calls, \$0\.05$/);
  await expect(one).toHaveCount(1);
  await expect(two).toHaveCount(1);
  await expect(log.getByText(/\$0\.24/)).toHaveCount(0);

  const replies = [
    log.getByText('The folder has no files; I looked six times.', {
      exact: true,
    }),
    log.getByText('Still empty.', { exact: true }),
  ];
  for (const [i, line] of [one, two].entries())
    expect((await line.boundingBox())!.y).toBeGreaterThan((await replies[i].boundingBox())!.y);

  // Quiet: the line has the muted colour of the run's time, not the reply's ink.
  const color = (l: typeof one) => l.evaluate((el) => getComputedStyle(el).color);
  const muted = await log
    .getByTestId('message-time')
    .first()
    .evaluate((el) => getComputedStyle(el).color);
  expect(await color(one)).toBe(muted);
  expect(await color(two)).toBe(muted);
});

test('a Codex run ends with its model and calls, and no dollars', async ({ page }) => {
  const { bot, thread } = await twoRuns('Codexer', 'codex');
  await page.goto(`${m.url}/bots/${bot.id}/threads/${thread.id}`);
  const log = page.getByRole('log', { name: 'Messages' });
  await expect(log.getByText(/gpt-5\.6-sol, 3 calls$/)).toHaveCount(1);
  await expect(log.getByText(/gpt-5\.6-sol, 2 calls$/)).toHaveCount(1);
  expect(await log.innerText()).not.toContain('$');
});

test("the Bots list shows each Bot's cost of the last 24 hours", async ({ page }) => {
  await twoRuns('Priced', 'claude');
  await twoRuns('Unpriced', 'codex');
  await newBotAPI(m, 'Resting', 'claude');
  await page.goto(m.url);
  // Both runs: $0.19 + $0.05, not the session's running total of either.
  await expect(sidebarRow(page, 'Priced')).toContainText('$0.24');
  await expect(sidebarRow(page, 'Unpriced')).not.toContainText('$');
  await expect(sidebarRow(page, 'Resting')).not.toContainText('$');
});
