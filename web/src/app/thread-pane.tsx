import { useEffect, useRef, useState } from 'react';
import { useNavigate } from 'react-router';
import { ClosedThreads } from '@/app/closed-threads';
import { ReplacePin } from '@/app/replace-pin';
import { Creature } from '@/components/creature';
import { ErrorNotice } from '@/components/error-notice';
import { TextField } from '@/components/text-field';
import { ApiError } from '@/lib/api-client';
import { useBots, type Bot, type SidebarThread } from '@/modules/bots/api/bots';
import { useGroups } from '@/modules/bots/api/groups';
import { BotMenu } from '@/modules/bots/components/bot-menu';
import { ThreadSection } from '@/modules/bots/components/thread-tree';
import {
  nowSections,
  threadNote,
  threadSections,
  workersByBoss,
} from '@/modules/bots/utils/thread-tree';
import {
  useCloseThread,
  usePinThread,
  useRenameThread,
  useThreads,
} from '@/modules/threads/api/threads';
import { mrRef } from '@/modules/threads/utils/mr-ref';
import { relativeTime } from '@/utils/time';

const WIDTH_DEFAULT = 284;
const WIDTH_MIN = 240;
const WIDTH_MAX = 420;
const WIDTH_STEP = 16;
const WIDTH_KEY = 'atoll.sidebarWidth';
const RAIL_WIDTH = 76;
const DRAWER_GUTTER = 48;

const isPinLimit = (error: Error) => error instanceof ApiError && error.status === 409;

function clampWidth(width: number) {
  return Math.min(WIDTH_MAX, Math.max(WIDTH_MIN, Math.round(width)));
}

export function usePaneWidth() {
  const [width, setWidth] = useState(() => {
    try {
      const stored = Number(localStorage.getItem(WIDTH_KEY));
      return stored ? clampWidth(stored) : WIDTH_DEFAULT;
    } catch {
      return WIDTH_DEFAULT;
    }
  });
  useEffect(() => {
    try {
      localStorage.setItem(WIDTH_KEY, String(width));
    } catch {
      // storage unavailable: the width lasts for this session only
    }
  }, [width]);
  return [width, (next: number) => setWidth(clampWidth(next))] as const;
}

function matchesQuery(t: SidebarThread, words: string[]) {
  const text = [t.title, t.branch, ...t.prs.map((p) => mrRef(p.url))].join(' ').toLowerCase();
  return words.every((w) => text.includes(w));
}

function ThreadSearch({ query, onQuery }: { query: string; onQuery: (query: string) => void }) {
  return (
    <div className="px-3 pb-1">
      <TextField
        size="bare"
        aria-label="Search threads"
        placeholder="Search threads"
        value={query}
        onChange={(event) => onQuery(event.target.value)}
        onKeyDown={(event) => {
          if (event.key === 'Escape') onQuery('');
        }}
        className="h-8.5 rounded-[10px] bg-field px-3"
      />
    </div>
  );
}

const scroller =
  'min-h-0 flex-1 overflow-y-auto px-2 pb-3 [scrollbar-gutter:stable] [&::-webkit-scrollbar]:w-1.5 [&::-webkit-scrollbar-thumb]:rounded-full [&::-webkit-scrollbar-thumb]:bg-segment';

function NowPane({ bots, openThreadId }: { bots: Bot[]; openThreadId?: string }) {
  const [query, setQuery] = useState('');
  const words = query.toLowerCase().split(/\s+/).filter(Boolean);
  const owners = new Map<string, { bot: Bot; note: string }>();
  const workers = workersByBoss(bots);
  const threads = bots.flatMap((bot) =>
    (bot.threads ?? []).filter((t) => matchesQuery(t, words)).map((t) => ({ bot, t })),
  );
  for (const { bot, t } of threads)
    owners.set(t.id, {
      bot,
      note: threadNote(t, workers.get(t.id)) || relativeTime(t.last_activity),
    });
  const sections = nowSections(
    threads.map(({ t }) => t),
    openThreadId,
  );
  return (
    <>
      <span className="px-3.5 pt-4 pb-3 text-[15px]">Now</span>
      <ThreadSearch query={query} onQuery={setQuery} />
      <div className={scroller}>
        {sections.map(({ label, threads }) => (
          <ThreadSection
            key={label}
            label={label}
            threads={threads}
            openThreadId={openThreadId}
            owners={owners}
          />
        ))}
        {sections.length === 0 && (
          <p className="px-2 py-3 text-sm text-muted">
            {words.length > 0 ? 'No threads match' : 'Nothing needs you'}
          </p>
        )}
      </div>
    </>
  );
}

function BotPane({
  bot,
  bots,
  openThreadId,
  onNewGroup,
}: {
  bot: Bot;
  bots: Bot[];
  openThreadId?: string;
  onNewGroup: (botId: string) => void;
}) {
  const navigate = useNavigate();
  const { data: groups = [] } = useGroups();
  const close = useCloseThread();
  const rename = useRenameThread();
  const pin = usePinThread();
  const { data: details = [] } = useThreads(bot.id);
  const [replacing, setReplacing] = useState<SidebarThread | null>(null);
  const [query, setQuery] = useState('');
  const [menu, setMenu] = useState<{ x: number; y: number } | null>(null);
  const workers = workersByBoss(bots);
  const words = query.toLowerCase().split(/\s+/).filter(Boolean);
  const matching = (bot.threads ?? []).filter((t) => matchesQuery(t, words));
  const sections = threadSections(matching, openThreadId);
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
  const created = new Map(details.map((t) => [t.id, t.created_at]));
  const newThread = () => navigate(`/bots/${bot.id}/new`);
  const group =
    bot.id === 'chief' ? 'Sets up your Bots' : groups.find((g) => g.id === bot.group_id)?.name;
  return (
    <>
      <div className="flex items-center gap-2.5 px-3.5 pt-4 pb-3">
        <Creature look={bot.look} colour={bot.colour} state={bot.state} size={28} />
        <span className="min-w-0 flex-1">
          <span className="block truncate text-[15px]">{bot.name}</span>
          {group && <span className="block truncate text-xs text-muted">{group}</span>}
        </span>
        <button
          type="button"
          aria-label="New thread"
          title="New thread (⌘N)"
          onClick={newThread}
          className="flex size-7 flex-none items-center justify-center rounded-control bg-tile text-ink"
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
        <button
          type="button"
          aria-label="Bot actions"
          aria-haspopup="menu"
          onClick={(event) => {
            const box = event.currentTarget.getBoundingClientRect();
            setMenu({ x: box.left, y: box.bottom + 4 });
          }}
          className="flex size-7 flex-none items-center justify-center rounded-control text-muted hover:text-ink"
        >
          <svg aria-hidden="true" width="14" height="14" viewBox="0 0 24 24" fill="currentColor">
            <circle cx="5" cy="12" r="1.7" />
            <circle cx="12" cy="12" r="1.7" />
            <circle cx="19" cy="12" r="1.7" />
          </svg>
        </button>
      </div>
      <ThreadSearch query={query} onQuery={setQuery} />
      {pin.isError && !isPinLimit(pin.error) && (
        <ErrorNotice error={pin.error} className="px-3 pb-2" />
      )}
      <div className={scroller}>
        {sections.map(({ label, threads }) => (
          <ThreadSection
            key={label}
            label={label}
            threads={threads}
            botId={bot.id}
            openThreadId={openThreadId}
            workers={workers}
            onPinThread={(thread) => setPinned(thread, !thread.pinned)}
            onCloseThread={(id) => close.mutate(id)}
            onRenameThread={(id, title) => rename.mutate({ id, title })}
          />
        ))}
        {words.length > 0 && sections.length === 0 && (
          <p className="px-2 py-3 text-sm text-muted">No threads match</p>
        )}
        <ClosedThreads
          botId={bot.id}
          threads={details.filter((t) => t.closed)}
          openThreadId={openThreadId}
        />
      </div>
      {replacing && (
        <ReplacePin
          pins={(bot.threads ?? [])
            .filter((t) => t.pinned)
            .sort((a, b) => (created.get(a.id) ?? '').localeCompare(created.get(b.id) ?? ''))}
          onPick={(id) => setPinned(replacing, true, id)}
          onClose={() => setReplacing(null)}
        />
      )}
      {menu && (
        <BotMenu
          bot={bot}
          at={menu}
          onClose={() => setMenu(null)}
          onNewGroup={() => onNewGroup(bot.id)}
        />
      )}
    </>
  );
}

export function ThreadPane({
  view,
  openThreadId,
  width,
  onWidth,
  onNewGroup,
}: {
  view: string;
  openThreadId?: string;
  width: number;
  onWidth?: (width: number) => void;
  onNewGroup: (botId: string) => void;
}) {
  const { data: bots = [] } = useBots();
  const resizeStart = useRef({ x: 0, width: 0 });
  const bot = bots.find((b) => b.id === view);
  const now = view === 'now';
  if (!now && !bot) return null;
  return (
    <section
      aria-label={now ? 'Now' : bot!.name}
      style={{
        width: onWidth ? width : `min(${width}px, calc(100vw - ${RAIL_WIDTH + DRAWER_GUTTER}px))`,
      }}
      className={`flex flex-none flex-col border-r border-sidebar-line bg-card ${onWidth ? 'relative' : 'fixed inset-y-0 left-19 z-40'}`}
    >
      {now ? (
        <NowPane bots={bots} openThreadId={openThreadId} />
      ) : (
        <BotPane
          key={bot!.id}
          bot={bot!}
          bots={bots}
          openThreadId={openThreadId}
          onNewGroup={onNewGroup}
        />
      )}
      {onWidth && (
        <div
          role="separator"
          aria-orientation="vertical"
          aria-label="Resize thread pane"
          aria-valuenow={width}
          aria-valuemin={WIDTH_MIN}
          aria-valuemax={WIDTH_MAX}
          tabIndex={0}
          onPointerDown={(event) => {
            event.currentTarget.setPointerCapture(event.pointerId);
            resizeStart.current = { x: event.clientX, width };
          }}
          onPointerMove={(event) => {
            if (event.currentTarget.hasPointerCapture(event.pointerId))
              onWidth(resizeStart.current.width + event.clientX - resizeStart.current.x);
          }}
          onDoubleClick={() => onWidth(WIDTH_DEFAULT)}
          onKeyDown={(event) => {
            const step = { ArrowRight: WIDTH_STEP, ArrowLeft: -WIDTH_STEP }[event.key];
            if (!step) return;
            event.preventDefault();
            onWidth(width + step);
          }}
          className="group absolute top-3 -right-1 bottom-0 z-20 w-1.5 cursor-col-resize outline-none"
        >
          <span className="absolute inset-y-0 right-[3px] w-px group-hover:bg-muted group-focus-visible:bg-muted group-active:bg-muted" />
        </div>
      )}
    </section>
  );
}
