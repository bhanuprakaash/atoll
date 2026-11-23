// The person merges a Bot's PR from the PR panel with two clicks: Merge, then Confirm merge?. Reef
// merges only the commit the panel showed, with the first method the repo allows, and checks the
// thread's PR at once. GitHub is the scripted fake of github-fixture.ts; the poll interval is an
// hour, so only that immediate check can turn the thread's PR merged.
import { mkdtempSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { expect, type Locator, type Page, test } from '@playwright/test';
import { api, type Machine, newBotAPI, startAtoll, waitStatus } from './atoll';
import { githubEnv, type GitHubFake, headSha, seedRepo, startGitHub } from './github-fixture';

const fixPrompt = 'Fix the seeded bug and open a PR.';
const moved = 'New commits were pushed since you opened this PR. Nothing was merged.';

let m: Machine;
let github: GitHubFake;
let root = '';
let bot: { id: string };
test.beforeEach(async ({ page }) => {
  await page.setViewportSize({ width: 1280, height: 800 });
  github = await startGitHub();
  root = mkdtempSync(join(tmpdir(), 'atoll-pr-merge-'));
  m = await startAtoll({
    fakes: ['claude', 'gh'],
    secretStore: true,
    env: { ...githubEnv(github), ATOLL_REPO_POLL_INTERVAL: '1h' },
  });
  const seeded = await seedRepo(m, github, root);
  bot = await newBotAPI(m, 'Coder', 'claude', {
    repo_access: [seeded.repo.id],
    connections: [seeded.connection.id],
  });
  github.describe(1, { head: 'fix/seeded-bug' });
});
test.afterEach(() => {
  m?.stop();
  github?.close();
  if (root) rmSync(root, { recursive: true, force: true });
});

const aside = (page: Page) => page.getByRole('complementary', { name: 'Pull request' });
const mergeName = /^(Merge|Squash and merge|Rebase and merge|Confirm merge\?|Merging…)$/;
const mergeButtons = (panel: Locator) => panel.getByRole('button', { name: mergeName });
const button = (panel: Locator, name: string) => panel.getByRole('button', { name, exact: true });
const loadedSha = headSha(1);
const short = loadedSha.slice(0, 7);

// prInDetails opens Thread details and returns its PR 1 row.
async function prInDetails(page: Page) {
  await page.getByRole('button', { name: 'Thread details', exact: true }).click();
  return page.getByRole('dialog', { name: 'Thread details' }).getByRole('link', { name: /^PR 1\b/ });
}

// openPanel starts a thread whose Bot opened PR 1, opens it and returns the thread and the panel
// once it has loaded.
async function openPanel(page: Page) {
  const t = await api(m, 'POST', `/api/bots/${bot.id}/threads`, {
    text: fixPrompt,
    repo_id: (await api(m, 'GET', '/api/repos'))[0].id,
  });
  await waitStatus(m, t.id, 'done', 30_000);
  await page.goto(`${m.url}/bots/${bot.id}/threads/${t.id}`);
  await (await prInDetails(page)).click();
  const panel = aside(page);
  await expect(panel.getByText('Open', { exact: true })).toBeVisible();
  return { id: t.id as string, panel };
}

// prState is the state of the thread's PR 1.
async function prState(threadID: string) {
  const prs = (await api(m, 'GET', `/api/threads/${threadID}`)).prs as {
    state: string;
  }[];
  return prs[0]?.state;
}

test('Merge then Confirm merge merges the loaded head with the first allowed method; the panel shows Merged and the thread the merged line without waiting for the poll', async ({
  page,
}) => {
  const { id, panel } = await openPanel(page);
  await expect(
    panel.getByText(`Merges fix/seeded-bug into main at ${short}, the commit shown here`),
  ).toBeVisible();
  expect(await prState(id)).not.toBe('merged');

  await button(panel, 'Merge').click();
  await button(panel, 'Confirm merge?').click();

  await expect(panel.getByText('Merged', { exact: true })).toBeVisible();
  await expect(mergeButtons(panel)).toHaveCount(0);
  expect(github.merges(1)).toEqual([{ sha: loadedSha, method: 'merge', merged: true }]);
  await expect.poll(() => prState(id), { timeout: 10_000 }).toBe('merged');
  await expect(page.getByRole('log', { name: 'Messages' })).toContainText(
    `PR #1 merged into main at ${short}`,
    { timeout: 30_000 },
  );
});

test('a head that moved after the panel loaded is refused with a plain line and Reload; nothing is merged, and Reload shows the new head', async ({
  page,
}) => {
  const { panel } = await openPanel(page);
  const next = github.pushHead(1);

  await button(panel, 'Merge').click();
  await button(panel, 'Confirm merge?').click();

  await expect(panel.getByText(moved, { exact: true })).toBeVisible();
  await expect(panel).not.toContainText(/[{}]|Head branch was modified/);
  await expect(panel.getByText('Merged', { exact: true })).toHaveCount(0);
  expect(github.merges(1)).toEqual([{ sha: loadedSha, method: 'merge', merged: false }]);

  await button(panel, 'Reload').click();
  await expect(
    panel.getByText(
      `Merges fix/seeded-bug into main at ${next.slice(0, 7)}, the commit shown here`,
    ),
  ).toBeVisible();
  await expect(panel.getByText(moved, { exact: true })).toHaveCount(0);
  await button(panel, 'Merge').click();
  await button(panel, 'Confirm merge?').click();
  await expect(panel.getByText('Merged', { exact: true })).toBeVisible();
  expect(github.merges(1).at(-1)).toEqual({
    sha: next,
    method: 'merge',
    merged: true,
  });
});

test('Confirm merge? resets after 3 seconds, on Esc without closing the panel, and on a click elsewhere', async ({
  page,
}) => {
  const { panel } = await openPanel(page);
  const merge = button(panel, 'Merge');
  const confirm = button(panel, 'Confirm merge?');

  await merge.click();
  const clicked = Date.now();
  await expect(confirm).toBeVisible();
  await page.waitForTimeout(1500);
  await expect(confirm).toBeVisible();
  await expect(merge).toBeVisible({ timeout: 4_000 });
  expect(Date.now() - clicked).toBeGreaterThanOrEqual(2800);
  await expect(confirm).toHaveCount(0);

  await merge.click();
  await expect(confirm).toBeVisible();
  await page.keyboard.press('Escape');
  await expect(merge).toBeVisible();
  await expect(confirm).toHaveCount(0);
  await expect(aside(page)).toBeVisible();

  await merge.click();
  await expect(confirm).toBeVisible();
  await panel.getByRole('heading', { level: 2 }).click();
  await expect(merge).toBeVisible();
  await expect(confirm).toHaveCount(0);

  await merge.click();
  await expect(confirm).toBeVisible();
  await page.getByRole('log', { name: 'Messages' }).click({ position: { x: 5, y: 5 } });
  await expect(merge).toBeVisible();
  await expect(confirm).toHaveCount(0);

  await merge.click();
  await page.keyboard.press('Escape');
  await page.keyboard.press('Escape');
  await expect(aside(page)).toHaveCount(0);
  expect(github.merges(1)).toEqual([]);
});

test('a repo that allows only squash shows Squash and merge and sends squash', async ({ page }) => {
  github.setMergeMethods({ merge: false, squash: true, rebase: false });
  const { panel } = await openPanel(page);
  await expect(button(panel, 'Merge')).toHaveCount(0);

  await button(panel, 'Squash and merge').click();
  await button(panel, 'Confirm merge?').click();

  await expect(panel.getByText('Merged', { exact: true })).toBeVisible();
  expect(github.merges(1)).toEqual([{ sha: loadedSha, method: 'squash', merged: true }]);
});

test('a PR with conflicts shows Has conflicts with main. and Merge is disabled', async ({
  page,
}) => {
  github.describe(1, { mergeable: false });
  const { panel } = await openPanel(page);
  await expect(panel.getByText('Has conflicts with main.', { exact: true })).toBeVisible();
  await expect(button(panel, 'Merge')).toBeDisabled();
  expect(github.merges(1)).toEqual([]);
});

test('a failed check is shown first in plain words and Merge stays enabled', async ({ page }) => {
  github.setChecks(1, [
    { name: 'e2e-go', status: 'completed', conclusion: 'success' },
    { name: 'build', status: 'in_progress', conclusion: null },
    { name: 'test-guard', status: 'completed', conclusion: 'failure' },
  ]);
  const { panel } = await openPanel(page);
  const line = panel.getByText('1 check failed: test-guard', { exact: true });
  await expect(line).toBeVisible();
  const merge = button(panel, 'Merge');
  await expect(merge).toBeEnabled();
  const [above, below] = [await line.boundingBox(), await merge.boundingBox()];
  expect(above && below && above.y < below.y).toBe(true);

  await merge.click();
  await expect(button(panel, 'Confirm merge?')).toBeEnabled();
});

for (const state of ['merged', 'closed'] as const) {
  test(`a ${state} PR shows no Merge button`, async ({ page }) => {
    const t = await api(m, 'POST', `/api/bots/${bot.id}/threads`, {
      text: fixPrompt,
      repo_id: (await api(m, 'GET', '/api/repos'))[0].id,
    });
    await waitStatus(m, t.id, 'done', 30_000);
    if (state === 'merged') github.merge(1);
    else github.closePull(1);
    await page.goto(`${m.url}/bots/${bot.id}/threads/${t.id}`);
    await (await prInDetails(page)).click();
    const panel = aside(page);
    await expect(
      panel.getByText(state === 'merged' ? 'Merged' : 'Closed', {
        exact: true,
      }),
    ).toBeVisible();
    await expect(mergeButtons(panel)).toHaveCount(0);
    await expect(panel.getByText(/^Merges .* the commit shown here$/)).toHaveCount(0);
  });
}
