// One rule per message kind: the run footer, the handoff line, clamps, Close thread in the island
// and a window that never scrolls. Contract: e2e/titles-contract.md and issue #93.
import { expect, test } from '@playwright/test';
import {
  api,
  type Machine,
  newBotAPI,
  openThreadAPI,
  openThreads,
  prompts,
  sendMessageAPI,
  startAtoll,
  threadTitle,
} from './atoll';

let m: Machine;
test.beforeAll(async () => {
  m = await startAtoll();
});
test.afterAll(() => m?.stop());

const escaped = (s: string) =>
  `"${Array.from(s, (c) => `\\u${c.charCodeAt(0).toString(16).padStart(4, '0')}`).join('')}"`;

test('a run ends with one footer under the reply: time, worked, tools', async ({ page }) => {
  const bot = await newBotAPI(m, 'Footer');
  const thread = await api(m, 'POST', `/api/bots/${bot.id}/threads`, {});
  await sendMessageAPI(m, thread.id, prompts.working);
  await page.goto(`${m.url}/bots/${bot.id}/threads/${thread.id}`);
  const log = page.getByRole('log', { name: 'Messages' });
  const reply = log.getByText('Fixed the wizard repo select; tests pass.', { exact: true });
  const footer = log.getByText(/^\d{1,2}:\d{2}( [AP]M)? · Worked \d+(m \d+)?s · /);
  await expect(footer).toHaveCount(1);
  expect((await footer.boundingBox())!.y).toBeGreaterThan((await reply.boundingBox())!.y);
  const replyX = (await reply.boundingBox())!.x;
  expect(Math.abs((await footer.boundingBox())!.x - replyX)).toBeLessThanOrEqual(1);
});

test('the handoff line names each Bot once, linking to its helper thread', async ({ page }) => {
  const poteto = await newBotAPI(m, 'Handoff Boss', 'claude', { look: 'whale' });
  const tester = await newBotAPI(m, 'Twice', 'claude', { look: 'shark' });
  await api(m, 'PUT', `/api/bots/${poteto.id}`, { delegates: [tester.id] });
  const call = `{"server":"atoll","tool":"delegate","args":{"bot":"Twice","brief":${escaped(prompts.hang)}}}`;
  const boss = await openThreadAPI(m, poteto.id, `Use MCP tools. [${call},${call}]`);
  await expect
    .poll(async () => (await api(m, 'GET', `/api/bots/${tester.id}/threads`)).length)
    .toBe(2);
  const helpers: { id: string }[] = await api(m, 'GET', `/api/bots/${tester.id}/threads`);

  await page.goto(`${m.url}/bots/${poteto.id}/threads/${boss.thread?.id ?? boss.id}`);
  const log = page.getByRole('log', { name: 'Messages' });
  await expect(log.getByText('Handed off to Twice ×2', { exact: true })).toHaveCount(1);
  const link = log.getByRole('link', { name: 'Twice', exact: true });
  await expect(link).toHaveCount(1);
  expect(helpers.map((h) => `/bots/${tester.id}/threads/${h.id}`)).toContain(
    await link.getAttribute('href'),
  );
  for (const h of await api(m, 'GET', `/api/bots/${tester.id}/threads`))
    await api(m, 'POST', `/api/runs/${h.run_id}/stop`);
});

test('a long user message clamps at 8 lines with Show more and Show less', async ({ page }) => {
  const bot = await newBotAPI(m, 'Clamp');
  const text = Array.from({ length: 30 }, (_, i) => `line ${i + 1}`).join('\n');
  const thread = await openThreadAPI(m, bot.id, text);
  await page.goto(`${m.url}/bots/${bot.id}/threads/${thread.thread?.id ?? thread.id}`);
  const log = page.getByRole('log', { name: 'Messages' });
  const bubble = log.getByRole('article', { name: 'You' });
  await expect(bubble).toBeVisible();
  const lineHeight = 22.5;
  expect((await bubble.boundingBox())!.height).toBeLessThan(lineHeight * 10);
  await log.getByRole('button', { name: 'Show more' }).click();
  expect((await bubble.boundingBox())!.height).toBeGreaterThan(lineHeight * 28);
  await log.getByRole('button', { name: 'Show less' }).click();
  expect((await bubble.boundingBox())!.height).toBeLessThan(lineHeight * 10);
});

test('Close thread lives in the island: This thread on top, Close on hover for others', async ({
  page,
}) => {
  const bot = await newBotAPI(m, 'Closer');
  const other = await openThreadAPI(m, bot.id, prompts.pong);
  const current = await openThreadAPI(m, bot.id, prompts.keep);
  const otherID = other.thread?.id ?? other.id;
  const currentID = current.thread?.id ?? current.id;
  for (const id of [otherID, currentID])
    await expect
      .poll(async () => (await api(m, 'GET', `/api/threads/${id}`)).run_status)
      .not.toBe('running');
  await page.goto(`${m.url}/bots/${bot.id}/threads/${currentID}`);
  await expect(page.getByRole('log', { name: 'Messages' })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Close thread' })).toHaveCount(0);

  const panel = await openThreads(page);
  const here = panel.getByRole('region', { name: 'This thread' });
  await expect(here).toContainText('Remember this fact');
  const row = panel.getByRole('listitem').filter({ hasText: threadTitle(prompts.pong) });
  await expect(row).toHaveCount(1);
  await row.hover();
  await row.getByRole('button', { name: 'Close', exact: true }).click();
  await expect.poll(async () => (await api(m, 'GET', `/api/threads/${otherID}`)).closed).toBe(true);

  await here.getByRole('button', { name: 'Close thread' }).click();
  await expect
    .poll(async () => (await api(m, 'GET', `/api/threads/${currentID}`)).closed)
    .toBe(true);
});

test('the window never scrolls', async ({ page }) => {
  const bot = await newBotAPI(m, 'Still');
  const thread = await openThreadAPI(m, bot.id, prompts.keep);
  await page.goto(`${m.url}/bots/${bot.id}/threads/${thread.thread?.id ?? thread.id}`);
  await expect(page.getByRole('log', { name: 'Messages' })).toContainText('ok');
  const size = await page.evaluate(() => ({
    scroll: document.scrollingElement!.scrollHeight,
    view: window.innerHeight,
  }));
  expect(size.scroll).toBeLessThanOrEqual(size.view);
});
