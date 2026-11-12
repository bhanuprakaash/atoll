// Every failure a person can cause shows a designed notice: plain title, one line of what to do,
// the action when there is one, the raw text behind Details; never a Go chain, JSON or a gateway's
// HTML. Failures are real ones: an unreachable GitLab, a failing folder picker, a taken name,
// a Bot deleted elsewhere, a failed run, a server that goes away. Design: docs/design.md, App and
// UI (Errors). Contract: designed-errors-contract.md.
import { chmodSync, readFileSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';
import { expect, type Page, test } from '@playwright/test';
import { expectDesigned, notice } from './designed';
import {
  api,
  type Machine,
  newBotAPI,
  prompts,
  sendMessageAPI,
  sidebarRow,
  startAtoll,
} from './atoll';

let m: Machine;
test.beforeAll(async () => {
  m = await startAtoll();
});
test.afterAll(() => m?.stop());

const generic = {
  title: 'Something went wrong on our side.',
  next: 'Try again. If it keeps happening, copy the details into a bug report.',
  action: 'Try again',
};

async function openSettings(page: Page, section: string) {
  await page.goto(m.url);
  await page
    .getByRole('navigation', { name: 'Bots' })
    .getByRole('link', { name: /Settings/ })
    .click();
  await page.getByRole('link', { name: section }).click();
}

async function openAddRepo(page: Page) {
  await openSettings(page, 'Repos');
  const choose = page.getByRole('button', { name: 'Choose…' });
  if (!(await choose.isVisible())) await page.getByRole('button', { name: /Add repo/ }).click();
  return choose;
}

// failingPicker makes the folder picker exit with an error nobody planned for, and counts its
// runs in picker.count.
function failingPicker() {
  const count = join(m.bin, 'picker.count');
  writeFileSync(count, '');
  for (const picker of ['zenity', 'osascript']) {
    const path = join(m.bin, picker);
    writeFileSync(path, `#!/bin/sh\necho x >> '${count}'\necho 'exploded: private detail' >&2\nexit 3\n`);
    chmodSync(path, 0o755);
  }
  return () => readFileSync(count, 'utf8').split('\n').filter(Boolean).length;
}

test('a failure Reef has no sentence for reads as a generic notice; its chain never reaches the page', async ({
  page,
}) => {
  const runs = failingPicker();
  const choose = await openAddRepo(page);
  await choose.click();

  const alert = notice(page, generic.title);
  await expectDesigned(page, alert, {
    ...generic,
    raw: /exit status|exploded|private detail|internal error/,
    details: /internal · 500 · POST \/api\/repos\/pick-folder/,
  });

  await alert.getByRole('button', { name: 'Try again' }).click();
  await expect.poll(runs).toBe(2);
});

test('a gateway page or an empty body reads as the same generic notice, not as HTML', async ({
  page,
}) => {
  await page.route('**/api/repos/pick-folder', (route) =>
    route.fulfill({
      status: 502,
      contentType: 'text/html',
      body: '<html><head><title>502 Bad Gateway</title></head><body><h1>502 Bad Gateway</h1>nginx</body></html>',
    }),
  );
  const choose = await openAddRepo(page);
  await choose.click();
  await expectDesigned(page, notice(page, generic.title), {
    ...generic,
    raw: /Bad Gateway|nginx|<html|<h1>/i,
    details: /internal · 502 · POST \/api\/repos\/pick-folder/,
  });
});

test('an unreachable GitLab names the host and offers Try again, not the dial error', async ({
  page,
}) => {
  await openSettings(page, 'Connections');
  await page.getByRole('textbox', { name: 'GitLab instance URL' }).fill('http://127.0.0.1:1');
  await page.getByRole('button', { name: 'Sign in to GitLab' }).click();

  const alert = notice(page, "Reef couldn't reach 127.0.0.1:1.");
  await expectDesigned(page, alert, {
    title: "Reef couldn't reach 127.0.0.1:1.",
    next: 'Check the address and your connection, then try again.',
    action: 'Try again',
    raw: /dial tcp|connection refused|OAuth request|authorize_device/,
    details: /unreachable · 502 · POST \/api\/connections\/gitlab\/start/,
  });
  await alert.getByRole('button', { name: 'Try again' }).click();
  await expect(notice(page, "Reef couldn't reach 127.0.0.1:1.")).toHaveCount(1);
});

test('a name that is taken says so and asks for another', async ({ page }) => {
  await api(m, 'POST', '/api/mcp-servers', { name: 'taken-docs', command: 'echo' });
  await openSettings(page, 'MCP servers');
  await page.getByRole('button', { name: 'Add local' }).click();
  const dialog = page.getByRole('dialog', { name: 'Add local' });
  await dialog.getByRole('textbox', { name: 'Name' }).fill('taken-docs');
  await dialog.getByRole('textbox', { name: 'Command' }).fill('echo');
  await dialog.getByRole('button', { name: 'Add', exact: true }).click();

  await expectDesigned(page, notice(dialog, 'Something with that name already exists.'), {
    title: 'Something with that name already exists.',
    next: 'Pick another name.',
    raw: /MCP server name|: already exists|^already exists$/m,
    details: /exists · 409 · POST \/api\/mcp-servers/,
  });
});

test('deleting a Bot that is already gone says it no longer exists', async ({ page }) => {
  const bot = await newBotAPI(m, 'Ghost');
  await page.goto(m.url);
  await sidebarRow(page, 'Ghost').click();
  await page.getByRole('button', { name: 'Bot settings' }).click();
  await page
    .getByRole('dialog', { name: 'Bot settings' })
    .getByRole('button', { name: 'Delete Bot' })
    .click();
  await api(m, 'DELETE', `/api/bots/${bot.id}`);
  const confirm = page.getByRole('dialog', { name: 'Delete Bot' });
  await confirm.getByRole('button', { name: 'Delete', exact: true }).click();

  await expectDesigned(page, notice(confirm, 'That no longer exists.'), {
    title: 'That no longer exists.',
    next: 'It was deleted or moved. Go back and pick another.',
    raw: new RegExp(`${bot.id}|: not found`),
    details: /not_found · 404 · DELETE \/api\/bots\/\S+/,
  });
});

test('a run that fails shows a plain line with Retry; the provider text is behind Details', async ({
  page,
}) => {
  const bot = await newBotAPI(m, 'Stumble');
  await page.goto(m.url);
  await sidebarRow(page, 'Stumble').click();
  await page.getByRole('textbox', { name: 'Message' }).fill(prompts.none);
  await page.getByRole('button', { name: 'Send' }).click();

  const log = page.getByRole('log', { name: 'Messages' });
  await expectDesigned(page, notice(log, /^Didn't run: \S/), {
    title: /^Didn't run: \S/,
    action: 'Retry',
    raw: /no recording for prompt/,
    details: /no recording for prompt/,
  });
  expect(bot.id).toBeTruthy();
});

test('a Bot on a provider that is not installed says so and points to Settings', async ({
  page,
}) => {
  await newBotAPI(m, 'Offprov', 'codex');
  await page.goto(m.url);
  await sidebarRow(page, 'Offprov').click();
  await page.getByRole('textbox', { name: 'Message' }).fill('Hello there.');
  await page.getByRole('button', { name: 'Send' }).click();

  const log = page.getByRole('log', { name: 'Messages' });
  await expectDesigned(page, notice(log, /isn't installed or isn't signed in\./), {
    title: "Didn't run: Codex isn't installed or isn't signed in.",
    action: 'Open settings',
    raw: /launch codex|codex is not installed/,
    details: /launch codex: codex is not installed/,
  });
});

test('losing the server shows one Reconnecting notice, not an error per request', async ({
  page,
}) => {
  const own = await startAtoll();
  try {
    await page.goto(`${own.url}/settings`);
    const root = page.getByRole('textbox', { name: 'Worktree folder' });
    await expect(root).toBeVisible();
    own.kill();

    await expect(page.getByRole('status').filter({ hasText: 'Reconnecting' })).toHaveCount(1);
    // A request made meanwhile fails quietly: the notice already says why.
    await root.fill('/tmp/elsewhere');
    await root.blur();
    await expect(page.getByRole('status').filter({ hasText: 'Reconnecting' })).toHaveCount(1);
    await expect(page.getByRole('alert')).toHaveCount(0);
    expect(await page.locator('body').innerText()).not.toMatch(/Failed to fetch|NetworkError|TypeError/i);

    await own.restart();
    await expect(page.getByText('Reconnecting')).toHaveCount(0, { timeout: 30_000 });
  } finally {
    own.stop();
  }
});

test('losing the server with a thread open shows one Reconnecting notice', async ({ page }) => {
  const own = await startAtoll();
  try {
    const bot = await newBotAPI(own, 'Tide');
    const thread = await api(own, 'POST', `/api/bots/${bot.id}/threads`, {});
    await sendMessageAPI(own, thread.id, prompts.pong);
    await page.goto(`${own.url}/bots/${bot.id}/threads/${thread.id}`);
    await expect(
      page.getByRole('log', { name: 'Messages' }).getByText('pong', { exact: true }),
    ).toBeVisible();
    await page.getByRole('textbox', { name: /^Message/ }).fill('still there?');
    const send = page.getByRole('button', { name: 'Send' });
    await expect(send).toBeEnabled();
    own.kill();

    // Both signals of a lost server: the app's notice and the thread's own stream.
    await expect(page.getByRole('status').filter({ hasText: 'Reconnecting' })).toHaveCount(1);
    await expect(send).toBeDisabled();
    await expect(page.getByText('Reconnecting')).toHaveCount(1);
  } finally {
    own.stop();
  }
});

test('a callback page has the same shape: a title, what to do, and a closed Details', async ({
  page,
}) => {
  await page.goto(`${m.url}/oauth/callback?state=nope`);
  await expect(page.getByText('This sign-in link is not valid or was already used.')).toBeVisible();
  await expect(page.getByText(/close this window/i)).toBeVisible();
  const details = page.locator('details');
  await expect(details).toHaveCount(1);
  await expect(details).not.toHaveAttribute('open', /.*/);
  await expect(details.locator('summary')).toHaveText('Details');
  expect(await page.locator('body').innerText()).not.toMatch(/\{\s*"error"\s*:/);
  await details.locator('summary').click();
  await expect(details).toContainText(/invalid|400/);
});
