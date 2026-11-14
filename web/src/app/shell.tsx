import { useEffect, useRef, useState, useSyncExternalStore } from 'react';
import { NavLink, Outlet, useLocation, useMatch, useNavigate, useSearchParams } from 'react-router';
import { BotRail } from '@/modules/bots/components/bot-rail';
import { Menu } from '@/components/menu';
import { Switcher } from '@/app/switcher';
import { ThreadPane, usePaneWidth } from '@/app/thread-pane';
import { ThreadsPopup } from '@/app/threads-popup';
import { useSettings } from '@/modules/settings/api/settings';
import { useNotifications } from '@/modules/threads/hooks/use-notifications';
import { useTypeToCompose } from '@/modules/threads/hooks/use-type-to-compose';
import { inDesktopApp, useAppUpdate } from '@/lib/app-update';

const NARROW = '(max-width: 899.98px)';
const isMac = /Mac/.test(navigator.platform);

// Below 900 px the sidebar is a drawer, closed whenever the window turns narrow and after any
// navigation.
function useDrawer() {
  const narrow = useSyncExternalStore(
    (notify) => {
      const query = matchMedia(NARROW);
      query.addEventListener('change', notify);
      return () => query.removeEventListener('change', notify);
    },
    () => matchMedia(NARROW).matches,
  );
  const [open, setOpen] = useState(false);
  const { key } = useLocation();
  useEffect(() => setOpen(false), [narrow, key]);
  return [narrow, narrow && open, setOpen] as const;
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
  const [paneWidth, setPaneWidth] = usePaneWidth();
  const [paneHidden, setPaneHidden] = useState(false);
  const [narrow, drawerOpen, setDrawerOpen] = useDrawer();
  const toggleButton = useRef<HTMLButtonElement>(null);
  const notifications = useNotifications();
  const [newMenu, setNewMenu] = useState<{ x: number; y: number } | null>(null);
  const [creatingGroup, setCreatingGroup] = useState<{ botId?: string } | null>(null);
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
  const [view, setView] = useState(botId);
  const [seenBotId, setSeenBotId] = useState(botId);
  if (seenBotId !== botId) {
    setSeenBotId(botId);
    if (botId && view !== 'now') setView(botId);
  }
  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      if ((isMac ? event.metaKey : event.ctrlKey) && event.key === 'b') {
        event.preventDefault();
        if (narrow) setDrawerOpen((open) => !open);
        else setPaneHidden((hidden) => !hidden);
      } else if (event.key === 'Escape' && drawerOpen && !event.defaultPrevented) {
        setDrawerOpen(false);
        toggleButton.current?.focus();
      }
    };
    window.addEventListener('keydown', onKeyDown);
    return () => window.removeEventListener('keydown', onKeyDown);
  }, [narrow, drawerOpen, setDrawerOpen]);
  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      if (!botId || !event.metaKey || event.key !== 'n') return;
      event.preventDefault();
      navigate(`/bots/${botId}/new`);
    };
    window.addEventListener('keydown', onKeyDown);
    return () => window.removeEventListener('keydown', onKeyDown);
  }, [botId, navigate]);
  const [switching, setSwitching] = useState(false);
  useTypeToCompose();
  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key.toLowerCase() !== 'k' || event.altKey || event.shiftKey) return;
      if (!(isMac ? event.metaKey && !event.ctrlKey : event.ctrlKey && !event.metaKey)) return;
      event.preventDefault();
      setSwitching(true);
    };
    window.addEventListener('keydown', onKeyDown);
    return () => window.removeEventListener('keydown', onKeyDown);
  }, []);
  const { data: settings } = useSettings();
  const userName = settings?.user_name || 'User';
  const updateAvailable = Boolean(useAppUpdate().data?.available);
  return (
    <div className="flex h-screen">
      {inDesktopApp && <div data-tauri-drag-region className="fixed inset-x-0 top-0 z-10 h-3" />}
      {narrow && (
        <button
          ref={toggleButton}
          type="button"
          aria-label="Sidebar"
          aria-expanded={drawerOpen}
          onClick={() => {
            setDrawerOpen(!drawerOpen);
            if (drawerOpen) toggleButton.current?.focus();
          }}
          className={`fixed top-3.5 z-50 flex size-10 items-center justify-center rounded-full text-muted ${inDesktopApp ? 'left-20' : 'left-3'}`}
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
            <rect x="3.5" y="4.5" width="17" height="15" rx="3" />
            <path d="M9.5 4.5v15" />
          </svg>
        </button>
      )}
      {drawerOpen && (
        <div
          aria-hidden="true"
          onClick={() => setDrawerOpen(false)}
          className="fixed inset-0 z-30 bg-scrim"
        />
      )}
      <div
        className={`flex-none ${narrow ? `fixed inset-y-0 left-0 z-40 ${drawerOpen ? 'flex' : 'hidden'}` : 'flex'}`}
      >
        <nav
          aria-label="Bots"
          className="flex w-19 flex-none flex-col border-r border-sidebar-line bg-card pt-4 pb-3"
        >
          {inDesktopApp && <div data-tauri-drag-region="deep" className="h-8" />}
          <div className="mt-2 min-h-0 flex-1 overflow-y-auto [scrollbar-width:none]">
            <BotRail
              view={view}
              onView={setView}
              creatingGroup={creatingGroup}
              onCreatingGroup={setCreatingGroup}
            />
          </div>
          <div className="mt-2 flex flex-none flex-col items-center gap-2">
            <button
              type="button"
              aria-label="New"
              aria-haspopup="menu"
              aria-expanded={newMenu !== null}
              onClick={(event) => {
                const box = event.currentTarget.getBoundingClientRect();
                setNewMenu({ x: box.right + 4, y: box.top });
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
            <NavLink
              to="/settings"
              aria-label={updateAvailable ? 'Settings, Update available' : 'Settings'}
              className={({ isActive }) =>
                `relative flex size-10 items-center justify-center rounded-full ${isActive ? 'bg-selected' : ''}`
              }
            >
              <span
                aria-hidden="true"
                className="flex size-8 items-center justify-center rounded-full bg-composer text-xs text-muted"
              >
                {initials(userName)}
              </span>
              {updateAvailable && (
                <span
                  aria-hidden="true"
                  className="absolute top-0.5 right-0.5 size-2 rounded-full bg-ok"
                />
              )}
            </NavLink>
          </div>
        </nav>
        {view && !(paneHidden && !narrow) && (
          <ThreadPane
            view={view}
            openThreadId={threadId}
            width={paneWidth}
            onWidth={narrow ? undefined : setPaneWidth}
            onNewGroup={(id) => setCreatingGroup({ botId: id })}
          />
        )}
      </div>
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
      {switching && <Switcher onClose={() => setSwitching(false)} />}
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
