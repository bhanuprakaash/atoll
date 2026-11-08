import type { SidebarThread } from '@/modules/bots/api/bots';

const maxRows = 3;

export type Tree = {
  rows: SidebarThread[];
  more:
    | { kind: 'running'; count: number }
    | { kind: 'less' }
    | { kind: 'more'; count: number; dot: 'new' | null }
    | null;
};

const rank = (t: SidebarThread) =>
  t.state === 'needs_you'
    ? 0
    : t.pinned
      ? 1
      : t.state === 'new'
        ? 2
        : t.state === 'running'
          ? 3
          : 4;

// Needs-you, pinned and the open thread always show, even past maxRows.
export function threadTree(
  threads: SidebarThread[],
  openId: string | undefined,
  unfolded: boolean,
): Tree {
  const ranked = threads
    .map((t) => (t.id === openId && t.state === 'new' ? { ...t, state: '' as const } : t))
    .sort((a, b) => rank(a) - rank(b));
  const kept = ranked.filter((t) => rank(t) <= 1 || t.id === openId);
  const room = Math.max(0, maxRows - kept.length);
  const shown = new Set([...kept, ...ranked.filter((t) => !kept.includes(t)).slice(0, room)]);
  const hidden = ranked.filter((t) => !shown.has(t));
  const running = hidden.filter((t) => t.state === 'running');
  if (running.length > 0 && unfolded)
    return {
      rows: ranked.filter((t) => shown.has(t) || running.includes(t)),
      more: { kind: 'less' },
    };
  const rows = ranked.filter((t) => shown.has(t));
  if (running.length > 0) return { rows, more: { kind: 'running', count: running.length } };
  if (hidden.length > 0)
    return {
      rows,
      more: {
        kind: 'more',
        count: hidden.length,
        dot: hidden.some((t) => t.state === 'new') ? 'new' : null,
      },
    };
  return { rows, more: null };
}
