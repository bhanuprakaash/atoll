// A run a provider's usage limit stopped is its own kind, not a failure: the thread shows one calm
// line, "<Bot> hit its <provider> limit; back at <time>", with no red, no Retry and none of the
// provider's text. Contract: usage-limit-contract.md.
import { expect, type Locator, test } from '@playwright/test';
import {
  api,
  type Machine,
  newBotAPI,
  prompts,
  recordText,
  seenText,
  sidebarRow,
  startAtoll,
} from './atoll';

const timeZone = 'Asia/Kolkata';
test.use({ locale: 'en-US', timezoneId: timeZone });

let m: Machine;
test.beforeAll(async () => {
  m = await startAtoll({
    fakes: ['claude', 'codex'],
    signedIn: ['claude', 'codex'],
  });
});
test.afterAll(() => m?.stop());

const cases = [
  { provider: 'claude', label: 'Claude', bot: 'Kelp' },
  { provider: 'codex', label: 'Codex', bot: 'Coral' },
] as const;

const providerText =
  /You've hit your|usage limit|Upgrade to Plus|chatgpt\.com|5-hour|resets \d|rate_limit|usageLimitExceeded/i;

// paintedWith lists the elements under (and including) root drawn in colour.
const paintedWith = (root: Locator, colour: string) =>
  root.evaluate((el, c) => {
    const bad: string[] = [];
    for (const e of [el, ...el.querySelectorAll('*')]) {
      const s = getComputedStyle(e);
      for (const v of [s.color, s.stroke, s.fill, s.backgroundColor, s.borderTopColor]) {
        if (v === c) bad.push(e.tagName);
      }
    }
    return bad;
  }, colour);

for (const c of cases) {
  test(`${c.label} limit: one calm line with the reset time, no Retry`, async ({ page }) => {
    const bot = await newBotAPI(m, c.bot, c.provider);
    const failing = await newBotAPI(m, `${c.bot}Ref`, c.provider);
    const log = page.getByRole('log', { name: 'Messages' });
    const open = async (name: string) => {
      await page.goto(m.url);
      await sidebarRow(page, name).click();
      await expect(
        page.getByRole('heading', { name: `What should ${name} work on?` }),
      ).toBeVisible();
    };

    // A plain failed run shows the red the limit line must not have.
    await open(failing.name);
    await page.getByRole('textbox', { name: 'Message' }).fill(prompts.none);
    await page.getByRole('button', { name: 'Send' }).click();
    await expect(log.getByText(/^(Run failed|Didn't run):/)).toBeVisible();
    const danger = await log
      .getByText(/^(Run failed|Didn't run):/)
      .locator('xpath=ancestor::*[.//*[local-name()="svg"]][1]')
      .locator('svg')
      .first()
      .evaluate((e) => getComputedStyle(e).color);

    const resetsAt = new Date(Math.floor((Date.now() + 90 * 60_000) / 1000) * 1000);
    m.limit(c.provider, resetsAt);
    // The page's today is the reset's day, so the time is the clock alone.
    await page.clock.setFixedTime(resetsAt);
    await open(bot.name);
    await recordText(page);
    await page.getByRole('textbox', { name: 'Message' }).fill(prompts.pong);
    await page.getByRole('button', { name: 'Send' }).click();

    const clock = resetsAt.toLocaleTimeString('en-US', {
      hour: 'numeric',
      minute: '2-digit',
      timeZone,
    });
    const back = (time: string) =>
      log.getByText(`${c.bot} hit its ${c.label} limit; back at ${time}`, { exact: true });
    const line = back(clock);
    await expect(line).toBeVisible();
    await expect(log.getByRole('button', { name: 'Retry' })).toHaveCount(0);
    await expect(log.getByText(/Run failed|Didn't run/)).toHaveCount(0);
    expect(await paintedWith(line.locator('xpath=..'), danger)).toEqual([]);
    expect(await seenText(page)).not.toMatch(providerText);

    const [thread] = await api(m, 'GET', `/api/bots/${bot.id}/threads`);
    const run = await api(m, 'GET', `/api/runs/${thread.run_id}`);
    expect(run.error_kind).toBe('rate_limited');
    expect(new Date(run.resets_at).getTime()).toBe(resetsAt.getTime());

    // The line comes from the run, so it is there again when the thread is reopened, a day or
    // three days before the reset.
    const day = 24 * 60 * 60_000;
    const weekday = resetsAt.toLocaleDateString('en-US', { weekday: 'short', timeZone });
    for (const [before, time] of [
      [day, `Tomorrow ${clock}`],
      [3 * day, `${weekday} ${clock}`],
    ] as const) {
      await page.clock.setFixedTime(resetsAt.getTime() - before);
      await page.goto(`${m.url}/bots/${bot.id}/threads/${thread.id}`);
      await expect(back(time)).toBeVisible();
      await expect(log.getByRole('button', { name: 'Retry' })).toHaveCount(0);
    }
  });
}
