// The poteto-mode fixture of e2e/skill_files_fixtures_test.go: SKILL.md and 22 files, in a skill
// folder under the machine's temporary HOME, imported through Chief like a user would.
import { chmodSync, mkdirSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { expect } from '@playwright/test';
import { api, type Machine, newBotAPI, openThreadAPI } from './atoll';

export type FixtureFile = { path: string; content: string; exec: boolean };
export type SkillFile = { path: string; content: string; executable: boolean };
export type Skill = {
  id: string;
  name: string;
  content: string;
  source: string;
  files: SkillFile[];
};

const home = '~/.claude/skills/poteto-mode/';
const skillMD =
  '---\nname: poteto-mode\ndescription: Work the poteto way.\n---\n' +
  `Scripts: \`S=${home}scripts\`. Always call scripts by that full path.\n` +
  'Proof goes in .reef-verify/<id> when a run ends, and notes in .poteto/state while it runs.\n' +
  'Read playbooks/ship.md before shipping. Proof is still in .reef-verify/<id> every time.\n';

const scripts = [
  'build',
  'check',
  'cleanup',
  'lint',
  'report',
  'snapshot',
  'status',
  'sync',
  'test',
  'verify',
];
const playbooks = [
  'audit',
  'debug',
  'design',
  'explore',
  'fix',
  'improve',
  'interrogate',
  'plan',
  'review',
  'ship',
  'verify',
];

// potetoFiles is SKILL.md's folder as it is on disk: SKILL.md and 22 files.
export function potetoFiles(): FixtureFile[] {
  return [
    { path: 'SKILL.md', content: skillMD, exec: false },
    ...scripts.map((n) => ({
      path: `scripts/${n}`,
      content: `#!/bin/sh\necho ${n}-ok\n`,
      exec: true,
    })),
    ...playbooks.map((n) => ({
      path: `playbooks/${n}.md`,
      content: `# ${n}\nRun ${home}scripts/check first.\n`,
      exec: false,
    })),
    {
      path: 'references/improve-report.md',
      content: '# Improve report\nHow to write one.\n',
      exec: false,
    },
  ];
}

export const potetoPaths = potetoFiles()
  .map((f) => f.path)
  .filter((p) => p !== 'SKILL.md')
  .sort();

export function withFile(files: FixtureFile[], path: string, content: string): FixtureFile[] {
  const out = files.map((f) => (f.path === path ? { ...f, content } : f));
  return out.some((f) => f.path === path) ? out : [...out, { path, content, exec: false }];
}

export function withoutFile(files: FixtureFile[], path: string): FixtureFile[] {
  return files.filter((f) => f.path !== path);
}

// stored is text as Reef stores it: the skill's own folder as the placement token.
export const stored = (text: string) =>
  text.replaceAll('~/.claude/skills/poteto-mode', '{{ATOLL_SKILL_DIR}}');

// libraryFiles is what the library holds of a folder: everything but SKILL.md, by path.
export function libraryFiles(files: FixtureFile[]): SkillFile[] {
  return files
    .filter((f) => f.path !== 'SKILL.md')
    .map((f) => ({ path: f.path, content: stored(f.content), executable: f.exec }))
    .sort((a, b) => (a.path < b.path ? -1 : 1));
}

// potetoDir is the skill folder of a machine.
export const potetoDir = (m: Machine) => join(m.home, '.claude', 'skills', 'poteto-mode');

// writeFolder replaces the skill folder with files.
export function writeFolder(m: Machine, files: FixtureFile[]) {
  rmSync(potetoDir(m), { recursive: true, force: true });
  for (const f of files) {
    const path = join(potetoDir(m), f.path);
    mkdirSync(dirname(path), { recursive: true });
    writeFileSync(path, f.content);
    chmodSync(path, f.exec ? 0o755 : 0o644);
  }
}

async function waitDone(m: Machine, threadID: string) {
  await expect
    .poll(async () => (await api(m, 'GET', `/api/threads/${threadID}`)).run_status, {
      timeout: 15_000,
    })
    .toBe('done');
}

// importPoteto has Chief import the folder, as Settings users do, and returns the stored skill.
export async function importPoteto(m: Machine, files = potetoFiles()): Promise<Skill> {
  writeFolder(m, files);
  const th = await openThreadAPI(m, 'chief', `Import the skill folder ${potetoDir(m)}`);
  await waitDone(m, th.id);
  const skills: Skill[] = await api(m, 'GET', '/api/skills');
  const skill = skills.find((s) => s.name === 'poteto-mode');
  if (!skill) throw new Error(`Chief did not import poteto-mode: ${JSON.stringify(skills)}`);
  return skill;
}

// runScript has a Bot with the skill run one of its scripts and returns the run's reply.
export async function runScript(m: Machine, skill: Skill, script: string) {
  const bot = await newBotAPI(m, `Skilled ${Math.random().toString(36).slice(2, 6)}`, 'claude', {
    skills: [skill.id],
  });
  const th = await openThreadAPI(m, bot.id, `Run the poteto-mode script ${script}.`);
  await waitDone(m, th.id);
  const run = await api(m, 'GET', `/api/runs/${th.run.id}`);
  return {
    id: th.run.id as string,
    reply: run.reply as string,
    status: run.status as string,
  };
}

export type Placed = { path: string; mode: number; text: string };

// placed is the files of poteto-mode as the last run's provider found them in its run folder.
export function placed(m: Machine): { dir: string; files: Placed[] } {
  const lines = readFileSync(join(m.bin, 'claude.log.skills'), 'utf8').split('\n').filter(Boolean);
  const last = JSON.parse(lines[lines.length - 1]) as {
    skills: { name: string; path: string; files: Placed[] }[];
  };
  const copy = last.skills.find((s) => s.name === 'poteto-mode');
  if (!copy) throw new Error(`the last run found no poteto-mode: ${lines[lines.length - 1]}`);
  return { dir: dirname(copy.path), files: copy.files };
}
