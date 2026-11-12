// A run that cannot read a secret fails in plain words and keeps the person's message.
// Contract: secret-store-down-contract.md.
import { expect, test } from '@playwright/test';
import { api, newBotAPI, prompts, sendMessageAPI, sidebarRow, startAtoll } from './atoll';

const sentence = "Reef can't reach its secret store. Restart Reef, or unlock it, then send again.";

test('with the secret store down the thread says so plainly, marks the message not read and Resend works once it is back', async ({
  page,
}) => {
  const env: Record<string, string> = {};
  const m = await startAtoll({ secretStore: true, env });
  try {
    const server = await api(m, 'POST', '/api/mcp-servers', {
      name: 'docs-local',
      command: 'echo',
      env: { TOKEN: 'secret' },
    });
    await newBotAPI(m, 'Moss', 'claude', { mcp_servers: [server.id] });

    m.kill();
    env.ATOLL_SECRET_STORE_UNAVAILABLE = '1';
    await m.restart();

    await page.goto(m.url);
    await sidebarRow(page, 'Moss').click();
    await page.getByRole('textbox', { name: 'Message' }).fill(prompts.pong);
    await page.getByRole('button', { name: 'Send' }).click();

    const log = page.getByRole('log', { name: 'Messages' });
    await expect(log.getByText(sentence, { exact: true })).toBeVisible();
    await expect(log.getByText(prompts.pong, { exact: true })).toBeVisible();
    await expect(log.getByText('Not read', { exact: true })).toBeVisible();
    await expect(log.getByRole('button', { name: 'Resend' })).toBeVisible();
    await expect(log.getByRole('button', { name: 'Retry' })).toHaveCount(0);
    const text = await page.locator('body').innerText();
    expect(text).not.toMatch(/Run failed/);
    expect(text).not.toMatch(/keyring|unavailable|\bread TOKEN\b|MCP server|dbus|\{\s*"error"/i);

    m.kill();
    delete env.ATOLL_SECRET_STORE_UNAVAILABLE;
    await m.restart();
    await page.reload();
    await expect(log.getByText('Not read', { exact: true })).toBeVisible();
    await log.getByRole('button', { name: 'Resend' }).click();
    await expect(log.getByText('pong', { exact: true })).toBeVisible();
    await expect(log.getByText('Not read', { exact: true })).toHaveCount(1);
  } finally {
    m.stop();
  }
});

test('two queued messages not read in one run: Resend on the second sends the second', async ({
  page,
}) => {
  const firstQueued = 'A first queued note.';
  const secondQueued = 'A second queued note.';
  const env: Record<string, string> = {};
  const m = await startAtoll({ secretStore: true, env });
  try {
    const server = await api(m, 'POST', '/api/mcp-servers', {
      name: 'docs-local',
      command: 'echo',
      env: { TOKEN: 'secret' },
    });
    const bot = await newBotAPI(m, 'Fern', 'claude', { mcp_servers: [server.id] });
    const thread = await api(m, 'POST', `/api/bots/${bot.id}/threads`, {});
    await sendMessageAPI(m, thread.id, prompts.hang, 'running');
    await api(m, 'POST', `/api/threads/${thread.id}/messages`, { text: firstQueued });
    await api(m, 'POST', `/api/threads/${thread.id}/messages`, { text: secondQueued });

    // The restart ends the going run and delivers both held messages as one run, which cannot
    // read the server's secret.
    m.kill();
    env.ATOLL_SECRET_STORE_UNAVAILABLE = '1';
    await m.restart();
    type Sent = { role: string; text: string; run_id: string; not_read?: boolean };
    const users = async (): Promise<Sent[]> =>
      (await api(m, 'GET', `/api/threads/${thread.id}/messages`)).filter(
        (s: Sent) => s.role === 'user',
      );
    await expect
      .poll(async () =>
        (await users()).filter((s) => s.not_read).map((s) => s.text),
      )
      .toEqual([firstQueued, secondQueued]);
    const [one, two] = (await users()).filter((s) => s.not_read);
    expect(two.run_id, 'both go out as one run').toBe(one.run_id);

    m.kill();
    delete env.ATOLL_SECRET_STORE_UNAVAILABLE;
    await m.restart();
    await page.goto(`${m.url}/bots/${bot.id}/threads/${thread.id}`);
    const log = page.getByRole('log', { name: 'Messages' });
    await expect(log.getByText('Not read', { exact: true })).toHaveCount(2);
    const before = (await users()).length;
    await log
      .getByTestId('message')
      .filter({ hasText: secondQueued })
      .getByRole('button', { name: 'Resend' })
      .click();
    await expect
      .poll(async () => (await users()).slice(before).map((s) => s.text))
      .toEqual([secondQueued]);
  } finally {
    m.stop();
  }
});
