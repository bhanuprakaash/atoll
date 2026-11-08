import { useState } from 'react';
import { NavLink } from 'react-router';
import { Menu } from '@/components/menu';
import { TextField } from '@/components/text-field';
import type { Bot, SidebarThread } from '@/modules/bots/api/bots';
import { threadTree } from '@/modules/bots/utils/thread-tree';

const marker = (t: SidebarThread) =>
  t.state ? t.state.replace('_', '-') : t.pinned ? 'pinned' : undefined;

const treeItem =
  'relative before:absolute before:top-0 before:-left-3.75 before:bottom-0 before:border-l-[1.5px] before:border-composer-line first:before:-top-2.5 after:absolute after:top-1/2 after:-left-3.75 after:w-2.25 after:border-t-[1.5px] after:border-composer-line last:before:bottom-1/2 last:before:w-2.25 last:before:rounded-bl-lg last:before:border-b-[1.5px] last:after:hidden';

const rowStyle = 'flex h-7 items-center gap-2 rounded-lg px-2 text-[13px]';

function Dot({ className = '' }: { className?: string }) {
  return <span aria-hidden="true" className={`size-1.5 flex-none rounded-full ${className}`} />;
}

function Wave({ label }: { label?: string }) {
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

function ThreadLabel({ thread }: { thread: SidebarThread }) {
  return (
    <>
      {thread.state === 'needs_you' ? (
        <Dot className="bg-danger" />
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
        <span className="flex-none text-xs text-needs-ink">Needs you</span>
      )}
      {thread.state === 'new' && <span className="flex-none text-xs text-kind">New</span>}
      {thread.state === 'running' && <Wave label="Running" />}
    </>
  );
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
  openThreadId,
  unfolded,
  onUnfold,
  onMore,
  onCloseThread,
  onRenameThread,
}: {
  bot: Pick<Bot, 'id' | 'name' | 'threads'>;
  openThreadId?: string;
  unfolded: boolean;
  onUnfold: (unfolded: boolean) => void;
  onMore: (botId: string) => void;
  onCloseThread: (threadId: string) => void;
  onRenameThread: (threadId: string, title: string) => void;
}) {
  const [menu, setMenu] = useState<{ x: number; y: number; thread: SidebarThread } | null>(null);
  const [renaming, setRenaming] = useState<string | null>(null);
  const { rows, more } = threadTree(bot.threads, openThreadId, unfolded);
  if (rows.length === 0) return null;
  return (
    <>
      <ul aria-label={`${bot.name} threads`} className="mb-1.5 pr-1 pl-11.5">
        {rows.map((thread) => (
          <li
            key={thread.id}
            data-marker={marker(thread)}
            className={`group ${treeItem}`}
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
                    `${rowStyle} ${isActive ? 'bg-selected' : 'hover:bg-field'}`
                  }
                >
                  <ThreadLabel thread={thread} />
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
          <li className={treeItem}>
            <button
              type="button"
              onClick={() =>
                more.kind === 'more' ? onMore(bot.id) : onUnfold(more.kind === 'running')
              }
              className={`${rowStyle} w-full text-left text-[12.5px] text-label hover:bg-field`}
            >
              <Dot className={more.kind === 'more' && more.dot ? 'bg-kind' : ''} />
              {more.kind === 'less' ? 'Show less' : `${more.count} more`}
              {more.kind === 'running' && (
                <>
                  {' running'}
                  <Wave />
                </>
              )}
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
