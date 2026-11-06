// A GitLab connection whose refresh GitLab rejects shows "Needs sign-in" in Settings, and Sign in
// again runs the device flow for the same connection. GitLab's OAuth is a local server; the
// rejection is found by a run, as in the Go e2e tests.
import { createServer, type Server } from 'node:http';
import { expect, test } from '@playwright/test';
import { api, type Machine, newBotAPI, openThreadAPI, prompts, startAtoll } from './atoll';

let m: Machine;
let gitlab: Server;
let rejectRefresh = false;
let issued = 0;

const sleep = (ms: number) => new Promise((ok) => setTimeout(ok, ms));
const connections = (): Promise<{ id: string; status: string }[]> =>
  api(m, 'GET', '/api/connections');

test.beforeAll(async () => {
  gitlab = createServer((req, res) => {
    let body = '';
    req.on('data', (c) => (body += c));
    req.on('end', () => {
      const form = new URLSearchParams(body);
      res.setHeader('Content-Type', 'application/json');
      if (req.method !== 'POST' || !form.get('client_id')) {
        res.writeHead(400).end('{"error":"invalid_request"}');
      } else if (req.url === '/oauth/authorize_device') {
        res.end(
          JSON.stringify({
            device_code: 'fixture-device',
            user_code: 'REEF-1234',
            verification_uri: 'https://gitlab.example/oauth/device',
            expires_in: 600,
            interval: 1,
          }),
        );
      } else if (req.url === '/oauth/token' && form.get('grant_type') === 'refresh_token') {
        if (rejectRefresh) {
          res.writeHead(400).end('{"error":"invalid_grant"}');
          return;
        }
        issued++;
        res.end(
          JSON.stringify({
            access_token: `glpat-ui-${issued}`,
            refresh_token: `glrt-ui-${issued}`,
            token_type: 'Bearer',
            expires_in: 7200,
            created_at: Math.floor(Date.now() / 1000),
          }),
        );
      } else if (req.url === '/oauth/token') {
        issued++;
        res.end(
          JSON.stringify({
            access_token: `glpat-ui-${issued}`,
            refresh_token: `glrt-ui-${issued}`,
            token_type: 'Bearer',
            expires_in: rejectRefresh ? 1 : 7200,
            created_at: Math.floor(Date.now() / 1000),
          }),
        );
      } else {
        res.writeHead(404).end('{}');
      }
    });
  });
  await new Promise<void>((ok) => gitlab.listen(0, '127.0.0.1', ok));
  const addr = gitlab.address();
  const gitlabURL = `http://127.0.0.1:${typeof addr === 'object' && addr ? addr.port : 0}`;

  m = await startAtoll({ secretStore: true });
  // Sign in with a token that lives 1 s, then have GitLab reject the refresh a run triggers.
  rejectRefresh = true;
  const start = await api(m, 'POST', '/api/connections/gitlab/start', {
    instance_url: gitlabURL,
  });
  for (let i = 0; i < 5; i++) {
    await sleep(1100);
    if ((await api(m, 'POST', `/api/connections/${start.id}/poll`)).status === 'connected') break;
  }
  const bot = await newBotAPI(m, 'Fixer', 'claude', {
    connections: [start.id],
  });
  await sleep(1200);
  await openThreadAPI(m, bot.id, prompts.keep);
  await expect
    .poll(async () => (await connections()).map((c) => c.status), {
      timeout: 20_000,
    })
    .toEqual(['needs_sign_in']);
});
test.afterAll(() => {
  m?.stop();
  gitlab?.close();
});

test('a rejected refresh shows Needs sign-in; Sign in again reconnects the same connection', async ({
  page,
}) => {
  const [before] = await connections();
  await page.goto(m.url);
  await page
    .getByRole('navigation', { name: 'Bots' })
    .getByRole('link', { name: /Settings/ })
    .click();
  await page.getByRole('link', { name: 'Connections' }).click();
  await expect(page.getByText('Needs sign-in', { exact: true })).toBeVisible();
  await expect(page.getByText(/^connected$/i)).toHaveCount(0);

  rejectRefresh = false;
  await page.getByRole('button', { name: 'Sign in again' }).click();
  await expect(page.getByText('REEF-1234')).toBeVisible();
  await expect(page.getByText(/^connected$/i).first()).toBeVisible({
    timeout: 20_000,
  });
  await expect(page.getByText('Needs sign-in', { exact: true })).toHaveCount(0);
  await expect(page.getByRole('button', { name: 'Sign in again' })).toHaveCount(0);

  const after = await connections();
  expect(after.map((c) => [c.id, c.status])).toEqual([[before.id, 'connected']]);
  const bots: { name: string; connections: string[] }[] = await api(m, 'GET', '/api/bots');
  expect(bots.find((b) => b.name === 'Fixer')?.connections).toEqual([before.id]);
});
