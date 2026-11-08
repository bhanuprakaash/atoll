// Helpers in the boss's thread: above the composer, in the header island, and a read-only helper
// thread. Contract: e2e/helpers-ui-contract.md.
import { expect, type Locator, type Page, test } from '@playwright/test';
import { api, type Machine, newBotAPI, prompts, sidebarRow, startAtoll, threadTree } from './atoll';

let m: Machine;
test.beforeEach(async () => {
  m = await startAtoll();
});
test.afterEach(() => m?.stop());

// The helper plays testdata/claude/helper-report.jsonl: it asks to run make check, then reports.
const reportPrompt = 'Run the checks, then report back.';
const reportReply = 'Tester here: the checks pass. Should I update the changelog too?';

// escaped is s as a JSON string with every character \u-escaped, so the boss's prompt does not
// hold the prompt of the recording the helper plays.
const escaped = (s: string) =>
  `"${Array.from(s, (c) => `\\u${c.charCodeAt(0).toString(16).padStart(4, '0')}`).join('')}"`;

const delegate = (handoffs: [bot: string, brief: string][]) =>
  `Use MCP tools. [${handoffs
    .map(
      ([bot, brief]) =>
        `{"server":"atoll","tool":"delegate","args":{"bot":"${bot}","brief":${escaped(brief)}}}`,
    )
    .join(',')}]`;

// handOff makes Poteto, whose delegate list holds the helpers, and has it hand off from a new
// thread in the page. It returns the Bots, the boss thread id and each helper's thread id.
async function handOff(page: Page, handoffs: [bot: string, brief: string][]) {
  const poteto = await newBotAPI(m, 'Poteto', 'claude', { look: 'whale' });
  const helpers: Record<string, { id: string }> = {};
  for (const [name] of handoffs)
    helpers[name] = await newBotAPI(m, name, 'claude', { look: 'shark' });
  await api(m, 'PUT', `/api/bots/${poteto.id}`, {
    delegates: Object.values(helpers).map((h) => h.id),
  });

  await page.goto(m.url);
  await sidebarRow(page, 'Poteto').click();
  await page.getByRole('textbox', { name: 'Message' }).fill(delegate(handoffs));
  await page.getByRole('button', { name: 'Send' }).click();
  await expect(page).toHaveURL(new RegExp(`/bots/${poteto.id}/threads/[^/]+$`));
  const boss = page.url().split('/').pop()!;

  const threads: Record<string, string> = {};
  for (const [name] of handoffs) {
    await expect
      .poll(async () => (await api(m, 'GET', `/api/bots/${helpers[name].id}/threads`)).length)
      .toBe(1);
    threads[name] = (await api(m, 'GET', `/api/bots/${helpers[name].id}/threads`))[0].id;
  }
  return { poteto, helpers, boss, threads };
}

async function runStatus(threadID: string) {
  return (await api(m, 'GET', `/api/threads/${threadID}`)).run_status;
}

const box = async (l: Locator) => (await l.boundingBox())!;

test('two helpers: one handoff line, a line each above the composer and in the island; Stop in the helper thread', async ({
  page,
}) => {
  const { helpers, threads } = await handOff(page, [
    ['Tester', prompts.hang],
    ['Reviewer', prompts.hang],
  ]);
  const log = page.getByRole('log', { name: 'Messages' });
  const handedOff = log.getByText('Handed off to Tester and Reviewer', { exact: true });
  await expect(handedOff).toHaveCount(1);
  await expect(log.getByText('Handed off to Tester', { exact: true })).toHaveCount(0);

  const region = page.getByRole('region', { name: 'Helpers' });
  await expect(region).toContainText('Tester');
  await expect(region).toContainText('Reviewer');
  await expect(region.getByText('Running make build')).toHaveCount(2);
  await expect(region).not.toContainText('Needs you');
  const open = region.getByRole('link', { name: 'Open' });
  await expect(open).toHaveCount(2);
  const composer = page.getByRole('textbox', { name: 'Message' });
  expect((await box(handedOff)).y + (await box(handedOff)).height).toBeLessThanOrEqual(
    (await box(region)).y,
  );
  expect((await box(region)).y + (await box(region)).height).toBeLessThanOrEqual(
    (await box(composer)).y,
  );

  const header = page.locator('header');
  const name = await box(header.getByRole('heading', { level: 1, name: 'Poteto' }));
  for (const label of ['Tester: working', 'Reviewer: working']) {
    const creature = header.getByLabel(label, { exact: true });
    await expect(creature).toBeVisible();
    expect((await box(creature)).x).toBeGreaterThanOrEqual(name.x + name.width);
  }

  await open.and(page.locator(`[href$="/threads/${threads.Tester}"]`)).click();
  await expect(page).toHaveURL(`${m.url}/bots/${helpers.Tester.id}/threads/${threads.Tester}`);
  await expect(page.getByRole('textbox', { name: /^Message/ })).toHaveCount(0);
  await expect(
    page.getByText('Poteto is handling this thread. Talk to Poteto to change it.'),
  ).toBeVisible();
  await page.getByRole('button', { name: 'Stop', exact: true }).click();
  await expect.poll(() => runStatus(threads.Tester)).toBe('stopped');
  await expect.poll(() => runStatus(threads.Reviewer)).toBe('running');
});

test('a helper approval shows under its line, Allow works there, and the report comes from the helper', async ({
  page,
  context,
}) => {
  const { helpers, threads } = await handOff(page, [['Tester', reportPrompt]]);
  const log = page.getByRole('log', { name: 'Messages' });
  await expect(log.getByText('Handed off to Tester', { exact: true })).toHaveCount(1);

  const region = page.getByRole('region', { name: 'Helpers' });
  await expect(region).toContainText('Tester');
  await expect(region).toContainText('Needs you');
  await expect(region).toContainText('make check');
  const allow = region.getByRole('button', { name: 'Allow', exact: true });
  await expect(allow).toHaveCount(1);
  await expect(region.getByRole('button', { name: 'Deny', exact: true })).toHaveCount(1);

  const header = page.locator('header');
  await expect(header.getByLabel('Tester: needs you', { exact: true })).toBeVisible();
  await expect(page.getByRole('button', { name: /^Notifications/ })).toHaveAccessibleName(
    /^Notifications\b.*\b1\b/,
  );
  await expect(sidebarRow(page, 'Poteto')).toContainText('Needs you');

  const helperPage = await context.newPage();
  await helperPage.goto(`${m.url}/bots/${helpers.Tester.id}/threads/${threads.Tester}`);
  const helperAllow = helperPage
    .getByRole('log', { name: 'Messages' })
    .getByRole('button', { name: 'Allow', exact: true });
  await expect(helperAllow).toHaveCount(1);

  await allow.click();
  await expect(allow).toHaveCount(0);
  await expect(helperAllow).toHaveCount(0);
  await expect.poll(() => runStatus(threads.Tester), { timeout: 30_000 }).toBe('done');

  const fromTester = log.getByRole('article', { name: 'Tester' });
  await expect(fromTester).toContainText(reportReply);
  await expect(fromTester.getByRole('img', { name: /^Shark\b/ })).toBeVisible();
  await expect(
    log.getByRole('article', { name: 'You' }).filter({ hasText: reportReply }),
  ).toHaveCount(0);
  await expect(fromTester).not.toContainText('reported back from thread');
  await expect(fromTester).not.toContainText('Done:');
  await expect(log.getByRole('link', { name: 'Open thread' })).toHaveAttribute(
    'href',
    `/bots/${helpers.Tester.id}/threads/${threads.Tester}`,
  );
  await expect(header.getByLabel('Tester: needs you', { exact: true })).toHaveCount(0);
  await expect(page.getByRole('button', { name: /^Notifications/ })).toHaveAccessibleName(
    'Notifications',
  );
});

test('the helper thread is under its Bot in the sidebar and read-only; Allow there leaves the boss thread', async ({
  page,
  context,
}) => {
  const { poteto, helpers, boss, threads } = await handOff(page, [['Tester', reportPrompt]]);
  const bossRegion = page.getByRole('region', { name: 'Helpers' });
  const bossAllow = bossRegion.getByRole('button', { name: 'Allow', exact: true });
  await expect(bossAllow).toHaveCount(1);

  const helperPage = await context.newPage();
  await helperPage.goto(m.url);
  const item = threadTree(helperPage, 'Tester').getByRole('listitem');
  await expect(item).toHaveCount(1);
  await item.getByRole('link').click();
  await expect(helperPage).toHaveURL(new RegExp(`/threads/${threads.Tester}$`));
  const helperLog = helperPage.getByRole('log', { name: 'Messages' });
  await expect(helperLog.getByRole('article', { name: 'Poteto' })).toContainText(reportPrompt);
  await expect(helperLog.getByRole('article', { name: 'You' })).toHaveCount(0);
  await item.hover();
  await expect(item.getByRole('button', { name: 'Close thread' })).toHaveCount(0);

  await expect(helperPage.getByRole('textbox', { name: /^Message/ })).toHaveCount(0);
  await expect(
    helperPage.getByText('Poteto is handling this thread. Talk to Poteto to change it.'),
  ).toBeVisible();
  const stop = helperPage.getByRole('button', { name: 'Stop', exact: true });
  await expect(stop).toBeVisible();

  await helperPage
    .getByRole('log', { name: 'Messages' })
    .getByRole('button', { name: 'Allow', exact: true })
    .click();
  await expect(bossAllow).toHaveCount(0);
  await expect.poll(() => runStatus(threads.Tester), { timeout: 30_000 }).toBe('done');
  await expect(stop).toHaveCount(0);

  const back = "Open Poteto's thread";
  await helperPage
    .getByRole('link', { name: back })
    .or(helperPage.getByRole('button', { name: back }))
    .click();
  await expect(helperPage).toHaveURL(`${m.url}/bots/${poteto.id}/threads/${boss}`);
});
