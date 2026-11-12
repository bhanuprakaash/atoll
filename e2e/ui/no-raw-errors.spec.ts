// The sweep behind designed-errors.spec.ts: no screen renders an error's message itself, and every
// screen that could (the sites #149 found) goes through the one component. Design: docs/design.md,
// App and UI (Errors). Contract: designed-errors-contract.md.
import { readdirSync, readFileSync } from 'node:fs';
import { join, relative } from 'node:path';
import { expect, test } from '@playwright/test';

const src = join(import.meta.dirname, '..', '..', 'web', 'src');
const component = join(src, 'components', 'error-notice.tsx');

function files(dir: string): string[] {
  return readdirSync(dir, { withFileTypes: true }).flatMap((e) =>
    e.isDirectory() ? files(join(dir, e.name)) : e.name.endsWith('.tsx') ? [join(dir, e.name)] : [],
  );
}

// Every file of web/src that rendered an error's text in red before the notice (grep text-danger
// and .message at the time of #149), by path under web/src.
const formerSites = [
  'app/threads-popup.tsx',
  'modules/bots/components/bot-settings.tsx',
  'modules/bots/components/new-bot-form.tsx',
  'modules/connections/components/connections-settings.tsx',
  'modules/connections/components/reconnect-button.tsx',
  'modules/mcp-servers/components/mcp-servers-settings.tsx',
  'modules/mcp-servers/components/setup-dialog.tsx',
  'modules/memory/components/bot-memory.tsx',
  'modules/memory/components/memory-settings.tsx',
  'modules/memory/components/saved-memory.tsx',
  'modules/onboarding/components/first-run.tsx',
  'modules/platform/components/platform-changes.tsx',
  'modules/providers/components/providers-settings.tsx',
  'modules/repos/components/repos-settings.tsx',
  'modules/settings/components/general-settings.tsx',
  'modules/settings/components/software-update.tsx',
  'modules/skills/components/skills-settings.tsx',
  'modules/threads/components/approval-card.tsx',
  'modules/threads/components/composer.tsx',
  'modules/threads/components/thread-view.tsx',
];

test('the error notice is one component, and only it reads an error message', () => {
  expect
    .soft(() => readFileSync(component, 'utf8'), 'web/src/components/error-notice.tsx is missing')
    .not.toThrow();
  const readers = files(src)
    .filter((f) => f !== component)
    .filter((f) => /\.message\b/.test(readFileSync(f, 'utf8')))
    .map((f) => relative(src, f));
  expect(readers, 'files that render an error message themselves').toEqual([]);
});

test('every screen that rendered a raw error now uses the notice', () => {
  const without = formerSites.filter(
    (f) => !/\bErrorNotice\b/.test(readFileSync(join(src, f), 'utf8')),
  );
  expect(without, 'former raw-error sites that do not use ErrorNotice').toEqual([]);
});
