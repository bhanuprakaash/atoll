// A CI wake for a helper's PR stays with the helper (it runs in the helper's thread), and the boss's
// view of its helpers says why the helper runs: the line under the boss thread and the helper's row
// in Thread details name CI and the PR, not only the Bot and its step. The helper opens a PR and
// ends; CI fails; the wake plays testdata/claude/pr-wake-ci-hang.jsonl, which waits so the run stays
// live while the spec reads the boss's view.
import { mkdtempSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { expect, test } from '@playwright/test';
import { api, type Machine, showPane, startAtoll, threadPane } from './atoll';
import { githubEnv, type GitHubFake, seedRepo, startGitHub } from './github-fixture';
import {
  delegate,
  details,
  helperThreads,
  makeBoss,
  openDetails,
  startThread,
} from './helpers-fixture';

const brief = 'Fix the seeded bug and open a PR.';

let m: Machine;
let github: GitHubFake;
let root = '';
test.beforeEach(async () => {
  github = await startGitHub();
  root = mkdtempSync(join(tmpdir(), 'atoll-ui-helper-wake-'));
  m = await startAtoll({
    fakes: ['claude', 'gh'],
    secretStore: true,
    env: githubEnv(github),
  });
});
test.afterEach(() => {
  m?.stop();
  github?.close();
  if (root) rmSync(root, { recursive: true, force: true });
});

test('a CI failure on a helper PR runs the helper, and the boss line and the helper row say CI and the PR', async ({
  page,
}) => {
  test.setTimeout(180_000);
  const { repo, connection } = await seedRepo(m, github, root);
  const coder = { repo_access: [repo.id], connections: [connection.id] };
  const { poteto, bots } = await makeBoss(m, ['Tester'], { Tester: coder });

  await page.goto(m.url);
  const boss = await startThread(page, 'Poteto', delegate([['Tester', brief, 'octo-hello']]));
  const [helper] = await helperThreads(m, boss, 1);
  expect(helper.bot_id).toBe(bots.Tester.id);

  // The helper's first run opens and tracks PR #1 and ends.
  await expect
    .poll(
      async () => {
        const t = await api(m, 'GET', `/api/threads/${helper.id}`);
        return [t.run_status, (t.prs as { url: string }[]).map((p) => p.url)];
      },
      { timeout: 60_000 },
    )
    .toEqual(['done', [`${github.url}/octo/hello/pull/1`]]);
  const firstRun = (await api(m, 'GET', `/api/threads/${helper.id}`)).run_id as string;

  github.setCI(1, 'failed');

  // The wake runs in the helper's thread, as a new run.
  await expect
    .poll(
      async () => {
        const t = await api(m, 'GET', `/api/threads/${helper.id}`);
        return t.run_id !== firstRun && t.run_status === 'running';
      },
      { timeout: 60_000 },
    )
    .toBe(true);

  await showPane(page, m, poteto.id, 'Poteto');
  const line = threadPane(page, 'Poteto').locator('[data-line="helpers"]');
  await expect(line).toHaveCount(1);
  await expect(line).toContainText('Tester');
  await expect(line).toContainText(/\bCI\b/);
  await expect(line).toContainText(/#1\b/);

  await page.goto(`${m.url}/bots/${poteto.id}/threads/${boss}`);
  await openDetails(page);
  const row = details(page).getByRole('listitem').filter({ hasText: 'Tester' });
  await expect(row).toContainText(/\bCI\b/);
  await expect(row).toContainText(/#1\b/);
});
