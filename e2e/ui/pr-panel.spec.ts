// A GitHub PR chip opens the PR in a panel docked on the right: details, checks, description and
// files with their diffs. The page reads it through Reef (GET /api/threads/{id}/pr), which uses the
// thread Bot's GitHub connection; the page never calls GitHub. GitHub is the scripted fake of
// github-fixture.ts. A Bot reply with a PR link plays testdata/claude/pr-link.jsonl.
import { mkdtempSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { expect, type Locator, type Page, test } from '@playwright/test';
import { api, type Machine, newBotAPI, startAtoll, waitStatus } from './atoll';
import {
  type ChangedFile,
  type FakeCheck,
  githubEnv,
  type GitHubFake,
  seedRepo,
  startGitHub,
  trackPR,
} from './github-fixture';
import {
  box,
  delegate,
  details,
  helperThreads,
  makeBoss,
  openDetails,
  startThread,
} from './helpers-fixture';

const prLink = 'Link me to the PR.';
const fixPrompt = 'Fix the seeded bug and open a PR.';
const gitlabMR = 'https://gitlab.example/fixtures/seeded/-/merge_requests/7';

let m: Machine;
let github: GitHubFake;
let root = '';
let bot: { id: string };
let repo: { id: string };
let connection: { id: string };
test.beforeEach(async () => {
  github = await startGitHub();
  root = mkdtempSync(join(tmpdir(), 'atoll-pr-panel-'));
  m = await startAtoll({
    fakes: ['claude', 'gh'],
    secretStore: true,
    env: githubEnv(github),
  });
  const seeded = await seedRepo(m, github, root);
  repo = seeded.repo;
  connection = seeded.connection;
  bot = await newBotAPI(m, 'Coder', 'claude', {
    repo_access: [repo.id],
    connections: [connection.id],
  });
});
test.afterEach(() => {
  m?.stop();
  github?.close();
  if (root) rmSync(root, { recursive: true, force: true });
});

const pull = (n = 1) => `${github.url}/octo/hello/pull/${n}`;
const aside = (page: Page) => page.getByRole('complementary', { name: 'Pull request' });
const chip = (scope: Page | Locator, n = 1) =>
  scope.getByRole('link', { name: new RegExp(`^PR ${n}\\b`) });
// detailsChip opens Thread details and returns its row of PR n.
async function detailsChip(page: Page, n = 1) {
  return chip(await openDetails(page), n);
}
const threadURL = (botID: string, id: string) => `${m.url}/bots/${botID}/threads/${id}`;

async function thread(text: string, b: { id: string } = bot, repoID: string | undefined = repo.id) {
  const t = await api(m, 'POST', `/api/bots/${b.id}/threads`, {
    text,
    ...(repoID && { repo_id: repoID }),
  });
  await waitStatus(m, t.id, 'done', 30_000);
  return await api(m, 'GET', `/api/threads/${t.id}`);
}

const checks: FakeCheck[] = [
  { name: 'e2e-go', status: 'completed', conclusion: 'success' },
  { name: 'build', status: 'in_progress', conclusion: null },
  { name: 'test-guard', status: 'completed', conclusion: 'failure' },
  { name: 'check', status: 'completed', conclusion: 'failure' },
];
const files: ChangedFile[] = [
  { filename: 'answer.txt', additions: 1, deletions: 1, patch: '@@ -1 +1 @@\n-41\n+42' },
  { filename: 'docs/big.bin', additions: 0, deletions: 0 },
  { filename: 'notes.md', additions: 3, deletions: 0, patch: '@@ -0,0 +1,3 @@\n+a\n+b\n+c' },
];

// describeFirst gives PR 1 what the panel shows.
function describeFirst() {
  github.describe(1, {
    author: 'octocat',
    head: 'fix/seeded-bug',
    body: 'Corrects the **answer**.\n\n- one\n- two',
  });
  github.setChecks(1, checks);
  github.setFiles(1, files);
}

// popups collects the pages the page opens.
function popups(page: Page) {
  const urls: string[] = [];
  page.on('popup', (p) => urls.push(p.url()));
  return urls;
}

test('the PR row in Thread details opens the panel with title, number, branches, state and the checks, failed first', async ({
  page,
}) => {
  await page.setViewportSize({ width: 1280, height: 800 });
  const t = await thread(fixPrompt);
  describeFirst();
  const opened = popups(page);
  await page.goto(threadURL(bot.id, t.id));
  await expect(aside(page)).toHaveCount(0);
  await (await detailsChip(page)).click();

  const panel = aside(page);
  await expect(panel).toBeVisible();
  await expect(panel.getByRole('heading', { level: 2, name: t.prs[0].title })).toBeVisible();
  await expect(panel.getByRole('link', { name: '#1', exact: true })).toHaveAttribute(
    'href',
    pull(),
  );
  await expect(panel).toContainText('octo/hello');
  await expect(panel).toContainText('octocat');
  await expect(panel).toContainText(/main\s*←\s*fix\/seeded-bug/);
  await expect(panel.getByText('Open', { exact: true })).toBeVisible();

  await expect(
    panel.getByText('2 checks failed: test-guard, check', { exact: true }),
  ).toBeVisible();
  const items = panel.getByRole('list', { name: 'Checks' }).getByRole('listitem');
  await expect(items).toHaveCount(4);
  const texts = await items.allTextContents();
  expect(texts.map((x) => x.match(/test-guard|check|build|e2e-go/)?.[0])).toEqual([
    'test-guard',
    'check',
    'build',
    'e2e-go',
  ]);
  for (const [i, word] of ['failed', 'failed', 'running', 'passed'].entries())
    await expect(items.nth(i).getByText(word, { exact: true })).toBeVisible();

  expect(page.url()).toBe(threadURL(bot.id, t.id));
  expect(opened).toEqual([]);
  const b = await box(panel);
  expect(b.width).toBeGreaterThan(460);
  expect(b.width).toBeLessThan(580);
  expect(Math.round(b.x + b.width)).toBe(1280);
});

test('the panel shows the description and files with counts; a file opens its diff; one with no patch links to GitHub', async ({
  page,
}) => {
  const t = await thread(fixPrompt);
  describeFirst();
  await page.goto(threadURL(bot.id, t.id));
  await (await detailsChip(page)).click();
  const panel = aside(page);

  await expect(panel).toContainText('Corrects the answer.');
  await expect(panel).not.toContainText('**');
  await expect(panel.getByText('one', { exact: true })).toBeVisible();

  await expect(panel).toContainText(/3 files/);
  await expect(panel).toContainText(/\+4\s*−1/);
  const file = (name: RegExp) => panel.getByRole('button', { name });
  await expect(file(/answer\.txt.*\+1\s*−1/)).toBeVisible();
  await expect(file(/docs\/big\.bin/)).toBeVisible();
  await expect(file(/notes\.md.*\+3\s*−0/)).toBeVisible();

  const diff = panel.locator('[data-component="diff"]');
  await expect(diff).toHaveCount(0);
  const answer = file(/answer\.txt/);
  await expect(answer).toHaveAttribute('aria-expanded', 'false');
  await answer.click();
  await expect(answer).toHaveAttribute('aria-expanded', 'true');
  await expect(diff).toHaveCount(1);
  await expect(diff).toContainText('41');
  await expect(diff).toContainText('42');
  await answer.click();
  await expect(diff).toHaveCount(0);

  const big = file(/docs\/big\.bin/);
  if ((await big.getAttribute('aria-expanded')) !== 'true') await big.click();
  await expect(panel.getByText('Too large to show here')).toBeVisible();
  await expect(panel.getByRole('link', { name: 'Open on GitHub' })).toHaveAttribute(
    'href',
    `${pull()}/files`,
  );

  github.describe(1, { body: 'A newer description.' });
  await panel.getByRole('button', { name: 'Reload' }).click();
  await expect(panel).toContainText('A newer description.');
});

test('a PR with no files says so', async ({ page }) => {
  const t = await thread(fixPrompt);
  await page.goto(threadURL(bot.id, t.id));
  await (await detailsChip(page)).click();
  await expect(aside(page).getByText('This PR has no file changes.')).toBeVisible();
});

test('a GitHub PR link in a Bot reply is the PR chip and opens the same panel; Cmd-click opens GitHub', async ({
  page,
  context,
}) => {
  github.open('Linked change');
  github.describe(1, { author: 'octocat', head: 'fix/linked' });
  const t = await thread(prLink, bot, '');
  await page.goto(threadURL(bot.id, t.id));
  const log = page.getByRole('log', { name: 'Messages' });
  const link = chip(log);
  await expect(link).toHaveAttribute('href', pull());
  await expect(log.getByText('the fix', { exact: true })).toHaveCount(0);
  await expect(log.locator(`a[href="${gitlabMR}"]`)).toHaveAttribute('href', gitlabMR);

  const [tab] = await Promise.all([
    context.waitForEvent('page'),
    link.click({ modifiers: ['Meta'] }),
  ]);
  try {
    await tab.waitForURL(pull());
  } finally {
    await tab.close();
  }
  await expect(aside(page)).toHaveCount(0);

  const opened = popups(page);
  await link.click();
  const panel = aside(page);
  await expect(panel.getByRole('heading', { level: 2, name: 'Linked change' })).toBeVisible();
  await expect(panel).toContainText(/main\s*←\s*fix\/linked/);
  expect(opened).toEqual([]);
  expect(page.url()).toBe(threadURL(bot.id, t.id));
});

test('the details popover PR rows open the panel, close the popover, and the next PR replaces the panel', async ({
  page,
}) => {
  const t = await thread(fixPrompt);
  github.open('Second change');
  await trackPR(m, t.id, github, 2, 'Second change');
  describeFirst();
  github.describe(2, { head: 'fix/second' });
  await page.goto(threadURL(bot.id, t.id));

  let popover = await openDetails(page);
  await chip(popover.getByRole('listitem').filter({ hasText: 'Second change' }), 2).click();
  await expect(details(page)).toHaveCount(0);
  await expect(aside(page)).toHaveCount(1);
  await expect(aside(page).getByRole('heading', { level: 2, name: 'Second change' })).toBeVisible();

  popover = await openDetails(page);
  await chip(popover.getByRole('listitem').filter({ hasText: t.prs[0].title }), 1).click();
  await expect(details(page)).toHaveCount(0);
  await expect(aside(page)).toHaveCount(1);
  await expect(aside(page).getByRole('heading', { level: 2, name: t.prs[0].title })).toBeVisible();
  await expect(aside(page).getByRole('heading', { level: 2, name: 'Second change' })).toHaveCount(
    0,
  );
});

test('a helper PR chip opens the panel, read with the helper thread', async ({ page }) => {
  test.setTimeout(120_000);
  const coder = { repo_access: [repo.id], connections: [connection.id] };
  const { bots } = await makeBoss(m, ['Tester'], { Tester: coder });
  await page.goto(m.url);
  const boss = await startThread(page, 'Poteto', delegate([['Tester', fixPrompt, 'octo-hello']]));
  const [helper] = await helperThreads(m, boss, 1);
  expect(helper.bot_id).toBe(bots.Tester.id);
  await expect
    .poll(
      async () => ((await api(m, 'GET', `/api/threads/${helper.id}`)).prs as unknown[]).length,
      {
        timeout: 60_000,
      },
    )
    .toBe(1);
  describeFirst();

  const reads: string[] = [];
  page.on('request', (r) => {
    if (r.url().includes('/pr?url=')) reads.push(new URL(r.url()).pathname);
  });
  const popover = await openDetails(page);
  await chip(popover.getByRole('listitem').filter({ hasText: 'Tester' })).click();
  await expect(details(page)).toHaveCount(0);
  const panel = aside(page);
  await expect(panel).toContainText(/main\s*←\s*fix\/seeded-bug/);
  expect(reads).toContain(`/api/threads/${helper.id}/pr`);
});

test('a GitLab MR chip still links out', async ({ page }) => {
  const t = await thread(
    `Use MCP tools. [{"server":"atoll","tool":"track_mr","args":{"url":"${gitlabMR}","title":"Seeded fix"}}]`,
    bot,
    '',
  );
  expect((t.prs as { url: string }[]).map((p) => p.url)).toEqual([gitlabMR]);
  await page.goto(threadURL(bot.id, t.id));
  const mr = (await openDetails(page)).getByRole('link', { name: /^!7\b/ });
  await expect(mr).toHaveAttribute('href', gitlabMR);
  await expect(mr).toHaveAttribute('target', '_blank');
  const popup = page.waitForEvent('popup');
  await mr.click();
  await (await popup).close();
  await expect(aside(page)).toHaveCount(0);
});

test('without a GitHub connection the panel says so and points to Settings › Connections', async ({
  page,
}) => {
  const loner = await newBotAPI(m, 'Loner', 'claude');
  const t = await thread(prLink, loner, '');
  await page.goto(threadURL(loner.id, t.id));
  await chip(page.getByRole('log', { name: 'Messages' })).click();
  const panel = aside(page);
  await expect(panel.getByText('Loner has no GitHub connection.')).toBeVisible();
  await expect(panel.getByRole('link', { name: 'Settings › Connections' })).toHaveAttribute(
    'href',
    '/settings/connections',
  );
});

test("a PR the connection cannot see shows the can't-see line with Retry and Open on GitHub", async ({
  page,
}) => {
  const t = await thread(prLink, bot, '');
  await page.goto(threadURL(bot.id, t.id));
  await chip(page.getByRole('log', { name: 'Messages' })).click();
  const panel = aside(page);
  await expect(panel.getByText("Coder's GitHub connection can't see PR #1.")).toBeVisible();
  await expect(panel.getByRole('link', { name: 'Open on GitHub' })).toHaveAttribute('href', pull());
  github.open('Now visible');
  await panel.getByRole('button', { name: 'Retry' }).click();
  await expect(panel.getByRole('heading', { level: 2, name: 'Now visible' })).toBeVisible();
});

test('another GitHub failure shows a plain line with Retry, Open on GitHub and tucked-away Details', async ({
  page,
}) => {
  github.open('Flaky');
  github.fail(1, true);
  const t = await thread(prLink, bot, '');
  await page.goto(threadURL(bot.id, t.id));
  await chip(page.getByRole('log', { name: 'Messages' })).click();
  const panel = aside(page);
  await expect(panel.getByText("Couldn't load PR #1.")).toBeVisible();
  await expect(panel.getByRole('link', { name: 'Open on GitHub' })).toHaveAttribute('href', pull());
  await expect(panel.getByRole('button', { name: 'Details' })).toBeVisible();
  await expect(panel).not.toContainText(/\{|Server Error|500/);
  github.fail(1, false);
  await panel.getByRole('button', { name: 'Retry' }).click();
  await expect(panel.getByRole('heading', { level: 2, name: 'Flaky' })).toBeVisible();
});

test('Esc and Close close the panel', async ({ page }) => {
  const t = await thread(fixPrompt);
  await page.goto(threadURL(bot.id, t.id));
  await (await detailsChip(page)).click();
  await expect(aside(page)).toBeVisible();
  await page.keyboard.press('Escape');
  await expect(aside(page)).toHaveCount(0);
  await (await detailsChip(page)).click();
  await expect(aside(page)).toBeVisible();
  await aside(page).getByRole('button', { name: 'Close' }).click();
  await expect(aside(page)).toHaveCount(0);
  expect(page.url()).toBe(threadURL(bot.id, t.id));
});

test('every request the page makes goes to Reef, never to GitHub', async ({ page }) => {
  const t = await thread(fixPrompt);
  describeFirst();
  const origin = new URL(m.url).origin;
  const outside: string[] = [];
  page.on('request', (r) => {
    if (/^https?:/.test(r.url()) && new URL(r.url()).origin !== origin) outside.push(r.url());
  });
  await page.goto(threadURL(bot.id, t.id));
  await (await detailsChip(page)).click();
  const panel = aside(page);
  await expect(panel.getByRole('heading', { level: 2, name: t.prs[0].title })).toBeVisible();
  await panel.getByRole('button', { name: /answer\.txt/ }).click();
  await panel.getByRole('button', { name: 'Reload' }).click();
  await expect(panel.getByRole('heading', { level: 2, name: t.prs[0].title })).toBeVisible();
  expect(github.reads(1)).toBeGreaterThan(0);
  expect(outside).toEqual([]);
});
