import { Link } from 'react-router';
import { Creature } from '@/components/creature';
import { TextField } from '@/components/text-field';
import type { Bot, SidebarThread } from '@/modules/bots/api/bots';
import { clockTime } from '@/utils/time';

const marker = (t: SidebarThread) =>
  t.state ? t.state.replace('_', '-') : t.pinned ? 'pinned' : undefined;

const rowStyle = 'flex items-center gap-2.5 rounded-lg px-2 text-sm';

export function Dot({ className = '' }: { className?: string }) {
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

export function ThreadMarker({ thread, showPin }: { thread: SidebarThread; showPin: boolean }) {
  const pinned = thread.pinned && showPin;
  if (thread.state === 'needs_you') return <Dot className="bg-accent" />;
  if (thread.state === 'failed') return <Dot className="bg-danger" />;
  if (thread.state === 'limited')
    return (
      <>
        {pinned && <Pin />}
        <span className="flex-none text-xs text-muted">{outUntil(thread)}</span>
      </>
    );
  if (thread.state === 'new')
    return (
      <>
        {pinned && <Pin />}
        <Dot className="bg-kind" />
        <span className="sr-only">Replied</span>
      </>
    );
  if (thread.state === 'running') return <Wave label="Working" />;
  if (pinned && thread.state === '') return <Pin />;
  return null;
}

export function RenameField({
  title,
  onDone,
}: {
  title: string;
  onDone: (title?: string) => void;
}) {
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

export function ThreadSection({
  label,
  threads,
  owners,
}: {
  label: string;
  threads: SidebarThread[];
  owners: Map<string, { bot: Bot; note: string }>;
}) {
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
          const owner = owners.get(thread.id)!;
          return (
            <div
              key={thread.id}
              role="listitem"
              data-marker={marker(thread)}
              className="relative flex items-center gap-2.5 rounded-lg py-2 hover:bg-field"
            >
              <span className="flex-none pl-2.5">
                <Creature
                  look={owner.bot.look}
                  colour={owner.bot.colour}
                  state={owner.bot.state}
                  size={22}
                />
              </span>
              <span className="min-w-0 flex-1">
                <Link
                  to={`/bots/${owner.bot.id}/threads/${thread.id}`}
                  draggable={false}
                  className={`${rowStyle} h-5 pl-0 after:absolute after:inset-0 after:rounded-lg`}
                >
                  <span
                    className={`min-w-0 truncate ${thread.state === 'new' ? 'font-semibold' : ''}`}
                  >
                    {thread.title || 'Empty thread'}
                  </span>
                </Link>
                <span className="block truncate text-xs text-muted">
                  {owner.bot.name} · {owner.note}
                </span>
              </span>
              <span className="pointer-events-none flex flex-none items-center gap-2 pr-2">
                <ThreadMarker thread={thread} showPin={false} />
              </span>
            </div>
          );
        })}
      </div>
    </>
  );
}
