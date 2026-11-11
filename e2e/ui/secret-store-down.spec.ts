// A run that cannot read a secret fails in plain words and keeps the person's message.
// Contract: secret-store-down-contract.md.
import { expect, test } from '@playwright/test';
import { api, newBotAPI, prompts, sidebarRow, startAtoll } from './atoll';

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
