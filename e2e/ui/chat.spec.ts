// Slice 6 chat: one working line by tool kind, never raw tool calls; a summary line above the
// reply; the user's message as the only bubble; approvals with Allow and Deny; blocked; Stop.
import { expect, test } from '@playwright/test';
import {
  api,
  type Machine,
  newBotAPI,
  openThreads,
  prompts,
  recordText,
  seenText,
  sidebarRow,
  startAtoll,
} from './atoll';

let m: Machine;
test.beforeAll(async () => {
  m = await startAtoll();
});
test.afterAll(() => m?.stop());

async function emptyThread(page: import('@playwright/test').Page, name: string) {
  await newBotAPI(m, name);
  await page.goto(m.url);
  await sidebarRow(page, name).click();
  await expect(page.getByRole('heading', { name: `What should ${name} work on?` })).toBeVisible();
}

async function send(page: import('@playwright/test').Page, text: string) {
  await page.getByRole('textbox', { name: 'Message' }).fill(text);
  await page.getByRole('button', { name: 'Send' }).click();
}

const rawTool =
  /\b(Grep|Read|Edit|Write|Bash|ToolSearch|commandExecution|fileChange|mcpToolCall)\b|mcp__|finish_run/;

test('one working line by tool kind, a summary above the reply, the user message the only bubble', async ({
  page,
}) => {
  await emptyThread(page, 'Wren');
  await recordText(page);
  await send(page, prompts.working);

  const log = page.getByRole('log', { name: 'Messages' });
  const working = log.getByRole('status');
  for (const step of [
    /Searching selectedRepo/,
    /Reading repo-step\.tsx/,
    /Editing wizard\.tsx/,
    /Running Run the tests/,
  ]) {
    await expect(working).toHaveCount(1);
    await expect(working).toHaveText(step);
  }

  const reply = log.getByText('Fixed the wizard repo select; tests pass.', {
    exact: true,
  });
  await expect(reply).toBeVisible();
  await expect(working).toHaveCount(0);
  const summary = log.getByText(/ · Worked \d+(m \d+)?s · /);
  await expect(summary).toContainText('read 1 file');
  await expect(summary).toContainText('edited 1');
  expect((await summary.boundingBox())!.y).toBeGreaterThan((await reply.boundingBox())!.y);

  const bubbles = log.getByRole('article');
  await expect(bubbles).toHaveCount(1);
  await expect(log.getByRole('article', { name: 'You' })).toHaveText(prompts.working);
  expect(await seenText(page)).not.toMatch(rawTool);

  // The summary comes from the run, not from events this page happened to see.
  await page.reload();
  await expect(log.getByText(/ · Worked \d+(m \d+)?s · /)).toContainText('edited 1');
});

test('an approval shows Allow and Deny, leaves once answered; Deny ends in a blocked line', async ({ page }) => {
  await emptyThread(page, 'Ash');
  const log = page.getByRole('log', { name: 'Messages' });

  await send(page, prompts.write);
  const allow = log.getByRole('button', { name: 'Allow', exact: true });
  await expect(allow).toBeVisible();
  await expect(log.getByRole('button', { name: 'Deny', exact: true })).toBeVisible();
  await expect(log.getByText(/hello\.txt/).first()).toBeVisible();
  await expect(log.getByRole('button', { name: 'Show diff' })).toBeVisible();
  await expect(sidebarRow(page, 'Ash').getByRole('img', { name: /, needs you$/ })).toBeVisible();
  await allow.click();
  await expect(log.getByText(/^Created .*hello\.txt/)).toBeVisible();
  await expect(allow).toHaveCount(0);
  await expect(log.getByText('Allowed', { exact: false })).toHaveCount(0);

  await send(page, prompts.write);
  await log.getByRole('button', { name: 'Deny', exact: true }).click();
  await expect(
    log.getByText('Blocked: User rejected the Write action, so hello.txt was not created.'),
  ).toBeVisible();
  await expect(log.getByRole('button', { name: 'Allow', exact: true })).toHaveCount(0);
});

test('Stop ends a running run and the chat says it was stopped', async ({ page }) => {
  await emptyThread(page, 'Fern');
  const log = page.getByRole('log', { name: 'Messages' });
  await send(page, prompts.hang);

  const stop = page.getByRole('button', { name: 'Stop' });
  await expect(stop).toBeVisible();
  await expect(page.getByRole('button', { name: 'Send' })).toHaveCount(0);
  await expect(log.getByRole('status')).toHaveText(/Running Run the build/);
  await stop.click();

  await expect(log.getByText(/Stopped by you after \d+(m \d+)?s/)).toBeVisible();
  await expect(log.getByRole('status')).toHaveCount(0);
  await expect(page.getByRole('button', { name: 'Send' })).toBeVisible();
  const bots = await api(m, 'GET', '/api/bots');
  const fern = bots.find((b: { name: string }) => b.name === 'Fern');
  const [thread] = await api(m, 'GET', `/api/bots/${fern.id}/threads`);
  expect(thread.run_status).toBe('stopped');
});

test('a failed run shows a line with Retry', async ({ page }) => {
  await emptyThread(page, 'Moss');
  const log = page.getByRole('log', { name: 'Messages' });
  await send(page, prompts.none);
  await expect(log.getByText(/^Run failed: .*no recording for prompt/)).toBeVisible();
  await expect(log.getByRole('button', { name: 'Retry' })).toBeVisible();
  await expect(log.getByRole('button', { name: 'Sign in again' })).toHaveCount(0);
  await expect(sidebarRow(page, 'Moss').getByRole('img', { name: /, failed$/ })).toBeVisible();
  const panel = await openThreads(page);
  await expect(panel.getByRole('button', { name: 'New thread' })).toBeVisible();
});
