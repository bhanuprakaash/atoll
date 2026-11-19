// Everything that needs the person in one place, said once: in a Bot's pane failed threads sit
// with Needs you, a row says what it asks, pinned and the rest are one list, Threads; the helper
// line names the helper that acted last; Now lists failed threads under Needs you, a row has one
// marker at its right end and the words "Needs you" are only the heading.
import { expect, type Locator, type Page, test } from '@playwright/test';
import {
  api,
  lines,
  type Machine,
  marks,
  newBotAPI,
  nowPane,
  openThreadAPI,
  prompts,
  showPane,
  startAtoll,
  threadTitle,
  waitStatus,
} from './atoll';
import {
  delegate,
  handOff,
  helperThreads,
  makeBoss,
  reportPrompt,
  runStatus,
  send,
} from './helpers-fixture';

let m: Machine;
test.beforeEach(async () => {
  m = await startAtoll();
});
test.afterEach(() => m?.stop());

// What the Bot asks: the approval card's words for testdata/claude/run-approve.jsonl (Write
// hello.txt) and helper-report.jsonl (Bash make check).
const editAsk = 'Wants to edit hello.txt';
const checkAsk = 'Wants to run make check';
// The helper plays testdata/claude/helper-loop.jsonl: one gh pr view per PR, then it hangs.
const loopPrompt = 'Check each open pull request.';
// A time today: the locale's clock, `6:21 AM` or `18:21`.
const time = String.raw`\d{1,2}:\d{2}(\s?[AP]M)?`;

const section = (pane: Locator, name: string) => pane.getByRole('list', { name, exact: true });
const rowOf = (pane: Locator, id: string) =>
  pane.getByRole('listitem').filter({ has: pane.page().locator(`a[href$="/threads/${id}"]`) });
const sections = (pane: Locator) =>
  pane.getByRole('list').evaluateAll((els) => els.map((el) => el.getAttribute('aria-label') ?? ''));
const box = async (l: Locator) => (await l.boundingBox())!;
const away = (page: Page) => page.mouse.move(1270, 5);
const stop = (t: { run: { id: string } }) => api(m, 'POST', `/api/runs/${t.run.id}/stop`);

// seen makes a thread whose run is done and that the person has opened since.
async function seen(botID: string, text: string) {
  const t = await openThreadAPI(m, botID, text);
  await waitStatus(m, t.id, 'done');
  await api(m, 'POST', `/api/threads/${t.id}/opened`);
  return t;
}

// oneMarkAtEnd: the row shows exactly one coloured dot, of the given colour, at its right end,
// right of the title.
async function oneMarkAtEnd(row: Locator, colour: 'light' | 'red' | 'blue') {
  await expect.poll(async () => (await marks(row)).map((d) => d.colour)).toEqual([colour]);
  const [mark] = await marks(row);
  const r = await box(row);
  const title = await box(row.getByRole('link'));
  expect(mark.right).toBeGreaterThanOrEqual(r.x + r.width - 20);
  expect(mark.x).toBeGreaterThan(title.x + title.width);
}

async function helperStep(id: string, step: string) {
  await expect
    .poll(async () => (await api(m, 'GET', `/api/threads/${id}`)).step, { timeout: 15_000 })
    .toBe(step);
}

// lastActivity is the thread's last activity as the sidebar gets it.
async function lastActivity(id: string): Promise<string> {
  const bots: { threads?: { id: string; last_activity: string }[] }[] = await api(
    m,
    'GET',
    '/api/bots',
  );
  return bots.flatMap((b) => b.threads ?? []).find((t) => t.id === id)!.last_activity;
}

async function helperAsks(id: string) {
  await expect
    .poll(async () => (await api(m, 'GET', `/api/threads/${id}`)).approval?.id ?? '', {
      timeout: 15_000,
    })
    .not.toBe('');
}

test('the Bot pane has Needs you (needs-you and failed), Running, Threads (pinned first with the pin, then by last activity) and Closed folded; an empty section is left out', async ({
  page,
}) => {
  const bot = await newBotAPI(m, 'Many');
  const waiting = await openThreadAPI(m, bot.id, prompts.write);
  const failed = await openThreadAPI(m, bot.id, prompts.none);
  const busy = await openThreadAPI(m, bot.id, prompts.hang);
  try {
    await waitStatus(m, waiting.id, 'waiting');
    await waitStatus(m, failed.id, 'failed');
    await waitStatus(m, busy.id, 'running');
    const [a, b, c, d] = [
      await seen(bot.id, prompts.pong),
      await seen(bot.id, prompts.pong),
      await seen(bot.id, prompts.pong),
      await seen(bot.id, prompts.pong),
    ];
    const closed = await seen(bot.id, prompts.keep);
    await api(m, 'POST', `/api/threads/${closed.id}/close`);
    await api(m, 'POST', `/api/threads/${a.id}/pin`);
    await api(m, 'POST', `/api/threads/${b.id}/pin`);

    const pane = await showPane(page, m, bot.id, 'Many');
    await away(page);
    await expect.poll(() => sections(pane)).toEqual(['Needs you', 'Running', 'Threads']);
    const needs = section(pane, 'Needs you');
    await expect(needs.getByRole('listitem')).toHaveCount(2);
    await expect(rowOf(needs, waiting.id)).toHaveAttribute('data-marker', 'needs-you');
    await expect(rowOf(needs, failed.id)).toHaveAttribute('data-marker', 'failed');
    await expect(section(pane, 'Running').getByRole('listitem')).toHaveCount(1);
    await expect(rowOf(section(pane, 'Running'), busy.id)).toHaveCount(1);

    const threads = section(pane, 'Threads').getByRole('listitem');
    const order = () =>
      threads.evaluateAll((els) =>
        els.map((el) =>
          el.querySelector('a[href*="/threads/"]')!.getAttribute('href')!.split('/').pop(),
        ),
      );
    await expect.poll(async () => (await order()).length).toBe(4);
    const ids = await order();
    expect(ids.slice(0, 2).sort()).toEqual([a.id, b.id].sort());
    expect(ids.slice(2)).toEqual([d.id, c.id]);
    for (const pinned of [a, b])
      await expect(
        rowOf(pane, pinned.id).getByRole('img', { name: 'Pinned', exact: true }),
      ).toBeVisible();
    for (const rest of [c, d])
      await expect(rowOf(pane, rest.id).getByRole('img', { name: 'Pinned' })).toHaveCount(0);

    const fold = pane.getByRole('button', { name: 'Closed · 1', exact: true });
    await expect(fold).toHaveAttribute('aria-expanded', 'false');
    const below = await box(section(pane, 'Threads'));
    expect((await box(fold)).y).toBeGreaterThanOrEqual(below.y + below.height - 1);

    const quiet = await newBotAPI(m, 'Quiet');
    await seen(quiet.id, prompts.pong);
    const calm = await showPane(page, m, quiet.id, 'Quiet');
    await expect.poll(() => sections(calm)).toEqual(['Threads']);
  } finally {
    await stop(busy);
  }
});

test('a needs-you row is its title over what the Bot asks, marked by the light dot alone; a failed row reads Run failed · time with the red dot', async ({
  page,
}) => {
  const bot = await newBotAPI(m, 'Asker');
  const waiting = await openThreadAPI(m, bot.id, prompts.write);
  const failed = await openThreadAPI(m, bot.id, prompts.none);
  await waitStatus(m, waiting.id, 'waiting');
  await waitStatus(m, failed.id, 'failed');

  const pane = await showPane(page, m, bot.id, 'Asker');
  await away(page);
  const asking = rowOf(pane, waiting.id);
  await expect(asking).toHaveAttribute('data-marker', 'needs-you');
  await expect.poll(() => lines(asking)).toEqual([threadTitle(prompts.write), editAsk]);
  await oneMarkAtEnd(asking, 'light');

  const failing = rowOf(pane, failed.id);
  await expect(failing).toHaveAttribute('data-marker', 'failed');
  await expect
    .poll(() => lines(failing))
    .toEqual([threadTitle(prompts.none), expect.stringMatching(/^Run failed · /)]);
  expect((await lines(failing))[1]).toMatch(new RegExp(`^Run failed · ${time}$`));
  await oneMarkAtEnd(failing, 'red');
});

test('the helper line shows the helpers’ creatures, then only the name and step of the helper that acted last; a helper that needs you shows its name and Needs you', async ({
  page,
}) => {
  const { poteto } = await makeBoss(m, ['Tester', 'Reviewer', 'Checker']);
  await page.goto(m.url);
  const { boss, helpers } = await handOff(m, page, [['Tester', prompts.hang]]);
  await helperStep(helpers[0].id, 'Running make build');
  await send(page, delegate([['Reviewer', loopPrompt]]));
  const reviewer = (await helperThreads(m, boss, 2)).find((h) => h.id !== helpers[0].id)!;
  await helperStep(reviewer.id, 'Running gh pr');
  expect(Date.parse(await lastActivity(reviewer.id))).toBeGreaterThan(
    Date.parse(await lastActivity(helpers[0].id)),
  );

  const pane = await showPane(page, m, poteto.id, 'Poteto');
  const line = pane.locator('[data-line="helpers"]');
  await expect(line).toHaveCount(1);
  await expect(line.locator('svg[role="img"]')).toHaveCount(2);
  await expect(line).toContainText('Reviewer');
  await expect(line).toContainText('Running gh pr');
  await expect(line).not.toContainText('Tester');
  const step = line.getByText('Running gh pr');
  expect(await step.evaluate((el) => el.scrollWidth <= el.clientWidth)).toBe(true);

  await page.goto(`${m.url}/bots/${poteto.id}/threads/${boss}`);
  await send(page, delegate([['Checker', reportPrompt]]));
  const checker = (await helperThreads(m, boss, 3)).find(
    (h) => h.id !== helpers[0].id && h.id !== reviewer.id,
  )!;
  await helperAsks(checker.id);
  await showPane(page, m, poteto.id, 'Poteto');
  await expect(line.locator('svg[role="img"]')).toHaveCount(3);
  await expect(line).toContainText('Checker');
  await expect(line).toContainText('Needs you');
  await expect(line).not.toContainText('Tester');
  await expect(line).not.toContainText('Reviewer');
});

test('the pane header shows no Ungrouped; a Bot in a group shows the group; Chief keeps Sets up your Bots', async ({
  page,
}) => {
  const loner = await newBotAPI(m, 'Loner');
  const oak = await newBotAPI(m, 'Oak');
  const frontend = await api(m, 'POST', '/api/groups', { name: 'Frontend' });
  await api(m, 'PUT', `/api/bots/${oak.id}/group`, { group_id: frontend.id });

  const alone = await showPane(page, m, loner.id, 'Loner');
  await expect(alone.getByText('Loner', { exact: true })).toBeVisible();
  await expect(alone).not.toContainText('Ungrouped');

  const search = (pane: Locator) => pane.getByRole('textbox', { name: 'Search threads' });
  const grouped = await showPane(page, m, oak.id, 'Oak');
  const name = grouped.getByText('Frontend', { exact: true });
  await expect(name).toBeVisible();
  expect((await box(name)).y).toBeLessThan((await box(search(grouped))).y);

  const chief = await showPane(page, m, 'chief', 'Chief');
  const tagline = chief.getByText('Sets up your Bots', { exact: true });
  await expect(tagline).toBeVisible();
  expect((await box(tagline)).y).toBeLessThan((await box(search(chief))).y);
});

test('Now lists a failed thread under Needs you, with the red dot and the note Bot · Run failed · time', async ({
  page,
}) => {
  const bot = await newBotAPI(m, 'Breaker');
  const failed = await openThreadAPI(m, bot.id, prompts.none);
  await waitStatus(m, failed.id, 'failed');

  await page.goto(`${m.url}/settings`);
  const now = await nowPane(page);
  await away(page);
  const row = rowOf(section(now, 'Needs you'), failed.id);
  await expect(row).toHaveCount(1);
  await expect(row).toHaveAttribute('data-marker', 'failed');
  await expect
    .poll(() => lines(row))
    .toEqual([threadTitle(prompts.none), expect.stringMatching(/^Breaker · Run failed · /)]);
  expect((await lines(row))[1]).toMatch(new RegExp(`^Breaker · Run failed · ${time}$`));
  await oneMarkAtEnd(row, 'red');
});

test('a Now row has one marker at its right end (light dot, red dot, blue dot or wave dots); Needs you is only the heading; a needs-you note is Bot · ask', async ({
  page,
}) => {
  const asker = await newBotAPI(m, 'Asker');
  const breaker = await newBotAPI(m, 'Breaker');
  const newsy = await newBotAPI(m, 'Newsy');
  const worker = await newBotAPI(m, 'Worker');
  const waiting = await openThreadAPI(m, asker.id, prompts.write);
  const failed = await openThreadAPI(m, breaker.id, prompts.none);
  const replied = await openThreadAPI(m, newsy.id, prompts.keep);
  const busy = await openThreadAPI(m, worker.id, prompts.hang);
  try {
    await waitStatus(m, waiting.id, 'waiting');
    await waitStatus(m, failed.id, 'failed');
    await waitStatus(m, replied.id, 'done');
    await waitStatus(m, busy.id, 'running');

    await page.goto(`${m.url}/settings`);
    const now = await nowPane(page);
    await away(page);
    const asking = rowOf(now, waiting.id);
    await expect(asking).toHaveCount(1);
    await expect(now.getByRole('listitem').filter({ hasText: 'Needs you' })).toHaveCount(0);
    await expect(now.getByText('Needs you')).toHaveCount(1);
    await expect
      .poll(() => lines(asking))
      .toEqual([threadTitle(prompts.write), `Asker · ${editAsk}`]);
    await oneMarkAtEnd(asking, 'light');
    await oneMarkAtEnd(rowOf(now, replied.id), 'blue');

    const working = rowOf(now, busy.id);
    expect(await marks(working)).toEqual([]);
    const wave = working.getByRole('img', { name: 'Working', exact: true });
    await expect(wave).toBeVisible();
    const r = await box(working);
    const w = await box(wave);
    expect(w.x + w.width).toBeGreaterThanOrEqual(r.x + r.width - 20);
    expect(w.x).toBeGreaterThan((await box(working.getByRole('link'))).x);

    await oneMarkAtEnd(rowOf(now, failed.id), 'red');
    await expect(now.getByRole('listitem')).toHaveCount(4);
  } finally {
    await stop(busy);
  }
});

test('a boss thread’s row in Now has no helper line; its note names the helper that acted last and its step, or the helper that needs you', async ({
  page,
}) => {
  await makeBoss(m, ['Tester', 'Checker']);
  await page.goto(m.url);
  const { boss, helpers } = await handOff(m, page, [['Tester', prompts.hang]]);
  await helperStep(helpers[0].id, 'Running make build');
  await expect.poll(() => runStatus(m, boss)).toBe('done');
  const title: string = (await api(m, 'GET', `/api/threads/${boss}`)).title;

  const now = await nowPane(page);
  await away(page);
  const row = rowOf(now, boss);
  await expect(row).toHaveCount(1);
  await expect.poll(() => lines(row)).toEqual([title, 'Poteto · Tester: Running make build']);
  await expect(now.locator('[data-line="helpers"]')).toHaveCount(0);

  await send(page, delegate([['Checker', reportPrompt]]));
  const checker = (await helperThreads(m, boss, 2)).find((h) => h.id !== helpers[0].id)!;
  await helperAsks(checker.id);
  await expect.poll(() => runStatus(m, boss)).toBe('done');
  await away(page);
  await expect.poll(() => lines(row)).toEqual([title, `Poteto · Checker: ${checkAsk}`]);
  await expect(row).not.toContainText('Needs you');
  await expect(now.locator('[data-line="helpers"]')).toHaveCount(0);
});
