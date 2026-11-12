import { useEffect, useState } from 'react';
import { Link, useNavigate } from 'react-router';
import { Creature } from '@/components/creature';
import { TextField } from '@/components/text-field';
import { ErrorNotice } from '@/components/error-notice';
import { ApiError } from '@/lib/api-client';
import { useBots, type SidebarThread } from '@/modules/bots/api/bots';
import { ThreadLabel } from '@/modules/bots/components/thread-tree';
import { threadTree } from '@/modules/bots/utils/thread-tree';
import {
  useCloseThread,
  usePinThread,
  useThreads,
  type Thread,
} from '@/modules/threads/api/threads';
import { mrRef } from '@/modules/threads/utils/mr-ref';
import { relativeTime } from '@/utils/time';

const isPinLimit = (error: Error) => error instanceof ApiError && error.status === 409;

const actionButton =
  'flex h-6 items-center justify-center rounded-md bg-tile px-1.5 text-xs text-ink';

function ThreadRow({
  botId,
  thread,
  detail,
  closed,
  openThreadId,
  onPin,
  onClose,
}: {
  botId: string;
  thread: SidebarThread;
  detail?: Thread;
  closed?: boolean;
  openThreadId?: string;
  onPin: () => void;
  onClose: () => void;
}) {
  const sub = [detail?.branch, mrRef(detail?.mr_url)].filter(Boolean).join(' · ');
  const selected = thread.id === openThreadId;
  return (
    <li className="group relative">
      <Link
        to={`/bots/${botId}/threads/${thread.id}`}
        aria-current={selected ? 'page' : undefined}
        className={`flex min-h-11 flex-col justify-center rounded-xl px-2.5 py-1 ${selected ? 'bg-selected' : 'hover:bg-selected'}`}
      >
        <span className="flex items-center gap-2 text-sm">
          <ThreadLabel thread={thread} />
          {detail && (
            <span className="flex-none text-xs text-muted">
              {relativeTime(detail.last_activity)}
            </span>
          )}
        </span>
        {sub && <span className="truncate pl-3.5 text-[12.5px] text-muted">{sub}</span>}
      </Link>
      {!closed && !thread.helper && (
        <span className="absolute top-1/2 right-2 flex -translate-y-1/2 gap-1 rounded-lg bg-selected opacity-0 group-focus-within:opacity-100 group-hover:opacity-100">
          <button type="button" onClick={onPin} className={actionButton}>
            {thread.pinned ? 'Unpin' : 'Pin to sidebar'}
          </button>
          <button
            type="button"
            aria-label="Close thread"
            title="Close thread"
            onClick={onClose}
            className={`${actionButton} w-6`}
          >
            <svg
              aria-hidden="true"
              width="10"
              height="10"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              strokeWidth="2.6"
              strokeLinecap="round"
            >
              <path d="M6 6l12 12M18 6L6 18" />
            </svg>
          </button>
        </span>
      )}
    </li>
  );
}

export function ThreadsPopup({
  botId,
  openThreadId,
  onClose,
}: {
  botId: string;
  openThreadId?: string;
  onClose: () => void;
}) {
  const navigate = useNavigate();
  const { data: bots = [] } = useBots();
  const { data: threads = [] } = useThreads(botId);
  const pin = usePinThread();
  const close = useCloseThread();
  const [query, setQuery] = useState('');
  const [showClosed, setShowClosed] = useState(false);
  const [replacing, setReplacing] = useState<SidebarThread | null>(null);
  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key !== 'Escape') return;
      event.preventDefault();
      if (replacing) setReplacing(null);
      else onClose();
    };
    window.addEventListener('keydown', onKeyDown, true);
    return () => window.removeEventListener('keydown', onKeyDown, true);
  }, [replacing, onClose]);
  const bot = bots.find((b) => b.id === botId);
  if (!bot) return null;

  const openThreads = bot.threads ?? [];
  const detail = new Map(threads.map((t) => [t.id, t]));
  const q = query.trim().toLowerCase();
  const matches = (t: SidebarThread) => {
    const d = detail.get(t.id);
    return [t.title, d?.branch, mrRef(d?.mr_url)].some((s) => s?.toLowerCase().includes(q));
  };
  const inSidebar = new Set(threadTree(openThreads, openThreadId, false).rows.map((t) => t.id));
  const closedThreads = threads
    .filter((t) => t.closed)
    .map((t): SidebarThread => ({ id: t.id, title: t.title, state: '', pinned: false }));
  const sections = [
    { title: 'In the sidebar', rows: openThreads.filter((t) => inSidebar.has(t.id)) },
    { title: 'Other open threads', rows: openThreads.filter((t) => !inSidebar.has(t.id)) },
  ].map((s) => ({ ...s, rows: s.rows.filter(matches) }));
  const closedRows = closedThreads.filter(matches);
  const setPinned = (thread: SidebarThread, pinned: boolean, replace?: string) =>
    pin.mutate(
      { id: thread.id, pinned, replace },
      {
        onSuccess: () => setReplacing(null),
        onError: (error) => {
          if (isPinLimit(error)) setReplacing(thread);
        },
      },
    );
  const row = (thread: SidebarThread, closed?: boolean) => (
    <ThreadRow
      key={thread.id}
      botId={botId}
      thread={thread}
      detail={detail.get(thread.id)}
      closed={closed}
      openThreadId={openThreadId}
      onPin={() => setPinned(thread, !thread.pinned)}
      onClose={() => close.mutate(thread.id)}
    />
  );
  return (
    <>
      <div
        className="fixed inset-0 z-30 bg-scrim"
        onMouseDown={(event) => event.target === event.currentTarget && onClose()}
      >
        <section
          role="dialog"
          aria-modal="true"
          aria-label={`Threads with ${bot.name}`}
          className="mx-auto mt-20 flex max-h-[min(740px,calc(100vh-6rem))] w-140 max-w-[calc(100vw-2rem)] flex-col overflow-hidden rounded-[22px] border border-composer-line bg-popover shadow-[0_24px_64px_rgba(0,0,0,.6)]"
        >
          <div className="flex items-center gap-3 px-4 pt-4 pb-3">
            <Creature look={bot.look} state={bot.state} size={40} />
            <span className="min-w-0 flex-1">
              <span className="block truncate text-base">{bot.name}</span>
              <span className="block text-[12.5px] text-muted">
                {openThreads.length} open threads · {closedThreads.length} closed
              </span>
            </span>
            <button
              type="button"
              onClick={() => navigate(`/bots/${botId}/new`)}
              className="h-8 rounded-control bg-accent px-3 text-[13px] font-medium text-accent-ink"
            >
              New thread
            </button>
            <button
              type="button"
              aria-label="Close"
              onClick={onClose}
              className="flex size-7 flex-none items-center justify-center rounded-lg text-muted hover:text-ink"
            >
              <svg
                aria-hidden="true"
                width="14"
                height="14"
                viewBox="0 0 24 24"
                fill="none"
                stroke="currentColor"
                strokeWidth="2.2"
                strokeLinecap="round"
              >
                <path d="M6 6l12 12M18 6L6 18" />
              </svg>
            </button>
          </div>
          <div className="px-4 pb-2">
            <TextField
              type="search"
              size="bare"
              autoFocus
              aria-label="Search threads"
              placeholder="Search threads, branches, MRs"
              value={query}
              onChange={(event) => setQuery(event.target.value)}
              className="h-9.5 w-full rounded-xl border border-outline bg-field px-3"
            />
          </div>
          {pin.isError && !isPinLimit(pin.error) && (
            <ErrorNotice error={pin.error} className="px-4 pb-2" />
          )}
          <div className="min-h-0 flex-1 overflow-y-auto border-t border-line px-2 pb-2">
            {sections.map(
              ({ title, rows }) =>
                rows.length > 0 && (
                  <section key={title}>
                    <h3 className="px-2.5 pt-3 pb-1 text-[11.5px] tracking-[.06em] text-label uppercase">
                      {title}
                    </h3>
                    <ul aria-label={title}>{rows.map((t) => row(t))}</ul>
                  </section>
                ),
            )}
            {q && sections.every((s) => s.rows.length === 0) && closedRows.length === 0 && (
              <p className="px-2.5 py-3 text-sm text-muted">No threads match</p>
            )}
            {showClosed && <ul aria-label="Closed">{closedRows.map((t) => row(t, true))}</ul>}
          </div>
          <button
            type="button"
            aria-expanded={showClosed}
            onClick={() => setShowClosed(!showClosed)}
            className="flex flex-none items-center gap-1.5 border-t border-line px-4.5 py-3 text-left text-[13px] text-muted"
          >
            Closed · {closedThreads.length}
            <svg
              aria-hidden="true"
              width="11"
              height="11"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              strokeWidth="2.2"
              strokeLinecap="round"
              className={showClosed ? 'rotate-180' : ''}
            >
              <path d="M6 9l6 6 6-6" />
            </svg>
            <span className="ml-auto text-xs text-label">Esc to close</span>
          </button>
        </section>
      </div>
      {replacing && (
        <div className="fixed inset-0 z-40 flex items-center justify-center bg-scrim p-4">
          <section
            role="dialog"
            aria-modal="true"
            aria-label="Replace which pin?"
            className="w-full max-w-100 rounded-[22px] border border-composer-line bg-popover p-5 shadow-[0_18px_48px_rgba(0,0,0,.55)]"
          >
            <h2 className="mb-1 text-base">Replace which pin?</h2>
            <p className="mb-3 text-[13px] text-muted">
              Three threads fit in the sidebar. Pick the one to unpin.
            </p>
            <div className="flex flex-col gap-1">
              {openThreads
                .filter((t) => t.pinned)
                .sort((a, b) =>
                  (detail.get(a.id)?.created_at ?? '').localeCompare(
                    detail.get(b.id)?.created_at ?? '',
                  ),
                )
                .map((t) => (
                  <button
                    key={t.id}
                    type="button"
                    onClick={() => setPinned(replacing, true, t.id)}
                    className="h-9 truncate rounded-control bg-tile px-3 text-left text-[13.5px]"
                  >
                    {t.title || 'Empty thread'}
                  </button>
                ))}
            </div>
          </section>
        </div>
      )}
    </>
  );
}
