// Runs the real atoll on a temporary machine (HOME, data dir and provider folder all temporary),
// as the Go e2e tests do. setup.ts builds once; a spec that needs its own state (first run,
// sidebar states, a restart) starts its own machine with startAtoll.
import { type ChildProcess, execFileSync, spawn } from 'node:child_process';
import { copyFileSync, mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join, resolve } from 'node:path';
import { createInterface } from 'node:readline';
import { expect, type Locator, type Page } from '@playwright/test';

export const e2e = resolve(import.meta.dirname, '..');

type Fake = 'claude' | 'codex' | 'glab';

// build compiles atoll and the fakes into a fresh folder and returns it.
export function build(): string {
  const out = mkdtempSync(join(tmpdir(), 'atoll-ui-build-'));
  const go = (...args: string[]) => execFileSync('go', args, { cwd: e2e, stdio: 'inherit' });
  go('build', '-o', join(out, 'atoll'), 'atoll/cmd/atoll');
  for (const fake of ['claude', 'codex', 'glab']) {
    go(
      'build',
      '-o',
      join(out, fake),
      '-ldflags',
      `-X atoll/e2e/fakes/fake._testdata=${join(e2e, 'testdata')}`,
      `atoll/e2e/fakes/${fake}`,
    );
  }
  return out;
}

export type Machine = {
  url: string;
  dir: string;
  home: string;
  data: string;
  bin: string;
  // kill ends the server at once, as a crash would; restart serves the same data on the same port.
  kill: () => void;
  restart: () => Promise<void>;
  stop: () => void;
  // release finishes the browser step of a provider's sign-in.
  release: (fake: 'claude' | 'codex') => void;
  // revoke marks a provider's login as revoked by its server: the fake fails the next turn with
  // the real CLI's auth error and is signed out from then on.
  revoke: (fake: 'claude' | 'codex') => void;
  calls: (fake: Fake) => { pid: number; argv?: string[] }[];
};

export type Options = {
  fakes?: Fake[];
  signedIn?: ('claude' | 'codex')[];
  // The user_name setting; null leaves it empty, so the app shows first run.
  userName?: string | null;
  env?: Record<string, string>;
  // Serve inside e2e/with-secret-store: a private Secret Service on Linux, as make e2e gives the
  // Go tests.
  secretStore?: boolean;
};

const credentials = { claude: '.credentials.json', codex: 'auth.json' };

export async function startAtoll(opts: Options = {}): Promise<Machine> {
  const out = process.env.ATOLL_E2E_OUT;
  if (!out) throw new Error('ATOLL_E2E_OUT unset: run through playwright.config.ts');
  const { fakes = ['claude'], signedIn = ['claude'], userName = 'Ada', env = {} } = opts;
  const dir = mkdtempSync(join(tmpdir(), 'atoll-ui-'));
  const [home, data, bin] = ['home', 'data', 'bin'].map((d) => join(dir, d));
  for (const d of [home, data, bin]) mkdirSync(d, { recursive: true });
  for (const fake of fakes) copyFileSync(join(out, fake), join(bin, fake));
  for (const p of signedIn) {
    mkdirSync(join(data, 'providers', p), { recursive: true });
    writeFileSync(join(data, 'providers', p, credentials[p]), '{}');
  }

  let server: ChildProcess | undefined;
  let addr = '127.0.0.1:0';
  const serve = async () => {
    const cmd = [join(out, 'atoll'), 'serve', '--addr', addr];
    if (opts.secretStore) cmd.unshift(join(e2e, 'with-secret-store'));
    server = spawn(cmd[0], cmd.slice(1), {
      cwd: dir,
      detached: true,
      env: {
        HOME: home,
        PATH: `${bin}:/usr/bin:/bin`,
        ATOLL_DATA_DIR: data,
        ATOLL_PROVIDER_PATH: bin,
        ...env,
      },
      stdio: ['ignore', 'pipe', 'inherit'],
    });
    const s = server;
    const url = await new Promise<string>((ok, fail) => {
      const timer = setTimeout(
        () => fail(new Error('atoll serve printed no address in 15s')),
        15_000,
      );
      createInterface({ input: s.stdout! }).on('line', (l) => {
        const m = l.match(/^listening on (http:\/\/127\.0\.0\.1:(\d+))$/);
        if (m) {
          clearTimeout(timer);
          addr = `127.0.0.1:${m[2]}`;
          ok(m[1]);
        }
      });
      s.once('exit', (code) => fail(new Error(`atoll serve exited with ${code} before listening`)));
    });
    machine.url = url;
  };
  const calls = (fake: Fake) => {
    const log = readFileSync(join(bin, `${fake}.log`), {
      encoding: 'utf8',
      flag: 'a+',
    });
    return log
      .split('\n')
      .filter(Boolean)
      .map((l) => JSON.parse(l))
      .filter((r) => r.argv !== undefined);
  };
  const kill = () => {
    try {
      process.kill(-server!.pid!, 'SIGKILL');
    } catch {
      // already gone
    }
  };
  const machine: Machine = {
    url: '',
    dir,
    home,
    data,
    bin,
    kill,
    restart: serve,
    calls,
    release: (fake) => writeFileSync(join(bin, `${fake}.login.release`), ''),
    revoke: (fake) =>
      writeFileSync(join(data, 'providers', fake, credentials[fake]), '{"fake":"revoked"}'),
    stop: () => {
      kill();
      // A fake provider is not killed with the server; its log names every process it ran.
      for (const fake of fakes) {
        for (const { pid } of calls(fake)) {
          try {
            process.kill(pid, 'SIGKILL');
          } catch {
            // already gone
          }
        }
      }
      rmSync(dir, { recursive: true, force: true });
    },
  };
  try {
    await serve();
    if (userName !== null) {
      await api(machine, 'PUT', '/api/settings', {
        worktree_root: join(dir, 'worktrees'),
        user_name: userName,
      });
    }
  } catch (err) {
    machine.stop();
    throw err;
  }
  return machine;
}

// api calls the real API; tests use it to set up state and to verify what the UI did.
export async function api(m: { url: string }, method: string, path: string, body?: unknown) {
  const res = await fetch(m.url + path, {
    method,
    headers: body === undefined ? {} : { 'Content-Type': 'application/json' },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  const text = await res.text();
  if (!res.ok) throw new Error(`${method} ${path}: ${res.status} ${text}`);
  return text ? JSON.parse(text) : undefined;
}

// Prompts of the scenarios in e2e/testdata.
export const prompts = {
  keep: 'Remember this fact: the codeword is kestrel. Reply with just: ok',
  write: 'Use the Write tool to create hello.txt containing the word hi.',
  hang: 'Wait for the build to finish.',
  chatPolishHang: 'Continue waiting for the build to finish.',
  working: 'Fix the wizard repo select and run its tests.',
  freshFix: 'Fix the seeded bug in answer.txt and open an MR.',
  saveMemory: 'Save the SQLite journal decision in user memory.',
  none: 'This message has no recording, so the provider fails.',
  pong: 'Reply with exactly: pong',
};

export const loginURL = {
  claude: 'https://login.reef.test/claude/oauth?code=true',
  codex: 'https://login.reef.test/codex/oauth?client_id=fake',
};

export const threadTitle = (s: string) => {
  const words = s.trim().split(/\s+/).slice(0, 6);
  if (words.length) words[words.length - 1] = words[words.length - 1].replace(/[.,;:!?]+$/, '');
  return words.join(' ');
};

// newBotAPI creates a Bot through the API.
export async function newBotAPI(
  m: Machine,
  name: string,
  provider = 'claude',
  extra: Record<string, unknown> = {},
) {
  return api(m, 'POST', '/api/bots', {
    name,
    persona: 'A careful Go engineer.',
    provider,
    ...extra,
  });
}

// openThreadAPI creates a thread with its first message.
export async function openThreadAPI(m: Machine, botID: string, text: string) {
  return api(m, 'POST', `/api/bots/${botID}/threads`, { text });
}

// sendMessageAPI sends a turn to an existing thread and waits for its run to reach status.
export async function sendMessageAPI(
  m: Machine,
  threadID: string,
  text: string,
  status: 'done' | 'running' = 'done',
) {
  const result = await api(m, 'POST', `/api/threads/${threadID}/messages`, { text });
  await expect
    .poll(async () => (await api(m, 'GET', `/api/threads/${threadID}`)).run_status, {
      timeout: 15_000,
    })
    .toBe(status);
  return result;
}

// choose picks an option of a combobox: a native select, or a searchable listbox.
export async function choose(page: Page, box: Locator, option: string | RegExp) {
  if ((await box.evaluate((el) => el.tagName)) === 'SELECT') {
    const label =
      typeof option === 'string'
        ? option
        : ((await box.locator('option').filter({ hasText: option }).first().textContent()) ?? '');
    await box.selectOption({ label });
    return;
  }
  await box.click();
  await page.getByRole('option', { name: option }).first().click();
}

// pickModel picks a model of provider (its label, e.g. "Claude Code") in the Model select,
// which groups models by provider: the named one, or the first.
export async function pickModel(
  page: Page,
  scope: Page | Locator,
  provider: string,
  model?: string,
) {
  const box = scope.getByRole('combobox', { name: 'Model' });
  if ((await box.evaluate((el) => el.tagName)) === 'SELECT') {
    const group = box.locator(`optgroup[label="${provider}"] option`);
    const opt = model ? group.filter({ hasText: model }).first() : group.first();
    await box.selectOption({ label: (await opt.textContent()) ?? '' });
    return;
  }
  await box.click();
  const group = page.getByRole('group', { name: provider });
  await (
    model ? group.getByRole('option', { name: model }) : group.getByRole('option').first()
  ).click();
}

// sidebarRow is a Bot's row in the sidebar.
export function sidebarRow(page: Page, name: string) {
  return page
    .getByRole('navigation', { name: 'Bots' })
    .getByRole('link', { name: new RegExp(`\\b${name}\\b`) });
}

// running counts the animations playing in the page.
export async function running(page: Page) {
  return page.evaluate(
    () => document.getAnimations().filter((a) => a.playState === 'running').length,
  );
}

// recordText starts collecting every text the page shows from now on, so a test can prove a
// text never appeared, not only that it is gone.
export async function recordText(page: Page) {
  await page.evaluate(() => {
    const w = window as unknown as { __seen: string[] };
    w.__seen = [document.body.innerText];
    new MutationObserver((records) => {
      for (const r of records) {
        if (r.type === 'characterData') w.__seen.push(r.target.textContent ?? '');
        for (const n of r.addedNodes) w.__seen.push(n.textContent ?? '');
      }
    }).observe(document.body, {
      subtree: true,
      childList: true,
      characterData: true,
    });
  });
}

export async function seenText(page: Page) {
  return page.evaluate(() => (window as unknown as { __seen: string[] }).__seen.join('\n'));
}

// openThreads opens the Threads panel of the Bot shown.
export async function openThreads(page: Page) {
  await page.getByRole('button', { name: /^Threads/ }).click();
  const panel = page.getByRole('dialog', { name: /^Threads with / });
  await expect(panel).toBeVisible();
  return panel;
}
