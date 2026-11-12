// The "Waiting on you" card: what waits on the person (a decision, a blocker, merge-ready PRs)
// shows in the thread, the sidebar, the Bot row, a folded group and the island, once, and clears in
// all of them together. Contract: e2e/waiting-card-contract.md.
import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import { DatabaseSync } from 'node:sqlite';
import { expect, type Page, test } from '@playwright/test';
import {
  api,
  type Machine,
  newBotAPI,
  openThreadAPI,
  sendMessageAPI,
  sidebarRow,
  startAtoll,
  threadTitle,
  threadTree,
} from './atoll';

test.use({ viewport: { width: 1280, height: 800 } });

let m: Machine;
test.beforeEach(async () => {
  m = await startAtoll();
});
test.afterEach(() => m?.stop());

// Each prompt plays its recording in e2e/testdata/claude (waiting-*.jsonl).
const say = {
  ask: 'Pick the deploy path.',
  blocked: 'Open the dashboard export.',
  ready: 'Open the PR and report it ready.',
  track: 'Track the fix PR.',
  two: 'Report both PRs ready and ask which goes first.',
  redNote: 'Report the test PR ready.',
  redBare: 'Report the other PR ready.',
  lead: 'Hand the changelog to Tester and ask me which format.',
};
const question = 'How should the deploy go out?';
const options = ['Behind a flag', 'All at once', 'Wait a week'];
const blocker = 'GitHub sign-in expired: reconnect it in Settings.';
const pr = (n: number) => `https://github.com/acme/app/pull/${n}`;
const sha = (c: string) => c.repeat(40);

async function waitStatus(threadID: string, status: string) {
  await expect
    .poll(async () => (await api(m, 'GET', `/api/threads/${threadID}`)).run_status, {
      timeout: 15_000,
    })
    .toBe(status);
}

// thread starts a thread with its first message and waits for the run to end.
async function thread(bot: { id: string }, text: string) {
  const t = await openThreadAPI(m, bot.id, text);
  await waitStatus(t.id, 'done');
  return t;
}

const open = (page: Page, bot: { id: string }, t: { id: string }) =>
  page.goto(`${m.url}/bots/${bot.id}/threads/${t.id}`);
const log = (page: Page) => page.getByRole('log', { name: 'Messages' });
const card = (page: Page) => page.getByRole('region', { name: 'Waiting on you' });
const composer = (page: Page) => page.getByRole('textbox', { name: 'Message' });
const header = (page: Page) => page.locator('header');
const ready = (page: Page) => card(page).getByRole('list', { name: 'Ready to merge' });

// The database is the one the server runs on: what only the PR watcher writes (checks, head,
// state) is set here, as the watcher would.
function write(sql: string, ...args: (string | number)[]) {
  const db = new DatabaseSync(join(m.data, 'atoll.db'));
  try {
    db.prepare(sql).run(...args);
  } finally {
    db.close();
  }
}
const checks = (url: string, head: string, passed: number, failed: number, names: string[] = []) =>
  write(
    'UPDATE threads SET mr_ci = ? WHERE mr_url = ?',
    JSON.stringify({ head, passed, failed, pending: 0, names }),
    url,
  );
const setState = (url: string, state: string) =>
  write('UPDATE threads SET mr_state = ? WHERE mr_url = ?', state, url);
const readyFor = (threadID: string, minutes: number) =>
  write(
    'UPDATE runs SET finished_at = ? WHERE thread_id = ?',
    (Date.now() - minutes * 60_000) * 1000,
    threadID,
  );
const messageCount = async (threadID: string) =>
  (await api(m, 'GET', `/api/threads/${threadID}/messages`)).length as number;

test('a decision from finish_run is a Waiting on you card: the options as buttons, a free-text composer', async ({
  page,
}) => {
  const pilot = await newBotAPI(m, 'Pilot');
  const t = await thread(pilot, say.ask);
  await open(page, pilot, t);

  await expect(card(page)).toContainText(question);
  for (const option of options)
    await expect(card(page).getByRole('button', { name: option, exact: true })).toBeVisible();
  await expect(card(page).getByRole('button')).toHaveCount(options.length);
  await expect(card(page)).toContainText('or reply below');
  await expect(composer(page)).toBeEnabled();
  await expect(header(page)).toContainText('1 waiting on you');

  // The reply that raised it stays a message; the card is not in the log, and a question is not
  // blocked, so no Blocked line.
  await expect(log(page)).toContainText('I need your call on the deploy');
  await expect(log(page).getByRole('button', { name: 'All at once' })).toHaveCount(0);
  await expect(log(page)).not.toContainText(question);
  await expect(log(page)).not.toContainText('Blocked');
  const run = await api(m, 'GET', `/api/runs/${t.run.id}`);
  expect(run.report.ask).toEqual({ question, options });
});

test('finish_run refuses an ask with one option, five options or no question', async () => {
  const pilot = await newBotAPI(m, 'Pilot');
  const bad = [
    ['one option', { question: 'Which one?', options: ['Only'] }, /option/i],
    ['five options', { question: 'Which one?', options: ['a', 'b', 'c', 'd', 'e'] }, /option/i],
    ['no question', { question: '', options: ['a', 'b'] }, /question/i],
  ] as const;
  const calls = bad.map(([, ask]) => ({
    server: 'atoll',
    tool: 'finish_run',
    args: { done: 'd', tried: 't', blocked: '', next: 'n', ask },
  }));
  const t = await openThreadAPI(m, pilot.id, `Use MCP tools. ${JSON.stringify(calls)}`);
  await waitStatus(t.id, 'done');
  const lines: string[] = (await api(m, 'GET', `/api/runs/${t.run.id}`)).reply.split('\n');
  expect(lines).toHaveLength(bad.length);
  bad.forEach(([name, , word], i) => {
    expect(lines[i], name).toMatch(/^atoll\/finish_run: /);
    expect(lines[i], name).not.toMatch(/saved/i);
    expect(lines[i], name).toMatch(word);
  });
});

test('the sidebar row, the Bot row, a folded group and the island show Waiting on you until it is answered', async ({
  page,
}) => {
  const nav = page.getByRole('navigation', { name: 'Bots' });
  const pilot = await newBotAPI(m, 'Pilot');
  const away = await newBotAPI(m, 'Elsewhere');
  await page.goto(`${m.url}/bots/${away.id}`);
  await nav.getByRole('button', { name: 'New', exact: true }).click();
  await page.getByRole('menuitem', { name: 'New group' }).click();
  const field = nav.getByRole('textbox', { name: 'Group name' });
  await field.fill('Ops');
  await field.press('Enter');
  await sidebarRow(page, 'Pilot').click({ button: 'right' });
  await page.getByRole('menuitem', { name: 'Move to group' }).click();
  await page.getByRole('menuitem', { name: 'Ops', exact: true }).click();
  const ops = nav.getByRole('group', { name: 'Ops', exact: true });
  await ops.getByRole('heading').hover();
  await ops.getByRole('button', { name: 'Ops', exact: true }).click();
  await expect(ops.getByRole('button', { name: 'Ops', exact: true })).toHaveAttribute(
    'aria-expanded',
    'false',
  );
  await expect(ops).not.toHaveAttribute('data-marker', /./);

  const t = await thread(pilot, say.ask);
  await expect(ops).toHaveAttribute('data-marker', 'needs-you');
  await ops.getByRole('button', { name: 'Ops', exact: true }).click();
  await expect(sidebarRow(page, 'Pilot')).toContainText('Waiting on you');
  const row = threadTree(page, 'Pilot')
    .getByRole('listitem')
    .filter({
      hasText: threadTitle(say.ask),
    });
  await expect(row).toHaveAttribute('data-marker', 'waiting');
  await expect(row).toContainText('Waiting on you');

  // The island counts it and lists it; it neither drops open nor widens.
  const island = header(page).getByRole('button', {
    name: 'Notifications, 1 waiting',
    exact: true,
  });
  await expect(island).toBeVisible();
  await expect(header(page).getByRole('button', { name: /^(Allow|Deny)$/ })).toHaveCount(0);
  await island.click();
  const list = page.getByRole('dialog', { name: 'Notifications' });
  const waiting = list.getByRole('list', { name: 'Waiting on you' }).getByRole('listitem');
  await expect(waiting).toHaveCount(1);
  await expect(waiting).toContainText(question);
  await expect(waiting.getByRole('link', { name: 'Open thread' })).toHaveAttribute(
    'href',
    `/bots/${pilot.id}/threads/${t.id}`,
  );
  await page.keyboard.press('Escape');

  // A reply in chat clears it everywhere.
  await sendMessageAPI(m, t.id, 'Behind a flag');
  await expect(sidebarRow(page, 'Pilot')).not.toContainText('Waiting on you');
  await expect(threadTree(page, 'Pilot')).not.toContainText('Waiting on you');
  await expect(threadTree(page, 'Pilot').locator('[data-marker="waiting"]')).toHaveCount(0);
  await ops.getByRole('button', { name: 'Ops', exact: true }).click();
  await expect(ops).not.toHaveAttribute('data-marker', /./);
  await expect(header(page).getByRole('button', { name: /^Notifications/ })).toHaveAccessibleName(
    'Notifications',
  );
});

test('an option button posts its text as the person’s message and closes the card everywhere', async ({
  page,
}) => {
  const pilot = await newBotAPI(m, 'Pilot');
  const t = await thread(pilot, say.ask);
  await open(page, pilot, t);
  await expect(card(page)).toContainText(question);
  const row = threadTree(page, 'Pilot')
    .getByRole('listitem')
    .filter({ hasText: /Waiting/ });
  await expect(row).toHaveCount(1);

  await card(page).getByRole('button', { name: 'Behind a flag', exact: true }).click();
  await expect(
    log(page).getByRole('article', { name: 'You' }).filter({ hasText: 'Behind a flag' }),
  ).toHaveCount(1);
  await waitStatus(t.id, 'done');
  await expect(log(page)).toContainText('Going behind a flag.');
  await expect(card(page)).toHaveCount(0);
  await expect(header(page)).not.toContainText('waiting on you');
  await expect(threadTree(page, 'Pilot')).not.toContainText('Waiting on you');
  const messages = await api(m, 'GET', `/api/threads/${t.id}/messages`);
  const mine = messages.filter((x: { role: string }) => x.role === 'user');
  expect(mine.map((x: { text: string }) => x.text)).toEqual([say.ask, 'Behind a flag']);
});

test('answering in the composer posts the text as the person’s message and closes the card', async ({
  page,
}) => {
  const pilot = await newBotAPI(m, 'Pilot');
  const t = await thread(pilot, say.ask);
  await open(page, pilot, t);
  await expect(card(page)).toContainText(question);

  await composer(page).fill('Canary first, please.');
  await page.getByRole('button', { name: 'Send' }).click();
  await expect(
    log(page).getByRole('article', { name: 'You' }).filter({ hasText: 'Canary first, please.' }),
  ).toHaveCount(1);
  await waitStatus(t.id, 'done');
  await expect(log(page)).toContainText('Canary first, then.');
  await expect(card(page)).toHaveCount(0);
  await expect(header(page)).not.toContainText('waiting on you');
  await expect(sidebarRow(page, 'Pilot')).not.toContainText('Waiting on you');
});

test('a wake while the decision is open leaves one card, updated; the Bot is told it is open', async ({
  page,
}) => {
  const tester = await newBotAPI(m, 'Tester', 'claude', { look: 'shark' });
  const poteto = await newBotAPI(m, 'Poteto', 'claude', { look: 'whale' });
  await api(m, 'PUT', `/api/bots/${poteto.id}`, { delegates: [tester.id] });
  const t = await openThreadAPI(m, poteto.id, say.lead);
  await open(page, poteto, t);
  await expect(card(page)).toContainText('Which changelog format?');
  await expect(card(page).getByRole('button')).toHaveCount(2);

  // The helper's report wakes the lead; its run asks again, with another question.
  await expect(card(page)).toContainText('Which changelog format, or neither?', {
    timeout: 30_000,
  });
  await waitStatus(t.id, 'done');
  await expect(card(page)).toContainText('Which changelog format, or neither?');
  await expect(card(page)).not.toContainText('Which changelog format?');
  await expect(card(page).getByRole('button')).toHaveCount(3);
  await expect(header(page)).toContainText('1 waiting on you');
  await expect(threadTree(page, 'Poteto').locator('[data-marker="waiting"]')).toHaveCount(1);
  expect((await api(m, 'GET', `/api/threads/${t.id}`)).run_status).toBe('done');

  const prompts = readFileSync(join(m.bin, 'claude.log.prompts'), 'utf8')
    .split('\n')
    .filter(Boolean)
    .map((l) => JSON.parse(l) as { kind: string; text: string });
  const wake = prompts.filter((p) => p.kind === 'user' && p.text.includes('changelog-checked'));
  expect(wake).toHaveLength(1);
  expect(wake[0].text).toContain(
    'Open question for the person (do not ask it again): Which changelog format?',
  );
  const first = prompts.find((p) => p.kind === 'user' && p.text.includes(say.lead));
  expect(first?.text).not.toContain('Open question for the person');
});

test('a real blocked keeps its Blocked line and is a row of the card; a reply closes it', async ({
  page,
}) => {
  const pilot = await newBotAPI(m, 'Pilot');
  const t = await thread(pilot, say.blocked);
  await open(page, pilot, t);

  await expect(log(page)).toContainText(`Blocked: ${blocker}`);
  await expect(card(page)).toContainText(blocker);
  await expect(card(page).getByRole('button')).toHaveCount(0);
  await expect(header(page)).toContainText('1 waiting on you');
  await expect(sidebarRow(page, 'Pilot')).toContainText('Waiting on you');

  await composer(page).fill('I reconnected it.');
  await page.getByRole('button', { name: 'Send' }).click();
  await waitStatus(t.id, 'done');
  await expect(card(page)).toHaveCount(0);
  await expect(header(page)).not.toContainText('waiting on you');
  await expect(sidebarRow(page, 'Pilot')).not.toContainText('Waiting on you');
});

test('a ready PR is a row: #n, title, checks, age and Open; the sidebar and the island count it', async ({
  page,
}) => {
  const pilot = await newBotAPI(m, 'Pilot');
  const t = await thread(pilot, say.ready);
  checks(pr(201), sha('a'), 3, 0);
  readyFor(t.id, 125);
  await open(page, pilot, t);

  const row = ready(page).getByRole('listitem');
  await expect(row).toHaveCount(1);
  await expect(row).toContainText('#201');
  await expect(row).toContainText('Add the export button');
  await expect(row).toContainText('green');
  await expect(row).toContainText('2h');
  await expect(row).toContainText('aaaaaaa');
  await expect(row).not.toContainText('changed since ready');
  await expect(row.getByRole('link', { name: 'Open' })).toHaveAttribute('href', pr(201));
  await expect(card(page).getByRole('button')).toHaveCount(0);
  await expect(header(page)).toContainText('1 waiting on you');
  await expect(sidebarRow(page, 'Pilot')).toContainText('Waiting on you');
  await expect(threadTree(page, 'Pilot').locator('[data-marker="waiting"]')).toHaveCount(1);

  // The same PR from another page: the island counts it.
  const other = await newBotAPI(m, 'Elsewhere');
  await page.goto(`${m.url}/bots/${other.id}`);
  await expect(
    header(page).getByRole('button', { name: 'Notifications, 1 waiting', exact: true }),
  ).toBeVisible();
});

for (const state of ['merged', 'closed']) {
  test(`a ready PR leaves when it is ${state}, with no message from the Bot`, async ({ page }) => {
    const pilot = await newBotAPI(m, 'Pilot');
    const t = await thread(pilot, say.ready);
    await open(page, pilot, t);
    await expect(ready(page).getByRole('listitem')).toHaveCount(1);
    await expect(sidebarRow(page, 'Pilot')).toContainText('Waiting on you');
    const before = await messageCount(t.id);
    const run = (await api(m, 'GET', `/api/threads/${t.id}`)).run_id;

    setState(pr(201), state);
    await expect(card(page)).toHaveCount(0);
    await expect(header(page)).not.toContainText('waiting on you');
    await expect(sidebarRow(page, 'Pilot')).not.toContainText('Waiting on you');
    await expect(threadTree(page, 'Pilot').locator('[data-marker="waiting"]')).toHaveCount(0);
    await page.waitForTimeout(2_500);
    expect(await messageCount(t.id)).toBe(before);
    expect((await api(m, 'GET', `/api/threads/${t.id}`)).run_id).toBe(run);
  });
}

test('a test PR reported ready as failing on main for the right reason says red on purpose; a bare red says red', async ({
  page,
}) => {
  const pilot = await newBotAPI(m, 'Pilot');
  const on = await thread(pilot, say.redNote);
  const bare = await thread(pilot, say.redBare);
  checks(pr(311), sha('d'), 4, 1, ['e2e']);
  checks(pr(312), sha('e'), 4, 1, ['e2e']);

  await open(page, pilot, on);
  const purpose = ready(page).getByRole('listitem');
  await expect(purpose).toContainText('#311');
  await expect(purpose).toContainText('red on purpose');

  await open(page, pilot, bare);
  const plain = ready(page).getByRole('listitem');
  await expect(plain).toContainText('#312');
  await expect(plain).toContainText('red');
  await expect(plain).not.toContainText('on purpose');
  await expect(plain).not.toContainText('green');
});

test('a ready PR says changed since ready when its head is not the ready commit', async ({
  page,
}) => {
  const pilot = await newBotAPI(m, 'Pilot');
  const t = await thread(pilot, say.ready);
  write('UPDATE threads SET mr_head = ? WHERE mr_url = ?', sha('a'), pr(201));
  await open(page, pilot, t);
  const row = ready(page).getByRole('listitem');
  await expect(row).toContainText('aaaaaaa');
  await expect(row).not.toContainText('changed since ready');

  write('UPDATE threads SET mr_head = ? WHERE mr_url = ?', sha('f'), pr(201));
  await expect(row).toContainText('changed since ready');
  await expect(row).toContainText('aaaaaaa');
  await expect(header(page)).toContainText('1 waiting on you');
});

test('two ready PRs and one decision make 3 waiting on you; answering the decision leaves 2', async ({
  page,
}) => {
  const pilot = await newBotAPI(m, 'Pilot');
  await thread(pilot, say.track);
  const t = await thread(pilot, say.two);
  await open(page, pilot, t);

  await expect(header(page)).toContainText('3 waiting on you');
  await expect(ready(page).getByRole('listitem')).toHaveCount(2);
  await expect(ready(page)).toContainText('#301');
  await expect(ready(page)).toContainText('Test the export');
  await expect(ready(page)).toContainText('#302');
  await expect(ready(page)).toContainText('Fix the export');
  await expect(card(page)).toContainText('Which PR do you merge first?');

  await card(page).getByRole('button', { name: 'The test PR', exact: true }).click();
  await expect(header(page)).toContainText('2 waiting on you');
  await expect(card(page)).not.toContainText('Which PR do you merge first?');
  await expect(card(page).getByRole('button')).toHaveCount(0);
  await expect(ready(page).getByRole('listitem')).toHaveCount(2);
  await waitStatus(t.id, 'done');
  await expect(header(page)).toContainText('2 waiting on you');
  await expect(sidebarRow(page, 'Pilot')).toContainText('Waiting on you');
});
