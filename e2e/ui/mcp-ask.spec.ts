// An MCP tool's ask names the server and tool in words, not as mcp__server__tool: on the Bot's
// row, in Now and on the approval card. Plays testdata/claude/mcp-ask.jsonl.
import { expect, type Locator, test } from '@playwright/test';
import {
  lines,
  type Machine,
  newBotAPI,
  openNow,
  openThreadAPI,
  sidebarRow,
  startAtoll,
  threadTitle,
  waitStatus,
} from './atoll';

let m: Machine;
test.beforeEach(async () => {
  m = await startAtoll();
});
test.afterEach(() => m?.stop());

const prompt = 'Open an issue on GitHub.';
const ask = 'Wants to use GitHub: create issue';

const rowOf = (pane: Locator, id: string) =>
  pane.getByRole('listitem').filter({ has: pane.page().locator(`a[href$="/threads/${id}"]`) });

async function waiting(name: string) {
  const bot = await newBotAPI(m, name);
  const thread = await openThreadAPI(m, bot.id, prompt);
  await waitStatus(m, thread.id, 'waiting');
  return { bot, thread };
}

test('an approval for mcp__github__create_issue reads Wants to use GitHub: create issue on the Bot row', async ({
  page,
}) => {
  await waiting('Asker');
  await page.goto(`${m.url}/settings`);
  await page.mouse.move(1270, 5);
  await expect.poll(() => lines(sidebarRow(page, 'Asker'))).toEqual(['Asker', ask]);
});

test('the same approval reads Asker · Wants to use GitHub: create issue in Now', async ({
  page,
}) => {
  const { thread } = await waiting('Asker');
  await page.goto(`${m.url}/settings`);
  const now = await openNow(page);
  await page.mouse.move(1270, 5);
  await expect
    .poll(() => lines(rowOf(now, thread.id)))
    .toEqual([threadTitle(prompt), `Asker · ${ask}`]);
});

test('the same approval reads Wants to use GitHub: create issue on the approval card', async ({
  page,
}) => {
  const { bot, thread } = await waiting('Asker');
  await page.goto(`${m.url}/bots/${bot.id}/threads/${thread.id}`);
  const card = page
    .getByRole('log', { name: 'Messages' })
    .getByRole('group', { name: 'Approval' });
  await expect(card).toHaveCount(1);
  await expect(card).toContainText(ask);
});
