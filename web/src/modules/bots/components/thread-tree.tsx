import { useState } from 'react';
import { NavLink } from 'react-router';
import { Menu } from '@/components/menu';
import { TextField } from '@/components/text-field';
import type { Bot, SidebarThread } from '@/modules/bots/api/bots';
import { threadTree } from '@/modules/bots/utils/thread-tree';

const marker = (t: SidebarThread) =>
  t.state ? t.state.replace('_', '-') : t.pinned ? 'pinned' : undefined;

const rowStyle = 'flex h-8 items-center gap-2.5 rounded-lg px-2 text-sm';

function Dot({ className = '' }: { className?: string }) {
  return <span aria-hidden="true" className={`size-1.5 flex-none rounded-full ${className}`} />;
}

export function Wave({ label }: { label?: string }) {
  return (
    <span
      {...(label ? { role: 'img', 'aria-label': label } : { 'aria-hidden': true })}
      className="flex flex-none items-center gap-0.5"
    >
      {[0, 1, 2].map((i) => (
        <i
          key={i}
          style={{ animationDelay: `${i * 0.15}s` }}
          className="size-0.75 rounded-full bg-muted motion-safe:animate-wave"
        />
      ))}
    </span>
  );
}

function Pin() {
  return (
    <svg
      role="img"
      aria-label="Pinned"
      width="10"
      height="10"
      viewBox="0 0 24 24"
      fill="currentColor"
      stroke="currentColor"
      strokeWidth="1.6"
      strokeLinejoin="round"
      className="-mx-0.5 flex-none text-muted"
    >
      <path d="M9 4h6l-1 6 3 3H7l3-3-1-6z" />
      <path d="M12 13v7" fill="none" strokeLinecap="round" />
    </svg>
  );
}

export function ThreadLabel({ thread }: { thread: SidebarThread }) {
  return (
    <>
      {thread.state === 'needs_you' ? (
        <Dot className="bg-accent" />
      ) : thread.state === 'new' ? (
        <Dot className="bg-kind" />
      ) : thread.pinned && thread.state === '' ? (
        <Pin />
      ) : (
        <Dot />
      )}
      <span className={`min-w-0 flex-1 truncate ${thread.state === 'new' ? 'font-semibold' : ''}`}>
        {thread.title || 'Empty thread'}
      </span>
      {thread.state === 'needs_you' && (
        <span className="flex-none text-xs text-ink">Needs you</span>
      )}
      {thread.state === 'new' && <span className="flex-none text-xs text-kind">New</span>}
      {thread.state === 'running' && <Wave label="Running" />}
    </>
  );
}

function ThreadMarker({ thread }: { thread: SidebarThread }) {
  if (thread.state === 'needs_you')
    return (
      <>
        <span className="flex-none text-xs text-ink">Needs you</span>
        <Dot className="bg-accent" />
      </>
    );
  if (thread.state === 'new')
    return (
      <>
        <Dot className="bg-kind" />
        <span className="sr-only">New</span>
      </>
    );
  if (thread.state === 'running') return <Wave label="Running" />;
  if (thread.pinned && thread.state === '') return <Pin />;
  return null;
}

function RenameField({ title, onDone }: { title: string; onDone: (title?: string) => void }) {
  return (
    <TextField
      size="bare"
      autoFocus
      aria-label="Thread title"
      defaultValue={title}
      onBlur={() => onDone()}
      onKeyDown={(event) => {
        if (event.key === 'Escape') onDone();
        if (event.key === 'Enter') onDone(event.currentTarget.value.trim());
      }}
      className={`${rowStyle} w-full border border-outline bg-field`}
    />
  );
}

export function ThreadTree({
  bot,
  selected,
  openThreadId,
  unfolded,
  onUnfold,
  onMore,
  onCloseThread,
  onRenameThread,
  only,
}: {
  bot: Pick<Bot, 'id' | 'name' | 'threads'>;
  selected: boolean;
  openThreadId?: string;
  unfolded: boolean;
  onUnfold: (unfolded: boolean) => void;
  onMore: (botId: string) => void;
  onCloseThread: (threadId: string) => void;
  onRenameThread: (threadId: string, title: string) => void;
  only?: SidebarThread[];
}) {
  const [menu, setMenu] = useState<{ x: number; y: number; thread: SidebarThread } | null>(null);
  const [renaming, setRenaming] = useState<string | null>(null);
  const { rows, more } = only
    ? { rows: only, more: null }
    : threadTree(bot.threads ?? [], openThreadId, unfolded);
  if (rows.length === 0) return null;
  return (
    <>
      <ul
        aria-label={`${bot.name} threads`}
        className={`relative mb-1.5 flex flex-col gap-0.5 pr-1 pl-10.5 before:absolute before:top-0 before:bottom-4 before:left-[27.25px] before:w-2.5 before:rounded-bl-[9px] before:border-b-[1.5px] before:border-l-[1.5px] before:border-composer-line ${selected ? 'mt-1' : ''}`}
      >
        {rows.map((thread) => (
          <li
            key={thread.id}
            data-marker={marker(thread)}
            className="group relative"
            onContextMenu={(event) => {
              event.preventDefault();
              setMenu({ x: event.clientX, y: event.clientY, thread });
            }}
          >
            {renaming === thread.id ? (
              <RenameField
                title={thread.title}
                onDone={(title) => {
                  setRenaming(null);
                  if (title && title !== thread.title) onRenameThread(thread.id, title);
                }}
              />
            ) : (
              <>
                <NavLink
                  to={`/bots/${bot.id}/threads/${thread.id}`}
                  className={({ isActive }) =>
                    `${rowStyle} ${thread.helper ? '' : 'group-hover:pr-9'} ${isActive ? 'bg-selected' : 'hover:bg-field'}`
                  }
                >
                  <span
                    className={`min-w-0 flex-1 truncate ${thread.state === 'new' ? 'font-semibold' : ''}`}
                  >
                    {thread.title || 'Empty thread'}
                  </span>
                  <ThreadMarker thread={thread} />
                </NavLink>
                {!thread.helper && (
                  <button
                    type="button"
                    aria-label="Close thread"
                    title="Close thread"
                    onClick={() => onCloseThread(thread.id)}
                    className="absolute top-1/2 right-2 flex size-5 -translate-y-1/2 items-center justify-center rounded-md bg-tile text-ink opacity-0 group-hover:opacity-100 focus-visible:opacity-100"
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
          </li>
        ))}
        {more && (
          <li>
            <button
              type="button"
              onClick={() =>
                more.kind === 'more' ? onMore(bot.id) : onUnfold(more.kind === 'running')
              }
              className={`${rowStyle} w-full text-left text-[13px] text-label hover:bg-field`}
            >
              <span className="min-w-0 flex-1 truncate">
                {more.kind === 'less' ? 'Show less' : `${more.count} more`}
                {more.kind === 'running' && ' running'}
              </span>
              {more.kind === 'running' && <Wave />}
              {more.kind === 'more' && more.dot && <Dot className="bg-kind" />}
            </button>
          </li>
        )}
      </ul>
      {menu && (
        <Menu
          at={menu}
          onClose={() => setMenu(null)}
          items={[
            { label: 'Rename', onSelect: () => setRenaming(menu.thread.id) },
            ...(menu.thread.helper
              ? []
              : [{ label: 'Close', onSelect: () => onCloseThread(menu.thread.id) }]),
          ]}
        />
      )}
    </>
  );
}
