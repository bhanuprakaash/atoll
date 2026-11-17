// A failed run that reached the provider shows Continue; one that never reached it shows Retry.
// Continue resumes the thread's session with a Reef notice and adds no bubble; Retry sends the
// message again on its own bubble. A run only Reef started, an older run and a helper thread show
// neither. Design: docs/design.md, App and UI, the failed-run line.
//
// Assumed names: the buttons are "Continue" and "Retry". The recordings are
// testdata/claude/continue-*.jsonl, which play the resumed session of lifecycle-exit.jsonl (the
// provider dies mid-run) and of run-hang.jsonl (a restart cuts the run short).
import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import { expect, type Page, test } from '@playwright/test';
import {
  api,
  type Machine,
  newBotAPI,
  prompts,
  sendMessageAPI,
  sidebarRow,
  startAtoll,
  waitStatus,
} from './atoll';
import { bossHandsOff } from './helpers-fixture';
import { red } from './image-fixture';

let m: Machine;
test.beforeEach(async () => {
  m = await startAtoll();
});
test.afterEach(() => m?.stop());

const dies = 'Lifecycle check: the process exits mid-request.';
const diesReply = 'Picked up after the exit.';
const restartReply = 'Picked up after the restart.';
const notice = 'Your last run stopped before it finished';
const anyLine = /^(Didn't run|Run failed)\b/;
// A run with a tool call and no reply may read either way, or name the restart.
const restartLine = /Didn't run|Run failed|Reef restarted/;

type Sent = {
  role: string;
  text: string;
  run_id: string;
  from_bot_id?: string;
  kind?: string;
};
const userMessages = async (threadID: string): Promise<(Sent & { attachments: unknown[] })[]> =>
  (await api(m, 'GET', `/api/threads/${threadID}/messages`)).filter(
    (s: Sent) => s.role === 'user' && !s.from_bot_id && !s.kind,
  );

// What the fake claude was asked, in order: one record per user line it read.
const providerPrompts = (): string[] =>
  readFileSync(join(m.bin, 'claude.log.prompts'), 'utf8')
    .split('\n')
    .filter(Boolean)
    .map((l) => JSON.parse(l) as { kind: string; text: string })
    .filter((r) => r.kind === 'user')
    .map((r) => r.text);

async function openBot(page: Page, name: string, provider = 'claude') {
  const bot = await newBotAPI(m, name, provider);
  await page.goto(m.url);
  await sidebarRow(page, name).click();
  await expect(page.getByRole('heading', { name: `What should ${name} work on?` })).toBeVisible();
  return { bot, log: page.getByRole('log', { name: 'Messages' }) };
}

async function send(page: Page, text: string) {
  await page.getByRole('textbox', { name: 'Message' }).fill(text);
  await page.getByRole('button', { name: 'Send' }).click();
}

test('a run that died mid-run shows Continue; Continue resumes with the notice, adds no bubble and the failed line gives way', async ({
  page,
}) => {
  const { bot, log } = await openBot(page, 'Heron');
  await send(page, dies);
  await expect(log.getByText(anyLine)).toBeVisible();
  await expect(log.getByRole('button', { name: 'Continue' })).toBeVisible();
  await expect(log.getByRole('button', { name: 'Retry' })).toHaveCount(0);
  const [thread] = await api(m, 'GET', `/api/bots/${bot.id}/threads`);

  await log.getByRole('button', { name: 'Continue' }).click();
  await expect(log.getByText(diesReply)).toBeVisible();
  await waitStatus(m, thread.id, 'done');

  const sent = providerPrompts();
  expect(sent.filter((p) => p.includes(notice))).toHaveLength(1);
  expect(sent.find((p) => p.includes(notice))).toContain(
    '). Continue from where you stopped. Check what already happened before you repeat a step.',
  );
  expect(
    sent.filter((p) => p.includes(dies)),
    'the text goes out once',
  ).toHaveLength(1);

  const check = async () => {
    await expect(log.getByText(diesReply)).toHaveCount(1);
    await expect(log.getByText(dies, { exact: true })).toHaveCount(1);
    await expect(log.getByText(anyLine)).toHaveCount(0);
    await expect(log.getByRole('button', { name: /^(Continue|Retry)$/ })).toHaveCount(0);
    await expect(log.getByText(new RegExp(notice))).toHaveCount(0);
    const text = await log.innerText();
    expect(text.indexOf(dies)).toBeGreaterThanOrEqual(0);
    expect(text.indexOf(diesReply)).toBeGreaterThan(text.indexOf(dies));
    expect(await userMessages(thread.id)).toHaveLength(1);
  };
  await check();
  await page.reload();
  await check();
});

test('a run a restart cut short shows Continue and it picks up the same session', async ({
  page,
}) => {
  const { bot, log } = await openBot(page, 'Egret');
  await send(page, prompts.hang);
  const [thread] = await api(m, 'GET', `/api/bots/${bot.id}/threads`);
  await waitStatus(m, thread.id, 'running');
  // step stays empty until the run's first tool call, so the run has reached the model by then.
  await expect
    .poll(async () => (await api(m, 'GET', `/api/threads/${thread.id}`)).step)
    .toBeTruthy();
  m.kill();
  await m.restart();
  await waitStatus(m, thread.id, 'failed');

  await page.reload();
  await expect(log.getByText(restartLine)).toBeVisible();
  await expect(log.getByRole('button', { name: 'Continue' })).toBeVisible();
  await expect(log.getByRole('button', { name: 'Retry' })).toHaveCount(0);

  await log.getByRole('button', { name: 'Continue' }).click();
  await expect(log.getByText(restartReply)).toBeVisible();
  await expect(log.getByText(prompts.hang, { exact: true })).toHaveCount(1);
  await expect(log.getByText(restartLine)).toHaveCount(0);
  expect(await userMessages(thread.id)).toHaveLength(1);
  expect(providerPrompts().filter((p) => p.includes(prompts.hang))).toHaveLength(1);
});

test('a failed run that never reached the provider shows Retry, which sends the message with its image again on the same bubble', async ({
  page,
}) => {
  const { bot, log } = await openBot(page, 'Tern');
  const thread = await api(m, 'POST', `/api/bots/${bot.id}/threads`, {});
  const text = 'Look at this picture and tell me what colour it is.';
  const image = {
    mime: 'image/png',
    data: Buffer.from(red.bytes).toString('base64'),
  };
  m.revoke('claude');
  await api(m, 'POST', `/api/threads/${thread.id}/messages`, {
    text,
    images: [image],
  });
  await waitStatus(m, thread.id, 'failed');
  const failedRun = (await api(m, 'GET', `/api/threads/${thread.id}`)).run_id;

  await page.goto(`${m.url}/bots/${bot.id}/threads/${thread.id}`);
  await expect(log.getByText(text, { exact: true })).toBeVisible();
  await expect(log.getByRole('button', { name: 'Retry' })).toBeVisible();
  await expect(log.getByRole('button', { name: 'Continue' })).toHaveCount(0);

  await log.getByRole('button', { name: 'Retry' }).click();
  await expect
    .poll(async () => (await api(m, 'GET', `/api/threads/${thread.id}`)).run_id)
    .not.toBe(failedRun);

  const check = async () => {
    await expect(log.getByText(text, { exact: true })).toHaveCount(1);
    const sent = await userMessages(thread.id);
    expect(sent.map((s) => s.text)).toEqual([text]);
    expect(sent[0].attachments).toHaveLength(1);
  };
  await check();
  await page.reload();
  await check();
});

test('queued messages that failed before the provider answered go again together, in order, on their own bubbles', async ({
  page,
}) => {
  const first = 'A first queued note that never reaches the provider.';
  const second = 'A second queued note that never reaches it either.';
  const { bot, log } = await openBot(page, 'Pair');
  const thread = await api(m, 'POST', `/api/bots/${bot.id}/threads`, {});
  // The first run plays testdata/claude/queued-slow.jsonl: about 6 s, then done.
  const slow = 'Take your time, then reply with just: ok';
  await sendMessageAPI(m, thread.id, slow, 'running');
  m.revoke('claude');
  const image = {
    mime: 'image/png',
    data: Buffer.from(red.bytes).toString('base64'),
  };
  await api(m, 'POST', `/api/threads/${thread.id}/messages`, { text: first });
  await api(m, 'POST', `/api/threads/${thread.id}/messages`, {
    text: second,
    images: [image],
  });
  await expect
    .poll(async () => (await userMessages(thread.id)).map((s) => s.text), {
      timeout: 30_000,
    })
    .toEqual([slow, first, second]);
  await waitStatus(m, thread.id, 'failed', 30_000);
  const failedRun = (await api(m, 'GET', `/api/threads/${thread.id}`)).run_id;

  await page.goto(`${m.url}/bots/${bot.id}/threads/${thread.id}`);
  await expect(log.getByText(second, { exact: true })).toBeVisible();
  await expect(log.getByRole('button', { name: 'Retry' })).toHaveCount(1);
  await expect(log.getByRole('button', { name: 'Continue' })).toHaveCount(0);

  await log.getByRole('button', { name: 'Retry' }).click();
  await expect
    .poll(async () => (await api(m, 'GET', `/api/threads/${thread.id}`)).run_id)
    .not.toBe(failedRun);
  const sent = await userMessages(thread.id);
  expect(sent.map((s) => s.text)).toEqual([slow, first, second]);
  expect(sent.reduce((n, s) => n + s.attachments.length, 0)).toBe(1);
  await page.reload();
  await expect(log.getByText(first, { exact: true })).toHaveCount(1);
  await expect(log.getByText(second, { exact: true })).toHaveCount(1);
});

test('a failed run that is not the thread’s newest shows no action', async ({ page }) => {
  const { bot, log } = await openBot(page, 'Ibis');
  await send(page, dies);
  await expect(log.getByRole('button', { name: 'Continue' })).toBeVisible();
  await send(page, prompts.pong);
  await expect(log.getByText('pong', { exact: true })).toBeVisible();
  const [thread] = await api(m, 'GET', `/api/bots/${bot.id}/threads`);
  await waitStatus(m, thread.id, 'done');

  const check = async () => {
    await expect(log.getByText(anyLine)).toHaveCount(1);
    await expect(log.getByRole('button', { name: /^(Continue|Retry)$/ })).toHaveCount(0);
  };
  await check();
  await page.reload();
  await check();
});

test('a run only a Reef wake started and a helper thread show no action', async ({ page }) => {
  // The boss hands off a helper that hangs. The sign-in is revoked, then Reef restarts: the
  // helper's run is cut short, and Reef wakes the boss, whose run fails on the revoked sign-in.
  const { poteto, boss, helpers } = await bossHandsOff(m, page, [['Tester', prompts.hang]]);
  const helper = helpers[0].id;
  await waitStatus(m, boss, 'done');
  await waitStatus(m, helper, 'running');
  m.revoke('claude');
  m.kill();
  await m.restart();
  await expect
    .poll(async () => (await api(m, 'GET', `/api/threads/${helper}`)).run_status)
    .toBe('failed');
  await expect
    .poll(async () => (await api(m, 'GET', `/api/threads/${boss}`)).run_status, {
      timeout: 30_000,
    })
    .toBe('failed');

  const log = page.getByRole('log', { name: 'Messages' });
  for (const url of [
    `${m.url}/bots/${poteto.id}/threads/${boss}`,
    `${m.url}/bots/${helpers[0].bot_id}/threads/${helper}`,
  ]) {
    await page.goto(url);
    await expect(
      log.getByText(/Run failed|Didn't run|sign-in expired|restarted/).first(),
    ).toBeVisible();
    await expect(log.getByRole('button', { name: /^(Continue|Retry)$/ })).toHaveCount(0);
  }
});
