// The "Waiting on you" card: what waits on the person (a decision, a blocker, merge-ready PRs)
// shows in the thread, the sidebar, the Bot row, a folded group and the island, once, and clears in
// all of them together. Contract: e2e/waiting-card-contract.md.
import { execFileSync } from 'node:child_process';
import { createServer, type Server } from 'node:http';
import { mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
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

// The database is the one the server runs on: what only the PR watcher writes (checks, head) is
// set here, as the watcher would.
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
const readyFor = (threadID: string, minutes: number) =>
  write(
    'UPDATE runs SET finished_at = ? WHERE thread_id = ?',
    (Date.now() - minutes * 60_000) * 1000,
    threadID,
  );

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

// The merge and the close go through the real PR watcher: GitHub is a local server it reads
// (ATOLL_GITHUB_URL), and the Bot opens and tracks the PR with the fake gh (repo-github-ready).
test.describe('a ready PR the watcher reads', () => {
  let github: Server;
  let githubURL = '';
  let token = '';
  let root = '';
  let pull: { title: string; state: string; merged: boolean } | undefined;
  const head = sha('a');

  test.beforeEach(async () => {
    m.stop();
    pull = undefined;
    github = createServer((req, res) => {
      let body = '';
      req.on('data', (c) => (body += c));
      req.on('end', () => {
        const json = (status: number, value: unknown) => {
          res.writeHead(status, { 'Content-Type': 'application/json' });
          res.end(JSON.stringify(value));
        };
        const url = req.url ?? '';
        if (req.method === 'POST' && url === '/login/device/code') {
          json(200, {
            device_code: 'fixture-device-code',
            user_code: 'ABCD-1234',
            verification_uri: 'https://github.com/login/device',
            expires_in: 900,
            interval: 1,
          });
        } else if (req.method === 'POST' && url === '/login/oauth/access_token') {
          token = 'fake-gho-ui-waiting-card';
          json(200, {
            access_token: token,
            token_type: 'bearer',
            scope: 'repo,read:org,workflow',
          });
        } else if (
          ![`Bearer ${token}`, `token ${token}`].includes(req.headers.authorization ?? '')
        ) {
          json(401, { message: 'Bad credentials' });
        } else if (req.method === 'GET' && url === '/user') {
          json(200, { login: 'reef-fixture', id: 1 });
        } else if (req.method === 'POST' && url === '/repos/octo/hello/pulls') {
          pull = {
            title: JSON.parse(body).title,
            state: 'open',
            merged: false,
          };
          json(201, { number: 1, html_url: `${githubURL}/octo/hello/pull/1` });
        } else if (req.method === 'GET' && url === '/repos/octo/hello/pulls/1' && pull) {
          json(200, {
            number: 1,
            ...pull,
            base: { ref: 'main' },
            head: { sha: head },
          });
        } else if (/^\/repos\/octo\/hello\/commits\/[0-9a-f]+\/check-runs/.test(url)) {
          json(200, { total_count: 0, check_runs: [] });
        } else if (/^\/repos\/octo\/hello\/commits\/[0-9a-f]+\/status/.test(url)) {
          json(200, { state: 'pending', total_count: 0, statuses: [] });
        } else {
          json(404, { message: 'Not Found' });
        }
      });
    });
    await new Promise<void>((ok) => github.listen(0, '127.0.0.1', ok));
    const addr = github.address();
    githubURL = `http://127.0.0.1:${typeof addr === 'object' && addr ? addr.port : 0}`;
    m = await startAtoll({
      fakes: ['claude', 'gh'],
      secretStore: true,
      env: {
        ATOLL_GITHUB_URL: githubURL,
        ATOLL_GITHUB_MCP_URL: `${githubURL}/mcp/`,
        ATOLL_REPO_POLL_INTERVAL: '200ms',
      },
    });
    writeFileSync(join(m.bin, 'github-url'), githubURL);
    root = mkdtempSync(join(tmpdir(), 'atoll-ui-waiting-card-'));
  });
  test.afterEach(() => {
    github?.close();
    if (root) rmSync(root, { recursive: true, force: true });
  });

  // pilot is a Bot with a repo on the fake GitHub and the GitHub connection.
  async function pilot() {
    const git = (dir: string, ...args: string[]) =>
      execFileSync('git', args, {
        cwd: dir,
        env: {
          ...process.env,
          GIT_AUTHOR_NAME: 'Fixture',
          GIT_AUTHOR_EMAIL: 'fixture@example.test',
          GIT_COMMITTER_NAME: 'Fixture',
          GIT_COMMITTER_EMAIL: 'fixture@example.test',
          GIT_CONFIG_NOSYSTEM: '1',
        },
      });
    const bare = join(root, 'hello.git');
    mkdirSync(bare);
    git(bare, 'init', '--bare', '-b', 'main');
    const path = join(root, 'hello');
    mkdirSync(path);
    git(path, 'init', '-b', 'main');
    writeFileSync(join(path, 'answer.txt'), '41\n');
    git(path, 'add', 'answer.txt');
    git(path, 'commit', '-m', 'seed bug');
    git(path, 'remote', 'add', 'origin', `${githubURL}/octo/hello.git`);
    git(path, 'config', 'remote.origin.pushurl', bare);
    git(path, 'push', bare, 'main');
    const repo = await api(m, 'POST', '/api/repos', {
      name: 'octo-hello',
      path,
    });
    const start = await api(m, 'POST', '/api/connections/github/start');
    await expect
      .poll(async () => (await api(m, 'POST', `/api/connections/${start.id}/poll`)).status, {
        intervals: [1_100],
      })
      .toBe('connected');
    const bot = await newBotAPI(m, 'Pilot', 'claude', {
      repo_access: [repo.id],
      connections: [start.id],
    });
    return { bot, repo };
  }

  for (const [state, merged, wake, reply] of [
    ['merged', true, `PR #1 merged into main at ${head.slice(0, 7)}`, 'Merge noted.'],
    ['closed', false, 'PR #1 closed', 'Close noted.'],
  ] as const) {
    test(`a ready PR leaves when it is ${state}; the Reef wake and its run still happen`, async ({
      page,
    }) => {
      const { bot, repo } = await pilot();
      const url = `${githubURL}/octo/hello/pull/1`;
      const t = await api(m, 'POST', `/api/bots/${bot.id}/threads`, {
        repo_id: repo.id,
        text: `Fix the seeded bug, open a PR and report it ready. READY=${JSON.stringify([{ url, sha: head }])}`,
      });
      await waitStatus(t.id, 'done');
      await open(page, bot, t);
      const row = ready(page).getByRole('listitem');
      await expect(row).toHaveCount(1);
      await expect(row).toContainText('#1');
      await expect(row.getByRole('link', { name: 'Open' })).toHaveAttribute('href', url);
      await expect(sidebarRow(page, 'Pilot')).toContainText('Waiting on you');
      const run = (await api(m, 'GET', `/api/threads/${t.id}`)).run_id;

      pull = { ...pull!, state: 'closed', merged };
      await expect(card(page)).toHaveCount(0);
      await expect(header(page)).not.toContainText('waiting on you');
      await expect(sidebarRow(page, 'Pilot')).not.toContainText('Waiting on you');
      await expect(threadTree(page, 'Pilot').locator('[data-marker="waiting"]')).toHaveCount(0);

      await expect
        .poll(async () => (await api(m, 'GET', `/api/threads/${t.id}`)).run_id)
        .not.toBe(run);
      await waitStatus(t.id, 'done');
      const messages: { role: string; text: string; kind?: string; from_bot_id?: string }[] =
        await api(m, 'GET', `/api/threads/${t.id}/messages`);
      const wakes = messages.filter((x) => x.kind);
      expect(wakes.map((x) => [x.kind, x.text, x.from_bot_id ?? ''])).toEqual([[state, wake, '']]);
      expect(messages.at(-1)).toMatchObject({ role: 'bot', text: reply });
      await expect(log(page)).toContainText(reply);
      await expect(card(page)).toHaveCount(0);
    });
  }
});

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
