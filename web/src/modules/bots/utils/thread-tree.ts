import type { Bot, SidebarThread } from '@/modules/bots/api/bots';
import { relativeTime } from '@/utils/time';

export const isLiveHelper = (t: SidebarThread) =>
  t.parent_thread_id !== '' && (t.state === 'running' || t.state === 'needs_you');

const sectionLabels = ['Needs you', 'Running', 'Threads'] as const;

const sectionOf = (t: SidebarThread) =>
  t.state === 'needs_you' || (t.state === 'failed' && t.parent_thread_id === '')
    ? 'Needs you'
    : t.state === 'running'
      ? 'Running'
      : 'Threads';

// threadSections splits a Bot's threads into the pane's sections, keeping the server's order except
// that pinned threads come first under Threads, and leaving out the empty ones. The open thread is
// never Replied; a live helper thread is no row (its boss thread's helper line shows it, with its
// Needs you); a failed helper thread is a row under Threads.
export function threadSections(all: SidebarThread[], openId: string | undefined) {
  const threads = all
    .filter((t) => !isLiveHelper(t))
    .map((t) => (t.id === openId && t.state === 'new' ? { ...t, state: '' as const } : t));
  return sectionLabels
    .map((label) => {
      const inSection = threads.filter((t) => sectionOf(t) === label);
      return {
        label,
        threads:
          label === 'Threads'
            ? inSection.sort((a, b) => Number(b.pinned) - Number(a.pinned))
            : inSection,
      };
    })
    .filter((section) => section.threads.length > 0);
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
// the empty ones. A live helper thread is no row of its own (its boss thread shows it, under Needs
// you while the helper asks), nor is a failed one (the boss's report carries the failure); a merged
// thread the watcher hides is none, and the open thread is never Replied.
export function nowSections(
  all: SidebarThread[],
  openId: string | undefined,
  workers: Map<string, Worker[]>,
) {
  const threads = all
    .filter(
      (t) =>
        !isLiveHelper(t) &&
        !(t.parent_thread_id !== '' && t.state === 'failed') &&
        !t.hidden &&
        !(t.id === openId && t.state === 'new'),
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

// bossBotId is the Bot whose pane shows the open thread: for a live helper thread, which has no row
// of its own, the Bot of its boss thread; else the Bot in the URL.
export function bossBotId(bots: Bot[], openBotId: string, threadId: string | undefined) {
  const helper = bots.flatMap((bot) => bot.threads ?? []).find((t) => t.id === threadId);
  if (!helper || !isLiveHelper(helper)) return openBotId;
  return (
    bots.find((bot) => bot.threads?.some((t) => t.id === helper.parent_thread_id))?.id ?? openBotId
  );
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
