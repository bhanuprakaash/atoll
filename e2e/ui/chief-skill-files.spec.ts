// Chief's edit_skill that changes a skill's files shows one change line naming files; its card
// lists the added and removed paths without their content, and Undo brings the old set back.
import { expect, test } from '@playwright/test';
import { api, type Machine, sidebarRow, startAtoll } from './atoll';

let m: Machine;
test.beforeEach(async () => {
  m = await startAtoll();
});
test.afterEach(() => m?.stop());

type File = { path: string; content: string; executable: boolean };

test('an edit_skill that swaps a file shows Edited <name>: files, lists the paths, and Undo restores them', async ({
  page,
}) => {
  const old: File[] = [
    { path: 'playbooks/query.md', content: '# query\nAsk first.\n', executable: false },
    { path: 'scripts/ro-sql', content: '#!/bin/sh\necho ro-sql-ok\n', executable: true },
  ];
  const skill = await api(m, 'POST', '/api/skills', {
    name: 'ro-sql',
    content: '---\nname: ro-sql\ndescription: Read-only SQL.\n---\nRun the script.\n',
    files: old,
  });
  const next: File[] = [
    old[1],
    { path: 'scripts/explain', content: '#!/bin/sh\necho plan-text\n', executable: true },
  ];

  await page.goto(m.url);
  await sidebarRow(page, 'Chief').click();
  await page
    .getByRole('button', { name: 'New thread', exact: true })
    .click();
  await page
    .getByRole('textbox', { name: 'Message' })
    .fill(
      `Call a platform tool. ${JSON.stringify({ tool: 'edit_skill', args: { id: skill.id, files: next } })}`,
    );
  await page.getByRole('button', { name: 'Send' }).click();

  const summary = 'Edited ro-sql: files';
  const change = page.getByRole('region', { name: 'Platform change' }).filter({
    has: page.getByRole('button', { name: summary }),
  });
  await expect(change).toHaveCount(1);
  const line = change.getByRole('button', { name: summary });
  await line.click();
  await expect(line).toHaveAttribute('aria-expanded', 'true');
  await expect(change.getByText('scripts/explain')).toBeVisible();
  await expect(change.getByText('playbooks/query.md')).toBeVisible();
  await expect(change.getByText('plan-text')).toHaveCount(0);
  await expect(change.getByText('Ask first.')).toHaveCount(0);

  await change.getByRole('button', { name: 'Undo', exact: true }).click();
  await expect(change.getByText('Undone', { exact: true })).toBeVisible();
  await expect
    .poll(async () => (await api(m, 'GET', `/api/skills/${skill.id}`)).files)
    .toEqual(old);
});
