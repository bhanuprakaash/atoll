import { useEffect, useRef, useState, useSyncExternalStore } from 'react';
import {
  NavLink,
  Outlet,
  useLocation,
  useMatch,
  useNavigate,
  useNavigationType,
} from 'react-router';
import { BotRail } from '@/modules/bots/components/bot-rail';
import { Menu } from '@/components/menu';
import { Switcher } from '@/app/switcher';
import { ThreadPane, usePaneWidth } from '@/app/thread-pane';
import { useSettings } from '@/modules/settings/api/settings';
import { useNotifications } from '@/modules/threads/hooks/use-notifications';
import { useTypeToCompose } from '@/modules/threads/hooks/use-type-to-compose';
import { inDesktopApp, useAppUpdate } from '@/lib/app-update';

const NARROW = '(max-width: 899.98px)';
const isMac = /Mac/.test(navigator.platform);

// Below 900 px the thread pane floats over the thread column, closed whenever the window turns
// narrow and after any navigation except a face's own (it opens the pane, then redirects).
function useFloatingPane() {
  const narrow = useSyncExternalStore(
    (notify) => {
      const query = matchMedia(NARROW);
      query.addEventListener('change', notify);
      return () => query.removeEventListener('change', notify);
    },
    () => matchMedia(NARROW).matches,
  );
  const [open, setOpen] = useState(false);
  const { key, pathname } = useLocation();
  const navigationType = useNavigationType();
  useEffect(() => setOpen(false), [narrow]);
  useEffect(() => {
    if (navigationType !== 'REPLACE' && !/^\/bots\/[^/]+$/.test(pathname)) setOpen(false);
  }, [key, pathname, navigationType]);
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
  const [narrow, paneOpen, setPaneOpen] = useFloatingPane();
  const opener = useRef<HTMLElement | null>(null);
  const openPane = (from: Element | null) => {
    opener.current = from as HTMLElement | null;
    setPaneOpen(true);
  };
  const closePane = () => {
    setPaneOpen(false);
    opener.current?.focus();
  };
  const notifications = useNotifications();
  const [newMenu, setNewMenu] = useState<{ x: number; y: number } | null>(null);
  const [creatingGroup, setCreatingGroup] = useState<{ botId?: string } | null>(null);
  const botMatch = useMatch('/bots/:botId/*')?.params.botId;
  const botId = botMatch === 'new' ? undefined : botMatch;
  const threadId = useMatch('/bots/:botId/threads/:threadId')?.params.threadId;
  const inSettings = useMatch('/settings/*') !== null;
  const [settingsHidesPane, setSettingsHidesPane] = useState(inSettings);
  useEffect(() => setSettingsHidesPane(inSettings), [inSettings]);
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
        if (!narrow) setPaneHidden((hidden) => !hidden);
        else if (paneOpen) closePane();
        else openPane(document.activeElement);
      } else if (event.key === 'Escape' && paneOpen && !event.defaultPrevented) {
        closePane();
      }
    };
    window.addEventListener('keydown', onKeyDown);
    return () => window.removeEventListener('keydown', onKeyDown);
  });
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
      {paneOpen && (
        <div
          aria-hidden="true"
          onClick={closePane}
          className="fixed inset-y-0 right-0 left-16 z-30 bg-scrim"
        />
      )}
      <div className="flex flex-none">
        <nav
          aria-label="Bots"
          className="flex w-16 flex-none flex-col border-r border-sidebar-line bg-card pt-4 pb-3"
        >
          {inDesktopApp && <div data-tauri-drag-region="deep" className="h-8" />}
          <BotRail
            view={view}
            openBotId={botId}
            onView={(next, from) => {
              setView(next);
              setSettingsHidesPane(false);
              openPane(from);
            }}
            creatingGroup={creatingGroup}
            onCreatingGroup={setCreatingGroup}
          />
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
        {view && !settingsHidesPane && (narrow ? paneOpen : !paneHidden) && (
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
      <main className="flex min-w-0 flex-1 flex-col">
        <Outlet context={notifications} />
      </main>
    </div>
  );
}
