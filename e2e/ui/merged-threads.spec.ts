// A thread the person started whose PR merged leaves the sidebar tree but stays open: it is in
// the threads popup under "Other open threads", opens with its history and comes back to the tree
// when the person writes in it. Waiting items and pins keep it in the tree. GitHub is a scripted
// server the PR watcher reads. Contract: e2e/helpers-sidebar-contract.md.
import { mkdtempSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { expect, type Page, test } from '@playwright/test';
import { api, type Machine, newBotAPI, prompts, startAtoll, threadTree } from './atoll';
import { githubEnv, type GitHubFake, seedRepo, startGitHub } from './github-fixture';
import { send } from './helpers-fixture';

const fix = 'Fix the seeded bug and open a PR.';

// finish ends a turn through the atoll MCP server's finish_run, so a follow-up message in a thread
// that has a session plays: the recordings only match a thread's first message.
const finish = `Use MCP tools. [{"server":"atoll","tool":"finish_run","args":${JSON.stringify({
  done: 'Done.',
  tried: 'none',
  blocked: '',
  next: 'none',
})}}]`;

// These recordings fix the bug and open the pull request as `fix` does, then end the run with a
// decision for the person or a blocker.
const askFix = 'Fix the seeded bug, open a PR and ask how to ship it.';
const blockedFix = 'Fix the seeded bug, open a PR and report the sign-in blocker.';

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
  await page.goto(`${m.url}/bots/${elsewhere.id}`);
  await expect(page.getByRole('heading', { name: 'What should Elsewhere work on?' })).toBeVisible();
}

const inTree = (page: Page, threadID: string) =>
  threadTree(page, 'Coder').locator(`a[href$="/threads/${threadID}"]`);

const rowOf = (page: Page, threadID: string) =>
  threadTree(page, 'Coder')
    .getByRole('listitem')
    .filter({ has: page.locator(`a[href$="/threads/${threadID}"]`) });

async function openPopup(page: Page) {
  await threadTree(page, 'Coder')
    .getByRole('button', { name: /^\d+ more$/ })
    .click();
  const popup = page.getByRole('dialog', { name: 'Threads with Coder' });
  await expect(popup).toBeVisible();
  return popup;
}

test('a thread whose PR merged leaves the sidebar tree, stays open in the threads popup and opens with its history', async ({
  page,
}) => {
  const id = await prThread(1);
  await away(page);
  await expect(inTree(page, id)).toHaveCount(1);

  await merged(id, 1);
  await woken(id);
  await expect(inTree(page, id)).toHaveCount(0, { timeout: 10_000 });
  expect((await api(m, 'GET', `/api/threads/${id}`)).closed).toBe(false);

  const popup = await openPopup(page);
  const other = popup.getByRole('list', { name: 'Other open threads' });
  await expect(other.locator(`a[href$="/threads/${id}"]`)).toHaveCount(1);
  await expect(
    popup.getByRole('list', { name: 'In the sidebar' }).locator(`a[href$="/threads/${id}"]`),
  ).toHaveCount(0);
  await other.locator(`a[href$="/threads/${id}"]`).click();
  await expect(page).toHaveURL(new RegExp(`/threads/${id}$`));
  const log = page.getByRole('log', { name: 'Messages' });
  await expect(log.getByRole('article', { name: 'You' }).first()).toContainText(fix);
  await expect(log).toContainText('Merge noted.');
});

for (const [what, first] of [
  ['an approval', fix],
  ['an ask', askFix],
  ['a blocker', blockedFix],
] as const) {
  test(`a merged thread with ${what} waiting stays in the tree while it waits`, async ({
    page,
    context,
  }) => {
    const id = await prThread(1, first);
    if (what === 'an approval') {
      await api(m, 'POST', `/api/threads/${id}/messages`, {
        text: prompts.write,
      });
      await waitStatus(id, 'waiting');
    }
    await away(page);
    await expect(inTree(page, id)).toHaveCount(1);

    await expect(rowOf(page, id)).toHaveAttribute(
      'data-marker',
      what === 'an approval' ? 'needs-you' : 'waiting',
    );

    await merged(id, 1);
    await expect(inTree(page, id)).toHaveCount(1);
    await expect(rowOf(page, id)).toHaveAttribute(
      'data-marker',
      what === 'an approval' ? 'needs-you' : 'waiting',
    );

    if (what === 'an approval') {
      const answering = await context.newPage();
      await answering.goto(`${m.url}/bots/${coder.id}/threads/${id}`);
      await answering
        .getByRole('log', { name: 'Messages' })
        .getByRole('button', { name: 'Allow', exact: true })
        .click();
    } else await api(m, 'POST', `/api/threads/${id}/messages`, { text: finish });
    await waitStatus(id, 'done');
    await expect
      .poll(async () => {
        const bots: { threads?: { id: string; waiting?: unknown[] }[] }[] = await api(
          m,
          'GET',
          '/api/bots',
        );
        return bots.flatMap((b) => b.threads ?? []).find((t) => t.id === id)?.waiting ?? [];
      })
      .toEqual([]);
    // Allow leaves the thread; an answer by message is the person's message after the merge, so
    // the thread stays for good.
    await expect(inTree(page, id)).toHaveCount(what === 'an approval' ? 0 : 1, { timeout: 15_000 });
    expect((await api(m, 'GET', `/api/threads/${id}`)).closed).toBe(false);
  });
}

test('a pinned thread whose PR merged stays in the sidebar tree', async ({ page }) => {
  const id = await prThread(1);
  await api(m, 'POST', `/api/threads/${id}/pin`);
  await away(page);
  await expect(inTree(page, id)).toHaveCount(1);

  await merged(id, 1);
  await woken(id);
  await expect(inTree(page, id)).toHaveCount(1);
});

test('a message sent to a merged thread from the popup brings it back to the tree', async ({
  page,
}) => {
  const id = await prThread(1);
  await away(page);
  await merged(id, 1);
  await woken(id);
  await expect(inTree(page, id)).toHaveCount(0, { timeout: 10_000 });

  const popup = await openPopup(page);
  await popup
    .getByRole('list', { name: 'Other open threads' })
    .locator(`a[href$="/threads/${id}"]`)
    .click();
  await expect(page).toHaveURL(new RegExp(`/threads/${id}$`));
  await send(page, prompts.hang);
  await waitStatus(id, 'running');
  await away(page);
  await expect(inTree(page, id)).toHaveCount(1);
});

test('a message sent to a merged thread brings it back for good: it is still in the tree after that run ends', async ({
  page,
}) => {
  const id = await prThread(1);
  await away(page);
  await merged(id, 1);
  await woken(id);
  await expect(inTree(page, id)).toHaveCount(0, { timeout: 10_000 });

  await page.goto(`${m.url}/bots/${coder.id}/threads/${id}`);
  await send(page, finish);
  await expect(page.getByRole('log', { name: 'Messages' })).toContainText('Done.', {
    timeout: 30_000,
  });
  await waitStatus(id, 'done');
  await away(page);
  await expect(inTree(page, id)).toHaveCount(1);
  await page.reload();
  await expect(inTree(page, id)).toHaveCount(1);
  await expect(rowOf(page, id)).not.toHaveAttribute('data-marker', 'running');
});

test('a Bot whose only open thread merged shows 1 more, which opens the popup with that thread', async ({
  page,
}) => {
  const id = await prThread(1);
  await away(page);
  await merged(id, 1);
  await woken(id);
  await expect(inTree(page, id)).toHaveCount(0, { timeout: 10_000 });

  await expect(threadTree(page, 'Coder').getByRole('button', { name: /^1 more$/ })).toBeVisible();
  const popup = await openPopup(page);
  await expect(
    popup.getByRole('list', { name: 'Other open threads' }).locator(`a[href$="/threads/${id}"]`),
  ).toHaveCount(1);
});
