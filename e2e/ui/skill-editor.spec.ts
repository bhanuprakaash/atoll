// Settings > Skills edits every file of a skill, not only SKILL.md, and re-imports it from its
// source folder. Contract: e2e/skill-editor-contract.md. Each test runs its own machine (temporary
// HOME and data), seeds poteto-mode through Chief's import, and checks what the UI did through the
// API and through the folder a Bot run was given.
import { expect, type Locator, type Page, test } from '@playwright/test';
import { api, type Machine, startAtoll } from './atoll';
import {
  importPoteto,
  libraryFiles,
  placed,
  potetoFiles,
  potetoPaths,
  runScript,
  type Skill,
  withFile,
  withoutFile,
  writeFolder,
} from './skill-fixture';

let m: Machine;
test.beforeEach(async () => {
  m = await startAtoll();
});
test.afterEach(() => m?.stop());

const escape = (s: string) => s.replace(/[.*+?^${}()|[\]\\/]/g, '\\$&');

// openSkill opens a skill's panel from the Skills list. The panel covers the right of the list, so
// the click lands on the row's left end.
async function openSkill(page: Page, name: string) {
  await page.goto(`${m.url}/settings/skills`);
  await page
    .getByRole('button', { name: new RegExp(escape(name)) })
    .click({ position: { x: 24, y: 12 } });
  const panel = page.getByRole('dialog', { name: `Edit ${name}` });
  await expect(panel).toBeVisible();
  return panel;
}

const fileList = (panel: Locator) => panel.getByRole('list', { name: 'Files' });
// A file's row is a button named by its path; a script's marker text may follow the path.
const fileRow = (panel: Locator, path: string) =>
  fileList(panel).getByRole('button', {
    name: new RegExp(`^${escape(path)}( script)?$`),
  });
const editor = (panel: Locator) => panel.getByRole('textbox', { name: 'Content', exact: true });
const scriptBox = (panel: Locator) => panel.getByRole('checkbox', { name: 'Script', exact: true });
const saveButton = (panel: Locator) => panel.getByRole('button', { name: /^Save/ });
const discardDialog = (page: Page) =>
  page.getByRole('dialog', { name: 'Discard unsaved changes?' });

async function save(page: Page, panel: Locator) {
  await Promise.all([
    page.waitForResponse((r) => r.request().method() === 'PUT' && r.url().includes('/api/skills/')),
    saveButton(panel).click(),
  ]);
}

// addFile types a path into the File path input, opening it first unless a refused path left it
// open, and presses Add.
async function addFile(panel: Locator, path: string) {
  const input = panel.getByRole('textbox', { name: 'File path' });
  if (!(await input.isVisible())) await panel.getByRole('button', { name: '+ File' }).click();
  await input.fill(path);
  await panel.getByRole('button', { name: 'Add', exact: true }).click();
}

const getSkill = async (id: string): Promise<Skill> => api(m, 'GET', `/api/skills/${id}`);

const otherSkill = () =>
  api(m, 'POST', '/api/skills', {
    name: 'other-skill',
    content: '---\nname: other-skill\ndescription: Another skill.\n---\nBody.\n',
  });

test('the panel lists SKILL.md first, then the 22 files by path, scripts marked', async ({
  page,
}) => {
  await importPoteto(m);
  const panel = await openSkill(page, 'poteto-mode');

  const rows = fileList(panel).getByRole('listitem');
  await expect(rows).toHaveCount(23);
  await expect(rows.nth(0).getByRole('button')).toHaveAccessibleName('SKILL.md');
  await expect(rows.nth(0).getByText('script', { exact: true })).toHaveCount(0);
  for (const [i, path] of potetoPaths.entries()) {
    const row = rows.nth(i + 1);
    const isScript = path.startsWith('scripts/');
    await expect(row.getByRole('button')).toHaveAccessibleName(
      new RegExp(`^${escape(path)}${isScript ? '( script)?' : ''}$`),
    );
    await expect(row.getByText('script', { exact: true })).toHaveCount(isScript ? 1 : 0);
  }
});

test('an edited file is kept after Save and reload, and the next run gets it', async ({ page }) => {
  const skill = await importPoteto(m);
  const panel = await openSkill(page, 'poteto-mode');
  const before = libraryFiles(potetoFiles()).find((f) => f.path === 'playbooks/ship.md')!;

  await fileRow(panel, 'playbooks/ship.md').click();
  await expect(editor(panel)).toHaveValue(before.content);
  const edited = '# ship\nEdited in the Reef editor.\n';
  await editor(panel).fill(edited);
  // One Save sends the whole skill: nothing is stored before it.
  expect((await getSkill(skill.id)).files.find((f) => f.path === 'playbooks/ship.md')).toEqual(
    before,
  );
  await save(page, panel);
  expect((await getSkill(skill.id)).files.find((f) => f.path === 'playbooks/ship.md')).toEqual({
    ...before,
    content: edited,
  });

  // Saved: leaving asks nothing.
  await panel.getByRole('button', { name: 'Close', exact: true }).click();
  await expect(discardDialog(page)).toHaveCount(0);
  await expect(panel).toHaveCount(0);

  await page.reload();
  const again = await openSkill(page, 'poteto-mode');
  await fileRow(again, 'playbooks/ship.md').click();
  await expect(editor(again)).toHaveValue(edited);

  const run = await runScript(m, skill, 'scripts/check');
  expect(run.status).toBe('done');
  const got = placed(m);
  expect(got.files.find((f) => f.path === 'playbooks/ship.md')).toMatchObject({
    text: edited,
    mode: 0o444,
  });
  expect(got.files).toHaveLength(23);
});

test('a new script turns Script on by itself; after Save a Bot run prints hi', async ({ page }) => {
  const skill = await importPoteto(m);
  const panel = await openSkill(page, 'poteto-mode');

  await addFile(panel, 'scripts/hello');
  await expect(fileRow(panel, 'scripts/hello')).toBeVisible();
  await expect(editor(panel)).toHaveValue('');
  await expect(scriptBox(panel)).not.toBeChecked();
  await editor(panel).click();
  await editor(panel).pressSequentially('#!/bin/sh');
  await expect(scriptBox(panel)).toBeChecked();
  await editor(panel).press('Enter');
  await editor(panel).pressSequentially('echo hi');
  await expect(scriptBox(panel)).toBeChecked();
  await expect(fileList(panel).getByRole('listitem')).toHaveCount(24);
  await expect(
    fileList(panel)
      .getByRole('listitem')
      .filter({ hasText: 'scripts/hello' })
      .getByText('script', { exact: true }),
  ).toHaveCount(1);

  await save(page, panel);
  expect((await getSkill(skill.id)).files.find((f) => f.path === 'scripts/hello')).toEqual({
    path: 'scripts/hello',
    content: '#!/bin/sh\necho hi',
    executable: true,
  });

  const run = await runScript(m, skill, 'scripts/hello');
  const got = placed(m);
  expect(run).toMatchObject({
    status: 'done',
    reply: `Ran ${got.dir}/scripts/hello: hi`,
  });
  expect(got.dir.startsWith(`${m.data}/providers/claude/runs/${run.id}/`)).toBe(true);
  expect(got.files.find((f) => f.path === 'scripts/hello')).toMatchObject({
    mode: 0o555,
  });
});

test('Script follows #! only until the user toggles it', async ({ page }) => {
  const skill = await importPoteto(m);
  const panel = await openSkill(page, 'poteto-mode');

  await addFile(panel, 'scripts/manual');
  await editor(panel).click();
  await editor(panel).pressSequentially('#!/bin/sh');
  await expect(scriptBox(panel)).toBeChecked();
  await scriptBox(panel).uncheck();
  await editor(panel).press('Enter');
  await editor(panel).pressSequentially('echo manual');
  await expect(editor(panel)).toHaveValue('#!/bin/sh\necho manual');
  await expect(scriptBox(panel)).not.toBeChecked();

  await save(page, panel);
  expect((await getSkill(skill.id)).files.find((f) => f.path === 'scripts/manual')).toMatchObject({
    executable: false,
  });
});

test('a deleted file is gone from the list and from the next run after Save', async ({ page }) => {
  const skill = await importPoteto(m);
  const panel = await openSkill(page, 'poteto-mode');

  await fileRow(panel, 'playbooks/audit.md').click();
  await panel.getByRole('button', { name: 'Delete file' }).click();
  await expect(fileRow(panel, 'playbooks/audit.md')).toHaveCount(0);
  await expect(fileList(panel).getByRole('listitem')).toHaveCount(22);
  // Nothing is stored before Save.
  expect((await getSkill(skill.id)).files).toHaveLength(22);

  await save(page, panel);
  const stored = await getSkill(skill.id);
  expect(stored.files.map((f) => f.path)).toEqual(
    potetoPaths.filter((p) => p !== 'playbooks/audit.md'),
  );

  await page.reload();
  const again = await openSkill(page, 'poteto-mode');
  await expect(fileList(again).getByRole('listitem')).toHaveCount(22);
  await expect(fileRow(again, 'playbooks/audit.md')).toHaveCount(0);

  expect((await runScript(m, skill, 'scripts/check')).status).toBe('done');
  const paths = placed(m).files.map((f) => f.path);
  expect(paths).toHaveLength(22);
  expect(paths).not.toContain('playbooks/audit.md');
});

test('a path with .. or a leading slash is refused inline and nothing is saved', async ({
  page,
}) => {
  const skill = await importPoteto(m);
  const before = await getSkill(skill.id);
  const writes: string[] = [];
  page.on('request', (r) => {
    if (r.method() !== 'GET' && r.url().includes('/api/skills'))
      writes.push(`${r.method()} ${r.url()}`);
  });
  const panel = await openSkill(page, 'poteto-mode');

  for (const bad of ['../x', '/x']) {
    await addFile(panel, bad);
    await expect(panel.getByRole('alert')).toBeVisible();
    await expect(fileList(panel).getByRole('listitem')).toHaveCount(23);
    await expect(fileRow(panel, bad)).toHaveCount(0);
  }

  // A valid path still works after a refusal.
  await addFile(panel, 'notes.md');
  await expect(fileRow(panel, 'notes.md')).toBeVisible();

  expect(writes).toEqual([]);
  expect(await getSkill(skill.id)).toEqual(before);
});

test('refused paths leave nothing unsaved', async ({ page }) => {
  await importPoteto(m);
  const panel = await openSkill(page, 'poteto-mode');
  await addFile(panel, '../x');
  await expect(panel.getByRole('alert')).toBeVisible();
  await panel.getByRole('button', { name: 'Close', exact: true }).click();
  await expect(discardDialog(page)).toHaveCount(0);
  await expect(panel).toHaveCount(0);
});

test.describe('leaving with unsaved changes asks first', () => {
  // An unsaved edit of playbooks/ship.md, then leave by leave(); Keep editing stays with the edit,
  // Discard drops it.
  async function edit(page: Page) {
    const panel = await openSkill(page, 'poteto-mode');
    await fileRow(panel, 'playbooks/ship.md').click();
    await editor(panel).fill('# ship\nUnsaved.\n');
    return panel;
  }

  async function original(skill: Skill, page: Page) {
    expect(
      (await getSkill(skill.id)).files.find((f) => f.path === 'playbooks/ship.md')?.content,
    ).toBe(libraryFiles(potetoFiles()).find((f) => f.path === 'playbooks/ship.md')!.content);
    const panel = await openSkill(page, 'poteto-mode');
    await fileRow(panel, 'playbooks/ship.md').click();
    await expect(editor(panel)).toHaveValue(
      libraryFiles(potetoFiles()).find((f) => f.path === 'playbooks/ship.md')!.content,
    );
  }

  test('closing the panel', async ({ page }) => {
    const skill = await importPoteto(m);
    const panel = await edit(page);
    const close = panel.getByRole('button', { name: 'Close', exact: true });

    await close.click();
    const ask = discardDialog(page);
    await expect(ask).toBeVisible();
    await ask.getByRole('button', { name: 'Keep editing' }).click();
    await expect(ask).toHaveCount(0);
    await expect(panel).toBeVisible();
    await expect(editor(panel)).toHaveValue('# ship\nUnsaved.\n');

    await close.click();
    await ask.getByRole('button', { name: 'Discard', exact: true }).click();
    await expect(panel).toHaveCount(0);
    await original(skill, page);
  });

  test('choosing another skill', async ({ page }) => {
    const skill = await importPoteto(m);
    await otherSkill();
    const panel = await edit(page);
    const other = page.getByRole('button', { name: /other-skill/ });

    await other.click({ position: { x: 24, y: 12 } });
    const ask = discardDialog(page);
    await expect(ask).toBeVisible();
    await ask.getByRole('button', { name: 'Keep editing' }).click();
    await expect(ask).toHaveCount(0);
    await expect(panel).toBeVisible();
    await expect(editor(panel)).toHaveValue('# ship\nUnsaved.\n');

    await other.click({ position: { x: 24, y: 12 } });
    await ask.getByRole('button', { name: 'Discard', exact: true }).click();
    await expect(page.getByRole('dialog', { name: 'Edit other-skill' })).toBeVisible();
    await expect(panel).toHaveCount(0);
    await original(skill, page);
  });

  test('going to another Settings section', async ({ page }) => {
    const skill = await importPoteto(m);
    const panel = await edit(page);
    const general = page.getByRole('link', { name: 'General' });

    await general.click();
    const ask = discardDialog(page);
    await expect(ask).toBeVisible();
    await ask.getByRole('button', { name: 'Keep editing' }).click();
    await expect(ask).toHaveCount(0);
    await expect(page).toHaveURL(/\/settings\/skills$/);
    await expect(editor(panel)).toHaveValue('# ship\nUnsaved.\n');

    await general.click();
    await ask.getByRole('button', { name: 'Discard', exact: true }).click();
    await expect(page).toHaveURL(/\/settings\/general$/);
    await original(skill, page);
  });

  test('a new skill with typed text', async ({ page }) => {
    await page.goto(`${m.url}/settings/skills`);
    await page.getByRole('button', { name: 'New skill' }).click();
    const panel = page.getByRole('dialog', { name: 'New skill' });
    await panel.getByRole('textbox', { name: 'Name', exact: true }).fill('half-done');
    await panel.getByRole('button', { name: 'Close', exact: true }).click();
    await expect(discardDialog(page)).toBeVisible();
    await discardDialog(page).getByRole('button', { name: 'Discard', exact: true }).click();
    await expect(panel).toHaveCount(0);
    expect(await api(m, 'GET', '/api/skills')).toEqual([]);
  });
});

test('a new skill with files is created only on Create', async ({ page }) => {
  await page.goto(`${m.url}/settings/skills`);
  await page.getByRole('button', { name: 'New skill' }).click();
  const panel = page.getByRole('dialog', { name: 'New skill' });
  await panel.getByRole('textbox', { name: 'Name', exact: true }).fill('fresh-skill');

  await addFile(panel, 'playbooks/first.md');
  await editor(panel).fill('# first\n');
  await addFile(panel, 'scripts/go');
  await editor(panel).click();
  await editor(panel).pressSequentially('#!/bin/sh');
  await expect(scriptBox(panel)).toBeChecked();
  await expect(fileList(panel).getByRole('listitem')).toHaveCount(3);
  expect(await api(m, 'GET', '/api/skills')).toEqual([]);

  await Promise.all([
    page.waitForResponse((r) => r.request().method() === 'POST' && r.url().endsWith('/api/skills')),
    panel.getByRole('button', { name: /^Create/ }).click(),
  ]);
  const skills: Skill[] = await api(m, 'GET', '/api/skills');
  expect(skills).toHaveLength(1);
  expect(skills[0]).toMatchObject({ name: 'fresh-skill', source: '' });
  expect(skills[0].files).toEqual([
    { path: 'playbooks/first.md', content: '# first\n', executable: false },
    { path: 'scripts/go', content: '#!/bin/sh', executable: true },
  ]);
  await expect(page.getByRole('dialog', { name: 'Edit fresh-skill' })).toBeVisible();
});

test.describe('Re-import', () => {
  const reefEdit = '# ship\nEdited in Reef.\n';
  // The folder moved on: one file added, one changed, one removed.
  const source = () =>
    withoutFile(
      withFile(
        withFile(potetoFiles(), 'playbooks/review.md', '# review\nNew.\n'),
        'playbooks/new.md',
        '# new\n',
      ),
      'playbooks/debug.md',
    );
  const list = (panel: Locator, name: string) =>
    panel.getByRole('list', { name, exact: true }).getByRole('listitem');
  const reimportRequest = (page: Page, id: string, replace: boolean) =>
    page.waitForRequest(
      (r) =>
        r.method() === 'POST' &&
        r.url().endsWith(`/api/skills/${id}/reimport`) &&
        r.postDataJSON()?.replace === replace,
    );

  test('after a Reef edit it lists the changes and warns; Cancel keeps the edit, Confirm replaces it', async ({
    page,
  }) => {
    const skill = await importPoteto(m);
    const panel = await openSkill(page, 'poteto-mode');
    await fileRow(panel, 'playbooks/ship.md').click();
    await editor(panel).fill(reefEdit);
    await save(page, panel);
    const edited = await getSkill(skill.id);
    writeFolder(m, source());

    const first = reimportRequest(page, skill.id, false);
    await panel.getByRole('button', { name: 'Re-import', exact: true }).click();
    expect((await first).postDataJSON()).toEqual({ replace: false });
    await expect(list(panel, 'Added')).toHaveText(['playbooks/new.md']);
    // The file edited in Reef would go back to the folder's text, so it is changed too.
    await expect(list(panel, 'Changed')).toHaveText(['playbooks/review.md', 'playbooks/ship.md']);
    await expect(list(panel, 'Removed')).toHaveText(['playbooks/debug.md']);
    await expect(panel.getByText(/overwritten/)).toBeVisible();

    await panel.getByRole('button', { name: 'Cancel', exact: true }).click();
    await expect(panel.getByText(/overwritten/)).toHaveCount(0);
    await expect(panel.getByRole('button', { name: 'Confirm', exact: true })).toHaveCount(0);
    expect(await getSkill(skill.id)).toEqual(edited);
    await expect(fileRow(panel, 'playbooks/new.md')).toHaveCount(0);
    await fileRow(panel, 'playbooks/ship.md').click();
    await expect(editor(panel)).toHaveValue(reefEdit);

    await panel.getByRole('button', { name: 'Re-import', exact: true }).click();
    await expect(panel.getByText(/overwritten/)).toBeVisible();
    const confirmed = reimportRequest(page, skill.id, true);
    await panel.getByRole('button', { name: 'Confirm', exact: true }).click();
    await confirmed;

    await expect(fileRow(panel, 'playbooks/new.md')).toBeVisible();
    await expect(fileRow(panel, 'playbooks/debug.md')).toHaveCount(0);
    await expect(fileList(panel).getByRole('listitem')).toHaveCount(23);
    await expect(list(panel, 'Added')).toHaveText(['playbooks/new.md']);
    await fileRow(panel, 'playbooks/ship.md').click();
    await expect(editor(panel)).toHaveValue(
      libraryFiles(potetoFiles()).find((f) => f.path === 'playbooks/ship.md')!.content,
    );
    const now = await getSkill(skill.id);
    expect(now.files).toEqual(libraryFiles(source()));

    // Nothing differs any more.
    await panel.getByRole('button', { name: 'Re-import', exact: true }).click();
    await expect(panel.getByText('No changes')).toBeVisible();
    await expect(panel.getByRole('button', { name: 'Confirm', exact: true })).toHaveCount(0);
    expect(await getSkill(skill.id)).toEqual(now);
  });

  test('with no Reef edit it replaces at once and shows what changed', async ({ page }) => {
    const skill = await importPoteto(m);
    const panel = await openSkill(page, 'poteto-mode');
    await expect(panel.getByRole('button', { name: 'Re-import', exact: true })).toBeVisible();

    await panel.getByRole('button', { name: 'Re-import', exact: true }).click();
    await expect(panel.getByText('No changes')).toBeVisible();

    writeFolder(m, source());
    const asked = reimportRequest(page, skill.id, false);
    await panel.getByRole('button', { name: 'Re-import', exact: true }).click();
    await asked;
    await expect(list(panel, 'Added')).toHaveText(['playbooks/new.md']);
    await expect(list(panel, 'Changed')).toHaveText(['playbooks/review.md']);
    await expect(list(panel, 'Removed')).toHaveText(['playbooks/debug.md']);
    await expect(panel.getByRole('button', { name: 'Confirm', exact: true })).toHaveCount(0);
    await expect(panel.getByText(/overwritten/)).toHaveCount(0);
    await expect(fileRow(panel, 'playbooks/new.md')).toBeVisible();
    await expect(fileRow(panel, 'playbooks/debug.md')).toHaveCount(0);
    expect((await getSkill(skill.id)).files).toEqual(libraryFiles(source()));
  });

  test('a skill with no source has no Re-import', async ({ page }) => {
    await otherSkill();
    const panel = await openSkill(page, 'other-skill');
    await expect(panel.getByRole('button', { name: 'Re-import', exact: true })).toHaveCount(0);
  });
});
