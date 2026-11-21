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

export type ChangedFile = {
  filename: string;
  additions: number;
  deletions: number;
  // patch is left out of GitHub's reply when it is absent, as for a binary or too large file.
  patch?: string;
};

export type FakeCheck = { name: string; status: string; conclusion: string | null };

export type PullDetails = {
  author?: string;
  head?: string;
  body?: string;
  draft?: boolean;
  mergeable?: boolean | null;
};

export type GitHubFake = {
  url: string;
  // merge turns pull request n merged, as GitHub does when the person merges it.
  merge: (n: number) => void;
  // close closes pull request n unmerged.
  closePull: (n: number) => void;
  // open adds a pull request no Bot opened and returns its number.
  open: (title: string) => number;
  setCI: (n: number, state: 'running' | 'passed' | 'failed') => void;
  // setChecks replaces the check runs of pull request n, in the order GitHub lists them.
  setChecks: (n: number, checks: FakeCheck[]) => void;
  // describe sets what the panel reads of pull request n beyond its title: author, head branch,
  // description, draft and mergeable.
  describe: (n: number, details: PullDetails) => void;
  // setFiles sets the files of pull request n. The fake answers one file a page, with a Link
  // header, whatever per_page says.
  setFiles: (n: number, files: ChangedFile[]) => void;
  // hide makes pull request n answer 404, as GitHub does for a PR the token cannot see.
  hide: (n: number, hidden: boolean) => void;
  // fail makes pull request n answer 500.
  fail: (n: number, failing: boolean) => void;
  // reads is how many times the server asked about pull request n.
  reads: (n: number) => number;
  close: () => void;
};

const sha = (n: number) => `head${n}`.padEnd(40, '0');

export async function startGitHub(): Promise<GitHubFake> {
  const pulls: { title: string; state: string; merged: boolean }[] = [];
  const reads = new Map<number, number>();
  const ci = new Map<number, 'running' | 'passed' | 'failed'>();
  const checks = new Map<number, FakeCheck[]>();
  const details = new Map<number, PullDetails>();
  const files = new Map<number, ChangedFile[]>();
  const hidden = new Set<number>();
  const failing = new Set<number>();
  let token = '';
  let url = '';
  const server: Server = createServer((req, res) => {
    let body = '';
    req.on('data', (c) => (body += c));
    req.on('end', () => {
      const json = (status: number, value: unknown, headers: Record<string, string> = {}) => {
        res.writeHead(status, { 'Content-Type': 'application/json', ...headers });
        res.end(JSON.stringify(value));
      };
      const path = req.url ?? '';
      const route = path.split('?')[0];
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
      } else if (req.method === 'GET' && route === '/repos/octo/hello') {
        json(200, {
          full_name: 'octo/hello',
          allow_merge_commit: true,
          allow_squash_merge: true,
          allow_rebase_merge: true,
        });
      } else if ((match = route.match(/^\/repos\/octo\/hello\/pulls\/(\d+)\/files$/))) {
        const n = Number(match[1]);
        if (!pulls[n - 1] || hidden.has(n)) return json(404, { message: 'Not Found' });
        const page = Math.max(Number(new URL(path, url).searchParams.get('page') ?? 1), 1);
        const all = files.get(n) ?? [];
        const file = all[page - 1];
        const headers: Record<string, string> = {};
        if (page < all.length)
          headers.Link = `<${url}/repos/octo/hello/pulls/${n}/files?per_page=100&page=${page + 1}>; rel="next"`;
        json(
          200,
          file ? [{ ...file, status: 'modified', changes: file.additions + file.deletions }] : [],
          headers,
        );
      } else if ((match = path.match(/^\/repos\/octo\/hello\/pulls\/(\d+)$/))) {
        const n = Number(match[1]);
        reads.set(n, (reads.get(n) ?? 0) + 1);
        const pull = pulls[n - 1];
        if (!pull || hidden.has(n)) return json(404, { message: 'Not Found' });
        if (failing.has(n)) return json(500, { message: 'Server Error' });
        const d = details.get(n) ?? {};
        const payload = JSON.stringify({
          number: n,
          ...pull,
          draft: d.draft ?? false,
          body: d.body ?? '',
          mergeable: d.mergeable === undefined ? true : d.mergeable,
          user: { login: d.author ?? 'reef-fixture' },
          base: { ref: 'main' },
          head: { sha: sha(n), ref: d.head ?? `fix/pr-${n}` },
        });
        const etag = `"${createHash('sha256').update(payload).digest('hex').slice(0, 16)}"`;
        if (req.headers['if-none-match'] === etag) {
          res.writeHead(304, { ETag: etag });
          return res.end();
        }
        res.writeHead(200, { 'Content-Type': 'application/json', ETag: etag });
        res.end(payload);
      } else if ((match = path.match(/^\/repos\/octo\/hello\/commits\/head(\d+)0*\/check-runs/))) {
        const n = pulls.findIndex((_, i) => path.includes(`/commits/${sha(i + 1)}/`)) + 1;
        const state = ci.get(n);
        const runs: FakeCheck[] = (checks.get(n) ??
          (state
            ? [
                {
                  name: 'build',
                  status: state === 'running' ? 'in_progress' : 'completed',
                  conclusion:
                    state === 'passed' ? 'success' : state === 'failed' ? 'failure' : null,
                },
              ]
            : [])) as FakeCheck[];
        json(200, { total_count: runs.length, check_runs: runs });
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
    open: (title) => pulls.push({ title, state: 'open', merged: false }),
    closePull: (n) => {
      pulls[n - 1].state = 'closed';
      pulls[n - 1].merged = false;
    },
    merge: (n) => {
      pulls[n - 1].state = 'closed';
      pulls[n - 1].merged = true;
    },
    setCI: (n, state) => {
      ci.set(n, state);
    },
    setChecks: (n, list) => {
      checks.set(n, list);
    },
    describe: (n, d) => {
      details.set(n, { ...details.get(n), ...d });
    },
    setFiles: (n, list) => {
      files.set(n, list);
    },
    hide: (n, on) => {
      if (on) hidden.add(n);
      else hidden.delete(n);
    },
    fail: (n, on) => {
      if (on) failing.add(n);
      else failing.delete(n);
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
  await pollConnected(m, connection.id);
  return { repo, connection };
}

// pollConnected polls a device-flow connection until its sign-in completes.
export async function pollConnected(m: Machine, id: string) {
  await expect
    .poll(async () => (await api(m, 'POST', `/api/connections/${id}/poll`)).status, {
      intervals: [1_100],
    })
    .toBe('connected');
}

// trackPR has the thread's Bot call track_mr with a pull request the fake already holds, in a
// follow-up message (a thread with a session plays the mcp-use recording), and waits until the
// thread's prs lists it.
export async function trackPR(
  m: Machine,
  threadID: string,
  github: GitHubFake,
  n: number,
  title: string,
) {
  const url = `${github.url}/octo/hello/pull/${n}`;
  await api(m, 'POST', `/api/threads/${threadID}/messages`, {
    text: `Use MCP tools. [{"server":"atoll","tool":"track_mr","args":{"url":"${url}","title":"${title}"}}]`,
  });
  await expect
    .poll(
      async () =>
        (
          (await api(m, 'GET', `/api/threads/${threadID}`)).prs as {
            url: string;
          }[]
        ).map((p) => p.url),
      { timeout: 30_000 },
    )
    .toContain(url);
}
