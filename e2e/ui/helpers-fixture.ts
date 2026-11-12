// What the helpers specs share: Poteto as the boss, its helpers, and the prompts that hand work
// off. Contract: e2e/helpers-ui-contract.md.
import { expect, type Locator, type Page } from '@playwright/test';
import { api, type Machine, newBotAPI, sidebarRow } from './atoll';

// The helper plays testdata/claude/helper-report.jsonl: it asks to run make check, then reports.
export const reportPrompt = 'Run the checks, then report back.';
export const reportReply = 'Tester here: the checks pass. Should I update the changelog too?';

// escaped is s as a JSON string with every character \u-escaped, so the boss's prompt does not
// hold the prompt of the recording the helper plays.
export const escaped = (s: string) =>
  `"${Array.from(s, (c) => `\\u${c.charCodeAt(0).toString(16).padStart(4, '0')}`).join('')}"`;

// A hand-off: the helper Bot's name, the brief, and a registered repo's name for a boss thread
// with no repo.
export type Handoff = [bot: string, brief: string, repo?: string];

// preamble is text before the call, which makes the boss's first message as tall as the test needs.
export const delegate = (handoffs: Handoff[], preamble = '') =>
  `${preamble}Use MCP tools. [${handoffs
    .map(
      ([bot, brief, repo]) =>
        `{"server":"atoll","tool":"delegate","args":{"bot":"${bot}","brief":${escaped(brief)}${
          repo ? `,"repo":"${repo}"` : ''
        }}}`,
    )
    .join(',')}]`;

export type HelperThread = {
  id: string;
  bot_id: string;
  title: string;
  run_status: string;
  step: string;
};

// makeBoss makes Poteto (whale), whose delegate list holds one shark Bot per name, and returns
// them. extra is merged into a helper Bot's create body.
export async function makeBoss(m: Machine, names: string[], extra: Record<string, object> = {}) {
  const poteto = await newBotAPI(m, 'Poteto', 'claude', { look: 'whale' });
  const bots: Record<string, { id: string }> = {};
  for (const name of names)
    bots[name] = await newBotAPI(m, name, 'claude', { look: 'shark', ...extra[name] });
  await api(m, 'PUT', `/api/bots/${poteto.id}`, {
    delegates: Object.values(bots).map((b) => b.id),
  });
  return { poteto, bots };
}

export async function send(page: Page, text: string) {
  await page.getByRole('textbox', { name: /^Message/ }).fill(text);
  await page.getByRole('button', { name: 'Send' }).click();
}

// startThread sends text as the first message of a new thread of the Bot and returns its id.
export async function startThread(page: Page, bot: string, text: string) {
  await sidebarRow(page, bot).click();
  await send(page, text);
  await expect(page).toHaveURL(/\/bots\/[^/]+\/threads\/[^/]+$/);
  return page.url().split('/').pop()!;
}

// helperThreads is the boss thread's helpers as its summary lists them, once there are n.
export async function helperThreads(m: Machine, boss: string, n: number) {
  let list: HelperThread[] = [];
  await expect
    .poll(async () => (list = (await api(m, 'GET', `/api/threads/${boss}`)).helpers).length)
    .toBe(n);
  return list;
}

// handOff sends the hand-offs from the open boss thread, or from a new one when none is open,
// and returns the boss thread id and its helper threads.
export async function handOff(m: Machine, page: Page, handoffs: Handoff[], boss = '') {
  if (boss) await send(page, delegate(handoffs));
  else boss = await startThread(page, 'Poteto', delegate(handoffs));
  return { boss, helpers: await helperThreads(m, boss, handoffs.length) };
}

// bossHandsOff makes the boss and the helper Bots the hand-offs name and hands off from a new thread.
// thread(name) is the id of the first helper thread of that Bot.
export async function bossHandsOff(
  m: Machine,
  page: Page,
  handoffs: Handoff[],
  extra: Record<string, object> = {},
) {
  const { poteto, bots } = await makeBoss(m, [...new Set(handoffs.map(([name]) => name))], extra);
  await page.goto(m.url);
  const { boss, helpers } = await handOff(m, page, handoffs);
  const thread = (name: string) => helpers.find((h) => h.bot_id === bots[name].id)!.id;
  return { poteto, bots, boss, helpers, thread };
}

export async function runStatus(m: Machine, threadID: string) {
  return (await api(m, 'GET', `/api/threads/${threadID}`)).run_status;
}

export const box = async (l: Locator) => (await l.boundingBox())!;

export const tray = (page: Page) => page.getByRole('region', { name: 'Helpers' });
export const popover = (page: Page) => page.getByRole('dialog', { name: 'Helper tasks' });
