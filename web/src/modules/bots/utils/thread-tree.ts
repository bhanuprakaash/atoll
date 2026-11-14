import type { Bot, SidebarThread } from '@/modules/bots/api/bots';

export const isLiveHelper = (t: SidebarThread) =>
  t.parent_thread_id !== '' && (t.state === 'running' || t.state === 'needs_you');

const sectionLabels = ['Needs you', 'Running', 'Pinned', 'Recent'] as const;

const sectionOf = (t: SidebarThread) =>
  t.state === 'needs_you'
    ? 'Needs you'
    : t.state === 'running'
      ? 'Running'
      : t.pinned
        ? 'Pinned'
        : 'Recent';

// threadSections splits a Bot's threads into the pane's sections, keeping the server's order and
// leaving out the empty ones. The open thread is never Replied; a live helper thread is Running,
// its Needs you shows on its boss thread's helper line.
export function threadSections(all: SidebarThread[], openId: string | undefined) {
  const threads = all.map((t) =>
    isLiveHelper(t)
      ? { ...t, state: 'running' as const }
      : t.id === openId && t.state === 'new'
        ? { ...t, state: '' as const }
        : t,
  );
  return sectionLabels
    .map((label) => ({ label, threads: threads.filter((t) => sectionOf(t) === label) }))
    .filter((section) => section.threads.length > 0);
}

export type Worker = { id: string; bot: Bot; needsYou: boolean; step: string };

// workersByBoss lists the helpers running for each boss thread: the one that needs the person
// first, then the one that acted last.
export function workersByBoss(bots: Bot[]): Map<string, Worker[]> {
  const out = new Map<string, Worker[]>();
  const live = bots.flatMap((bot) =>
    (bot.threads ?? []).filter(isLiveHelper).map((t) => ({ bot, t })),
  );
  live.sort(
    (a, b) =>
      Number(b.t.state === 'needs_you') - Number(a.t.state === 'needs_you') ||
      Date.parse(b.t.last_activity) - Date.parse(a.t.last_activity),
  );
  for (const { bot, t } of live)
    out.set(t.parent_thread_id, [
      ...(out.get(t.parent_thread_id) ?? []),
      { id: t.id, bot, needsYou: t.state === 'needs_you', step: t.step ?? '' },
    ]);
  return out;
}

export const workerNames = (workers: Worker[]) =>
  workers.length > 2
    ? `${workers[0].bot.name} +${workers.length - 1}`
    : workers.map((w) => w.bot.name).join(', ');
