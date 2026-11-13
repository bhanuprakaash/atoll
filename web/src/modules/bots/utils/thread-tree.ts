import type { Bot, SidebarThread } from '@/modules/bots/api/bots';

const maxRows = 3;

export type Tree = {
  rows: SidebarThread[];
  hidden: SidebarThread[];
  more:
    | { kind: 'running'; count: number }
    | { kind: 'less' }
    | { kind: 'more'; count: number; dot: 'new' | null }
    | null;
};

const rank = (t: SidebarThread) =>
  t.state === 'needs_you'
    ? 0
    : t.state === 'waiting'
      ? 1
      : t.pinned
        ? 2
        : t.state === 'new'
          ? 3
          : t.state === 'running'
            ? 4
            : 5;

export const isLiveHelper = (t: SidebarThread) =>
  t.parent_thread_id !== '' && (t.state === 'running' || t.state === 'needs_you');

// Needs-you, waiting, pinned and the open thread always show, even past maxRows. A helper thread
// is a row only while it is open: while it runs it is the line under its boss thread, after that it is counted in
// the "more" line.
export function threadTree(
  all: SidebarThread[],
  openId: string | undefined,
  unfolded: boolean,
): Tree {
  const threads = all.filter((t) => !t.parent_thread_id || t.id === openId);
  const finished = all.filter((t) => t.parent_thread_id && t.id !== openId && !isLiveHelper(t));
  const ranked = threads
    .map((t) => (t.id === openId && t.state === 'new' ? { ...t, state: '' as const } : t))
    .sort((a, b) => rank(a) - rank(b));
  const kept = ranked.filter((t) => rank(t) <= 2 || t.id === openId);
  const room = Math.max(0, maxRows - kept.length);
  const shown = new Set([...kept, ...ranked.filter((t) => !kept.includes(t)).slice(0, room)]);
  const hidden = ranked.filter((t) => !shown.has(t));
  const folded = [...hidden, ...finished];
  const running = hidden.filter((t) => t.state === 'running');
  if (running.length > 0 && unfolded)
    return {
      rows: ranked.filter((t) => shown.has(t) || running.includes(t)),
      hidden: [],
      more: { kind: 'less' },
    };
  const rows = ranked.filter((t) => shown.has(t));
  if (running.length > 0) return { rows, hidden, more: { kind: 'running', count: running.length } };
  if (folded.length > 0)
    return {
      rows,
      hidden,
      more: {
        kind: 'more',
        count: folded.length,
        dot: folded.some((t) => t.state === 'new') ? 'new' : null,
      },
    };
  return { rows, hidden, more: null };
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
