// Settings › Usage, Plan limits: how much of each provider's plan is used, read from the provider
// when the page opens. Design: docs/design.md, "Settings > Usage". Claude's usage endpoint is a
// local server (ATOLL_CLAUDE_USAGE_URL) that answers only the sign-in's token; Codex's limits are
// the fake app-server's rate-limits.json.
import { writeFileSync } from 'node:fs';
import { createServer, type Server } from 'node:http';
import type { AddressInfo } from 'node:net';
import { join } from 'node:path';
import { expect, type Locator, type Page, test } from '@playwright/test';
import { type Machine, newBotAPI, openThreadAPI, prompts, startAtoll, waitStatus } from './atoll';

const token = 'sk-ant-oat01-reef-ui-usage-token';
const hour = 3_600_000;
const day = 24 * hour;

let usage: Server;
let usageURL = '';
let claude = { status: 200, body: '' };

test.beforeAll(async () => {
  usage = createServer((req, res) => {
    res.setHeader('Content-Type', 'application/json');
    if (
      req.method !== 'GET' ||
      req.url !== '/api/oauth/usage' ||
      req.headers.authorization !== `Bearer ${token}` ||
      req.headers['anthropic-beta'] !== 'oauth-2025-04-20'
    ) {
      res.statusCode = 401;
      res.end(
        '{"type":"error","error":{"type":"authentication_error","message":"Invalid bearer token"}}',
      );
      return;
    }
    res.statusCode = claude.status;
    res.end(claude.body);
  });
  await new Promise<void>((ok) => usage.listen(0, '127.0.0.1', ok));
  usageURL = `http://127.0.0.1:${(usage.address() as AddressInfo).port}/api/oauth/usage`;
});
test.afterAll(() => usage?.close());

// claudeAnswer is the usage endpoint's body, among keys Reef ignores.
const claudeAnswer = (session: number, weekly: number, sessionAt: number, weeklyAt: number) => {
  const at = (ms: number) => new Date(ms).toISOString().replace(/\.\d+Z$/, '.699683+00:00');
  return JSON.stringify({
    five_hour: { utilization: session, resets_at: at(sessionAt) },
    seven_day: { utilization: weekly, resets_at: at(weeklyAt) },
    seven_day_opus: { utilization: 0, resets_at: null },
    extra_usage: { is_enabled: false, monthly_limit: null },
  });
};

type CodexWindow = { used: number; minutes: number; at: number };

// codexLimits is what the fake codex answers to account/rateLimits/read.
function codexLimits(m: Machine, primary: CodexWindow, secondary: CodexWindow | null) {
  const win = (w: CodexWindow) => ({
    usedPercent: w.used,
    windowDurationMins: w.minutes,
    resetsAt: Math.floor(w.at / 1000),
  });
  writeFileSync(
    join(m.bin, 'rate-limits.json'),
    JSON.stringify({
      rateLimits: {
        limitId: 'codex',
        limitName: null,
        primary: win(primary),
        secondary: secondary && win(secondary),
        credits: null,
        planType: 'plus',
      },
    }),
  );
}

async function signedInMachine() {
  const m = await startAtoll({
    fakes: ['claude', 'codex'],
    signedIn: ['claude', 'codex'],
    env: { ATOLL_CLAUDE_USAGE_URL: usageURL },
  });
  writeFileSync(
    join(m.data, 'providers', 'claude', '.credentials.json'),
    JSON.stringify({
      claudeAiOauth: {
        accessToken: token,
        refreshToken: 'sk-ant-ort01-reef-ui-refresh-token',
        expiresAt: Date.now() + 8 * hour,
        scopes: ['user:inference', 'user:profile'],
        subscriptionType: 'max',
      },
    }),
  );
  return m;
}

const row = (page: Page, provider: string) =>
  page.getByRole('group', { name: provider, exact: true });
const bar = (r: Locator, name: string) => r.getByRole('progressbar', { name, exact: true });

// colours are the colours an element and everything in it paint, and the computed amber and
// danger tokens. The app's amber is --color-blocked (docs/design.md, Bot colours: amber is a state).
const colours = (b: Locator) =>
  b.evaluate((el) => {
    const token = (name: string) => {
      const probe = document.createElement('div');
      probe.style.background = `var(--color-${name})`;
      document.body.append(probe);
      const c = getComputedStyle(probe).backgroundColor;
      probe.remove();
      return c;
    };
    const painted = new Set<string>();
    for (const e of [el, ...el.querySelectorAll('*')]) {
      const s = getComputedStyle(e);
      painted.add(s.backgroundColor);
      painted.add(s.color);
      if (e instanceof SVGElement) painted.add(s.fill);
    }
    return {
      painted: [...painted],
      amber: token('blocked'),
      danger: token('danger'),
    };
  });

test.describe('with both providers answering', () => {
  let m: Machine;
  const now = Date.now();

  test.beforeAll(async () => {
    m = await signedInMachine();
    claude = {
      status: 200,
      body: claudeAnswer(52, 89, now + 3 * hour, now + 4 * day),
    };
    codexLimits(
      m,
      { used: 90, minutes: 300, at: now + 2 * hour },
      { used: 40, minutes: 10080, at: now + 6 * day },
    );
  });
  test.afterAll(() => m?.stop());

  test('Plan limits sits above What Bots used, a row per provider with its logo, bars, percent and reset', async ({
    page,
  }) => {
    const res = page.waitForResponse((r) => new URL(r.url()).pathname === '/api/usage/limits', {
      timeout: 15_000,
    });
    await page.goto(`${m.url}/settings/usage`);
    expect(await (await res).text(), 'the token reached the web app').not.toContain(token);

    const plan = page.getByRole('heading', { name: 'Plan limits', exact: true });
    const used = page.getByRole('heading', { name: 'What Bots used', exact: true });
    await expect(plan).toBeVisible();
    await expect(used).toBeVisible();
    expect((await plan.boundingBox())!.y).toBeLessThan((await used.boundingBox())!.y);

    await expect(row(page, 'Claude')).toBeVisible();
    await expect(row(page, 'Codex')).toBeVisible();
    expect((await row(page, 'Claude').boundingBox())!.y).toBeLessThan(
      (await row(page, 'Codex').boundingBox())!.y,
    );

    const want = { Claude: { Session: 52, Weekly: 89 }, Codex: { Session: 90, Weekly: 40 } };
    for (const [provider, bars] of Object.entries(want)) {
      const r = row(page, provider);
      await expect(r.getByRole('img', { name: provider, exact: true })).toBeVisible();
      await expect(r.getByRole('progressbar')).toHaveCount(2);
      for (const [name, percent] of Object.entries(bars)) {
        await expect(bar(r, name)).toBeVisible();
        await expect(bar(r, name)).toHaveAttribute('aria-valuenow', String(percent));
        await expect(r.getByText(`${percent}%`, { exact: true })).toBeVisible();
      }
      await expect(r.getByText(/^Resets \S/)).toHaveCount(2);
    }
  });

  test('a bar at 90% or more is amber, never red; one under 90% is neither', async ({ page }) => {
    await page.goto(`${m.url}/settings/usage`);
    await expect(bar(row(page, 'Codex'), 'Session')).toBeVisible();
    const full = await colours(bar(row(page, 'Codex'), 'Session'));
    expect(full.painted, 'the 90% bar is not amber').toContain(full.amber);
    expect(full.painted, 'the 90% bar is red').not.toContain(full.danger);
    for (const [provider, name] of [
      ['Claude', 'Session'],
      ['Claude', 'Weekly'],
      ['Codex', 'Weekly'],
    ]) {
      await expect(bar(row(page, provider), name)).toBeVisible();
      const c = await colours(bar(row(page, provider), name));
      expect(c.painted, `${provider} ${name} under 90% is amber`).not.toContain(c.amber);
      expect(c.painted, `${provider} ${name} is red`).not.toContain(c.danger);
    }
  });

  test('opening the page again reads each provider again', async ({ page }) => {
    await page.goto(`${m.url}/settings/usage`);
    await expect(bar(row(page, 'Claude'), 'Session')).toHaveAttribute('aria-valuenow', '52');
    claude = {
      status: 200,
      body: claudeAnswer(61, 89, now + 3 * hour, now + 4 * day),
    };
    codexLimits(
      m,
      { used: 12, minutes: 300, at: now + 2 * hour },
      { used: 40, minutes: 10080, at: now + 6 * day },
    );
    await page.reload();
    await expect(bar(row(page, 'Claude'), 'Session')).toHaveAttribute('aria-valuenow', '61');
    await expect(bar(row(page, 'Codex'), 'Session')).toHaveAttribute('aria-valuenow', '12');
  });
});

test.describe('with a failed read', () => {
  let m: Machine;

  test.beforeAll(async () => {
    m = await signedInMachine();
    claude = {
      status: 500,
      body: '{"type":"error","error":{"type":"api_error","message":"Internal server error"}}',
    };
    codexLimits(m, { used: 7, minutes: 43200, at: Date.now() + 20 * day }, null);
    const bot = await newBotAPI(m, 'Pinger');
    await waitStatus(m, (await openThreadAPI(m, bot.id, prompts.pong)).id, 'done');
  });
  test.afterAll(() => m?.stop());

  test('the provider shows Not available right now. with the reason under Details; the rest still loads', async ({
    page,
  }) => {
    await page.goto(`${m.url}/settings/usage`);
    const r = row(page, 'Claude');
    await expect(r.getByText('Not available right now.', { exact: true })).toBeVisible();
    await expect(r.getByRole('img', { name: 'Claude', exact: true })).toBeVisible();
    await expect(r.getByRole('progressbar')).toHaveCount(0);

    const closed = await r.innerText();
    await r.getByText('Details', { exact: true }).click();
    await expect.poll(async () => (await r.innerText()).length).toBeGreaterThan(closed.length);
    const open = await r.innerText();
    expect(open, 'the raw answer shows').not.toMatch(/[{}]|api_error/);

    const c = await colours(r);
    expect(c.painted, 'a failed read is red').not.toContain(c.danger);

    await expect(page.getByRole('table', { name: 'By Bot' })).toBeVisible();
    await expect(
      page.getByRole('table', { name: 'By Bot' }).getByRole('row').filter({ hasText: 'Pinger' }),
    ).toBeVisible();
    await expect(page.getByRole('radiogroup', { name: 'Range' })).toBeVisible();
  });

  test('a Codex plan with one 30-day window shows one bar named for it', async ({ page }) => {
    await page.goto(`${m.url}/settings/usage`);
    const r = row(page, 'Codex');
    await expect(r.getByRole('img', { name: 'Codex', exact: true })).toBeVisible();
    await expect(r.getByRole('progressbar')).toHaveCount(1);
    await expect(bar(r, '30 days')).toHaveAttribute('aria-valuenow', '7');
    await expect(r.getByText('7%', { exact: true })).toBeVisible();
    await expect(r.getByText(/^Resets \S/)).toHaveCount(1);
  });
});
