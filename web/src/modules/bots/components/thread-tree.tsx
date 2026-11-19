import { Fragment, useState } from 'react';
import { Link } from 'react-router';
import { Creature } from '@/components/creature';
import { Menu } from '@/components/menu';
import { TextField } from '@/components/text-field';
import type { Bot, SidebarThread } from '@/modules/bots/api/bots';
import { workerNames, type Worker } from '@/modules/bots/utils/thread-tree';
import { clockTime } from '@/utils/time';

const marker = (t: SidebarThread) =>
  t.state ? t.state.replace('_', '-') : t.pinned ? 'pinned' : undefined;

const rowStyle = 'flex items-center gap-2.5 rounded-lg px-2 text-sm';

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

const outUntil = (thread: SidebarThread) =>
  thread.resets_at ? `Out until ${clockTime(thread.resets_at)}` : 'Out';

function ThreadMarker({ thread }: { thread: SidebarThread }) {
  if (thread.state === 'needs_you')
    return (
      <>
        <span className="flex-none text-xs text-ink">Needs you</span>
        <Dot className="bg-accent" />
      </>
    );
  if (thread.state === 'failed')
    return (
      <>
        <span className="flex-none text-xs text-ink">Failed</span>
        <Dot className="bg-danger" />
      </>
    );
  if (thread.state === 'limited')
    return <span className="flex-none text-xs text-muted">{outUntil(thread)}</span>;
  if (thread.state === 'new')
    return (
      <>
        <Dot className="bg-kind" />
        <span className="sr-only">Replied</span>
      </>
    );
  if (thread.state === 'running') return <Wave label="Working" />;
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
      className={`${rowStyle} h-8 w-full border border-outline bg-field`}
    />
  );
}

function HelpersLine({
  botId,
  threadId,
  workers,
}: {
  botId: string;
  threadId: string;
  workers: Worker[];
}) {
  const needsYou = workers[0].needsYou;
  return (
    <div className="relative ml-2 pl-4 before:absolute before:-top-0.5 before:left-2 before:h-4.5 before:w-3.5 before:rounded-bl-[9px] before:border-b-[1.5px] before:border-l-[1.5px] before:border-composer-line">
      <Link
        data-line="helpers"
        to={`/bots/${botId}/threads/${threadId}`}
        draggable={false}
        state={{ helpers: true }}
        className="flex h-8 items-center gap-2 rounded-lg px-2 text-[13px] text-muted hover:bg-field"
      >
        <span aria-hidden="true" className="flex flex-none items-center -space-x-1">
          {workers.slice(0, 3).map((w) => (
            <Creature
              key={w.id}
              look={w.bot.look}
              colour={w.bot.colour}
              state={w.needsYou ? 'needs_you' : 'working'}
              size={18}
            />
          ))}
        </span>
        <span className="flex-none text-ink">
          {needsYou ? workers[0].bot.name : workerNames(workers)}
        </span>{' '}
        {needsYou ? (
          <>
            <span className="flex-none text-ink">Needs you</span>
            <Dot className="bg-accent" />
            {workers.length > 1 && <span className="flex-none">+{workers.length - 1}</span>}
          </>
        ) : (
          <>
            <span className="min-w-0 flex-1 truncate">
              {[workers[0].wake, workers[0].step].filter(Boolean).join(' · ') || 'Working'}
            </span>
            <Wave />
          </>
        )}
      </Link>
    </div>
  );
}

export function ThreadSection({
  label,
  threads,
  botId,
  openThreadId,
  workers,
  owners,
  onPinThread,
  onCloseThread,
  onRenameThread,
}: {
  label: string;
  threads: SidebarThread[];
  botId?: string;
  openThreadId?: string;
  workers: Map<string, Worker[]>;
  owners?: Map<string, { bot: Bot; note: string }>;
  onPinThread?: (thread: SidebarThread) => void;
  onCloseThread?: (threadId: string) => void;
  onRenameThread?: (threadId: string, title: string) => void;
}) {
  const [menu, setMenu] = useState<{ x: number; y: number; thread: SidebarThread } | null>(null);
  const [renaming, setRenaming] = useState<string | null>(null);
  return (
    <>
      <div
        aria-hidden="true"
        className="px-2 pt-3 pb-1 text-[11.5px] tracking-[.06em] text-label uppercase"
      >
        {label}
      </div>
      <div role="list" aria-label={label} className="flex flex-col gap-0.5">
        {threads.map((thread) => {
          const selected = thread.id === openThreadId;
          const owner = owners?.get(thread.id);
          const ownerId = owner?.bot.id ?? botId!;
          return (
            <Fragment key={thread.id}>
              <div
                role="listitem"
                data-marker={marker(thread)}
                onContextMenu={(event) => {
                  if (!onRenameThread) return;
                  event.preventDefault();
                  setMenu({ x: event.clientX, y: event.clientY, thread });
                }}
                className={`group relative flex items-center rounded-lg ${owner ? 'gap-2.5 py-2' : 'gap-2'} ${selected ? 'bg-selected' : 'hover:bg-field'}`}
              >
                {renaming === thread.id ? (
                  <RenameField
                    title={thread.title}
                    onDone={(title) => {
                      setRenaming(null);
                      if (title && title !== thread.title) onRenameThread?.(thread.id, title);
                    }}
                  />
                ) : (
                  <>
                    {owner && (
                      <span className="flex-none pl-2.5">
                        <Creature
                          look={owner.bot.look}
                          colour={owner.bot.colour}
                          state={owner.bot.state}
                          size={22}
                        />
                      </span>
                    )}
                    <span className="min-w-0 flex-1">
                      <Link
                        to={`/bots/${ownerId}/threads/${thread.id}`}
                        aria-current={selected ? 'page' : undefined}
                        draggable={false}
                        className={`${rowStyle} ${owner ? 'h-5 pl-0' : 'h-8'} after:absolute after:inset-0 after:rounded-lg`}
                      >
                        <span
                          className={`min-w-0 truncate ${thread.state === 'new' ? 'font-semibold' : ''}`}
                        >
                          {thread.title || 'Empty thread'}
                        </span>
                      </Link>
                      {owner && (
                        <span className="block truncate text-xs text-muted">
                          {owner.bot.name} · {owner.note}
                        </span>
                      )}
                    </span>
                    <span
                      className={`pointer-events-none flex flex-none items-center gap-2 pr-2 ${thread.helper ? '' : 'group-focus-within:hidden group-hover:hidden'}`}
                    >
                      <ThreadMarker thread={thread} />
                    </span>
                    {!thread.helper && onPinThread && onCloseThread && (
                      <span className="relative z-10 mr-2 hidden flex-none items-center gap-1 group-focus-within:flex group-hover:flex">
                        <button
                          type="button"
                          onClick={() => onPinThread(thread)}
                          className="flex h-5 items-center rounded-md bg-tile px-1.5 text-xs text-ink"
                        >
                          {thread.pinned ? 'Unpin' : 'Pin'}
                        </button>
                        <button
                          type="button"
                          aria-label="Close thread"
                          title="Close thread"
                          onClick={() => onCloseThread(thread.id)}
                          className="flex size-5 items-center justify-center rounded-md bg-tile text-ink"
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
                  </>
                )}
              </div>
              {workers.get(thread.id) && (
                <HelpersLine
                  botId={ownerId}
                  threadId={thread.id}
                  workers={workers.get(thread.id)!}
                />
              )}
            </Fragment>
          );
        })}
      </div>
      {menu && (
        <Menu
          at={menu}
          onClose={() => setMenu(null)}
          items={[
            { label: 'Rename', onSelect: () => setRenaming(menu.thread.id) },
            ...(menu.thread.helper || !onCloseThread
              ? []
              : [{ label: 'Close', onSelect: () => onCloseThread(menu.thread.id) }]),
          ]}
        />
      )}
    </>
  );
}
