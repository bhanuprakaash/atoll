import { useState } from 'react';
import type { Thread } from '@/modules/threads/api/threads';
import { approvalTarget } from '@/modules/threads/components/approval-card';
import { mrRef } from '@/modules/threads/utils/mr-ref';
import { relativeTime } from '@/utils/time';

export function ThreadsPanel({
  name,
  botNames,
  threads,
  repos,
  onNew,
  onSelect,
  currentId,
}: {
  name: string;
  botNames: Record<string, string>;
  threads: Thread[];
  repos: { id: string; name: string }[];
  onNew: () => void;
  onSelect: (id: string) => void;
  currentId?: string;
}) {
  const [showClosed, setShowClosed] = useState(false);
  const [query, setQuery] = useState('');
  const searchable = threads.filter((t) => !t.closed).length > 8;
  const match = (t: Thread) =>
    !searchable || t.title.toLowerCase().includes(query.trim().toLowerCase());
  const groups: [string, Thread[]][] = [
    ['Needs you', threads.filter((t) => !t.closed && !!t.approval && match(t))],
    [
      'Running',
      threads.filter((t) => !t.closed && !t.approval && t.run_status === 'running' && match(t)),
    ],
    [
      'Recent',
      threads.filter((t) => !t.closed && !t.approval && t.run_status !== 'running' && match(t)),
    ],
  ];
  const shown = groups.filter(([, items]) => items.length > 0);
  const list = (label: string, items: Thread[]) => (
    <section key={label}>
      {label !== 'Closed' && (
        <h3
          className={`sticky top-0 bg-composer px-2.5 pt-2.5 pb-0.5 text-[11.5px] tracking-[.06em] uppercase ${label === 'Needs you' ? 'text-danger' : 'text-label'}`}
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
                  {t.parent_thread_id && (
                    <span className="text-xs text-muted">
                      {` · from ${botNames[t.parent_bot_id] ?? 'a Bot'}`}
                    </span>
                  )}
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
                          `${mrRef(t.mr_url) ?? '!MR'}${t.mr_state ? ` ${t.mr_state}` : ''}`,
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
    <div
      role="dialog"
      aria-label={`Threads with ${name}`}
      className="flex min-h-0 flex-1 flex-col gap-1 border-t border-composer-line p-2"
    >
      <div className="flex flex-none items-center gap-2">
        <button
          type="button"
          className="flex h-9 flex-none items-center gap-2 rounded-[14px] px-3 text-[13.5px] hover:bg-line"
          onClick={onNew}
        >
          <svg
            aria-hidden="true"
            width="15"
            height="15"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            strokeWidth="1.8"
            strokeLinecap="round"
          >
            <path d="M12 5v14M5 12h14" />
          </svg>
          New thread
        </button>
        {searchable && (
          <label className="flex h-8.5 min-w-0 flex-1 items-center gap-2 rounded-xl border border-composer-line bg-card px-2.5 text-muted">
            <svg
              aria-hidden="true"
              width="14"
              height="14"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              strokeWidth="1.8"
              strokeLinecap="round"
            >
              <circle cx="11" cy="11" r="6.5" />
              <path d="M16 16l4 4" />
            </svg>
            <span className="sr-only">Search threads</span>
            <input
              type="search"
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              placeholder="Search threads"
              className="min-w-0 flex-1 bg-transparent text-[13px] text-ink outline-none"
            />
          </label>
        )}
      </div>
      <div className="min-h-0 flex-1 overflow-y-auto">
        {shown.map(([label, items]) => list(label, items))}
        {shown.length === 0 && (
          <p className="px-2.5 py-3 text-[13px] text-muted">
            {threads.length === closed.length ? 'No open threads.' : 'No open thread matches.'}
          </p>
        )}
      </div>
      <button
        type="button"
        className="mt-1 flex flex-none items-center gap-1.5 border-t border-line p-2.5 text-left text-[13px] text-muted"
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
      {showClosed && (
        <div className="max-h-48 flex-none overflow-y-auto">
          {list('Closed', closed.filter(match))}
        </div>
      )}
    </div>
  );
}
