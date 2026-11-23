// A tell_user message reaches the person when the Bot sends it, not when its background run ends:
// in the open chat, and in the chat after a reload.
import { expect, type Page, test } from '@playwright/test';
import { api, type Machine, startAtoll } from './atoll';
import { delegate, helperThreads, makeBoss, runStatus, startThread } from './helpers-fixture';

let m: Machine;
test.beforeEach(async () => {
  m = await startAtoll();
});
test.afterEach(() => m?.stop());

// The helper plays testdata/claude/helper-key.jsonl. The boss woken with its reply plays
// wake-tell-live.jsonl: a tell_user call, a pause of 8 s, a second call, then a final message.
const keyJob = 'Run the checks, then say what is missing.';
const toldFirst = 'Tester is stuck: the staging key is missing.';

// handOff has Poteto hand Tester the job from a new thread and returns the boss thread id once
// the helper waits on an approval and Poteto's run for the hand-off has ended.
async function handOff(page: Page) {
  await page.goto(m.url);
  const boss = await startThread(page, 'Poteto', delegate([['Tester', keyJob]]));
  const [helper] = await helperThreads(m, boss, 1);
  await expect
    .poll(async () => (await api(m, 'GET', `/api/threads/${helper.id}`)).approval?.id ?? '', {
      timeout: 15_000,
    })
    .not.toBe('');
  await expect.poll(() => runStatus(m, boss), { timeout: 15_000 }).not.toBe('running');
  return { boss, helper: helper.id };
}

async function approve(helper: string) {
  const approval = (await api(m, 'GET', `/api/threads/${helper}`)).approval.id;
  await api(m, 'POST', `/api/approvals/${approval}`, { approve: true });
}

// told waits until the boss's first tell_user message is stored.
async function told(boss: string) {
  await expect
    .poll(
      async () =>
        (await api(m, 'GET', `/api/threads/${boss}/messages`)).some(
          (msg: { text: string }) => msg.text === toldFirst,
        ),
      { timeout: 30_000, intervals: [50] },
    )
    .toBe(true);
}

test('a tell_user message shows in the open chat within a second, while the run goes', async ({
  page,
}) => {
  await makeBoss(m, ['Tester']);
  const { boss, helper } = await handOff(page);
  await approve(helper);
  await told(boss);

  const log = page.getByRole('log', { name: 'Messages' });
  await expect(log.getByText(toldFirst)).toBeVisible({ timeout: 1_000 });
  expect(await runStatus(m, boss)).toBe('running');
});

test('a tell_user message is in the chat after a reload, while the run goes', async ({ page }) => {
  await makeBoss(m, ['Tester']);
  const { boss, helper } = await handOff(page);
  await approve(helper);
  await told(boss);

  await page.reload();
  await expect(page.getByRole('log', { name: 'Messages' }).getByText(toldFirst)).toBeVisible({
    timeout: 2_000,
  });
  expect(await runStatus(m, boss)).toBe('running');
});
