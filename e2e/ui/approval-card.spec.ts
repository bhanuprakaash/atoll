// The approval card: compact and quiet, answered by writing in the chat; red only for real danger.
// Contract: e2e/approval-card-contract.md.
import { join } from 'node:path';
import { DatabaseSync } from 'node:sqlite';
import { expect, type Locator, type Page, test } from '@playwright/test';
import {
  api,
  type Machine,
  newBotAPI,
  openThreadAPI,
  prompts,
  sidebarRow,
  startAtoll,
} from './atoll';

let m: Machine;
test.beforeEach(async () => {
  m = await startAtoll();
});
test.afterEach(() => m?.stop());

const push = 'git push origin HEAD';
const tmp = 'rm -rf /tmp/codex-gotmp-161';
const run = (...cmds: string[]) => `Run commands. ${JSON.stringify(cmds)}`;

async function openBot(page: Page, name: string) {
  const bot = await newBotAPI(m, name, 'claude', { look: 'whale' });
  await page.goto(m.url);
  await sidebarRow(page, name).click();
  await expect(page.getByRole('heading', { name: `What should ${name} work on?` })).toBeVisible();
  return bot;
}

async function send(page: Page, text: string) {
  await page.getByRole('textbox', { name: 'Message' }).fill(text);
  await page.getByRole('button', { name: 'Send' }).click();
}

async function waitStatus(threadID: string, status: string) {
  await expect
    .poll(async () => (await api(m, 'GET', `/api/threads/${threadID}`)).run_status, {
      timeout: 15_000,
      intervals: [100],
    })
    .toBe(status);
}

// reds lists what inside root is drawn red: a text, background, border, or icon colour with hue
// near 0 and a clear red lead. A creature is a Bot's own colour, not state: only its needs-you
// pulse counts.
function reds(root: Locator): Promise<string[]> {
  return root.evaluate((el) => {
    const ctx = document.createElement('canvas').getContext('2d', { willReadFrequently: true })!;
    const rgba = (css: string): [number, number, number, number] | null => {
      if (!css || css === 'none' || css === 'transparent') return null;
      ctx.clearRect(0, 0, 1, 1);
      ctx.fillStyle = '#000';
      ctx.fillStyle = css;
      ctx.fillRect(0, 0, 1, 1);
      const d = ctx.getImageData(0, 0, 1, 1).data;
      return [d[0], d[1], d[2], d[3] / 255];
    };
    const red = (css: string) => {
      const c = rgba(css);
      if (!c || c[3] < 0.2) return false;
      const [r, g, b] = c;
      const lead = r - Math.max(g, b);
      if (lead < 12) return false;
      const hue = (60 * ((g - b) / (r - Math.min(g, b)) + 6)) % 360;
      return hue <= 10 || hue >= 340;
    };
    const found: string[] = [];
    for (const e of [el, ...el.querySelectorAll('*')]) {
      if (e.closest('svg[role="img"]') && !e.matches('.creature-pulse')) continue;
      const cs = getComputedStyle(e);
      const box = e.getBoundingClientRect();
      if (cs.display === 'none' || cs.visibility === 'hidden' || !box.width || !box.height)
        continue;
      const hit = (what: string, css: string) =>
        red(css) &&
        found.push(
          `${e.tagName.toLowerCase()} ${what} ${css} "${(e.textContent ?? '').trim().slice(0, 40)}"`,
        );
      if ([...e.childNodes].some((n) => n.nodeType === 3 && n.textContent?.trim()))
        hit('color', cs.color);
      hit('background', cs.backgroundColor);
      for (const side of ['Top', 'Right', 'Bottom', 'Left'] as const) {
        if (parseFloat(cs[`border${side}Width`]) > 0 && cs[`border${side}Style`] !== 'none') {
          hit(`border-${side.toLowerCase()}`, cs[`border${side}Color`]);
        }
      }
      if (e instanceof SVGElement && e.tagName.toLowerCase() !== 'svg') {
        hit('fill', cs.fill);
        hit('stroke', cs.stroke);
      }
    }
    return found;
  });
}

const redIn = async (l: Locator) => (await reds(l)).length > 0;

test('a one-line command is a compact card: the command in a code line, Allow and Deny on its row', async ({
  page,
}) => {
  await page.setViewportSize({ width: 1280, height: 800 });
  await openBot(page, 'Kelp');
  const log = page.getByRole('log', { name: 'Messages' });
  await send(page, run(push));

  const card = log.getByRole('group', { name: 'Approval' });
  await expect(card).toHaveCount(1);
  const code = card.locator('code');
  await expect(code).toHaveText(push);
  const [c, line, allow, deny] = await Promise.all([
    card.boundingBox(),
    code.boundingBox(),
    card.getByRole('button', { name: 'Allow', exact: true }).boundingBox(),
    card.getByRole('button', { name: 'Deny', exact: true }).boundingBox(),
  ]);
  expect(c!.height).toBeLessThanOrEqual(72);
  for (const b of [allow!, deny!]) {
    const middle = b.y + b.height / 2;
    expect(middle).toBeGreaterThan(line!.y);
    expect(middle).toBeLessThan(line!.y + line!.height);
  }
  await expect(card.getByRole('button', { name: /^Show (all|less)$/ })).toHaveCount(0);
  await test.info().attach('card', { body: await card.screenshot(), contentType: 'image/png' });
});

test('the card is quiet: nothing red at rest, Deny red only on hover and keyboard focus, the thread says Needs you', async ({
  page,
}) => {
  await openBot(page, 'Moss');
  const log = page.getByRole('log', { name: 'Messages' });
  await send(page, run(push));
  const allow = log.getByRole('button', { name: 'Allow', exact: true });
  const deny = log.getByRole('button', { name: 'Deny', exact: true });
  await expect(deny).toHaveCount(1);

  await page.mouse.move(0, 0);
  expect(await reds(log)).toEqual([]);
  await expect(page.locator('header')).toContainText('Needs you');
  await expect(sidebarRow(page, 'Moss').getByRole('img', { name: /, needs you$/ })).toBeVisible();

  await allow.hover();
  expect(await redIn(allow)).toBe(false);
  await deny.hover();
  await expect.poll(() => redIn(deny)).toBe(true);
  await page.mouse.move(0, 0);
  await expect.poll(() => redIn(deny)).toBe(false);

  await allow.focus();
  await page.keyboard.press('Tab');
  await expect(deny).toBeFocused();
  await expect.poll(() => redIn(deny)).toBe(true);
  await page.keyboard.press('Shift+Tab');
  await expect(allow).toBeFocused();
  await expect.poll(() => redIn(deny)).toBe(false);
  await expect(log.getByRole('group', { name: 'Approval' })).not.toContainText(/needs you/i);
});

test('a long command wraps inside its code line, Show all opens it and Show less closes it', async ({
  page,
}) => {
  await page.setViewportSize({ width: 1280, height: 800 });
  await openBot(page, 'Reed');
  const log = page.getByRole('log', { name: 'Messages' });
  const long = `go test ./... -run '${Array.from({ length: 30 }, (_, i) => `TestApprovalCardCase${i}`).join('|')}' -count=1`;
  await send(page, run(long));

  const card = log.getByRole('group', { name: 'Approval' });
  const code = card.locator('code');
  await expect(code).toHaveText(long);
  const clamped = (await card.boundingBox())!.height;
  expect(clamped).toBeLessThanOrEqual(120);

  await card.getByRole('button', { name: 'Show all' }).click();
  await expect(card.getByRole('button', { name: 'Show less' })).toBeVisible();
  await expect(code).toHaveText(long);
  expect((await card.boundingBox())!.height).toBeGreaterThan(clamped + 20);

  await card.getByRole('button', { name: 'Show less' }).click();
  await expect(card.getByRole('button', { name: 'Show all' })).toBeVisible();
  expect(Math.abs((await card.boundingBox())!.height - clamped)).toBeLessThan(2);
  await expect(card.getByRole('button', { name: 'Allow', exact: true })).toBeVisible();
});

test('an edit shows its file in the code line and keeps its diff toggle', async ({ page }) => {
  await openBot(page, 'Fern');
  const log = page.getByRole('log', { name: 'Messages' });
  await send(page, prompts.write);

  const card = log.getByRole('group', { name: 'Approval' });
  await expect(card.locator('code')).toContainText('hello.txt');
  await card.getByRole('button', { name: 'Show diff' }).click();
  await expect(card.locator('pre')).toContainText('hi');
  await card.getByRole('button', { name: 'Hide diff' }).click();
  await expect(card.locator('pre')).toHaveCount(0);
});

test('writing in the chat answers the card: it collapses to You replied instead and the run goes on', async ({
  page,
}) => {
  const bot = await openBot(page, 'Ash');
  const log = page.getByRole('log', { name: 'Messages' });
  const card = log.getByRole('group', { name: 'Approval' });
  await send(page, run(push, tmp));
  await expect(log.getByRole('button', { name: 'Deny', exact: true })).toHaveCount(1);

  const reply = 'Push to a branch instead.';
  await send(page, reply);
  await expect(log.getByText('You replied instead')).toHaveCount(1);
  await expect(log.getByText(reply, { exact: true })).toHaveCount(1);
  await expect(page.getByText(/run going|could not|refused/i)).toHaveCount(0);

  // The same run asks about the next command, with the first card gone.
  await expect(card).toHaveCount(1);
  await expect(card.locator('code')).toHaveText(tmp);
  await card.getByRole('button', { name: 'Allow', exact: true }).click();
  await expect(card).toHaveCount(0);
  await expect(
    log.getByText(new RegExp(`${push}: denied, told: .*${reply}`)).first(),
  ).toBeVisible();
  await expect(log.getByText(`${tmp}: ran`).first()).toBeVisible();

  const [t] = await api(m, 'GET', `/api/bots/${bot.id}/threads`);
  await waitStatus(t.id, 'done');
  await page.reload();
  await expect(log.getByText('You replied instead')).toHaveCount(1);
  await expect(card).toHaveCount(0);
});

// A helper's approval, as helpers.spec.ts makes one: the helper plays testdata/claude/helper-report.jsonl.
const reportPrompt = 'Run the checks, then report back.';
const escaped = (s: string) =>
  `"${Array.from(s, (c) => `\\u${c.charCodeAt(0).toString(16).padStart(4, '0')}`).join('')}"`;

// handOff has Poteto hand the report prompt to Tester, whose approval then waits in Poteto's thread.
async function handOff(page: Page) {
  const poteto = await newBotAPI(m, 'Poteto', 'claude', { look: 'whale' });
  const tester = await newBotAPI(m, 'Tester', 'claude', { look: 'shark' });
  await api(m, 'PUT', `/api/bots/${poteto.id}`, { delegates: [tester.id] });
  await page.goto(m.url);
  await sidebarRow(page, 'Poteto').click();
  await send(
    page,
    `Use MCP tools. [{"server":"atoll","tool":"delegate","args":{"bot":"Tester","brief":${escaped(reportPrompt)}}}]`,
  );
  await expect(page).toHaveURL(new RegExp(`/bots/${poteto.id}/threads/[^/]+$`));
  const region = page.getByRole('region', { name: 'Helpers' });
  await expect(region.getByRole('button', { name: 'Allow', exact: true })).toBeVisible();
  return { tester, region };
}

test('a helper approval waiting in the boss thread is not red anywhere', async ({ page }) => {
  const { region } = await handOff(page);
  await expect(region).toContainText('wants to run');
  await page.mouse.move(0, 0);
  expect(await reds(page.locator('body'))).toEqual([]);
});

test('a helper approval in the boss thread is answered by writing there', async ({ page }) => {
  const { tester, region } = await handOff(page);
  const log = page.getByRole('log', { name: 'Messages' });

  const reply = 'Run the linter instead.';
  await send(page, reply);
  await expect(log.getByText('You replied instead')).toHaveCount(1);
  await expect(log.getByText(reply, { exact: true })).toHaveCount(1);
  await expect(region.getByRole('button', { name: 'Allow', exact: true })).toHaveCount(0);
  await expect(page.getByText(/run going|could not|refused/i)).toHaveCount(0);

  const [helper] = await api(m, 'GET', `/api/bots/${tester.id}/threads`);
  await expect
    .poll(async () => (await api(m, 'GET', `/api/threads/${helper.id}`)).approval ?? null)
    .toBeNull();
  await waitStatus(helper.id, 'done');
  await page.reload();
  await expect(log.getByText('You replied instead')).toHaveCount(1);
});

test('while an approval waits nothing on screen is red: thread, sidebar, header, creature', async ({
  page,
}) => {
  await openBot(page, 'Tide');
  await send(page, run(push));
  await expect(
    page.getByRole('log', { name: 'Messages' }).getByRole('button', { name: 'Deny', exact: true }),
  ).toHaveCount(1);
  await expect(page.locator('header')).toContainText('Needs you');
  await page.mouse.move(0, 0);
  expect(await reds(page.locator('body'))).toEqual([]);
});

test('an approval in another thread: the sidebar, the dropped island, its count and the list are not red', async ({
  page,
}) => {
  const asker = await newBotAPI(m, 'Asker', 'claude', { look: 'whale' });
  await openBot(page, 'Elsewhere');
  const waiting = await openThreadAPI(m, asker.id, prompts.write);
  await waitStatus(waiting.id, 'waiting');
  const header = page.locator('header');
  const body = page.locator('body');

  await expect(header.getByRole('button', { name: 'Allow', exact: true })).toBeVisible();
  await expect(page.getByText('Needs you', { exact: true }).first()).toBeVisible();
  await page.mouse.move(0, 0);
  expect(await reds(body)).toEqual([]);

  await page.keyboard.press('Escape');
  await expect(header.getByRole('button', { name: 'Allow', exact: true })).toHaveCount(0);
  await expect(header.getByRole('button', { name: /^Notifications, 1 waiting/ })).toBeVisible();
  expect(await reds(body)).toEqual([]);

  await header.getByRole('button', { name: /^Notifications/ }).click();
  const list = page.getByRole('dialog', { name: 'Notifications' });
  await expect(list.getByRole('list', { name: 'Needs you' }).getByRole('listitem')).toHaveCount(1);
  await page.mouse.move(0, 0);
  expect(await reds(body)).toEqual([]);
});

test('a folded group with a Bot that needs you shows no red', async ({ page }) => {
  const nav = page.getByRole('navigation', { name: 'Bots' });
  const group = nav.getByRole('group', { name: 'Ops', exact: true });
  const pager = await newBotAPI(m, 'Pager', 'claude', { look: 'whale' });
  await page.goto(m.url);
  await nav.getByRole('button', { name: 'New', exact: true }).click();
  await page.getByRole('menuitem', { name: 'New group' }).click();
  const field = nav.getByRole('textbox', { name: 'Group name' });
  await field.fill('Ops');
  await field.press('Enter');
  await expect(group).toBeVisible();
  await sidebarRow(page, 'Pager').click({ button: 'right' });
  await page.getByRole('menuitem', { name: 'Move to group' }).click();
  await page.getByRole('menuitem', { name: 'Ops', exact: true }).click();
  await group.getByRole('heading').hover();
  await group.getByRole('button', { name: 'Ops', exact: true }).click();
  await expect(group.getByRole('button', { name: 'Ops', exact: true })).toHaveAttribute(
    'aria-expanded',
    'false',
  );

  const waiting = await openThreadAPI(m, pager.id, prompts.write);
  await waitStatus(waiting.id, 'waiting');
  await expect(group).toHaveAttribute('data-marker', 'needs-you');
  await page.mouse.move(0, 0);
  expect(await reds(group)).toEqual([]);
});

test('a closed MR is not red', async ({ page }) => {
  const bot = await newBotAPI(m, 'Pilot', 'claude', { look: 'whale' });
  const url = 'https://github.com/o/r/pull/12';
  const thread = await openThreadAPI(
    m,
    bot.id,
    `Use MCP tools. [{"server":"atoll","tool":"track_mr","args":{"url":"${url}","title":"Closed change"}}]`,
  );
  await waitStatus(thread.id, 'done');
  // What the PR watcher writes for a closed PR, set in the server's database as it would.
  const db = new DatabaseSync(join(m.data, 'atoll.db'));
  try {
    db.prepare('UPDATE threads SET mr_state = ? WHERE id = ?').run('closed', thread.id);
  } finally {
    db.close();
  }
  expect(await api(m, 'GET', `/api/threads/${thread.id}`)).toMatchObject({
    mr_url: url,
    mr_state: 'closed',
  });
  await page.goto(`${m.url}/bots/${bot.id}/threads/${thread.id}`);
  const chip = page.getByRole('link', { name: '#12' });
  await expect(chip).toBeVisible();
  await page.mouse.move(0, 0);
  expect(await reds(page.locator('body'))).toEqual([]);
});

// Guard: a failure keeps its red, so the rule above is not met by removing red everywhere.
test('a failed run keeps its red icon and nothing else in the header is red', async ({ page }) => {
  const bot = await newBotAPI(m, 'Wren', 'claude', { look: 'whale' });
  const failed = await openThreadAPI(m, bot.id, prompts.none);
  await waitStatus(failed.id, 'failed');
  await page.goto(`${m.url}/bots/${bot.id}/threads/${failed.id}`);
  const log = page.getByRole('log', { name: 'Messages' });
  await expect(log.getByText(/^(Run failed|Didn't run)/)).toBeVisible();
  await page.mouse.move(0, 0);
  const inLog = await reds(log);
  expect(inLog.length).toBeGreaterThan(0);
  expect(inLog.every((r) => /^(circle|path) (fill|stroke) /.test(r))).toBe(true);
  expect(await reds(page.locator('header'))).toEqual([]);
});
