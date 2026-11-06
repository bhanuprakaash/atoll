// Slice 6 acceptance, from a fresh install with no terminal: sign in to a provider and GitLab,
// register a repo, ask a Bot to fix a seeded bug, approve a step, get the MR link. Fakes stand in
// for the provider (its login too), glab and GitLab's OAuth; git and the origin are real. Real
// providers and gitlab.com are in mac-app-contract.md, "Only you can check".
import { execFileSync } from 'node:child_process';
import { createServer, type Server } from 'node:http';
import { mkdirSync, mkdtempSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { expect, test } from '@playwright/test';
import { loginURL, type Machine, pickModel, prompts, startAtoll, choose } from './atoll';

const mrURL = 'https://gitlab.example/fixtures/seeded/-/merge_requests/1';

let m: Machine;
let gitlab: Server;
let gitlabURL = '';
let allowed = false;
let repo = '';

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

test.beforeAll(async () => {
  // GitLab's device flow: pending until the test plays the user approving in the browser.
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
      } else if (req.url === '/oauth/token' && !allowed) {
        res.writeHead(400).end('{"error":"authorization_pending"}');
      } else if (req.url === '/oauth/token') {
        res.end(
          JSON.stringify({
            access_token: 'glpat-ui-fresh',
            refresh_token: 'glrt-ui-fresh',
            token_type: 'Bearer',
            scope: 'api',
            expires_in: 7200,
          }),
        );
      } else {
        res.writeHead(404).end('{}');
      }
    });
  });
  await new Promise<void>((ok) => gitlab.listen(0, '127.0.0.1', ok));
  const addr = gitlab.address();
  gitlabURL = `http://127.0.0.1:${typeof addr === 'object' && addr ? addr.port : 0}`;

  m = await startAtoll({
    fakes: ['claude', 'glab'],
    signedIn: [],
    userName: null,
    secretStore: true,
    env: { ATOLL_REPO_POLL_INTERVAL: '100ms' },
  });
  writeFileSync(join(m.bin, 'mr-state'), 'opened');
  const root = mkdtempSync(join(tmpdir(), 'atoll-ui-repo-'));
  const origin = join(root, 'origin.git');
  repo = join(root, 'seeded');
  mkdirSync(repo);
  git(root, 'init', '--bare', origin);
  git(repo, 'init', '-b', 'main');
  writeFileSync(join(repo, 'answer.txt'), '41\n');
  git(repo, 'add', 'answer.txt');
  git(repo, 'commit', '-m', 'seed bug');
  git(repo, 'remote', 'add', 'origin', origin);
  git(repo, 'push', '-u', 'origin', 'main');
  git(root, '--git-dir', origin, 'symbolic-ref', 'HEAD', 'refs/heads/main');
});
test.afterAll(() => {
  m?.stop();
  gitlab?.close();
  if (repo) rmSync(join(repo, '..'), { recursive: true, force: true });
});

test('fresh install to an MR link without a terminal', async ({ page, context }) => {
  test.setTimeout(120_000);
  await context.route('https://login.reef.test/**', (r) => r.fulfill({ body: 'signing in' }));
  await page.goto(m.url);

  // First run: name, provider sign-in, Chief.
  await page.getByRole('textbox', { name: 'What should your Bots call you?' }).fill('Ada');
  await page.getByRole('button', { name: 'Continue' }).click();
  const claude = page.getByRole('group', { name: 'Claude Code' });
  const popup = page.waitForEvent('popup');
  await claude.getByRole('button', { name: 'Sign in' }).click();
  expect((await popup).url()).toBe(loginURL.claude);
  m.release('claude');
  await expect(claude).toContainText('Signed in');
  await page.getByRole('button', { name: 'Continue' }).click();
  await page.getByRole('button', { name: 'Open Chief' }).click();
  await expect(page.getByRole('navigation', { name: 'Bots' })).toBeVisible();

  // GitLab sign-in.
  const nav = page.getByRole('navigation', { name: 'Bots' });
  await nav.getByRole('link', { name: /Settings/ }).click();
  await page.getByRole('link', { name: 'Connections' }).click();
  await page.getByRole('textbox', { name: 'GitLab instance URL' }).fill(gitlabURL);
  await page.getByRole('button', { name: 'Sign in to GitLab' }).click();
  await expect(page.getByText('REEF-1234')).toBeVisible();
  allowed = true;
  await expect(page.getByText(/^connected$/i).first()).toBeVisible({
    timeout: 20_000,
  });

  // Register the repo.
  await page.getByRole('link', { name: 'Repos' }).click();
  await page.getByRole('textbox', { name: 'Repo name' }).fill('seeded');
  await page.getByRole('textbox', { name: 'Local path' }).fill(repo);
  await page.getByRole('button', { name: 'Register repo' }).click();
  await expect(page.getByText(repo)).toBeVisible();

  // A Bot with GitLab.
  await nav.getByRole('button', { name: 'New Bot' }).click();
  await page.getByRole('textbox', { name: 'Name' }).fill('Fixer');
  await page.getByRole('textbox', { name: 'Persona' }).fill('Fixes bugs and opens MRs.');
  await pickModel(page, page, 'Claude Code');
  await page.getByRole('button', { name: 'Add connection' }).click();
  await page.getByRole('menuitem', { name: /GitLab/ }).click();
  await page.getByRole('button', { name: 'Create Bot' }).click();
  await expect(page.getByRole('heading', { name: 'What should Fixer work on?' })).toBeVisible();

  // Ask it to fix the seeded bug in the repo; approve its step; get the MR.
  await choose(page, page.getByRole('combobox', { name: 'Repo' }), 'seeded');
  await page.getByRole('textbox', { name: 'Message' }).fill(prompts.freshFix);
  await page.getByRole('button', { name: 'Send' }).click();
  const log = page.getByRole('log', { name: 'Messages' });
  await expect(log.getByText('go test ./...').first()).toBeVisible();
  await log.getByRole('button', { name: 'Allow', exact: true }).click();

  const mr = log.getByRole('link', { name: /!1\b/ });
  await expect(mr).toHaveAttribute('href', mrURL, { timeout: 30_000 });
  await expect(mr).toContainText('Open');
  const context_ = page.getByRole('group', { name: 'Thread context' });
  await expect(context_).toContainText('seeded');
  await expect(context_).toContainText('fix/seeded-bug');
  await expect(context_).toContainText('!1');
});
