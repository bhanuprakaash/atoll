// The lead's ledger: helper reports and status-only boss replies fold into one block between the
// person's messages; what needs the person stays a message. Contract: e2e/lead-ledger-contract.md.
import { randomUUID } from 'node:crypto';
import { join } from 'node:path';
import { DatabaseSync } from 'node:sqlite';
import { expect, type Locator, type Page, test } from '@playwright/test';
import { api, type Machine, newBotAPI, prompts, startAtoll } from './atoll';
import { bossHandsOff, runStatus } from './helpers-fixture';

test.use({
  locale: 'en-US',
  timezoneId: 'UTC',
  viewport: { width: 1280, height: 800 },
});

const looks = {
  Poteto: 'whale',
  Tester: 'shark',
  Reviewer: 'octopus',
  Coder: 'crab',
};
type Name = keyof typeof looks;
type Report = {
  done: string;
  tried?: string;
  blocked?: string;
  next?: string;
  ready?: { url: string; sha: string }[];
  ask?: { question: string; options: string[] };
};

const today = new Date();
const at = (minute: number, second = 0) =>
  Date.UTC(today.getUTCFullYear(), today.getUTCMonth(), today.getUTCDate(), 8, minute, second) *
  1000;

// Script writes what a lead's thread holds after a round of work into the database: the person's
// messages with the lead's answers, each helper's thread and run, the wake message each helper's
// run posted into the lead's thread, and the lead's wake run with its reply, if any.
class Script {
  threads: Record<string, string> = {};
  constructor(
    private db: DatabaseSync,
    private boss: string,
    private bots: Record<Name, string>,
  ) {}

  private run(
    thread: string,
    status: string,
    started: number,
    finished: number,
    report: Report | null,
    reply = '',
    error = '',
  ) {
    const id = randomUUID();
    this.db
      .prepare(
        `INSERT INTO runs (id, thread_id, status, reply, error, report, started_at, finished_at, provider)
         VALUES (?, ?, ?, ?, ?, ?, ?, ?, 'claude')`,
      )
      .run(
        id,
        thread,
        status,
        reply,
        error,
        report && JSON.stringify({ tried: '', blocked: '', next: '', ...report }),
        started,
        finished,
      );
    return id;
  }

  private message(thread: string, role: string, text: string, run: string, from = '', kind = '') {
    this.db
      .prepare(
        'INSERT INTO messages (id, thread_id, role, text, run_id, from_bot_id, kind) VALUES (?, ?, ?, ?, ?, ?, ?)',
      )
      .run(randomUUID(), thread, role, text, run, from, kind);
  }

  // person is a message of the person's and the lead's answer to it.
  person(minute: number, text: string, answer: string) {
    const run = this.run(
      this.boss,
      'done',
      at(minute),
      at(minute, 30),
      { done: 'Answered.' },
      answer,
    );
    this.message(this.boss, 'user', text, run);
    this.message(this.boss, 'bot', answer, run);
  }

  // wake is the end of a helper's run at minute and the lead's run it woke. created is when the
  // helper's thread began: a handoff line stands after the last message before it. merge is the
  // helper's branch the lead is told to merge; running is the helpers line's other runs.
  wake(
    minute: number,
    helper: Name,
    h: {
      reply: string;
      report: Report;
      mr?: [url: string, state: string];
      status?: 'done' | 'failed';
      error?: string;
      created?: number;
      merge?: string;
      running?: string[];
    },
    // reef is a Reef wake line, e.g. a tracked PR that merged, that the same lead run took.
    lead?: { reply: string; report: Report; reef?: [kind: string, text: string] },
  ) {
    const thread = randomUUID();
    this.threads[h.report.done] = thread;
    this.db
      .prepare(
        `INSERT INTO threads (id, bot_id, created, last_activity, parent_thread_id, mr_url, mr_state, mr_title)
         VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
      )
      .run(
        thread,
        this.bots[helper],
        h.created ?? at(0),
        at(minute),
        this.boss,
        h.mr?.[0] ?? '',
        h.mr?.[1] ?? '',
        h.mr ? 'A pull request' : '',
      );
    const status = h.status ?? 'done';
    this.run(
      thread,
      status,
      at(minute - 1),
      at(minute),
      status === 'done' ? h.report : null,
      h.reply,
      h.error ?? '',
    );
    const woken = this.run(
      this.boss,
      'done',
      at(minute),
      at(minute, 20),
      lead?.report ?? { done: 'Noted.' },
      lead?.reply ?? '',
    );
    // The text run/inbox.go WakeText builds, with HelpersLine of run/delegate.go.
    const running = h.running ?? [];
    const helpers = running.length
      ? `Helpers: ${running.length} of 3 running (${running.join('; ')}). ${3 - running.length} free.`
      : 'Helpers: 0 of 3 running. 3 free.';
    const text = `${
      status === 'done'
        ? [
            `${helper} reported back from thread ${thread}.`,
            h.reply,
            '',
            ...(['Done', 'Tried', 'Blocked', 'Next'] as const).flatMap((k) => {
              const v = h.report[k.toLowerCase() as 'done'];
              return v ? [`${k}: ${v}`] : [];
            }),
            ...(h.merge ? [`Merge ${h.merge} into your branch.`] : []),
          ]
            .join('\n')
            .trim()
        : `${helper}'s run failed: ${h.error}\n(thread ${thread})`
    }\n\n${helpers}`;
    this.message(this.boss, 'user', text, woken, this.bots[helper]);
    if (lead?.reef) this.message(this.boss, 'user', lead.reef[1], woken, '', lead.reef[0]);
    if (lead?.reply) this.message(this.boss, 'bot', lead.reply, woken);
  }
}

type Lead = {
  m: Machine;
  boss: string;
  thread: string;
  bots: Record<Name, string>;
  threads: Record<string, string>;
};

// startLead makes the Bots and the lead's thread through the API, stops the server, lets script
// fill the thread and starts the server again on the same data. No provider runs.
async function startLead(script: (s: Script) => void): Promise<Lead> {
  const m = await startAtoll();
  try {
    const bots = {} as Record<Name, string>;
    for (const name of Object.keys(looks) as Name[])
      bots[name] = (await newBotAPI(m, name, 'claude', { look: looks[name] })).id;
    await api(m, 'PUT', `/api/bots/${bots.Poteto}`, {
      delegates: [bots.Tester, bots.Reviewer, bots.Coder],
    });
    const thread: string = (await api(m, 'POST', `/api/bots/${bots.Poteto}/threads`, {})).id;
    m.kill();
    const db = new DatabaseSync(join(m.data, 'atoll.db'));
    const s = new Script(db, thread, bots);
    try {
      script(s);
    } finally {
      db.close();
    }
    await m.restart();
    return { m, boss: bots.Poteto, thread, bots, threads: s.threads };
  } catch (err) {
    m.stop();
    throw err;
  }
}

// open shows the thread once the chat has scrolled to its latest message, so positions are steady.
async function open(page: Page, l: Lead) {
  await page.goto(`${l.m.url}/bots/${l.boss}/threads/${l.thread}`);
  const log = page.getByRole('log', { name: 'Messages' });
  await expect(log.getByTestId('message').last()).toBeVisible();
  await expect
    .poll(() => log.evaluate((el) => el.scrollHeight - el.scrollTop - el.clientHeight))
    .toBeLessThanOrEqual(1);
}
const box = async (l: Locator) => (await l.boundingBox())!;
const pad = (i: number) => String(i).padStart(2, '0');
const messages = (page: Page) => page.getByRole('log', { name: 'Messages' });
const ledgers = (page: Page) => messages(page).getByRole('list', { name: 'Updates' });

const done = (i: number) =>
  i === 12
    ? 'Checked item-12: rebased the branch on main, fixed the three conflicts in the thread view, reran every check and the e2e suite, and updated the design note and the issue checklist with the commands and their output as proof.'
    : `Checked item-${pad(i)}: all checks pass`;
const prose = (i: number) => `Full report item-${pad(i)}: the checks pass on every commit.`;
const ack = (i: number) => `Noted item-${pad(i)}. Nothing needs you yet; I keep going.`;

// Thread A: a person's message and the lead's answer, 30 helper reports (4 of them followed by a
// status reply of the lead's, one failed, some with a PR) with the 30th followed by a decision
// the person must make, then the person's answer and the lead's.
const kickoff = 'Work through the backlog and tell me what needs me.';
const decision =
  'I need your call on how Chief reads agent files: import_agent(path) is my pick, a scoped Read rule leaks in Auto mode. Tell me which.';
const decisionBlocked = 'Decision needed from Ada: the tool for agent files.';
const answer = 'Go with import_agent.';
const answerReply = 'Going with import_agent. I will tell the coders.';
const failure = 'Atoll stopped while the run was going.';
const helperOf: Name[] = ['Coder', 'Tester', 'Reviewer'];

function threadA(s: Script) {
  s.person(19, kickoff, "On it. I'll hand the backlog to the team and tell you what needs you.");
  for (let i = 1; i <= 30; i++) {
    const running = i % 2 ? [] : [`${helperOf[(i + 1) % 3]} on Check item-${pad(i + 1)}`];
    s.wake(
      20 + i,
      helperOf[i % 3],
      i === 7
        ? {
            reply: '',
            report: { done: 'Failed item-07' },
            status: 'failed',
            error: failure,
            running,
          }
        : {
            reply: prose(i),
            report: { done: done(i), blocked: i % 5 === 0 ? 'none' : '' },
            mr:
              i % 4 === 1
                ? [`https://github.com/acme/app/pull/${200 + i}`, i % 8 === 1 ? 'merged' : 'opened']
                : undefined,
            running,
          },
      i === 30
        ? {
            reply: decision,
            report: { done: 'Asked for a decision.', blocked: decisionBlocked },
          }
        : i % 6 === 0
          ? {
              reply: ack(i),
              report: {
                done: `Noted item-${pad(i)}`,
                blocked: i % 12 === 0 ? 'none' : '',
              },
            }
          : undefined,
    );
  }
  s.person(55, answer, answerReply);
}

let a: Lead;
test.describe('30 helper reports and 1 decision', () => {
  test.beforeAll(async () => {
    a = await startLead(threadA);
  });
  test.afterAll(() => a?.m.stop());

  const you = (page: Page, text: string) =>
    messages(page).getByRole('article', { name: 'You' }).filter({ hasText: text });

  test('one ledger between two messages of the person: a row per event, no message per report', async ({
    page,
  }) => {
    await open(page, a);
    const log = messages(page);
    await expect(ledgers(page)).toHaveCount(1);
    const ledger = ledgers(page);
    const rows = ledger.getByRole('listitem');
    await expect(rows).toHaveCount(30 + 4);

    // What is left as messages: the person's two, the lead's answer to each and the decision.
    await expect(log.getByTestId('message')).toHaveCount(5);
    await expect(log.getByText(/Full report item/)).toHaveCount(0);
    await expect(log.getByRole('link', { name: 'Open thread' })).toHaveCount(0);
    await expect(log).not.toContainText('Helpers:');
    await expect(log).not.toContainText('(thread ');

    const first = await box(you(page, kickoff));
    const last = await box(you(page, answer));
    const block = await box(ledger);
    expect(block.y).toBeGreaterThan(first.y + first.height);
    expect(block.y + block.height).toBeLessThan(last.y);

    // A helper's row: time, creature, who, the line from finish_run.done, the PR chip, the state.
    const tester = rows.filter({ hasText: done(1) });
    await expect(tester).toHaveCount(1);
    await expect(tester).toContainText(/8:21\s*AM/);
    await expect(tester).toContainText('Tester');
    await expect(tester.getByRole('img', { name: /^Shark\b/ })).toBeVisible();
    await expect(tester).toContainText('Done');
    await expect(tester.getByRole('link', { name: '#201' })).toHaveAttribute(
      'href',
      'https://github.com/acme/app/pull/201',
    );
    await expect(rows.filter({ hasText: done(2) }).getByRole('link')).toHaveCount(0);

    // The lead's status reply is a row of its own, by the lead.
    const lead = rows.filter({ hasText: 'Noted item-06' });
    await expect(lead).toHaveCount(1);
    await expect(lead).toContainText(/8:26\s*AM/);
    await expect(lead).toContainText('Poteto');
    await expect(lead.getByRole('img', { name: /^Whale\b/ })).toBeVisible();
    await expect(log.getByText(ack(6))).toHaveCount(0);
    // A blocked of "none" is no blocked.
    await expect(rows.filter({ hasText: 'Noted item-12' })).toHaveCount(1);

    // A failed helper's row says so, with its error as the line.
    const failed = rows.filter({ hasText: failure });
    await expect(failed).toHaveCount(1);
    await expect(failed).toContainText('Failed');
    await expect(failed).toContainText(helperOf[7 % 3]);

    // Every row is one line, the longest report too.
    await expect(rows.filter({ hasText: 'Checked item-12' })).toHaveCount(1);
    for (const row of await rows.all()) expect((await box(row)).height).toBeLessThanOrEqual(40);

    // The failed row opens to its thread; open, it shows neither the helpers line nor the id.
    await failed.getByRole('button').first().click();
    await expect(ledger.getByRole('link', { name: 'Open thread' })).toHaveAttribute(
      'href',
      `/bots/${a.bots[helperOf[7 % 3]]}/threads/${a.threads['Failed item-07']}`,
    );
    await expect(log).not.toContainText('Helpers:');
    await expect(log).not.toContainText('(thread ');
  });

  test('clicking a row opens the full report in place and clicking again folds it', async ({
    page,
  }) => {
    await open(page, a);
    await expect(ledgers(page)).toHaveCount(1);
    const ledger = ledgers(page);
    const rows = ledger.getByRole('listitem');
    const url = page.url();
    const row = rows.filter({ hasText: done(1) });
    const line = row.getByRole('button').first();
    const next = rows.filter({ hasText: done(2) });
    // The log keeps its scroll, so positions are read from the top of the ledger.
    const ledgerBox = (await ledger.elementHandle())!;
    const rel = (l: Locator) =>
      l.evaluate(
        (el, root) => el.getBoundingClientRect().top - root.getBoundingClientRect().top,
        ledgerBox,
      );
    const top = await rel(row);
    const nextTop = await rel(next);

    await line.click();
    const report = ledger.getByText(prose(1));
    await expect(report).toBeVisible();
    await expect(ledger.getByRole('link', { name: 'Open thread' })).toHaveAttribute(
      'href',
      `/bots/${a.bots.Tester}/threads/${a.threads[done(1)]}`,
    );
    await expect(ledger.getByText(/Full report item/)).toHaveCount(1);
    expect(page.url()).toBe(url);
    expect(Math.abs((await rel(row)) - top)).toBeLessThanOrEqual(1);
    const shown = await rel(report);
    expect(shown).toBeGreaterThanOrEqual(top + (await box(line)).height - 1);
    expect(shown + (await box(report)).height).toBeLessThanOrEqual((await rel(next)) + 1);
    expect(await rel(next)).toBeGreaterThan(nextTop);

    await line.click();
    await expect(report).toHaveCount(0);
    await expect(ledger.getByRole('link', { name: 'Open thread' })).toHaveCount(0);
    expect(Math.abs((await rel(next)) - nextTop)).toBeLessThanOrEqual(1);

    // The lead's row opens to the lead's reply.
    await rows.filter({ hasText: 'Noted item-06' }).getByRole('button').first().click();
    await expect(ledger.getByText(ack(6))).toBeVisible();
  });

  test('a lead reply with a real blocked is a normal message after the ledger', async ({
    page,
  }) => {
    await open(page, a);
    const log = messages(page);
    await expect(ledgers(page)).toHaveCount(1);
    const ledger = ledgers(page);
    const message = log.getByText(decision);
    await expect(message).toBeVisible();
    await expect(ledger.getByText(decision)).toHaveCount(0);
    await expect(ledger.getByText('Asked for a decision.')).toHaveCount(0);
    await expect(log.getByText(`Blocked: ${decisionBlocked}`)).toBeVisible();

    const block = await box(ledger);
    const shown = await box(message);
    expect(shown.y).toBeGreaterThanOrEqual(block.y + block.height);
    expect(shown.y + shown.height).toBeLessThan((await box(you(page, answer))).y);

    // The person's answer gets the lead's reply as a message, however short.
    await expect(log.getByText(answerReply)).toBeVisible();
    await expect(ledger.getByText(answerReply)).toHaveCount(0);
  });

  test('the thread fits in 3 screens at 800 px', async ({ page }) => {
    await page.setViewportSize({ width: 1280, height: 800 });
    await open(page, a);
    const log = messages(page);
    await expect(log.getByText(answerReply)).toBeVisible();
    const height = await log.evaluate((el) => el.scrollHeight);
    expect(height, `the chat is ${height} px tall; 3 screens are 2400`).toBeLessThanOrEqual(
      3 * 800,
    );
    await expect(ledgers(page)).toHaveCount(1);
  });
});

test('a lead question and a helper blocked end a ledger; a handoff line joins it', async ({
  page,
}) => {
  const question =
    'Reviewer wants to know whether the changelog should list item-02. Should I include it?';
  const b = await startLead((s) => {
    s.person(19, 'Check the changelog.', 'On it.');
    s.wake(
      21,
      'Tester',
      { reply: prose(1), report: { done: done(1) } },
      { reply: ack(1), report: { done: 'Noted item-01' } },
    );
    s.wake(
      23,
      'Reviewer',
      { reply: prose(2), report: { done: done(2) }, created: at(21, 30) },
      { reply: question, report: { done: 'Asked about the changelog.' } },
    );
    s.wake(
      25,
      'Tester',
      { reply: prose(3), report: { done: done(3) }, merge: 'fix/changelog--tester' },
      { reply: ack(3), report: { done: 'Noted item-03' } },
    );
    s.wake(27, 'Coder', {
      reply: prose(4),
      report: { done: done(4), blocked: 'No access to the release notes.' },
    });
  });
  try {
    await open(page, b);
    const log = messages(page);
    const lists = ledgers(page);
    await expect(lists).toHaveCount(2);

    // Before the question: the first report, the lead's reply, the handoff, the second report.
    const before = lists.nth(0).getByRole('listitem');
    await expect(before).toHaveCount(4);
    await expect(before.nth(0)).toContainText(done(1));
    await expect(before.nth(1)).toContainText('Noted item-01');
    await expect(before.nth(2)).toContainText('Handed off to Reviewer');
    await expect(before.nth(3)).toContainText(done(2));
    await expect(log.getByText('Handed off to Reviewer')).toHaveCount(1);

    // After it: the third report and the lead's reply.
    const after = lists.nth(1).getByRole('listitem');
    await expect(after).toHaveCount(2);
    await expect(after.nth(0)).toContainText(done(3));
    await expect(after.nth(0)).not.toContainText('Merge');
    await expect(log).not.toContainText('Helpers:');
    await expect(after.nth(1)).toContainText('Noted item-03');

    // The question is a message between the two blocks, a report with a real blocked one after both.
    const ask = log.getByText(question);
    await expect(ask).toBeVisible();
    await expect(lists.getByText(question)).toHaveCount(0);
    const one = await box(lists.nth(0));
    const two = await box(lists.nth(1));
    const asked = await box(ask);
    expect(asked.y).toBeGreaterThanOrEqual(one.y + one.height);
    expect(asked.y + asked.height).toBeLessThanOrEqual(two.y);

    const blocked = log.getByRole('article', { name: 'Coder' });
    await expect(blocked).toContainText(prose(4));
    await expect(blocked.getByRole('link', { name: 'Open thread' })).toBeVisible();
    await expect(lists.getByText(prose(4))).toHaveCount(0);
    expect((await box(blocked)).y).toBeGreaterThanOrEqual(two.y + two.height);

    // The person's own message got its answer as a message, not a row.
    await expect(log.getByText('On it.', { exact: true })).toBeVisible();
    await expect(lists.getByText('On it.')).toHaveCount(0);
  } finally {
    b.m.stop();
  }
});

test('a Reef wake line joins the ledger, and the lead reply to the run that took it folds', async ({
  page,
}) => {
  const merged = 'PR #1 merged into main at abc1234';
  const c = await startLead((s) => {
    s.person(19, 'Ship the changelog.', 'On it.');
    s.wake(
      21,
      'Tester',
      { reply: prose(1), report: { done: done(1) } },
      { reply: ack(1), report: { done: 'Noted item-01' }, reef: ['merged', merged] },
    );
  });
  try {
    await open(page, c);
    const lists = ledgers(page);
    await expect(lists).toHaveCount(1);
    await expect(lists).toContainText(merged);
    await expect(messages(page).getByText(merged)).toHaveCount(1);
    await expect(lists.getByRole('listitem').filter({ hasText: 'Noted item-01' })).toHaveCount(1);
    await expect(messages(page).getByText(ack(1))).toHaveCount(0);
  } finally {
    c.m.stop();
  }
});

test('a lead reply with ready or ask and a helper report with ready stay messages that end a ledger', async ({
  page,
}) => {
  const review = 'Reviewed the change and proved it. Squash-merge it when you are happy.';
  const choice = 'Both ways work and I need your pick. The card below has the options.';
  const pr = { url: 'https://github.com/acme/app/pull/12', sha: 'a'.repeat(40) };
  const d = await startLead((s) => {
    s.person(19, 'Review the change.', 'On it.');
    s.wake(
      21,
      'Tester',
      { reply: prose(1), report: { done: done(1) } },
      { reply: ack(1), report: { done: 'Noted item-01' } },
    );
    s.wake(
      23,
      'Reviewer',
      { reply: prose(2), report: { done: done(2) } },
      { reply: review, report: { done: 'Reviewed the change.', ready: [pr] } },
    );
    s.wake(
      25,
      'Tester',
      { reply: prose(3), report: { done: done(3) } },
      {
        reply: choice,
        report: { done: 'Asked for a pick.', ask: { question: 'Which way?', options: ['A', 'B'] } },
      },
    );
    s.wake(27, 'Coder', {
      reply: prose(4),
      report: { done: done(4), ready: [pr] },
      mr: [pr.url, 'opened'],
    });
    s.wake(
      29,
      'Tester',
      { reply: prose(5), report: { done: done(5) } },
      { reply: ack(5), report: { done: 'Noted item-05' } },
    );
  });
  try {
    await open(page, d);
    const log = messages(page);
    const lists = ledgers(page);
    await expect(lists).toHaveCount(3);

    await expect(lists.nth(0).getByRole('listitem')).toHaveCount(3);
    await expect(lists.nth(0)).toContainText(done(1));
    await expect(lists.nth(0)).toContainText('Noted item-01');
    await expect(lists.nth(0)).toContainText(done(2));
    await expect(lists.nth(1).getByRole('listitem')).toHaveCount(1);
    await expect(lists.nth(1)).toContainText(done(3));
    await expect(lists.nth(2).getByRole('listitem')).toHaveCount(2);
    await expect(lists.nth(2)).toContainText(done(5));
    await expect(lists.nth(2)).toContainText('Noted item-05');

    for (const text of [review, choice]) {
      await expect(log.getByText(text)).toBeVisible();
      await expect(lists.getByText(text)).toHaveCount(0);
    }
    const helper = log.getByRole('article', { name: 'Coder' });
    await expect(helper).toContainText(prose(4));
    await expect(lists.getByText(prose(4))).toHaveCount(0);
    await expect(lists.getByText(done(4))).toHaveCount(0);

    const first = await box(lists.nth(0));
    const second = await box(lists.nth(1));
    const third = await box(lists.nth(2));
    const reviewed = await box(log.getByText(review));
    const asked = await box(log.getByText(choice));
    const reported = await box(helper);
    expect(reviewed.y).toBeGreaterThanOrEqual(first.y + first.height);
    expect(reviewed.y + reviewed.height).toBeLessThanOrEqual(second.y);
    expect(asked.y).toBeGreaterThanOrEqual(second.y + second.height);
    expect(asked.y + asked.height).toBeLessThanOrEqual(reported.y);
    expect(reported.y + reported.height).toBeLessThanOrEqual(third.y);
    await expect(log.getByText(ack(1))).toHaveCount(0);
    await expect(log.getByText(ack(5))).toHaveCount(0);

    const msgs: { role: string; text: string; ready?: boolean; ask?: boolean }[] = await api(
      d.m,
      'GET',
      `/api/threads/${d.thread}/messages`,
    );
    const bot = (text: string) => msgs.find((m) => m.role === 'bot' && m.text === text)!;
    expect(bot(review).ready).toBe(true);
    expect(bot(review).ask).toBeFalsy();
    expect(bot(choice).ask).toBe(true);
    expect(bot(choice).ready).toBeFalsy();
    for (const text of ['On it.', ack(1), ack(5)]) {
      expect(bot(text).ready).toBeFalsy();
      expect(bot(text).ask).toBeFalsy();
    }
  } finally {
    d.m.stop();
  }
});

// Wakes the real server posts when a helper's run ends other than done. realWake hands brief to
// Tester from a new Poteto thread on m, has end end the helper's run, and waits for the wake and
// the end of the lead's run it starts.
async function realWake(
  m: Machine,
  page: Page,
  brief: string,
  end: (helper: string, tester: string) => Promise<void>,
  tester: object = {},
) {
  const { bots, boss, helpers } = await bossHandsOff(m, page, [['Tester', brief]], {
    Tester: tester,
  });
  // The run of the person's message; a helper that fails at once wakes the lead right after it.
  const handoff = (await api(m, 'GET', `/api/threads/${boss}/messages`))[0].run_id;
  const [helper, testerID] = [helpers[0].id, bots.Tester.id];
  await end(helper, testerID);
  await expect
    .poll(
      async () =>
        (await api(m, 'GET', `/api/threads/${boss}/messages`)).some(
          (s: { from_bot_id?: string }) => s.from_bot_id === testerID,
        ),
      { timeout: 30_000 },
    )
    .toBe(true);
  await expect
    .poll(
      async () => {
        const t = await api(m, 'GET', `/api/threads/${boss}`);
        return t.run_id !== handoff && t.run_status !== 'running';
      },
      { timeout: 30_000 },
    )
    .toBe(true);
  return { helper, tester: testerID };
}

// expectFolded checks the wake is one row of the lead's ledger, showing state, and opening to href,
// with neither the helpers line nor the thread id in sight, folded or open.
async function expectFolded(page: Page, state?: string, href?: string) {
  // The lead's own reply to the wake (testdata/claude/wake.jsonl) is the ledger's other row.
  const row = ledgers(page).getByRole('listitem').filter({ hasNotText: "Read Tester's message." });
  await expect(row).toHaveCount(1);
  if (state) await expect(row).toContainText(state);
  const raw = async () =>
    (
      await messages(page)
        .locator('[aria-label="Updates"], article[aria-label="Tester"], article[aria-label="Helper"]')
        .allTextContents()
    ).join('\n');
  expect(await raw()).not.toMatch(/Helpers:|\(thread /);
  if (!href) return;
  await row.getByRole('button').first().click();
  await expect(ledgers(page).getByRole('link', { name: 'Open thread' })).toHaveAttribute(
    'href',
    href,
  );
  expect(await raw()).not.toMatch(/Helpers:|\(thread /);
}

const hangBrief = `Watch the build. ${prompts.hang}`;

test.describe('real wakes fold into the ledger', () => {
  let m: Machine | undefined;
  test.afterEach(() => {
    m?.stop();
    m = undefined;
  });

  test('a helper the person stopped is a Stopped row with an Open thread link', async ({
    page,
  }) => {
    const own = (m = await startAtoll());
    const { helper, tester } = await realWake(own, page, hangBrief, async (helper) => {
      await expect.poll(() => runStatus(own, helper)).toBe('running');
      const run = (await api(own, 'GET', `/api/threads/${helper}`)).run_id;
      await api(own, 'POST', `/api/runs/${run}/stop`);
    });
    await expectFolded(page, 'Stopped', `/bots/${tester}/threads/${helper}`);
  });

  test('a helper whose run failed is a Failed row with an Open thread link', async ({ page }) => {
    m = await startAtoll();
    const { helper, tester } = await realWake(
      m,
      page,
      `Look into the flake. ${prompts.none}`,
      async () => {},
    );
    await expectFolded(page, 'Failed', `/bots/${tester}/threads/${helper}`);
  });

  test('a helper out of usage is a Failed row with an Open thread link', async ({ page }) => {
    m = await startAtoll({ fakes: ['claude', 'codex'], signedIn: ['claude', 'codex'] });
    m.limit('codex', new Date(Date.now() + 3 * 3600_000));
    const { helper, tester } = await realWake(m, page, hangBrief, async () => {}, {
      provider: 'codex',
    });
    await expectFolded(page, 'Failed', `/bots/${tester}/threads/${helper}`);
  });

  test('a helper whose Bot was deleted folds, with neither the helpers line nor its id', async ({
    page,
  }) => {
    const own = (m = await startAtoll());
    await realWake(own, page, hangBrief, async (helper, tester) => {
      await expect.poll(() => runStatus(own, helper)).toBe('running');
      await api(own, 'DELETE', `/api/bots/${tester}`);
    });
    await expectFolded(page);
  });
});
