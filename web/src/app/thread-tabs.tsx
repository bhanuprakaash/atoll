import { useEffect, useState } from 'react';
import { Link, useLocation, useNavigate } from 'react-router';
import { ClosedThreads } from '@/app/closed-threads';
import { ReplacePin } from '@/app/replace-pin';
import { ErrorNotice } from '@/components/error-notice';
import { Menu } from '@/components/menu';
import { TextField } from '@/components/text-field';
import { ApiError } from '@/lib/api-client';
import type { Bot, SidebarThread } from '@/modules/bots/api/bots';
import { RenameField, ThreadMarker } from '@/modules/bots/components/thread-tree';
import { orderTabs, otherThreads, tabThreads } from '@/modules/bots/utils/thread-tree';
import {
  useCloseThread,
  usePinThread,
  useRenameThread,
  useThreads,
  type Thread,
} from '@/modules/threads/api/threads';
import { mrRef } from '@/modules/threads/utils/mr-ref';

const isPinLimit = (error: Error) => error instanceof ApiError && error.status === 409;

function matchesQuery(t: SidebarThread, words: string[]) {
  const text = [t.title, t.branch, ...t.prs.map((p) => mrRef(p.url))].join(' ').toLowerCase();
  return words.every((w) => text.includes(w));
}

const barButton =
  'flex h-8 flex-none items-center justify-center rounded-lg text-[13px] text-muted hover:text-ink';

function Tab({
  botId,
  thread,
  selected,
  onPin,
  onClose,
  onRename,
}: {
  botId: string;
  thread: SidebarThread;
  selected: boolean;
  onPin: () => void;
  onClose: () => void;
  onRename: (title: string) => void;
}) {
  const [menu, setMenu] = useState<{ x: number; y: number } | null>(null);
  const [renaming, setRenaming] = useState(false);
  return (
    <>
      <div
        role="presentation"
        onContextMenu={(event) => {
          event.preventDefault();
          setMenu({ x: event.clientX, y: event.clientY });
        }}
        className={`group relative flex h-8 max-w-56 min-w-0 flex-none items-center rounded-lg ${selected ? 'bg-selected text-ink' : 'text-muted hover:bg-field hover:text-ink'}`}
      >
        {renaming ? (
          <RenameField
            title={thread.title}
            onDone={(title) => {
              setRenaming(false);
              if (title && title !== thread.title) onRename(title);
            }}
          />
        ) : (
          <>
            <Link
              role="tab"
              aria-selected={selected}
              to={`/bots/${botId}/threads/${thread.id}`}
              draggable={false}
              className="flex h-8 min-w-0 items-center gap-2 rounded-lg pr-2 pl-3 text-sm outline-none"
            >
              <span className={`min-w-0 truncate ${thread.state === 'new' ? 'font-semibold' : ''}`}>
                {thread.title || 'Empty thread'}
              </span>
              <span
                className={`flex flex-none items-center gap-2 ${thread.helper ? '' : 'group-focus-within:hidden group-hover:hidden'}`}
              >
                <ThreadMarker thread={thread} showPin />
              </span>
            </Link>
            {!thread.helper && (
              <button
                type="button"
                aria-label="Close thread"
                title="Close thread"
                onClick={onClose}
                className="mr-1.5 hidden size-5 flex-none items-center justify-center rounded-md bg-tile text-ink group-focus-within:flex group-hover:flex"
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
            )}
          </>
        )}
      </div>
      {menu && (
        <Menu
          at={menu}
          onClose={() => setMenu(null)}
          items={[
            { label: 'Rename', onSelect: () => setRenaming(true) },
            ...(thread.helper
              ? []
              : [
                  { label: thread.pinned ? 'Unpin' : 'Pin', onSelect: onPin },
                  { label: 'Close', onSelect: onClose },
                ]),
          ]}
        />
      )}
    </>
  );
}

function OtherThreads({
  botId,
  threads,
  closed,
  openThreadId,
  at,
  onClose,
}: {
  botId: string;
  threads: SidebarThread[];
  closed: Thread[];
  openThreadId?: string;
  at: { x: number; y: number };
  onClose: () => void;
}) {
  const [query, setQuery] = useState('');
  const words = query.toLowerCase().split(/\s+/).filter(Boolean);
  const matching = threads.filter((t) => matchesQuery(t, words));
  return (
    <>
      <div className="fixed inset-0 z-40" onClick={onClose} />
      <section
        role="dialog"
        aria-label="Other threads"
        onKeyDown={(event) => {
          if (event.key !== 'Escape') return;
          event.preventDefault();
          onClose();
        }}
        style={{ left: Math.max(8, Math.min(at.x, window.innerWidth - 328)), top: at.y }}
        className="fixed z-50 flex max-h-[min(28rem,calc(100vh-6rem))] w-80 max-w-[calc(100vw-1rem)] flex-col rounded-card border border-outline bg-composer p-1.25 shadow-[0_12px_32px_rgba(0,0,0,.5)]"
      >
        <TextField
          type="search"
          size="bare"
          autoFocus
          aria-label="Search threads"
          placeholder="Search threads"
          value={query}
          onChange={(event) => setQuery(event.target.value)}
          onKeyDown={(event) => {
            if (event.key === 'Escape' && query) {
              event.stopPropagation();
              setQuery('');
            }
          }}
          className="mb-1 h-8.5 flex-none rounded-[10px] bg-field px-3"
        />
        <div className="min-h-0 flex-1 overflow-y-auto">
          <div role="list" aria-label="Other threads" className="flex flex-col gap-0.5">
            {matching.map((thread) => (
              <div key={thread.id} role="listitem">
                <Link
                  to={`/bots/${botId}/threads/${thread.id}`}
                  aria-current={thread.id === openThreadId ? 'page' : undefined}
                  draggable={false}
                  className="flex h-8 items-center gap-2 rounded-lg px-2 text-sm hover:bg-field"
                >
                  <span
                    className={`min-w-0 flex-1 truncate ${thread.state === 'new' ? 'font-semibold' : ''}`}
                  >
                    {thread.title || 'Empty thread'}
                  </span>
                  <ThreadMarker thread={thread} showPin />
                </Link>
              </div>
            ))}
          </div>
          {words.length > 0 && matching.length === 0 && (
            <p className="px-2 py-3 text-sm text-muted">No threads match</p>
          )}
          <ClosedThreads botId={botId} threads={closed} openThreadId={openThreadId} />
        </div>
      </section>
    </>
  );
}

// ThreadTabs is the bar above the chat: a tab for the open thread and for each of the Bot's
// threads that need the person, run or are pinned, in the order they got their tab.
export function ThreadTabs({ bot, openThreadId }: { bot: Bot; openThreadId?: string }) {
  const navigate = useNavigate();
  const { key } = useLocation();
  const close = useCloseThread();
  const rename = useRenameThread();
  const pin = usePinThread();
  const { data: details = [] } = useThreads(bot.id);
  const [orders, setOrders] = useState<Record<string, string[]>>({});
  const [replacing, setReplacing] = useState<SidebarThread | null>(null);
  const [more, setMore] = useState<{ x: number; y: number } | null>(null);
  useEffect(() => setMore(null), [key]);
  const open = bot.threads ?? [];
  const closedOpen = details.find((t) => t.id === openThreadId && t.closed);
  const tabs = tabThreads(
    closedOpen
      ? [
          ...open,
          {
            id: closedOpen.id,
            title: closedOpen.title,
            state: '' as const,
            pinned: false,
            parent_thread_id: closedOpen.parent_thread_id,
            branch: closedOpen.branch,
            prs: closedOpen.prs,
            last_activity: closedOpen.last_activity,
          },
        ]
      : open,
    openThreadId,
  );
  const order = orderTabs(orders[bot.id] ?? [], tabs);
  if (order.join() !== (orders[bot.id] ?? []).join()) setOrders({ ...orders, [bot.id]: order });
  const others = otherThreads(open, tabs);
  const closed = details.filter((t) => t.closed);
  const created = new Map(details.map((t) => [t.id, t.created_at]));
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
  return (
    <>
      <div className="flex min-w-0 items-center gap-1">
        <div
          role="tablist"
          aria-label="Threads"
          className="flex min-w-0 items-center gap-1 overflow-x-auto [scrollbar-width:none]"
        >
          {order.map((id) => {
            const thread = tabs.find((t) => t.id === id)!;
            return (
              <Tab
                key={id}
                botId={bot.id}
                thread={thread}
                selected={id === openThreadId}
                onPin={() => setPinned(thread, !thread.pinned)}
                onClose={() =>
                  close.mutate(id, {
                    onSuccess: () => {
                      if (id !== openThreadId) return;
                      const at = order.indexOf(id);
                      const next = order[at + 1] ?? order[at - 1];
                      navigate(next ? `/bots/${bot.id}/threads/${next}` : `/bots/${bot.id}/new`);
                    },
                  })
                }
                onRename={(title) => rename.mutate({ id, title })}
              />
            );
          })}
        </div>
        {others.length + closed.length > 0 && (
          <button
            type="button"
            aria-haspopup="dialog"
            aria-expanded={more !== null}
            onClick={(event) => {
              const box = event.currentTarget.getBoundingClientRect();
              setMore({ x: box.left, y: box.bottom + 4 });
            }}
            className={`${barButton} px-2.5`}
          >
            {others.length > 0 ? `${others.length} more` : 'Closed'}
          </button>
        )}
        <button
          type="button"
          aria-label="New thread"
          title="New thread (⌘N)"
          onClick={() => navigate(`/bots/${bot.id}/new`)}
          className={`${barButton} w-8`}
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
            <path d="M12 5v14M5 12h14" />
          </svg>
        </button>
      </div>
      {pin.isError && !isPinLimit(pin.error) && (
        <ErrorNotice error={pin.error} className="absolute top-full left-4 z-20" />
      )}
      {more && (
        <OtherThreads
          botId={bot.id}
          threads={others}
          closed={closed}
          openThreadId={openThreadId}
          at={more}
          onClose={() => setMore(null)}
        />
      )}
      {replacing && (
        <ReplacePin
          pins={open
            .filter((t) => t.pinned)
            .sort((a, b) => (created.get(a.id) ?? '').localeCompare(created.get(b.id) ?? ''))}
          onPick={(id) => setPinned(replacing, true, id)}
          onClose={() => setReplacing(null)}
        />
      )}
    </>
  );
}
