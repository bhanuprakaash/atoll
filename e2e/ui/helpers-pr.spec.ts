// The helpers tray carries each helper's PR and its CI. GitHub is a local server the watcher
// reads (ATOLL_GITHUB_URL); the helpers play testdata/claude/helper-pr-hang.jsonl (opens and
// tracks a PR with the fake gh, then waits) and run-hang. Needs the PR watcher of #163 (CI) and
// the tray of #175. Contract: e2e/helpers-ui-contract.md.
import { execFileSync } from 'node:child_process';
import { createHash } from 'node:crypto';
import { createServer, type Server } from 'node:http';
import { mkdirSync, mkdtempSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { expect, test } from '@playwright/test';
import { api, type Machine, prompts, startAtoll } from './atoll';
import { delegate, helperThreads, makeBoss, popover, startThread, tray } from './helpers-fixture';

const prBrief = 'Fix the seeded bug, open a PR and wait for CI.';

type Check = { name: string; status: string; conclusion: string | null };

let m: Machine;
let github: Server;
let githubURL = '';
let root = '';
let token = '';
const pulls: { title: string }[] = [];
const pr = new Map<number, { state: string; merged: boolean }>();
const checks = new Map<string, Check[]>();
const sha = (n: number) => `head${n}`.padEnd(40, '0');

function git(dir: string, ...args: string[]) {
  execFileSync('git', args, {
    cwd: dir,
    env: {
      ...process.env,
      GIT_AUTHOR_NAME: 'Fixture',
      GIT_AUTHOR_EMAIL: 'fixture@example.test',
      GIT_COMMITTER_NAME: 'Fixture',
      GIT_COMMITTER_EMAIL: 'fixture@example.test',
      GIT_CONFIG_NOSYSTEM: '1',
    },
  });
}

test.beforeEach(async () => {
  pulls.length = 0;
  pr.clear();
  checks.clear();
  github = createServer((req, res) => {
    let body = '';
    req.on('data', (c) => (body += c));
    req.on('end', () => {
      const json = (status: number, value: unknown, headers: Record<string, string> = {}) => {
        res.writeHead(status, { 'Content-Type': 'application/json', ...headers });
        res.end(JSON.stringify(value));
      };
      const form = (value: Record<string, string | number>) => {
        const wantsJSON = (req.headers.accept ?? '').includes('application/json');
        if (wantsJSON) return json(200, value);
        res.writeHead(200, { 'Content-Type': 'application/x-www-form-urlencoded' });
        res.end(
          new URLSearchParams(Object.entries(value).map(([k, v]) => [k, String(v)])).toString(),
        );
      };
      const authorised = [`Bearer ${token}`, `token ${token}`].includes(
        req.headers.authorization ?? '',
      );
      const url = req.url ?? '';
      let match: RegExpMatchArray | null;
      if (req.method === 'POST' && url === '/login/device/code') {
        form({
          device_code: 'fixture-device-code',
          user_code: 'ABCD-1234',
          verification_uri: 'https://github.com/login/device',
          expires_in: 900,
          interval: 1,
        });
      } else if (req.method === 'POST' && url === '/login/oauth/access_token') {
        token = 'fake-gho-ui-helpers-pr';
        form({ access_token: token, token_type: 'bearer', scope: 'repo,read:org,workflow' });
      } else if (req.method === 'GET' && url === '/user') {
        json(
          authorised ? 200 : 401,
          authorised ? { login: 'reef-fixture', id: 1 } : { message: 'Bad credentials' },
        );
      } else if (!authorised) {
        json(401, { message: 'Bad credentials' });
      } else if (req.method === 'POST' && url === '/repos/octo/hello/pulls') {
        pulls.push(JSON.parse(body));
        const n = pulls.length;
        pr.set(n, { state: 'open', merged: false });
        json(201, { number: n, html_url: `${githubURL}/octo/hello/pull/${n}` });
      } else if ((match = url.match(/^\/repos\/octo\/hello\/pulls\/(\d+)$/))) {
        const n = Number(match[1]);
        const state = pr.get(n);
        if (!state) return json(404, { message: 'Not Found' });
        const payload = JSON.stringify({
          number: n,
          ...state,
          title: pulls[n - 1].title,
          head: { sha: sha(n) },
        });
        const etag = `"${createHash('sha256').update(payload).digest('hex').slice(0, 16)}"`;
        if (req.headers['if-none-match'] === etag) {
          res.writeHead(304, { ETag: etag });
          return res.end();
        }
        res.writeHead(200, { 'Content-Type': 'application/json', ETag: etag });
        res.end(payload);
      } else if ((match = url.match(/^\/repos\/octo\/hello\/commits\/([0-9a-z]+)\/check-runs/))) {
        const runs = checks.get(match[1]) ?? [];
        json(200, { total_count: runs.length, check_runs: runs });
      } else if (/^\/repos\/octo\/hello\/commits\/[0-9a-z]+\/status/.test(url)) {
        json(200, { state: 'pending', total_count: 0, statuses: [] });
      } else {
        json(404, {});
      }
    });
  });
  await new Promise<void>((ok) => github.listen(0, '127.0.0.1', ok));
  const addr = github.address();
  githubURL = `http://127.0.0.1:${typeof addr === 'object' && addr ? addr.port : 0}`;
  m = await startAtoll({
    fakes: ['claude', 'gh'],
    secretStore: true,
    env: {
      ATOLL_GITHUB_URL: githubURL,
      ATOLL_GITHUB_MCP_URL: `${githubURL}/mcp/`,
      ATOLL_REPO_POLL_INTERVAL: '200ms',
    },
  });
  writeFileSync(join(m.bin, 'github-url'), githubURL);
  root = mkdtempSync(join(tmpdir(), 'atoll-ui-helpers-pr-'));
});
test.afterEach(() => {
  m?.stop();
  github?.close();
  if (root) rmSync(root, { recursive: true, force: true });
});

test('a helper row shows its PR and CI as the watcher read them; a helper with no PR shows none; the resting line has no PR text', async ({
  page,
}) => {
  test.setTimeout(180_000);
  const bare = join(root, 'hello.git');
  mkdirSync(bare);
  git(bare, 'init', '--bare', '-b', 'main');
  const path = join(root, 'hello');
  mkdirSync(path);
  git(path, 'init', '-b', 'main');
  writeFileSync(join(path, 'answer.txt'), '41\n');
  git(path, 'add', 'answer.txt');
  git(path, 'commit', '-m', 'seed bug');
  git(path, 'remote', 'add', 'origin', `${githubURL}/octo/hello.git`);
  git(path, 'config', 'remote.origin.pushurl', bare);
  git(path, 'push', bare, 'main');
  const repo = await api(m, 'POST', '/api/repos', { name: 'scratch-pr', path });
  const start = await api(m, 'POST', '/api/connections/github/start');
  for (let i = 0; i < 5; i++) {
    await new Promise((ok) => setTimeout(ok, 1100));
    if ((await api(m, 'POST', `/api/connections/${start.id}/poll`)).status === 'connected') break;
  }
  const coder = { repo_access: [repo.id], connections: [start.id] };
  const { bots } = await makeBoss(m, ['Tester', 'Builder', 'Reviewer'], {
    Tester: coder,
    Builder: coder,
  });

  await page.goto(m.url);
  const boss = await startThread(
    page,
    'Poteto',
    delegate([
      ['Tester', prBrief, 'scratch-pr'],
      ['Builder', prBrief, 'scratch-pr'],
      ['Reviewer', prompts.hang],
    ]),
  );
  await helperThreads(m, boss, 3);
  const prOf: Record<string, number> = {};
  await expect
    .poll(
      async () => {
        const list = (await api(m, 'GET', `/api/threads/${boss}`)).helpers as {
          bot_id: string;
          mr_url: string;
        }[];
        for (const name of ['Tester', 'Builder'])
          prOf[name] = Number(
            list
              .find((h) => h.bot_id === bots[name].id)
              ?.mr_url.split('/')
              .pop() ?? 0,
          );
        return Object.values(prOf).every(Boolean) && prOf.Tester !== prOf.Builder;
      },
      { timeout: 60_000 },
    )
    .toBe(true);

  const region = tray(page);
  await expect(region).toContainText('3 working');
  await expect(region).not.toContainText(/#\d|running|passed|failed|merged|closed/);
  await region.getByRole('button', { name: 'Show', exact: true }).click();
  const rows = popover(page).getByRole('listitem');
  const row = (name: string) => rows.filter({ hasText: name });
  const link = (name: string) =>
    row(name).getByRole('link', { name: `#${prOf[name]}`, exact: true });

  for (const name of ['Tester', 'Builder']) {
    await expect(link(name)).toHaveAttribute('href', `${githubURL}/octo/hello/pull/${prOf[name]}`);
    await expect(row(name).getByText('running', { exact: true })).toHaveCount(0);
  }
  await expect(row('Reviewer').getByRole('link', { name: /^[#!]\d+$/ })).toHaveCount(0);
  await expect(row('Reviewer')).not.toContainText(/running|passed|failed|merged|closed/);

  const running: Check[] = [
    { name: 'build', status: 'in_progress', conclusion: null },
    { name: 'unit', status: 'queued', conclusion: null },
  ];
  checks.set(sha(prOf.Tester), running);
  checks.set(sha(prOf.Builder), running);
  for (const name of ['Tester', 'Builder'])
    await expect(row(name).getByText('running', { exact: true })).toBeVisible();

  checks.set(sha(prOf.Tester), [
    { name: 'build', status: 'completed', conclusion: 'success' },
    { name: 'unit', status: 'completed', conclusion: 'success' },
  ]);
  checks.set(sha(prOf.Builder), [
    { name: 'build', status: 'completed', conclusion: 'success' },
    { name: 'unit', status: 'completed', conclusion: 'failure' },
  ]);
  await expect(row('Tester').getByText('passed', { exact: true })).toBeVisible();
  await expect(row('Builder').getByText('failed', { exact: true })).toBeVisible();
  await expect(row('Tester').getByText('running', { exact: true })).toHaveCount(0);
  await expect(row('Builder').getByText('passed', { exact: true })).toHaveCount(0);

  pr.set(prOf.Tester, { state: 'closed', merged: true });
  pr.set(prOf.Builder, { state: 'closed', merged: false });
  await expect(row('Tester').getByText('merged', { exact: true })).toBeVisible();
  await expect(row('Builder').getByText('closed', { exact: true })).toBeVisible();
  await expect(row('Tester').getByText('passed', { exact: true })).toHaveCount(0);
  await expect(row('Builder').getByText('failed', { exact: true })).toHaveCount(0);
  await expect(link('Tester')).toBeVisible();
});
