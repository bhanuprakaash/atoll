import { useState } from 'react';
import type { Thread } from '@/modules/threads/api/threads';
import { approvalTarget } from '@/modules/threads/components/approval-card';

export function ThreadsPanel({
  name,
  threads,
  repos,
  onClose,
  onNew,
  onSelect,
}: {
  name: string;
  threads: Thread[];
  repos: { id: string; name: string }[];
  onClose: () => void;
  onNew: () => void;
  onSelect: (id: string) => void;
}) {
  const [showClosed, setShowClosed] = useState(false);
  const groups: [string, Thread[]][] = [
    ['Needs you', threads.filter((t) => !t.closed && !!t.approval)],
    ['Running', threads.filter((t) => !t.closed && !t.approval && t.run_status === 'running')],
    ['Recent', threads.filter((t) => !t.closed && !t.approval && t.run_status !== 'running')],
  ];
  const list = (label: string, items: Thread[]) => (
    <section key={label}>
      <h3 className="mb-2 text-xs text-muted">{label}</h3>
      <ul aria-label={label} className="flex flex-col gap-1">
        {items.map((t) => (
          <li key={t.id}>
            <button
              type="button"
              className="w-full rounded-card p-2 text-left hover:bg-card"
              onClick={() => onSelect(t.id)}
            >
              <span className="flex justify-between gap-3">
                <span className="truncate">{t.title || 'Empty thread'}</span>
                {t.updated_at && (
                  <time className="shrink-0 text-xs text-muted" dateTime={t.updated_at}>
                    {new Date(t.updated_at).toLocaleDateString(undefined, {
                      month: 'short',
                      day: 'numeric',
                    })}
                  </time>
                )}
              </span>
              {t.approval && (
                <span className="block text-xs text-muted">
                  Wants to{' '}
                  {/edit|write|filechange/i.test(t.approval.tool)
                    ? 'edit'
                    : /bash|command/i.test(t.approval.tool)
                      ? 'run'
                      : 'use'}{' '}
                  {approvalTarget(t.approval.input)}
                </span>
              )}
              {label === 'Recent' && (
                <span className="block text-xs text-muted">
                  {[repos.find((repo) => repo.id === t.repo_id)?.name, t.run_status]
                    .filter(Boolean)
                    .join(' · ')}
                </span>
              )}
              {label === 'Running' && (
                <span className="block text-xs text-muted">
                  {[
                    t.verb,
                    t.branch,
                    t.mr_url &&
                      `!${t.mr_url.match(/(?:merge_requests|merge-requests)\/(\d+)/)?.[1] ?? 'MR'}`,
                  ]
                    .filter(Boolean)
                    .join(' · ')}
                </span>
              )}
            </button>
          </li>
        ))}
      </ul>
    </section>
  );
  return (
    <div className="fixed inset-0 z-20 flex justify-end bg-bg/70" onClick={onClose}>
      <div
        role="dialog"
        aria-modal="true"
        aria-label={`Threads with ${name}`}
        className="flex h-full w-full max-w-sm flex-col gap-5 overflow-auto border-l border-line bg-bg p-5"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="flex items-center justify-between">
          <h2>Threads with {name}</h2>
          <button type="button" onClick={onClose}>
            Close
          </button>
        </div>
        <button
          type="button"
          className="rounded-full bg-accent px-3 py-2 text-accent-ink"
          onClick={onNew}
        >
          New thread
        </button>
        {groups.map(([label, items]) => list(label, items))}
        <button
          type="button"
          className="text-left text-sm text-muted"
          onClick={() => setShowClosed(!showClosed)}
        >
          Closed ({threads.filter((t) => t.closed).length})
        </button>
        {showClosed &&
          list(
            'Closed',
            threads.filter((t) => t.closed),
          )}
      </div>
    </div>
  );
}
