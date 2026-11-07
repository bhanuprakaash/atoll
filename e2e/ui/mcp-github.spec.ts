// Settings > MCP servers with a GitHub connection: the connection is a read-only row, and a server
// added at GitHub's MCP URL asks whether to use the connection instead. GitHub is a local server
// (sign-in only), as in github-connection.spec.ts. Contract: mcp-servers-contract.md, "From
// connections". The Go tests prove what a run gets.
import { createServer, type Server } from 'node:http';
import { expect, type Page, test } from '@playwright/test';
import { api, type Machine, startAtoll } from './atoll';

let m: Machine;
let github: Server;
let mcpURL = '';
let token = '';

const servers = (): Promise<{ id: string; name: string; kind: string; url?: string }[]> =>
  api(m, 'GET', '/api/mcp-servers');

async function openPage(page: Page) {
  await page.goto(`${m.url}/settings`);
  await page.getByRole('link', { name: 'MCP servers' }).click();
  await expect(page.getByRole('list', { name: 'Catalog' })).toBeVisible();
}

const yours = (page: Page) => page.getByRole('list', { name: 'Your servers' });

async function fillAddByURL(page: Page, name: string, url: string) {
  await page.getByRole('button', { name: 'Add by URL' }).click();
  const dialog = page.getByRole('dialog', { name: 'Add by URL' });
  await dialog.getByRole('textbox', { name: 'Name' }).fill(name);
  await dialog.getByRole('textbox', { name: 'URL' }).fill(url);
  await dialog.getByRole('button', { name: 'Add', exact: true }).click();
  return dialog;
}

// countAdds lists the requests that add a server.
function countAdds(page: Page) {
  const seen: string[] = [];
  page.on('request', (r) => {
    if (r.method() === 'POST' && new URL(r.url()).pathname === '/api/mcp-servers')
      seen.push(r.url());
  });
  return seen;
}

test.beforeAll(async () => {
  github = createServer((req, res) => {
    let body = '';
    req.on('data', (c) => (body += c));
    req.on('end', () => {
      const send = (value: Record<string, string | number>) => {
        res.writeHead(200, { 'Content-Type': 'application/json' });
        res.end(JSON.stringify(value));
      };
      if (req.method === 'POST' && req.url === '/login/device/code') {
        send({
          device_code: 'fixture-device-code',
          user_code: 'ABCD-1234',
          verification_uri: 'https://github.com/login/device',
          expires_in: 900,
          interval: 1,
        });
      } else if (req.method === 'POST' && req.url === '/login/oauth/access_token') {
        token = 'fake-gho-ui-1';
        send({ access_token: token, token_type: 'bearer', scope: 'repo,read:org,workflow' });
      } else if (req.method === 'GET' && req.url === '/user') {
        const ok = req.headers.authorization === `Bearer ${token}` && token !== '';
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
  mcpURL = `${url}/mcp/`;
  m = await startAtoll({
    secretStore: true,
    env: { ATOLL_GITHUB_URL: url, ATOLL_GITHUB_MCP_URL: mcpURL },
  });
});
test.afterAll(() => {
  m?.stop();
  github?.close();
});

test('without a GitHub connection there is no GitHub row and the GitHub URL is added like any other', async ({
  page,
}) => {
  await openPage(page);
  await expect(page.getByRole('list', { name: 'From connections' })).toHaveCount(0);
  await expect(page.getByText('from GitHub connection')).toHaveCount(0);

  const dialog = await fillAddByURL(page, 'gh-plain', mcpURL);
  await expect(dialog).toHaveCount(0);
  await expect(page.getByText('GitHub has a connection')).toHaveCount(0);
  await expect(yours(page).getByRole('listitem').filter({ hasText: 'gh-plain' })).toBeVisible();
  const added = (await servers()).find((s) => s.name === 'gh-plain')!;
  await api(m, 'DELETE', `/api/mcp-servers/${added.id}`);
});

test('with the connection the page lists GitHub read-only, from the connection', async ({
  page,
}) => {
  const start = await api(m, 'POST', '/api/connections/github/start');
  await expect
    .poll(async () => (await api(m, 'POST', `/api/connections/${start.id}/poll`)).status, {
      timeout: 20_000,
      intervals: [1100],
    })
    .toBe('connected');

  await openPage(page);
  const from = page.getByRole('list', { name: 'From connections' });
  await expect(from.getByRole('listitem')).toHaveCount(1);
  const item = from.getByRole('listitem');
  await expect(item).toContainText('GitHub');
  await expect(item).toContainText(mcpURL);
  await expect(item).toContainText('from GitHub connection');
  await expect(item.getByRole('button')).toHaveCount(0);
  await expect(page.getByRole('list', { name: 'Catalog' }).getByRole('listitem')).toHaveCount(4);
  await expect(yours(page).getByRole('listitem')).toHaveCount(0);
  expect((await servers()).map((s) => [s.id, s.name, s.kind]).slice(4)).toEqual([
    ['github', 'GitHub', 'connection'],
  ]);
});

test('a server at the GitHub MCP URL shows GitHub has a connection; Use connection adds nothing, Add anyway adds it', async ({
  page,
}) => {
  await openPage(page);
  const adds = countAdds(page);

  const dialog = await fillAddByURL(page, 'gh-dup', mcpURL);
  await expect(dialog.getByText('GitHub has a connection')).toBeVisible();
  await dialog.getByRole('button', { name: 'Use connection' }).click();
  await expect(dialog).toHaveCount(0);
  await expect(yours(page).getByRole('listitem')).toHaveCount(0);
  expect(adds).toEqual([]);
  expect((await servers()).map((s) => s.name)).not.toContain('gh-dup');

  // The same URL without its trailing slash is still GitHub's.
  const again = await fillAddByURL(page, 'gh-dup', mcpURL.replace(/\/$/, ''));
  await expect(again.getByText('GitHub has a connection')).toBeVisible();
  expect(adds).toEqual([]);
  await again.getByRole('button', { name: 'Add anyway' }).click();
  await expect(again).toHaveCount(0);
  await expect(yours(page).getByRole('listitem').filter({ hasText: 'gh-dup' })).toBeVisible();
  expect(adds).toHaveLength(1);
  const list = await servers();
  expect(list.filter((s) => s.kind === 'remote').map((s) => [s.name, s.url])).toEqual([
    ['gh-dup', mcpURL.replace(/\/$/, '')],
  ]);

  // Any other URL is added at once.
  const other = await fillAddByURL(page, 'docs-remote', 'https://mcp.fake.test/mcp');
  await expect(other).toHaveCount(0);
  await expect(page.getByText('GitHub has a connection')).toHaveCount(0);
  await expect(yours(page).getByRole('listitem').filter({ hasText: 'docs-remote' })).toBeVisible();
});
