import { useState } from 'react';
import type { Thread } from '@/modules/threads/api/threads';
import { approvalTarget } from '@/modules/threads/components/approval-card';
import { relativeTime } from '@/utils/time';

export function ThreadsPanel({
  name,
  threads,
  repos,
  onClose,
  onNew,
  onSelect,
  currentId,
}: {
  name: string;
  threads: Thread[];
  repos: { id: string; name: string }[];
  onClose: () => void;
  onNew: () => void;
  onSelect: (id: string) => void;
  currentId?: string;
}) {
  const [showClosed, setShowClosed] = useState(false);
  const groups: [string, Thread[]][] = [
    ['Needs you', threads.filter((t) => !t.closed && !!t.approval)],
    ['Running', threads.filter((t) => !t.closed && !t.approval && t.run_status === 'running')],
    ['Recent', threads.filter((t) => !t.closed && !t.approval && t.run_status !== 'running')],
  ];
  const list = (label: string, items: Thread[]) => (
    <section key={label}>
      {label !== 'Closed' && (
        <h3
          className={`px-2.5 pt-2.5 pb-0.5 text-[11.5px] tracking-[.06em] uppercase ${label === 'Needs you' ? 'text-danger' : 'text-label'}`}
        >
          {label}
        </h3>
      )}
      <ul aria-label={label} className="flex flex-col">
        {items.map((t) => (
          <li key={t.id}>
            <button
              type="button"
              className={`flex w-full items-center gap-2.5 rounded-[14px] p-2.5 text-left ${label === 'Needs you' ? 'bg-needs hover:bg-needs/80' : t.id === currentId ? 'bg-line' : 'hover:bg-line'}`}
              onClick={() => onSelect(t.id)}
            >
              <span
                aria-hidden="true"
                className={`size-2 flex-none rounded-full ${label === 'Needs you' ? 'bg-danger' : label === 'Running' ? 'animate-pulse bg-ok' : ''}`}
              />
              <span className="min-w-0 flex-1">
                <span className="block truncate text-sm">
                  {t.title || 'Empty thread'}
                  {t.id === currentId && <span className="text-xs text-muted"> · this thread</span>}
                </span>
                <span
                  className={`block truncate text-[12.5px] ${label === 'Needs you' ? 'text-ink/80' : 'text-muted'}`}
                >
                  {label === 'Needs you' && t.approval
                    ? `Wants to ${
                        /edit|write|filechange/i.test(t.approval.tool)
                          ? 'edit'
                          : /bash|command/i.test(t.approval.tool)
                            ? 'run'
                            : 'use'
                      } ${approvalTarget(t.approval.input)}`
                    : [
                        label === 'Running' ? t.verb : repos.find((r) => r.id === t.repo_id)?.name,
                        t.branch,
                        t.mr_url &&
                          `!${t.mr_url.match(/(?:merge_requests|merge-requests)\/(\d+)/)?.[1] ?? 'MR'}${t.mr_state ? ` ${t.mr_state}` : ''}`,
                        label !== 'Running' && t.run_status,
                      ]
                        .filter(Boolean)
                        .join(' · ')}
                </span>
              </span>
              {t.updated_at && (
                <time className="flex-none text-xs text-muted" dateTime={t.updated_at}>
                  {relativeTime(t.updated_at)}
                </time>
              )}
            </button>
          </li>
        ))}
      </ul>
    </section>
  );
  const closed = threads.filter((t) => t.closed);
  return (
    <div className="fixed inset-0 z-20" onClick={onClose}>
      <div
        role="dialog"
        aria-modal="true"
        aria-label={`Threads with ${name}`}
        className="absolute top-15 right-4 flex max-h-[calc(100%-5rem)] w-100 max-w-[calc(100%-2rem)] flex-col gap-1 overflow-auto rounded-[22px] border border-composer-line bg-popover p-2 shadow-[0_18px_48px_rgba(0,0,0,.55)]"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="flex items-center gap-2 pt-1.5 pr-1.5 pb-2 pl-2.5">
          <h2 className="text-sm">Threads with {name}</h2>
          <button
            type="button"
            className="ml-auto flex h-7.5 items-center gap-1.5 rounded-full bg-accent px-3 text-[13px] text-accent-ink"
            onClick={onNew}
          >
            <svg
              aria-hidden="true"
              width="13"
              height="13"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              strokeWidth="2.2"
              strokeLinecap="round"
            >
              <path d="M12 5v14M5 12h14" />
            </svg>
            New thread
          </button>
          <button
            type="button"
            aria-label="Close"
            onClick={onClose}
            className="flex size-7.5 items-center justify-center rounded-full text-muted hover:bg-line"
          >
            <svg
              aria-hidden="true"
              width="14"
              height="14"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              strokeWidth="2"
              strokeLinecap="round"
            >
              <path d="M6 6l12 12M18 6L6 18" />
            </svg>
          </button>
        </div>
        {groups.filter(([, items]) => items.length > 0).map(([label, items]) => list(label, items))}
        {threads.length === closed.length && (
          <p className="px-2.5 py-3 text-[13px] text-muted">No open threads.</p>
        )}
        <button
          type="button"
          className="mt-1 flex items-center gap-1.5 border-t border-line p-2.5 text-left text-[13px] text-muted"
          onClick={() => setShowClosed(!showClosed)}
        >
          Closed · {closed.length}
          <svg
            aria-hidden="true"
            width="12"
            height="12"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            strokeWidth="2"
            strokeLinecap="round"
            className={showClosed ? 'rotate-180' : ''}
          >
            <path d="M6 9l6 6 6-6" />
          </svg>
        </button>
        {showClosed && list('Closed', closed)}
      </div>
    </div>
  );
}
