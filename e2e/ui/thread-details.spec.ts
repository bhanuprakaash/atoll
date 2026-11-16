// The thread's status belongs in its header panel, leaving the composer for the next message.
import { mkdtempSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { expect, test } from '@playwright/test';
import { api, type Machine, newBotAPI, prompts, startAtoll, waitStatus } from './atoll';
import { githubEnv, type GitHubFake, seedRepo, startGitHub } from './github-fixture';
import { box, composerArea, details, openDetails } from './helpers-fixture';

let m: Machine;
let github: GitHubFake;
let root = '';
let bot: { id: string };
let repo: { id: string };
test.beforeEach(async () => {
  github = await startGitHub();
  root = mkdtempSync(join(tmpdir(), 'atoll-thread-details-'));
  m = await startAtoll({
    fakes: ['claude', 'gh'],
    secretStore: true,
    env: githubEnv(github),
  });
  const seeded = await seedRepo(m, github, root);
  repo = seeded.repo;
  bot = await newBotAPI(m, 'Coder', 'claude', {
    repo_access: [repo.id],
    connections: [seeded.connection.id],
  });
});
test.afterEach(() => {
  m?.stop();
  github?.close();
  if (root) rmSync(root, { recursive: true, force: true });
});

async function thread(text = prompts.pong, withRepo = true) {
  const t = await api(m, 'POST', `/api/bots/${bot.id}/threads`, {
    text,
    ...(withRepo && { repo_id: repo.id }),
  });
  await waitStatus(m, t.id, 'done', 30_000);
  return await api(m, 'GET', `/api/threads/${t.id}`);
}

test('the header icon is before Bot settings; switching threads closes Thread details', async ({
  page,
}) => {
  const first = await thread();
  const second = await thread();
  await page.goto(`${m.url}/bots/${bot.id}/threads/${first.id}`);
  const icon = page.getByRole('button', {
    name: 'Thread details',
    exact: true,
  });
  const settings = page.getByRole('button', {
    name: 'Bot settings',
    exact: true,
  });
  await expect(icon).toBeVisible();
  expect((await box(icon)).x).toBeLessThan((await box(settings)).x);
  await openDetails(page);
  // Use an in-app link so the check covers a route change, not a document reload.
  await page.locator(`a[href$="/threads/${second.id}"]`).first().click();
  await expect(page).toHaveURL(`${m.url}/bots/${bot.id}/threads/${second.id}`);
  await expect(details(page)).toHaveCount(0);
});

test('Workspace shows No repo or the repo and No branch yet before work starts', async ({
  page,
}) => {
  for (const withRepo of [false, true]) {
    const t = await thread(prompts.pong, withRepo);
    expect(t.worktree).toBe('');
    await page.goto(`${m.url}/bots/${bot.id}/threads/${t.id}`);
    const panel = await openDetails(page);
    await expect(panel.getByText('Workspace', { exact: true })).toBeVisible();
    await expect(
      panel.getByText(withRepo ? 'No branch yet' : 'No repo', { exact: true }),
    ).toBeVisible();
    if (withRepo) await expect(panel.getByText('octo-hello', { exact: true })).toBeVisible();
    await expect(panel.getByText('Pull requests', { exact: true })).toHaveCount(0);
    await expect(panel.getByText('Helpers', { exact: true })).toHaveCount(0);
  }
});

test('Workspace copies repo, full worktree path and branch; the composer has no status chips', async ({
  page,
  context,
}) => {
  await context.grantPermissions(['clipboard-read', 'clipboard-write']);
  const t = await thread(
    'Use MCP tools. [{"server":"atoll","tool":"start_work","args":{"type":"fix","slug":"panel-copy"}}]',
  );
  expect(t.worktree).toBeTruthy();
  expect(t.branch).toBe('fix/panel-copy');
  await page.goto(`${m.url}/bots/${bot.id}/threads/${t.id}`);
  await expect(composerArea(page)).not.toContainText(/octo-hello|fix\/panel-copy/);
  await expect(page.getByRole('group', { name: 'Thread context' })).toHaveCount(0);
  const panel = await openDetails(page);
  await expect(panel).toContainText('octo-hello');
  await expect(panel.getByTitle(t.worktree, { exact: true })).toBeVisible();
  await expect(panel.getByTitle(t.branch, { exact: true })).toBeVisible();
  const copies = panel.getByRole('button', { name: /Copy/i });
  await expect(copies).toHaveCount(3);
  for (const [i, value] of ['octo-hello', t.worktree, t.branch].entries()) {
    await copies.nth(i).click();
    await expect.poll(() => page.evaluate(() => navigator.clipboard.readText())).toBe(value);
  }
});

test('Pull requests is a list with the tracked reference, title, state dot and open CI word; its row opens the PR', async ({
  page,
}) => {
  const t = await thread('Fix the seeded bug and open a PR.');
  expect(t.mr_url).toBe(`${github.url}/octo/hello/pull/1`);
  await page.goto(`${m.url}/bots/${bot.id}/threads/${t.id}`);
  await expect(composerArea(page).getByRole('link', { name: /#1/ })).toHaveCount(0);
  github.setCI(1, 'running');
  const panel = await openDetails(page);
  await expect(panel.getByText('Pull requests', { exact: true })).toBeVisible();
  const row = panel.getByRole('listitem').filter({ hasText: '#1' });
  await expect(row).toHaveCount(1);
  await expect(row).toContainText(t.mr_title);
  await expect(row.getByText('running', { exact: true })).toBeVisible();
  const pr = row.getByRole('link', { name: /#1/ });
  await expect(pr).toHaveAttribute('href', t.mr_url);
  const dot = pr.locator('[aria-hidden="true"]').first();
  await expect(dot).toBeVisible();
  const size = await box(dot);
  expect(size.width).toBeGreaterThan(0);
  expect(size.width).toBe(size.height);
  github.setCI(1, 'failed');
  await expect(row.getByText('failed', { exact: true })).toBeVisible();
  const popup = page.waitForEvent('popup');
  await pr.click();
  const opened = await popup;
  try {
    await expect(opened).toHaveURL(t.mr_url);
  } finally {
    await opened.close();
  }
  const color = await dot.evaluate((el) => getComputedStyle(el).backgroundColor);
  github.merge(1);
  await expect(row.getByText('failed', { exact: true })).toHaveCount(0);
  await expect
    .poll(() => dot.evaluate((el) => getComputedStyle(el).backgroundColor))
    .not.toBe(color);
});

test('at 420px Thread details fits the thread column minus 32px without sideways page scrolling', async ({
  page,
}) => {
  await page.setViewportSize({ width: 420, height: 800 });
  const t = await thread();
  await page.goto(`${m.url}/bots/${bot.id}/threads/${t.id}`);
  const log = page.getByRole('log', { name: 'Messages' });
  const column = log.locator('xpath=..');
  const panel = await openDetails(page);
  expect((await box(panel)).width).toBeCloseTo(Math.min(340, (await box(column)).width - 32), 0);
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(
    true,
  );
  const p = await box(panel);
  expect(p.x).toBeGreaterThanOrEqual(0);
  expect(p.x + p.width).toBeLessThanOrEqual(420);
});
