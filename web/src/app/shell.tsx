import { useEffect, useRef, useState } from 'react';
import { NavLink, Outlet, useMatch, useNavigate, useSearchParams } from 'react-router';
import { BotList } from '@/modules/bots/components/bot-list';
import { Menu } from '@/components/menu';
import { ThreadsPopup } from '@/app/threads-popup';
import { useSettings } from '@/modules/settings/api/settings';
import { useNotifications } from '@/modules/threads/hooks/use-notifications';
import { TextField } from '@/components/text-field';
import { useCloseThread, useRenameThread } from '@/modules/threads/api/threads';

const inDesktopApp = '__TAURI_INTERNALS__' in window;

const SIDEBAR_DEFAULT = 340;
const SIDEBAR_MIN = 260;
const SIDEBAR_MAX = 480;
const SIDEBAR_STEP = 16;
const SIDEBAR_KEY = 'atoll.sidebarWidth';

function clampSidebar(width: number) {
  return Math.min(SIDEBAR_MAX, Math.max(SIDEBAR_MIN, Math.round(width)));
}

function useSidebarWidth() {
  const [width, setWidth] = useState(() => {
    try {
      const stored = Number(localStorage.getItem(SIDEBAR_KEY));
      return stored ? clampSidebar(stored) : SIDEBAR_DEFAULT;
    } catch {
      return SIDEBAR_DEFAULT;
    }
  });
  useEffect(() => {
    try {
      localStorage.setItem(SIDEBAR_KEY, String(width));
    } catch {
      // storage unavailable: the width lasts for this session only
    }
  }, [width]);
  return [width, (next: number) => setWidth(clampSidebar(next))] as const;
}

function initials(name: string) {
  const words = name.split(/\s+/).filter(Boolean);
  return words
    .slice(0, 2)
    .map((word) => word[0].toUpperCase())
    .join('');
}

export function Shell() {
  const navigate = useNavigate();
  const [search, setSearch] = useState('');
  const [sidebarWidth, setSidebarWidth] = useSidebarWidth();
  const resizeStart = useRef({ x: 0, width: 0 });
  const notifications = useNotifications();
  const [newMenu, setNewMenu] = useState<{ x: number; y: number } | null>(null);
  const [creatingGroup, setCreatingGroup] = useState<{ botId?: string } | null>(null);
  const close = useCloseThread();
  const rename = useRenameThread();
  const [params, setParams] = useSearchParams();
  const threadsBot = params.get('threads');
  const setThreadsBot = (id: string | null) =>
    setParams((prev) => {
      const next = new URLSearchParams(prev);
      if (id) next.set('threads', id);
      else next.delete('threads');
      return next;
    });
  const botMatch = useMatch('/bots/:botId/*')?.params.botId;
  const botId = botMatch === 'new' ? undefined : botMatch;
  const threadId = useMatch('/bots/:botId/threads/:threadId')?.params.threadId;
  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      if (!botId || !event.metaKey || event.key !== 'n') return;
      event.preventDefault();
      navigate(`/bots/${botId}/new`);
    };
    window.addEventListener('keydown', onKeyDown);
    return () => window.removeEventListener('keydown', onKeyDown);
  }, [botId, navigate]);
  const { data: settings } = useSettings();
  const userName = settings?.user_name || 'User';
  return (
    <div className="flex h-screen">
      {inDesktopApp && <div data-tauri-drag-region className="fixed inset-x-0 top-0 z-10 h-3" />}
      <nav
        aria-label="Bots"
        style={{ width: sidebarWidth }}
        className="relative flex flex-none flex-col border-r border-sidebar-line bg-card px-2.5 pt-4 pb-3"
      >
        {inDesktopApp && <div data-tauri-drag-region="deep" className="h-8" />}
        <div className={`flex items-center gap-2 ${inDesktopApp ? 'mt-3.5' : ''}`}>
          <div className="relative min-w-0 flex-1">
            <svg
              aria-hidden="true"
              width="16"
              height="16"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              strokeWidth="1.7"
              strokeLinecap="round"
              className="pointer-events-none absolute top-1/2 left-3 -translate-y-1/2 text-muted"
            >
              <circle cx="11" cy="11" r="7" />
              <path d="m20 20-4-4" />
            </svg>
            <TextField
              size="bare"
              aria-label="Search Bots"
              placeholder="Search"
              value={search}
              onChange={(event) => setSearch(event.target.value)}
              onKeyDown={(event) => {
                if (event.key === 'Escape') setSearch('');
              }}
              className="h-[34px] rounded-[10px] bg-field pr-3 pl-9"
            />
          </div>
          <button
            type="button"
            aria-label="New"
            aria-haspopup="menu"
            aria-expanded={newMenu !== null}
            onClick={(event) => {
              const box = event.currentTarget.getBoundingClientRect();
              setNewMenu({ x: box.left, y: box.bottom + 4 });
            }}
            className="flex size-8 flex-none items-center justify-center rounded-control text-muted"
          >
            <svg
              aria-hidden="true"
              width="18"
              height="18"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              strokeWidth="1.6"
              strokeLinecap="round"
            >
              <path d="M12 4v16M4 12h16" />
            </svg>
          </button>
        </div>
        <div className="-mr-2.5 mt-3.5 mb-2 min-h-0 overflow-y-auto pr-1 [scrollbar-gutter:stable] [&::-webkit-scrollbar]:w-1.5 [&::-webkit-scrollbar-thumb]:rounded-full [&::-webkit-scrollbar-thumb]:bg-segment">
          <BotList
            search={search}
            open={{ botId, threadId }}
            creatingGroup={creatingGroup}
            onCreatingGroup={setCreatingGroup}
            onMore={(id) => setThreadsBot(id)}
            onCloseThread={(id) => close.mutate(id)}
            onRenameThread={(id, title) => rename.mutate({ id, title })}
          />
        </div>
        <NavLink
          to="/settings"
          className={({ isActive }) =>
            `mt-auto flex items-center gap-2.5 rounded-card p-2.5 text-sm ${isActive ? 'bg-selected' : ''}`
          }
        >
          <span
            aria-hidden="true"
            className="flex size-8 flex-none items-center justify-center rounded-full bg-composer text-xs text-muted"
          >
            {initials(userName)}
          </span>
          <span className="truncate">{userName}</span>
          <span className="sr-only">Settings</span>
          <svg
            aria-hidden="true"
            width="17"
            height="17"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            strokeWidth="1.7"
            strokeLinecap="round"
            className="ml-auto flex-none text-muted"
          >
            <circle cx="12" cy="12" r="3" />
            <path d="M12 2v3M12 19v3M2 12h3M19 12h3M4.9 4.9l2.1 2.1M17 17l2.1 2.1M4.9 19.1L7 17M17 7l2.1-2.1" />
          </svg>
        </NavLink>
        <div
          role="separator"
          aria-orientation="vertical"
          aria-label="Resize sidebar"
          aria-valuenow={sidebarWidth}
          aria-valuemin={SIDEBAR_MIN}
          aria-valuemax={SIDEBAR_MAX}
          tabIndex={0}
          onPointerDown={(event) => {
            event.currentTarget.setPointerCapture(event.pointerId);
            resizeStart.current = { x: event.clientX, width: sidebarWidth };
          }}
          onPointerMove={(event) => {
            if (event.currentTarget.hasPointerCapture(event.pointerId))
              setSidebarWidth(resizeStart.current.width + event.clientX - resizeStart.current.x);
          }}
          onDoubleClick={() => setSidebarWidth(SIDEBAR_DEFAULT)}
          onKeyDown={(event) => {
            const step = { ArrowRight: SIDEBAR_STEP, ArrowLeft: -SIDEBAR_STEP }[event.key];
            if (!step) return;
            event.preventDefault();
            setSidebarWidth(sidebarWidth + step);
          }}
          className="group absolute top-3 -right-1 bottom-0 z-20 w-1.5 cursor-col-resize outline-none"
        >
          <span className="absolute inset-y-0 right-[3px] w-px group-hover:bg-muted group-focus-visible:bg-muted group-active:bg-muted" />
        </div>
      </nav>
      {newMenu && (
        <Menu
          at={newMenu}
          onClose={() => setNewMenu(null)}
          items={[
            { label: 'New Bot', onSelect: () => navigate('/bots/new') },
            { label: 'New group', onSelect: () => setCreatingGroup({}) },
          ]}
        />
      )}
      {threadsBot && (
        <ThreadsPopup
          botId={threadsBot}
          openThreadId={botId === threadsBot ? threadId : undefined}
          onClose={() => setThreadsBot(null)}
        />
      )}
      <main className="flex min-w-0 flex-1 flex-col">
        <Outlet context={notifications} />
      </main>
    </div>
  );
}
