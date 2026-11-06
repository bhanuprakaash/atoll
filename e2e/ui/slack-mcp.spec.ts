// Settings > MCP servers: Slack needs one internal Slack app per workspace. The row shows Set up
// with the steps and a Client ID field; once the Client ID is saved it connects like the other
// hosted servers. Contract: e2e/slack-mcp-contract.md. Slack is a local fake (slack-fixture.ts,
// reached through ATOLL_SLACK_MCP_URL): no network. The Go tests prove tokens, runs and refresh.
import { expect, type Page, test } from "@playwright/test";
import { api, type Machine, newBotAPI, openThreadAPI, prompts, startAtoll } from "./atoll";
import { followAuthorize } from "./oauth-mcp-fixture";
import { type Slack, slackClientID, slackScopes, startSlack } from "./slack-fixture";

let m: Machine;
let slack: Slack;
test.beforeEach(async () => {
  slack = await startSlack();
  m = await startAtoll({ secretStore: true, env: { ATOLL_SLACK_MCP_URL: slack.url } });
});
test.afterEach(async () => {
  m?.stop();
  await slack?.close();
});

const slackStatus = async (): Promise<string | undefined> =>
  (await api(m, 'GET', '/api/mcp-servers')).find((s: { id: string }) => s.id === 'slack')?.status;

async function openPage(page: Page) {
  await page.goto(`${m.url}/settings`);
  await page.getByRole('link', { name: 'MCP servers' }).click();
  await expect(page.getByRole('list', { name: 'Catalog' })).toBeVisible();
  return page
    .getByRole('list', { name: 'Catalog' })
    .getByRole('listitem')
    .filter({ hasText: 'Slack' });
}

test('the Slack row offers Set up with the steps and a Client ID field; saving it offers Connect', async ({
  page,
}) => {
  const item = await openPage(page);
  await expect(item).toContainText(slack.url);
  await expect(item.getByText('Needs setup', { exact: true })).toBeVisible();
  await expect(item.getByRole('button', { name: 'Connect', exact: true })).toHaveCount(0);

  await item.getByRole('button', { name: 'Set up', exact: true }).click();
  const dialog = page.getByRole('dialog', { name: 'Set up Slack' });
  await expect(dialog).toBeVisible();
  await expect(dialog.getByRole('list', { name: 'Steps' }).getByRole('listitem')).not.toHaveCount(
    0,
  );
  await expect(dialog).toContainText(/PKCE/i);
  await expect(dialog).toContainText(`${m.url}/oauth/callback`);
  for (const scope of slackScopes) await expect(dialog).toContainText(scope);
  await expect(dialog.getByRole('textbox')).toHaveCount(1);
  await expect(dialog.getByRole('textbox', { name: 'Client ID' })).toBeVisible();

  await dialog.getByRole('textbox', { name: 'Client ID' }).fill(slackClientID);
  await dialog.getByRole('button', { name: 'Save', exact: true }).click();
  await expect(dialog).toHaveCount(0);
  await expect(item.getByText('Not connected', { exact: true })).toBeVisible();
  await expect(item.getByRole('button', { name: 'Connect', exact: true })).toBeVisible();
  await expect(item.getByRole('button', { name: 'Set up', exact: true })).toHaveCount(0);
  expect(await slackStatus()).toBe('not_connected');
  await page.reload();
  await expect(
    page.getByRole('list', { name: 'Catalog' }).getByRole('listitem').filter({ hasText: 'Slack' }),
  ).toContainText('Not connected');
});

test('Connect opens the Allow page in a popup and the Slack row shows Connected', async ({
  page,
}) => {
  await api(m, 'POST', '/api/mcp-servers/slack/setup', { client_id: slackClientID });
  const item = await openPage(page);
  await expect(item.getByText('Not connected', { exact: true })).toBeVisible();

  const [popup] = await Promise.all([
    page.waitForEvent('popup'),
    item.getByRole('button', { name: 'Connect', exact: true }).click(),
  ]);
  await popup.waitForURL(`${m.url}/oauth/callback?**`);
  await expect(item.getByText('Connected', { exact: true })).toBeVisible();
  await expect(item.getByRole('button', { name: 'Connect', exact: true })).toHaveCount(0);
  expect(await slackStatus()).toBe('connected');

  expect(slack.authorizes).toHaveLength(1);
  const q = slack.authorizes[0];
  expect(q.get('client_id')).toBe(slackClientID);
  expect(q.get('redirect_uri')).toBe(`${m.url}/oauth/callback`);
  expect(q.get('code_challenge_method')).toBe('S256');
  expect((q.get('scope') ?? '').split(' ')).toEqual(slackScopes);
  expect(slack.exchanges).toHaveLength(1);
  const exchange = slack.exchanges[0];
  expect(exchange.form.get('client_id')).toBe(slackClientID);
  expect(exchange.form.get('redirect_uri')).toBe(`${m.url}/oauth/callback`);
  expect(exchange.form.get('code_verifier')).toBeTruthy();
  expect(exchange.form.has('client_secret')).toBe(false);
  expect(exchange.authorization).toBeUndefined();

  const { access, refresh } = slack.tokens();
  const text = await page.locator('body').innerText();
  for (const secret of [access, refresh, slackClientID]) expect(text).not.toContain(secret);
});

test('a rejected refresh shows Needs sign-in on the Slack row; Sign in again reconnects', async ({
  page,
}) => {
  slack.config.ttl = 300;
  await api(m, 'POST', '/api/mcp-servers/slack/setup', { client_id: slackClientID });
  const connected = await api(m, 'POST', '/api/mcp-servers/slack/connect');
  await followAuthorize(connected.authorize_url);
  expect(await slackStatus()).toBe('connected');
  const kelp = await newBotAPI(m, 'Kelp', 'claude', { mcp_servers: ['slack'] });

  // The token expires within ten minutes, so the next run refreshes it first and Slack rejects it.
  slack.config.reject = true;
  await openThreadAPI(m, kelp.id, prompts.keep);
  await expect.poll(slackStatus, { timeout: 20_000 }).toBe('needs_sign_in');
  expect(slack.refreshes).toHaveLength(1);

  const item = await openPage(page);
  await expect(item.getByText('Needs sign-in', { exact: true })).toBeVisible();
  await expect(item.getByText('Connected', { exact: true })).toHaveCount(0);

  slack.config.reject = false;
  slack.config.ttl = 43200;
  const [popup] = await Promise.all([
    page.waitForEvent('popup'),
    item.getByRole('button', { name: 'Sign in again' }).click(),
  ]);
  await popup.waitForURL(`${m.url}/oauth/callback?**`);
  await expect(item.getByText('Connected', { exact: true })).toBeVisible();
  await expect(item.getByText('Needs sign-in', { exact: true })).toHaveCount(0);
  await expect(item.getByRole('button', { name: 'Sign in again' })).toHaveCount(0);
  expect(await slackStatus()).toBe('connected');
  expect((await api(m, 'GET', `/api/bots/${kelp.id}`)).mcp_servers).toEqual(['slack']);
});
