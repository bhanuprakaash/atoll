// Builds the real atoll and the fake providers, then serves atoll on a temporary machine (HOME,
// data dir and provider folder all temporary) with the fake claude signed in. Same setup as the
// Go e2e tests: docs in e2e/ and the Test contract of issue #3.
import { execFileSync, spawn } from 'node:child_process';
import { copyFileSync, mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join, resolve } from 'node:path';
import { createInterface } from 'node:readline';

const e2e = resolve(import.meta.dirname, '..');

export default async function setup() {
  const dir = mkdtempSync(join(tmpdir(), 'atoll-ui-'));
  const [home, data, bin, out] = ['home', 'data', 'bin', 'out'].map((d) => join(dir, d));
  for (const d of [home, data, bin, out]) mkdirSync(d, { recursive: true });

  const go = (...args: string[]) => execFileSync('go', args, { cwd: e2e, stdio: 'inherit' });
  go('build', '-o', join(out, 'atoll'), 'atoll/cmd/atoll');
  go(
    'build',
    '-o',
    join(out, 'claude'),
    '-ldflags',
    `-X atoll/e2e/fakes/fake._testdata=${join(e2e, 'testdata')}`,
    'atoll/e2e/fakes/claude',
  );
  copyFileSync(join(out, 'claude'), join(bin, 'claude'));
  mkdirSync(join(data, 'providers', 'claude'), { recursive: true });
  writeFileSync(join(data, 'providers', 'claude', '.credentials.json'), '{}');

  const server = spawn(join(out, 'atoll'), ['serve', '--addr', '127.0.0.1:0'], {
    cwd: dir,
    env: {
      HOME: home,
      PATH: `${bin}:/usr/bin:/bin`,
      ATOLL_DATA_DIR: data,
      ATOLL_PROVIDER_PATH: bin,
    },
    stdio: ['ignore', 'pipe', 'inherit'],
  });
  const stop = () => {
    server.kill('SIGKILL');
    rmSync(dir, { recursive: true, force: true });
  };
  const first = await new Promise<string>((ok, fail) => {
    const timer = setTimeout(() => fail(new Error('atoll serve printed nothing in 15s')), 15_000);
    createInterface({ input: server.stdout }).once('line', (l) => (clearTimeout(timer), ok(l)));
    server.once('exit', (code) =>
      fail(new Error(`atoll serve exited with ${code} before listening`)),
    );
  }).catch((err) => {
    stop();
    throw err;
  });
  const url = first.match(/^listening on (http:\/\/127\.0\.0\.1:\d+)$/)?.[1];
  if (!url) {
    stop();
    throw new Error(
      `atoll serve: first line ${JSON.stringify(first)}, want "listening on http://127.0.0.1:<port>"`,
    );
  }
  process.env.ATOLL_URL = url;

  return () => {
    // A fake provider is not killed with the server; its log names every process it ran.
    const log = readFileSync(join(bin, 'claude.log'), {
      encoding: 'utf8',
      flag: 'a+',
    });
    for (const line of log.split('\n').filter(Boolean)) {
      try {
        process.kill(JSON.parse(line).pid, 'SIGKILL');
      } catch {
        // already gone
      }
    }
    stop();
  };
}
