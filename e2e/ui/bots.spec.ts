// Slice 2 in the browser: create a Bot, chat over two messages, watch tool calls stream in,
// approve a step. Only the UI is used; elements are found by role and visible text.
import { expect, test } from '@playwright/test';

const keep = 'Remember this fact: the codeword is kestrel. Reply with just: ok';
const ask = 'Which codeword did I give you? Reply with just the word.';
const tools = 'How many lines does notes.txt have? List the folder first, then read the file.';
const write = 'Use the Write tool to create hello.txt containing the word hi.';

test('create a Bot, chat, watch tool calls, approve a step', async ({ page }) => {
  const name = `Ada ${Date.now()}`; // the server outlives a retry
  await page.goto('/');

  await page.getByRole('button', { name: 'New Bot' }).click();
  await page.getByRole('textbox', { name: 'Name' }).fill(name);
  await page.getByRole('textbox', { name: 'Persona' }).fill('A careful Go engineer.');
  await page.getByRole('combobox', { name: 'Model' }).selectOption('Claude Code');
  await page.getByRole('button', { name: 'Create' }).click();

  await page.getByRole('navigation').getByText(name).click();
  await page.getByRole('button', { name: 'New thread' }).click();

  const box = page.getByRole('textbox', { name: 'Message' });
  const send = async (text: string) => {
    await box.fill(text);
    await page.getByRole('button', { name: 'Send' }).click();
  };

  await send(keep);
  await expect(page.getByText('ok', { exact: true })).toBeVisible();
  await send(ask);
  await expect(page.getByText('kestrel', { exact: true })).toBeVisible();

  // Tool calls show while the run is going, before its reply.
  const reply = page.getByText(/notes\.txt has 3 lines/);
  await send(tools);
  await expect(page.getByText(/\bBash\b/).first()).toBeVisible();
  await expect(reply).toHaveCount(0);
  await expect(page.getByText(/\bRead\b/).first()).toBeVisible();
  await expect(reply).toBeVisible();

  await send(write);
  const approve = page.getByRole('button', { name: 'Approve' });
  await expect(approve).toBeVisible();
  await expect(page.getByRole('button', { name: 'Reject' })).toBeVisible();
  await approve.click();
  await expect(page.getByText(/Created .*hello\.txt/)).toBeVisible();
  await expect(approve).toHaveCount(0);
});
