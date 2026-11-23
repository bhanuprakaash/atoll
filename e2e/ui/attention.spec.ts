// Everything that needs the person in one place, said once: a Bot row says what its Bot asks or
// that its run failed; Now lists failed threads under Needs you, a row has one marker at its right
// end and the words "Needs you" are only the heading; a pinned thread that replied shows the blue
// dot and the pin on its tab.
import { expect, type Locator, type Page, test } from '@playwright/test';
import {
  api,
  lines,
  type Machine,
  marks,
  newBotAPI,
  openNow,
  openThread,
  openThreadAPI,
  prompts,
  sidebarRow,
  startAtoll,
  threadTab,
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

// What the Bot asks: the approval card's verb with the short target of the live verb, for
// testdata/claude/run-approve.jsonl (Write /work/hello.txt) and helper-report.jsonl (Bash make check).
const editAsk = 'Wants to edit hello.txt';
const checkAsk = 'Wants to run make check';
// A time today: the locale's clock, `6:21 AM` or `18:21`.
const time = String.raw`\d{1,2}:\d{2}(\s?[AP]M)?`;

const section = (pane: Locator, name: string) => pane.getByRole('list', { name, exact: true });
const rowOf = (pane: Locator, id: string) =>
  pane.getByRole('listitem').filter({ has: pane.page().locator(`a[href$="/threads/${id}"]`) });
const box = async (l: Locator) => (await l.boundingBox())!;
const away = (page: Page) => page.mouse.move(1270, 5);
const stop = (t: { run: { id: string } }) => api(m, 'POST', `/api/runs/${t.run.id}/stop`);
const colours = async (el: Locator) => (await marks(el)).map((d) => d.colour);

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

async function helperAsks(id: string) {
  await expect
    .poll(async () => (await api(m, 'GET', `/api/threads/${id}`)).approval?.id ?? '', {
      timeout: 15_000,
    })
    .not.toBe('');
}

test('a Bot row that needs you is its name over what the Bot asks, with the light dot; a failed Bot row reads Run failed · time with the red dot', async ({
  page,
}) => {
  const asker = await newBotAPI(m, 'Asker');
  const breaker = await newBotAPI(m, 'Breaker');
  const waiting = await openThreadAPI(m, asker.id, prompts.write);
  const failed = await openThreadAPI(m, breaker.id, prompts.none);
  await waitStatus(m, waiting.id, 'waiting');
  await waitStatus(m, failed.id, 'failed');

  await page.goto(`${m.url}/settings`);
  await away(page);
  const asking = sidebarRow(page, 'Asker');
  await expect.poll(() => lines(asking)).toEqual(['Asker', editAsk]);
  await expect.poll(() => colours(asking)).toContain('light');
  expect(await colours(asking)).not.toContain('red');

  const failing = sidebarRow(page, 'Breaker');
  await expect
    .poll(() => lines(failing))
    .toEqual(['Breaker', expect.stringMatching(/^Run failed · /)]);
  expect((await lines(failing))[1]).toMatch(new RegExp(`^Run failed · ${time}$`));
  await expect.poll(() => colours(failing)).toContain('red');
  expect(await colours(failing)).not.toContain('light');
});

test('Now lists a failed thread under Needs you, with the red dot and the note Bot · Run failed · time', async ({
  page,
}) => {
  const bot = await newBotAPI(m, 'Breaker');
  const failed = await openThreadAPI(m, bot.id, prompts.none);
  await waitStatus(m, failed.id, 'failed');

  await page.goto(`${m.url}/settings`);
  const now = await openNow(page);
  await away(page);
  const row = rowOf(section(now, 'Needs you'), failed.id);
  await expect(row).toHaveCount(1);
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
    const now = await openNow(page);
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
  const { poteto } = await makeBoss(m, ['Tester', 'Checker']);
  await page.goto(m.url);
  const { boss, helpers } = await handOff(m, page, [['Tester', prompts.hang]]);
  await helperStep(helpers[0].id, 'Running make build');
  await expect.poll(() => runStatus(m, boss)).toBe('done');
  const title: string = (await api(m, 'GET', `/api/threads/${boss}`)).title;

  const now = await openNow(page);
  await away(page);
  const row = rowOf(now, boss);
  await expect(row).toHaveCount(1);
  await expect.poll(() => lines(row)).toEqual([title, 'Poteto · Tester: Running make build']);
  await expect(now.locator('[data-line="helpers"]')).toHaveCount(0);

  await openThread(page, m, poteto.id, boss);
  await send(page, delegate([['Checker', reportPrompt]]));
  const checker = (await helperThreads(m, boss, 2)).find((h) => h.id !== helpers[0].id)!;
  await helperAsks(checker.id);
  await expect.poll(() => runStatus(m, boss)).toBe('done');
  await openNow(page);
  await away(page);
  await expect.poll(() => lines(row)).toEqual([title, `Poteto · Checker: ${checkAsk}`]);
  await expect(row).not.toContainText('Needs you');
  await expect(now.locator('[data-line="helpers"]')).toHaveCount(0);
});

test('a pinned thread that replied shows only the blue dot in Now, and the blue dot, bold title and pin on its tab', async ({
  page,
}) => {
  const bot = await newBotAPI(m, 'Newsy');
  const other = await seen(bot.id, prompts.pong);
  const replied = await openThreadAPI(m, bot.id, prompts.keep);
  await waitStatus(m, replied.id, 'done');
  await api(m, 'POST', `/api/threads/${replied.id}/pin`);

  await page.goto(`${m.url}/settings`);
  const now = await openNow(page);
  await away(page);
  const inNow = rowOf(section(now, 'Replied'), replied.id);
  await expect(inNow).toHaveCount(1);
  await oneMarkAtEnd(inNow, 'blue');
  await expect(inNow.getByRole('img', { name: 'Pinned' })).toHaveCount(0);

  await openThread(page, m, bot.id, other.id);
  await away(page);
  const title = threadTitle(prompts.keep);
  const tab = threadTab(page, title);
  await expect.poll(() => colours(tab)).toEqual(['blue']);
  await expect(tab.getByRole('img', { name: 'Pinned', exact: true })).toBeVisible();
  const weight = await tab
    .getByText(title, { exact: true })
    .evaluate((el) => Number(getComputedStyle(el).fontWeight));
  expect(weight).toBeGreaterThanOrEqual(600);
});
