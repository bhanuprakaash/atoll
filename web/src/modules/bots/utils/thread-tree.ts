import type { Bot, SidebarThread } from '@/modules/bots/api/bots';
import { relativeTime } from '@/utils/time';

export const isLiveHelper = (t: SidebarThread) =>
  t.parent_thread_id !== '' && (t.state === 'running' || t.state === 'needs_you');

const hasNoRow = (t: SidebarThread) => t.parent_thread_id !== '' && t.state !== 'failed';

// tabThreads is a Bot's threads that get a tab: the open one and those that need the person, run,
// failed or are pinned. A helper thread has none unless its run failed. The open thread is never
// Replied.
export const tabThreads = (all: SidebarThread[], openId: string | undefined) =>
  all
    .filter(
      (t) =>
        !hasNoRow(t) &&
        (t.id === openId || t.pinned || ['needs_you', 'running', 'failed'].includes(t.state)),
    )
    .map((t) => (t.id === openId && t.state === 'new' ? { ...t, state: '' as const } : t));

// otherThreads is a Bot's open threads without a tab, in the server's order.
export const otherThreads = (all: SidebarThread[], tabs: SidebarThread[]) =>
  all.filter((t) => !hasNoRow(t) && !tabs.some((tab) => tab.id === t.id));

// orderTabs keeps the tabs already shown in place, drops the ones that left and puts new ones on
// the right, so a tab never moves when its state changes.
export function orderTabs(order: string[], threads: SidebarThread[]) {
  const ids = threads.map((t) => t.id);
  return [...order.filter((id) => ids.includes(id)), ...ids.filter((id) => !order.includes(id))];
}

const nowLabels = [
  ['Needs you', ['needs_you', 'failed']],
  ['Running', ['running']],
  ['Replied', ['new']],
] as const;

// helperAsks is true when a live helper of the thread needs the person and the thread has no
// ask or failure of its own: the thread then sits under Needs you for it.
const helperAsks = (t: SidebarThread, workers?: Worker[]) =>
  !!workers?.[0].needsYou && t.state !== 'needs_you' && t.state !== 'failed';

// nowSections splits every Bot's threads into Now's sections, newest activity first, leaving out
// the empty ones. A helper thread is no row of its own (its boss thread shows it, under Needs you
// while the helper asks), not even a failed one (the boss's report carries the failure); a merged
// thread the watcher hides is none, and the open thread is never Replied.
export function nowSections(
  all: SidebarThread[],
  openId: string | undefined,
  workers: Map<string, Worker[]>,
) {
  const threads = all
    .filter(
      (t) => t.parent_thread_id === '' && !t.hidden && !(t.id === openId && t.state === 'new'),
    )
    .map((t) => (helperAsks(t, workers.get(t.id)) ? { ...t, state: 'needs_you' as const } : t))
    .sort((a, b) => Date.parse(b.last_activity) - Date.parse(a.last_activity));
  return nowLabels
    .map(([label, states]) => ({
      label,
      threads: threads.filter((t) => (states as readonly string[]).includes(t.state)),
    }))
    .filter((section) => section.threads.length > 0);
}

// helperBoss is the boss thread and its Bot for an open helper thread that has no tab of its own:
// the sidebar stays with them.
export function helperBoss(bots: Bot[], threadId: string | undefined) {
  const helper = bots.flatMap((bot) => bot.threads ?? []).find((t) => t.id === threadId);
  if (!helper || !hasNoRow(helper)) return undefined;
  const bot = bots.find((b) => b.threads?.some((t) => t.id === helper.parent_thread_id));
  return bot && { botId: bot.id, threadId: helper.parent_thread_id };
}

export type Worker = {
  id: string;
  bot: Bot;
  needsYou: boolean;
  ask: string;
  step: string;
  wake: string;
};

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
      {
        id: t.id,
        bot,
        needsYou: t.state === 'needs_you',
        ask: t.activity ?? '',
        step: t.step ?? '',
        wake: t.wake ?? '',
      },
    ]);
  return out;
}

export const workerNote = (w: Worker) =>
  w.needsYou ? w.ask : [w.wake, w.step].filter(Boolean).join(' · ') || 'Working';

// threadNote is the second line of a row that asks something of the person: what a needs-you thread
// asks, or when a failed run failed. A boss thread with a running or asking helper names its first
// helper and its note.
export function threadNote(t: SidebarThread, workers?: Worker[]) {
  if (t.state === 'needs_you') return t.activity ?? '';
  if (t.state === 'failed') return `Run failed · ${relativeTime(t.last_activity)}`;
  if (workers && (t.state === 'running' || helperAsks(t, workers)))
    return `${workers[0].bot.name}: ${workerNote(workers[0])}`;
  return t.activity ?? '';
}
