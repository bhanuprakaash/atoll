// A thread the person started whose PR merged is hidden but stays open: it stays a row under Recent
// in its Bot's pane, opens with its history, and is left out of Now until the person writes in it.
// An open approval and a pin keep it shown. GitHub is a scripted server the PR watcher reads.
import { mkdtempSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { expect, type Page, test } from '@playwright/test';
import {
  api,
  type Machine,
  newBotAPI,
  nowPane,
  prompts,
  sendMessageAPI,
  showPane,
  startAtoll,
  threadPane,
} from './atoll';
import { githubEnv, type GitHubFake, seedRepo, startGitHub } from './github-fixture';

const fix = 'Fix the seeded bug and open a PR.';

// recall is a follow-up message that plays through the atoll MCP server's recall tool, so a
// follow-up message in a thread that has a session plays: the recordings only match a thread's
// first message. Its reply starts with the tool's name.
const recall = 'Use MCP tools. [{"server":"atoll","tool":"recall","args":{"query":"x"}}]';

let m: Machine;
let github: GitHubFake;
let root = '';
let coder: { id: string };
let repo: { id: string };
let elsewhere: { id: string } | undefined;
test.beforeEach(async () => {
  elsewhere = undefined;
  github = await startGitHub();
  root = mkdtempSync(join(tmpdir(), 'atoll-ui-merged-threads-'));
  m = await startAtoll({
    fakes: ['claude', 'gh'],
    secretStore: true,
    // A wake in the thread ends a waiting item (the scripted wake finishes with none), so the
    // tests that need one to stay hold the wake back for longer than they run.
    env: {
      ...githubEnv(github),
      ...(test.info().title.includes(' waiting ') && {
        ATOLL_WAKE_WINDOW: '300s',
      }),
    },
  });
  const seeded = await seedRepo(m, github, root);
  repo = seeded.repo;
  coder = await newBotAPI(m, 'Coder', 'claude', {
    repo_access: [repo.id],
    connections: [seeded.connection.id],
  });
});
test.afterEach(() => {
  m?.stop();
  github?.close();
  if (root) rmSync(root, { recursive: true, force: true });
});

async function waitStatus(threadID: string, status: string) {
  await expect
    .poll(async () => (await api(m, 'GET', `/api/threads/${threadID}`)).run_status, {
      timeout: 30_000,
    })
    .toBe(status);
}

// prThread is a thread of Coder whose Bot opened pull request n.
async function prThread(n: number, text = fix) {
  const t = await api(m, 'POST', `/api/bots/${coder.id}/threads`, {
    text,
    repo_id: repo.id,
  });
  await waitStatus(t.id, 'done');
  expect((await api(m, 'GET', `/api/threads/${t.id}`)).mr_url).toBe(
    `${github.url}/octo/hello/pull/${n}`,
  );
  return t.id as string;
}

// merged merges pull request n on GitHub, waits until the watcher has read the merge three more
// times, so an absence after it is not just a poll that never ran, and until the thread's run
// from the Reef wake has ended.
async function merged(threadID: string, n: number) {
  const before = github.reads(n);
  github.merge(n);
  await expect.poll(() => github.reads(n), { timeout: 30_000 }).toBeGreaterThanOrEqual(before + 3);
  await expect
    .poll(async () => (await api(m, 'GET', `/api/threads/${threadID}`)).mr_state, {
      timeout: 30_000,
    })
    .toBe('merged');
}

async function woken(threadID: string) {
  await expect
    .poll(
      async () =>
        (
          (await api(m, 'GET', `/api/threads/${threadID}/messages`)) as {
            kind?: string;
          }[]
        ).filter((x) => x.kind === 'merged').length,
      { timeout: 30_000 },
    )
    .toBe(1);
  await waitStatus(threadID, 'done');
}

// away is a page on another Bot, so no thread of Coder is open: an open thread always shows.
async function away(page: Page) {
  elsewhere ??= await newBotAPI(m, 'Elsewhere');
  await showPane(page, m, elsewhere.id, 'Elsewhere');
}

// inNow is the thread's link in Now; Now lists a thread that needs you, runs or replied, unless the
// watcher hides it. A thread whose run ended while it was not on screen is Replied.
const inNow = (page: Page, threadID: string) =>
  threadPane(page, 'Now').locator(`a[href$="/threads/${threadID}"]`);

const rowOf = (page: Page, threadID: string) =>
  threadPane(page, 'Now')
    .getByRole('listitem')
    .filter({ has: page.locator(`a[href$="/threads/${threadID}"]`) });

// recentOf is the thread's link under Recent in Coder's pane.
async function recentOf(page: Page, threadID: string) {
  await showPane(page, m, coder.id, 'Coder');
  return threadPane(page, 'Coder')
    .getByRole('list', { name: 'Recent', exact: true })
    .locator(`a[href$="/threads/${threadID}"]`);
}

test('a thread whose PR merged leaves Now, stays a row under Recent in its Bot pane and opens with its history', async ({
  page,
}) => {
  const id = await prThread(1);
  await away(page);
  await nowPane(page);
  await expect(inNow(page, id)).toHaveCount(1);

  await merged(id, 1);
  await woken(id);
  await expect(inNow(page, id)).toHaveCount(0, { timeout: 10_000 });
  expect((await api(m, 'GET', `/api/threads/${id}`)).closed).toBe(false);

  const recent = await recentOf(page, id);
  await expect(recent).toHaveCount(1);
  await recent.click();
  await expect(page).toHaveURL(new RegExp(`/threads/${id}$`));
  const log = page.getByRole('log', { name: 'Messages' });
  await expect(log.getByRole('article', { name: 'You' }).first()).toContainText(fix);
  await expect(log).toContainText('PR #1 merged');
  await expect(log.getByText('Merge noted.')).toHaveCount(0);
});

test('a merged thread with an approval waiting stays in Now while it waits', async ({
  page,
  context,
}) => {
  const id = await prThread(1, fix);
  await api(m, 'POST', `/api/threads/${id}/messages`, { text: prompts.write });
  await waitStatus(id, 'waiting');
  await away(page);
  await nowPane(page);
  await expect(inNow(page, id)).toHaveCount(1);
  await expect(rowOf(page, id)).toHaveAttribute('data-marker', 'needs-you');

  await merged(id, 1);
  await expect(inNow(page, id)).toHaveCount(1);
  await expect(rowOf(page, id)).toHaveAttribute('data-marker', 'needs-you');

  const answering = await context.newPage();
  await answering.goto(`${m.url}/bots/${coder.id}/threads/${id}`);
  await answering
    .getByRole('log', { name: 'Messages' })
    .getByRole('button', { name: 'Allow', exact: true })
    .click();
  await waitStatus(id, 'done');
  // Allow ends the wait: the merged thread leaves Now again, still open.
  await expect(inNow(page, id)).toHaveCount(0, { timeout: 15_000 });
  expect((await api(m, 'GET', `/api/threads/${id}`)).closed).toBe(false);
});

test('a pinned thread whose PR merged stays in Now', async ({ page }) => {
  const id = await prThread(1);
  await api(m, 'POST', `/api/threads/${id}/pin`);
  await away(page);
  await nowPane(page);
  await expect(inNow(page, id)).toHaveCount(1);

  await merged(id, 1);
  await woken(id);
  await expect(inNow(page, id)).toHaveCount(1);
});

test('a message sent to a merged thread brings it back for good: it is in Now after that run ends', async ({
  page,
}) => {
  const id = await prThread(1);
  await away(page);
  await nowPane(page);
  await merged(id, 1);
  await woken(id);
  await expect(inNow(page, id)).toHaveCount(0, { timeout: 10_000 });

  await sendMessageAPI(m, id, recall);
  await expect(inNow(page, id)).toHaveCount(1);
  await expect(rowOf(page, id)).not.toHaveAttribute('data-marker', 'running');
  await page.reload();
  await nowPane(page);
  await expect(inNow(page, id)).toHaveCount(1);
});
