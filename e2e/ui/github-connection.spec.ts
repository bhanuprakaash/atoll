// The GitHub connection in the browser: Settings → Connections signs in with the device flow, the
// card says what Bots get, a Bot takes the connection from its settings, and a token GitHub
// rejects shows Needs sign-in until Sign in again. GitHub is a local server, as in the Go tests.
// Contract: github-connection-contract.md.
import { createServer, type Server } from 'node:http';
import { expect, test, type Page } from '@playwright/test';
import { api, type Machine, newBotAPI, openThreadAPI, prompts, sidebarRow, startAtoll } from './atoll';

let m: Machine;
let github: Server;
let approved = false;
let rejectUser = false;
let issued = 0;
let token = '';

const connections = (): Promise<{ id: string; kind: string; status: string }[]> =>
  api(m, 'GET', '/api/connections');

// githubConnection is the signed-in GitHub connection: the one the first test made, or one made
// here through the API.
async function githubConnection(): Promise<{ id: string; kind: string; status: string }> {
  const existing = (await connections()).find((c) => c.kind === 'github');
  if (existing) return existing;
  approved = true;
  const start = await api(m, 'POST', '/api/connections/github/start');
  for (let i = 0; i < 5; i++) {
    await new Promise((ok) => setTimeout(ok, 1100));
    if ((await api(m, 'POST', `/api/connections/${start.id}/poll`)).status === 'connected') break;
  }
  return (await connections()).find((c) => c.kind === 'github')!;
}

async function openConnections(page: Page) {
  await page.goto(m.url);
  await page
    .getByRole('navigation', { name: 'Bots' })
    .getByRole('link', { name: /Settings/ })
    .click();
  await page.getByRole('link', { name: 'Connections' }).click();
}

test.beforeAll(async () => {
  github = createServer((req, res) => {
    let body = '';
    req.on('data', (c) => (body += c));
    req.on('end', () => {
      const form = new URLSearchParams(body);
      const send = (status: number, value: Record<string, string | number>) => {
        const json = (req.headers.accept ?? '').includes('application/json');
        res.writeHead(status, {
          'Content-Type': json ? 'application/json' : 'application/x-www-form-urlencoded',
        });
        res.end(
          json
            ? JSON.stringify(value)
            : new URLSearchParams(Object.entries(value).map(([k, v]) => [k, String(v)])).toString(),
        );
      };
      if (req.method === 'POST' && req.url === '/login/device/code') {
        send(200, {
          device_code: 'fixture-device-code',
          user_code: 'ABCD-1234',
          verification_uri: 'https://github.com/login/device',
          expires_in: 900,
          interval: 1,
        });
      } else if (req.method === 'POST' && req.url === '/login/oauth/access_token') {
        if (!approved || form.get('device_code') !== 'fixture-device-code') {
          send(200, { error: 'authorization_pending' });
          return;
        }
        token = `fake-gho-ui-${++issued}`;
        send(200, { access_token: token, token_type: 'bearer', scope: 'repo,read:org,workflow' });
      } else if (req.method === 'GET' && req.url === '/user') {
        const auth = req.headers.authorization ?? '';
        const ok = !rejectUser && token !== '' && [`Bearer ${token}`, `token ${token}`].includes(auth);
        res.writeHead(ok ? 200 : 401, { 'Content-Type': 'application/json' });
        res.end(ok ? '{"login":"reef-fixture","id":1}' : '{"message":"Bad credentials"}');
      } else {
        res.writeHead(404).end('{}');
      }
    });
  });
  await new Promise<void>((ok) => github.listen(0, '127.0.0.1', ok));
  const addr = github.address();
  const url = `http://127.0.0.1:${typeof addr === 'object' && addr ? addr.port : 0}`;
  m = await startAtoll({
    secretStore: true,
    env: { ATOLL_GITHUB_URL: url, ATOLL_GITHUB_MCP_URL: `${url}/mcp/` },
  });
});
test.afterAll(() => {
  m?.stop();
  github?.close();
});

test('the GitHub card says what Bots get; Connect shows the code, opens GitHub and connects', async ({
  page,
  context,
}) => {
  await context.route('https://github.com/**', (r) => r.fulfill({ body: 'signing in' }));
  await openConnections(page);
  await expect(page.getByText('Bots get gh, git and GitHub tools.')).toBeVisible();
  await expect(page.getByText('Coming later')).toHaveCount(1);

  const popup = page.waitForEvent('popup');
  await page.getByRole('button', { name: 'Connect GitHub' }).click();
  expect((await popup).url()).toBe('https://github.com/login/device');
  await expect(page.getByText('ABCD-1234')).toBeVisible();
  await expect(page.getByRole('link', { name: 'https://github.com/login/device' })).toHaveAttribute(
    'href',
    'https://github.com/login/device',
  );

  approved = true;
  await expect(page.getByText(/^connected$/i).first()).toBeVisible({ timeout: 20_000 });
  await expect(page.getByRole('button', { name: 'Connect GitHub' })).toHaveCount(0);
  await expect(page.getByText('Bots get gh, git and GitHub tools.')).toBeVisible();
  // The GitHub connection is not listed as a GitLab one and leaves GitLab's sign-in alone.
  await expect(page.getByRole('button', { name: 'Sign in to GitLab' })).toBeVisible();
  await expect(page.getByText('GitLab', { exact: true })).toHaveCount(1);

  const list = await connections();
  expect(list.map((c) => [c.kind, c.status])).toEqual([['github', 'connected']]);
});

test('Bot settings take and drop the GitHub connection like GitLab', async ({ page }) => {
  const conn = await githubConnection();
  const bot = await newBotAPI(m, 'Octo');
  await page.goto(m.url);
  await sidebarRow(page, 'Octo').click();
  await page.getByRole('button', { name: 'Bot settings' }).click();
  const panel = page.getByRole('dialog', { name: 'Bot settings' });

  await panel.getByRole('button', { name: 'Add connection' }).click();
  const add = page.getByRole('dialog', { name: 'Add connection' });
  await add.getByRole('menuitem', { name: /GitHub/ }).click();
  await add.getByRole('button', { name: 'Done' }).click();
  await expect(panel.getByRole('button', { name: /^Remove GitHub/ })).toBeVisible();
  await expect
    .poll(async () => (await api(m, 'GET', `/api/bots/${bot.id}`)).connections)
    .toEqual([conn.id]);

  await panel.getByRole('button', { name: /^Remove GitHub/ }).click();
  await expect
    .poll(async () => (await api(m, 'GET', `/api/bots/${bot.id}`)).connections)
    .toEqual([]);
});

test('a rejected token shows Needs sign-in; Sign in again reconnects the same connection', async ({
  page,
  context,
}) => {
  await context.route('https://github.com/**', (r) => r.fulfill({ body: 'signing in' }));
  const before = await githubConnection();
  const bot = await newBotAPI(m, 'Fixer', 'claude', { connections: [before.id] });
  rejectUser = true;
  const thread = await openThreadAPI(m, bot.id, prompts.keep);
  await expect
    .poll(async () => (await connections()).map((c) => c.status), { timeout: 20_000 })
    .toEqual(['needs_sign_in']);

  await page.goto(`${m.url}/bots/${bot.id}/threads/${thread.id}`);
  await expect(
    page.getByRole('log', { name: 'Messages' }).getByText(/GitHub needs sign-in/),
  ).toBeVisible();

  await openConnections(page);
  await expect(page.getByText('Needs sign-in', { exact: true })).toBeVisible();
  await expect(page.getByText(/^connected$/i)).toHaveCount(0);

  rejectUser = false;
  approved = true;
  await page.getByRole('button', { name: 'Sign in again' }).click();
  await expect(page.getByText('ABCD-1234')).toBeVisible();
  await expect(page.getByText(/^connected$/i).first()).toBeVisible({ timeout: 20_000 });
  await expect(page.getByText('Needs sign-in', { exact: true })).toHaveCount(0);
  await expect(page.getByRole('button', { name: 'Sign in again' })).toHaveCount(0);

  const after = await connections();
  expect(after.map((c) => [c.id, c.status])).toEqual([[before.id, 'connected']]);
  const bots: { name: string; connections: string[] }[] = await api(m, 'GET', '/api/bots');
  expect(bots.find((b) => b.name === 'Fixer')?.connections).toEqual([before.id]);
});
