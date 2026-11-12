// A run that fails before any reply leaves a "Didn't run: <reason>" line under its message, and the
// line stays in its thread, in order, after later runs and after a reload. Retry shows only on the
// newest run. The raw error stays behind Details. Design: docs/design.md, chat lines.
import { expect, type Locator, type Page, test } from '@playwright/test';
import {
  type Machine,
  newBotAPI,
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
