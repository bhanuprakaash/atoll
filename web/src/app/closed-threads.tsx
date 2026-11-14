import { useState } from 'react';
import { Link } from 'react-router';
import { TextField } from '@/components/text-field';
import type { Thread } from '@/modules/threads/api/threads';
import { mrRef } from '@/modules/threads/utils/mr-ref';

const LATEST = 5;

export function ClosedThreads({
  botId,
  threads,
  openThreadId,
}: {
  botId: string;
  threads: Thread[];
  openThreadId?: string;
}) {
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState('');
  if (threads.length === 0) return null;
  const words = query.toLowerCase().split(/\s+/).filter(Boolean);
  const rows = words.length
    ? threads.filter((t) => {
        const text = [t.title, t.branch, mrRef(t.mr_url)].join(' ').toLowerCase();
        return words.every((w) => text.includes(w));
      })
    : threads.slice(0, LATEST);
  return (
    <>
      <button
        type="button"
        aria-expanded={open}
        onClick={() => setOpen(!open)}
        className="mt-3 flex items-center gap-1.5 px-2 py-1 text-[13px] text-muted"
      >
        Closed · {threads.length}
        <svg
          aria-hidden="true"
          width="11"
          height="11"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          strokeWidth="2.2"
          strokeLinecap="round"
          className={open ? 'rotate-180' : ''}
        >
          <path d="M6 9l6 6 6-6" />
        </svg>
      </button>
      {open && (
        <>
          <TextField
            size="bare"
            aria-label="Search closed threads"
            placeholder="Search closed threads"
            value={query}
            onChange={(event) => setQuery(event.target.value)}
            onKeyDown={(event) => {
              if (event.key === 'Escape') setQuery('');
            }}
            className="mt-1 mb-1 h-8 w-full rounded-[10px] bg-field px-3"
          />
          <div role="list" aria-label="Closed" className="flex flex-col gap-0.5">
            {rows.map((t) => {
              const selected = t.id === openThreadId;
              return (
                <div
                  key={t.id}
                  role="listitem"
                  className={`rounded-lg ${selected ? 'bg-selected' : 'hover:bg-field'}`}
                >
                  <Link
                    to={`/bots/${botId}/threads/${t.id}`}
                    aria-current={selected ? 'page' : undefined}
                    draggable={false}
                    className="flex h-8 items-center px-2 text-sm text-muted"
                  >
                    <span className="min-w-0 truncate">{t.title || 'Empty thread'}</span>
                  </Link>
                </div>
              );
            })}
          </div>
          {words.length > 0 && rows.length === 0 && (
            <p className="px-2 py-3 text-sm text-muted">No closed threads match</p>
          )}
        </>
      )}
    </>
  );
}
