// A scripted GitHub the PR watcher reads (ATOLL_GITHUB_URL): it accepts the pull requests the
// Bots' fake gh opens, numbers them from 1 and keeps each open until a test merges it. The same
// server shape as helpers-pr.spec.ts, for specs that need several.
import { execFileSync } from 'node:child_process';
import { createHash } from 'node:crypto';
import { createServer, type Server } from 'node:http';
import { mkdirSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';
import { expect } from '@playwright/test';
import { api, type Machine } from './atoll';

export type GitHubFake = {
  url: string;
  // merge turns pull request n merged, as GitHub does when the person merges it.
  merge: (n: number) => void;
  // reads is how many times the server asked about pull request n.
  reads: (n: number) => number;
  close: () => void;
};

const sha = (n: number) => `head${n}`.padEnd(40, '0');

export async function startGitHub(): Promise<GitHubFake> {
  const pulls: { title: string; state: string; merged: boolean }[] = [];
  const reads = new Map<number, number>();
  let token = '';
  let url = '';
  const server: Server = createServer((req, res) => {
    let body = '';
    req.on('data', (c) => (body += c));
    req.on('end', () => {
      const json = (status: number, value: unknown) => {
        res.writeHead(status, { 'Content-Type': 'application/json' });
        res.end(JSON.stringify(value));
      };
      const path = req.url ?? '';
      let match: RegExpMatchArray | null;
      if (req.method === 'POST' && path === '/login/device/code') {
        json(200, {
          device_code: 'fixture-device-code',
          user_code: 'ABCD-1234',
          verification_uri: 'https://github.com/login/device',
          expires_in: 900,
          interval: 1,
        });
      } else if (req.method === 'POST' && path === '/login/oauth/access_token') {
        token = 'fake-gho-ui-merged-threads';
        json(200, {
          access_token: token,
          token_type: 'bearer',
          scope: 'repo,read:org,workflow',
        });
      } else if (![`Bearer ${token}`, `token ${token}`].includes(req.headers.authorization ?? '')) {
        json(401, { message: 'Bad credentials' });
      } else if (req.method === 'GET' && path === '/user') {
        json(200, { login: 'reef-fixture', id: 1 });
      } else if (req.method === 'POST' && path === '/repos/octo/hello/pulls') {
        pulls.push({
          title: JSON.parse(body).title,
          state: 'open',
          merged: false,
        });
        const n = pulls.length;
        json(201, { number: n, html_url: `${url}/octo/hello/pull/${n}` });
      } else if ((match = path.match(/^\/repos\/octo\/hello\/pulls\/(\d+)$/))) {
        const n = Number(match[1]);
        reads.set(n, (reads.get(n) ?? 0) + 1);
        const pull = pulls[n - 1];
        if (!pull) return json(404, { message: 'Not Found' });
        const payload = JSON.stringify({
          number: n,
          ...pull,
          base: { ref: 'main' },
          head: { sha: sha(n) },
        });
        const etag = `"${createHash('sha256').update(payload).digest('hex').slice(0, 16)}"`;
        if (req.headers['if-none-match'] === etag) {
          res.writeHead(304, { ETag: etag });
          return res.end();
        }
        res.writeHead(200, { 'Content-Type': 'application/json', ETag: etag });
        res.end(payload);
      } else if (/^\/repos\/octo\/hello\/commits\/[0-9a-z]+\/check-runs/.test(path)) {
        json(200, { total_count: 0, check_runs: [] });
      } else if (/^\/repos\/octo\/hello\/commits\/[0-9a-z]+\/status/.test(path)) {
        json(200, { state: 'pending', total_count: 0, statuses: [] });
      } else {
        json(404, { message: 'Not Found' });
      }
    });
  });
  await new Promise<void>((ok) => server.listen(0, '127.0.0.1', ok));
  const addr = server.address();
  url = `http://127.0.0.1:${typeof addr === 'object' && addr ? addr.port : 0}`;
  return {
    url,
    merge: (n) => {
      pulls[n - 1].state = 'closed';
      pulls[n - 1].merged = true;
    },
    reads: (n) => reads.get(n) ?? 0,
    close: () => {
      server.close();
      server.closeAllConnections();
    },
  };
}

// The env a machine needs to talk to the fake: the watcher polls every 200ms.
export const githubEnv = (github: GitHubFake) => ({
  ATOLL_GITHUB_URL: github.url,
  ATOLL_GITHUB_MCP_URL: `${github.url}/mcp/`,
  ATOLL_REPO_POLL_INTERVAL: '200ms',
});

// seedRepo registers a repo whose origin is the fake GitHub (pushes go to a local bare repo in
// root), signs in to the fake and returns the repo and the connection.
export async function seedRepo(m: Machine, github: GitHubFake, root: string) {
  writeFileSync(join(m.bin, 'github-url'), github.url);
  const git = (dir: string, ...args: string[]) =>
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
  const bare = join(root, 'hello.git');
  mkdirSync(bare);
  git(bare, 'init', '--bare', '-b', 'main');
  const path = join(root, 'hello');
  mkdirSync(path);
  git(path, 'init', '-b', 'main');
  writeFileSync(join(path, 'answer.txt'), '41\n');
  git(path, 'add', 'answer.txt');
  git(path, 'commit', '-m', 'seed bug');
  git(path, 'remote', 'add', 'origin', `${github.url}/octo/hello.git`);
  git(path, 'config', 'remote.origin.pushurl', bare);
  git(path, 'push', bare, 'main');
  const repo = await api(m, 'POST', '/api/repos', { name: 'octo-hello', path });
  const connection = await api(m, 'POST', '/api/connections/github/start');
  await expect
    .poll(async () => (await api(m, 'POST', `/api/connections/${connection.id}/poll`)).status, {
      intervals: [1_100],
    })
    .toBe('connected');
  return { repo, connection };
}
