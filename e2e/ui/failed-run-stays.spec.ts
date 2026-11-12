// A run that fails before any reply leaves a "Didn't run: <reason>" line under its message, and the
// line stays in its thread, in order, after later runs and after a reload. Retry shows only on the
// newest run. The raw error stays behind Details. Design: docs/design.md, chat lines.
import { expect, type Locator, type Page, test } from '@playwright/test';
import {
  api,
  type Machine,
  newBotAPI,
  prompts,
  recordText,
  seenText,
  sendMessageAPI,
  sidebarRow,
  startAtoll,
} from './atoll';
import { red } from './image-fixture';

let m: Machine;
test.beforeAll(async () => {
  m = await startAtoll();
});
test.afterAll(() => m?.stop());

const second = 'A second message with no recording, so the provider fails again.';
const lineText = /^Didn't run: \S/;
// What the line says is checked once, below; the stay specs only need a failed-run line.
const anyLine = /^(Didn't run|Run failed)\b/;
const anyLineAt = /Didn't run|Run failed/;

async function openBot(page: Page, name: string) {
  await newBotAPI(m, name);
  await page.goto(m.url);
  await sidebarRow(page, name).click();
  await expect(page.getByRole('heading', { name: `What should ${name} work on?` })).toBeVisible();
  return page.getByRole('log', { name: 'Messages' });
}

async function send(page: Page, text: string) {
  await page.getByRole('textbox', { name: 'Message' }).fill(text);
  await page.getByRole('button', { name: 'Send' }).click();
}

async function inOrder(log: Locator, parts: (string | RegExp)[]) {
  await expect
    .poll(async () => {
      let rest = await log.innerText();
      for (const p of parts) {
        const at = typeof p === 'string' ? rest.indexOf(p) : rest.search(p);
        if (at < 0) return false;
        rest = rest.slice(at + 1);
      }
      return true;
    })
    .toBe(true);
}

test('a failed run keeps its line after a newer run starts and after a reload', async ({
  page,
}) => {
  const log = await openBot(page, 'Tern');
  await recordText(page);
  await send(page, prompts.none);
  await expect(log.getByText(anyLine)).toBeVisible();
  await expect(log.getByRole('button', { name: 'Retry' })).toBeVisible();

  await send(page, prompts.pong);
  await expect(log.getByText('pong', { exact: true })).toBeVisible();
  await expect(log.getByText(anyLine)).toHaveCount(1);
  await expect(log.getByRole('button', { name: 'Retry' })).toHaveCount(0);
  await inOrder(log, [prompts.none, anyLineAt, prompts.pong]);

  await page.reload();
  await expect(log.getByText('pong', { exact: true })).toBeVisible();
  await expect(log.getByText(anyLine)).toHaveCount(1);
  await expect(log.getByRole('button', { name: 'Retry' })).toHaveCount(0);
  await inOrder(log, [prompts.none, anyLineAt, prompts.pong]);
});

test('the line is calm words, and the raw error is behind Details', async ({ page }) => {
  const log = await openBot(page, 'Plover');
  await recordText(page);
  await send(page, prompts.none);
  const line = log.getByText(lineText);
  await expect(line).toBeVisible();
  expect(await seenText(page)).not.toMatch(/no recording for prompt/);
  await log.getByRole('button', { name: 'Details' }).click();
  await expect(log.getByText(/no recording for prompt/)).toBeVisible();
});

test('only the newest failed run shows Retry, and it stays so after a reload', async ({ page }) => {
  const log = await openBot(page, 'Knot');
  await send(page, prompts.none);
  await expect(log.getByText(anyLine)).toHaveCount(1);
  await send(page, second);
  await expect(log.getByText(anyLine)).toHaveCount(2);

  const check = async () => {
    await expect(log.getByText(anyLine)).toHaveCount(2);
    await expect(log.getByRole('button', { name: 'Retry' })).toHaveCount(1);
    await inOrder(log, [prompts.none, anyLineAt, second, anyLineAt, 'Retry']);
  };
  await check();
  await page.reload();
  await check();
});

type Sent = { role: string; text: string; run_id: string; from_bot_id?: string; kind?: string };
const userMessages = async (threadID: string): Promise<(Sent & { attachments: unknown[] })[]> =>
  (await api(m, 'GET', `/api/threads/${threadID}/messages`)).filter(
    (s: Sent) => s.role === 'user' && !s.from_bot_id && !s.kind,
  );

test('two queued messages that fail as one run show one line under the last, and Retry sends both in order', async ({
  page,
}) => {
  const firstQueued = 'A first queued note that no recording plays.';
  const secondQueued = 'A second queued note that no recording plays either.';
  const bot = await newBotAPI(m, 'Pair');
  const thread = await api(m, 'POST', `/api/bots/${bot.id}/threads`, {});
  // The first run plays testdata/claude/queued-slow.jsonl: about 6 s, then done.
  const slow = 'Take your time, then reply with just: ok';
  await sendMessageAPI(m, thread.id, slow, 'running');
  const image = { mime: 'image/png', data: Buffer.from(red.bytes).toString('base64') };
  await api(m, 'POST', `/api/threads/${thread.id}/messages`, { text: firstQueued });
  await api(m, 'POST', `/api/threads/${thread.id}/messages`, {
    text: secondQueued,
    images: [image],
  });
  let delivered: Sent[] = [];
  await expect
    .poll(
      async () => {
        delivered = (await userMessages(thread.id)).filter((s) => s.text !== slow);
        return delivered.map((s) => s.text);
      },
      { timeout: 30_000 },
    )
    .toEqual([firstQueued, secondQueued]);
  expect(delivered[1].run_id, 'both go out as one run').toBe(delivered[0].run_id);
  await expect
    .poll(async () => (await api(m, 'GET', `/api/runs/${delivered[0].run_id}`)).status)
    .toBe('failed');

  await page.goto(`${m.url}/bots/${bot.id}/threads/${thread.id}`);
  const log = page.getByRole('log', { name: 'Messages' });
  await expect(log.getByText(secondQueued)).toBeVisible();
  await expect(log.getByText(anyLine)).toHaveCount(1);
  await inOrder(log, [firstQueued, secondQueued, anyLineAt, 'Retry']);

  const before = (await userMessages(thread.id)).length;
  await log.getByRole('button', { name: 'Retry' }).click();
  await expect
    .poll(async () => {
      const sent = (await userMessages(thread.id)).slice(before);
      const text = sent.map((s) => s.text).join('\n');
      return {
        inOrder: text.includes(firstQueued) && text.indexOf(firstQueued) < text.indexOf(secondQueued),
        images: sent.reduce((n, s) => n + s.attachments.length, 0),
      };
    })
    .toEqual({ inOrder: true, images: 1 });
});
