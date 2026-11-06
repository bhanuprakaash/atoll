// Slice 2 in the browser, on the Slice 6 UI: create a Bot, chat over two messages, watch the run
// work, approve a step. Only the UI is used; elements are found by role and visible text.
import { expect, test } from '@playwright/test';
import { pickModel, recordText, seenText } from './atoll';

const keep = 'Remember this fact: the codeword is kestrel. Reply with just: ok';
const ask = 'Which codeword did I give you? Reply with just the word.';
const tools = 'How many lines does notes.txt have? List the folder first, then read the file.';
const write = 'Use the Write tool to create hello.txt containing the word hi.';

test('create a Bot, chat, watch the run work, approve a step', async ({ page }) => {
  const name = `Ada ${Date.now()}`; // the server outlives a retry
  await page.goto('/');

  await page.getByRole('button', { name: 'New Bot' }).click();
  await page.getByRole('textbox', { name: 'Name' }).fill(name);
  await page.getByRole('textbox', { name: 'Persona' }).fill('A careful Go engineer.');
  await pickModel(page, page, 'Claude Code');
  await page.getByRole('button', { name: 'Create Bot' }).click();
  await expect(page.getByRole('heading', { name: `What should ${name} work on?` })).toBeVisible();

  const box = page.getByRole('textbox', { name: 'Message' });
  const send = async (text: string) => {
    await box.fill(text);
    await page.getByRole('button', { name: 'Send' }).click();
  };

  await send(keep);
  await expect(page.getByRole('log', { name: 'Messages' }).getByText('ok', { exact: true })).toBeVisible();
  await send(ask);
  await expect(page.getByText('kestrel', { exact: true })).toBeVisible();

  // The run shows as one working line while it goes, before its reply; never raw tool calls.
  const reply = page.getByText(/notes\.txt has 3 lines/);
  await recordText(page);
  await send(tools);
  const working = page.getByRole('log', { name: 'Messages' }).getByRole('status');
  await expect(working).toBeVisible();
  await expect(reply).toHaveCount(0);
  await expect(reply).toBeVisible();
  expect(await seenText(page)).not.toMatch(/\b(Bash|Read|ToolSearch)\b|mcp__|finish_run/);

  await send(write);
  const allow = page.getByRole('button', { name: 'Allow', exact: true });
  await expect(allow).toBeVisible();
  await expect(page.getByRole('button', { name: 'Deny', exact: true })).toBeVisible();
  await allow.click();
  await expect(page.getByText(/Created .*hello\.txt/)).toBeVisible();
  await expect(allow).toHaveCount(0);
});
