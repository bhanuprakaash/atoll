// Chief has no repo access, so its settings and empty thread offer no repo. A thread whose Bot
// lost access to the thread's repo says so. Contract: prompt-contract.md.
import { execFileSync } from 'node:child_process';
import { mkdirSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';
import { expect, test } from '@playwright/test';
import { api, type Machine, newBotAPI, prompts, sidebarRow, startAtoll } from './atoll';

let m: Machine;
let repo: { id: string; name: string };
test.beforeAll(async () => {
  m = await startAtoll();
  const path = join(m.dir, 'seeded');
  mkdirSync(path);
  const git = (...args: string[]) =>
    execFileSync('git', args, {
      cwd: path,
      env: {
        ...process.env,
        GIT_AUTHOR_NAME: 'Fixture',
        GIT_AUTHOR_EMAIL: 'fixture@example.test',
        GIT_COMMITTER_NAME: 'Fixture',
        GIT_COMMITTER_EMAIL: 'fixture@example.test',
        GIT_CONFIG_NOSYSTEM: '1',
      },
    });
  git('init', '-b', 'main');
  writeFileSync(join(path, 'answer.txt'), '41\n');
  git('add', 'answer.txt');
  git('commit', '-m', 'seed');
  repo = await api(m, 'POST', '/api/repos', { name: 'seeded', path });
});
test.afterAll(() => m?.stop());

test('Chief has no Repo access control and its empty thread no Repo picker', async ({ page }) => {
  await newBotAPI(m, 'Mako');
  await page.goto(m.url);
  for (const [name, shown] of [
    ['Mako', 1],
    ['Chief', 0],
  ] as const) {
    await sidebarRow(page, name).click();
    await expect(page.getByRole('heading', { name: `What should ${name} work on?` })).toBeVisible();
    await expect(page.getByRole('combobox', { name: 'Repo' })).toHaveCount(shown);
    await page.getByRole('button', { name: 'Bot settings' }).click();
    const settings = page.getByRole('dialog', { name: 'Bot settings' });
    await expect(settings.getByRole('textbox', { name: 'Persona' })).toBeVisible();
    await expect(settings.getByRole('radiogroup', { name: 'Repo access' })).toHaveCount(shown);
    await settings.getByRole('button', { name: 'Close' }).click();
  }
});

test('a thread whose Bot lost access to its repo says so', async ({ page }) => {
  for (const [name, kept] of [
    ['Coral', true],
    ['Kelp', false],
  ] as const) {
    const bot = await newBotAPI(m, name, 'claude', { repo_access: [repo.id] });
    const thread = await api(m, 'POST', `/api/bots/${bot.id}/threads`, {
      text: prompts.pong,
      repo_id: repo.id,
    });
    await expect
      .poll(async () => (await api(m, 'GET', `/api/threads/${thread.id}`)).run_status)
      .toBe('done');
    if (!kept) await api(m, 'PUT', `/api/bots/${bot.id}`, { repo_access: [] });

    await page.goto(`${m.url}/bots/${bot.id}/threads/${thread.id}`);
    await expect(
      page.getByRole('log', { name: 'Messages' }).getByText('pong', { exact: true }),
    ).toBeVisible();
    const line = page.getByText(`no longer has access to ${repo.name}`);
    if (kept) await expect(line).toHaveCount(0);
    else await expect(line).toBeVisible();
  }
});
