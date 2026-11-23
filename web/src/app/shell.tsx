import { useEffect, useRef, useState, useSyncExternalStore } from 'react';
import { Outlet, useLocation, useMatch, useNavigate } from 'react-router';
import { useBots } from '@/modules/bots/api/bots';
import { helperBoss } from '@/modules/bots/utils/thread-tree';
import { Sidebar, useSidebarWidth } from '@/app/sidebar';
import { Switcher } from '@/app/switcher';
import { useTypeToCompose } from '@/modules/threads/hooks/use-type-to-compose';
import { inDesktopApp } from '@/lib/app-update';

const NARROW = '(max-width: 899.98px)';
const isMac = /Mac/.test(navigator.platform);

// Below 900 px the sidebar floats over the chat, closed whenever the window turns narrow and after
// any navigation.
function useFloatingSidebar() {
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

export function Shell() {
  const navigate = useNavigate();
  const [width, setWidth] = useSidebarWidth();
  const [hidden, setHidden] = useState(false);
  const [narrow, floating, setFloating] = useFloatingSidebar();
  const opener = useRef<HTMLElement | null>(null);
  const openSidebar = (from: Element | null) => {
    opener.current = from as HTMLElement | null;
    setFloating(true);
  };
  const closeSidebar = () => {
    setFloating(false);
    opener.current?.focus();
  };
  const botMatch = useMatch('/bots/:botId/*')?.params.botId;
  const urlBotId = botMatch === 'new' ? undefined : botMatch;
  const threadId = useMatch('/bots/:botId/threads/:threadId')?.params.threadId;
  const { data: bots = [] } = useBots();
  const botId = urlBotId && (helperBoss(bots, threadId)?.botId ?? urlBotId);
  const now = useMatch('/now') !== null;
  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      if ((isMac ? event.metaKey : event.ctrlKey) && event.key === 'b') {
        event.preventDefault();
        if (!narrow) setHidden((was) => !was);
        else if (floating) closeSidebar();
        else openSidebar(document.activeElement);
      } else if (event.key === 'Escape' && floating && !event.defaultPrevented) {
        closeSidebar();
      }
    };
    window.addEventListener('keydown', onKeyDown);
    return () => window.removeEventListener('keydown', onKeyDown);
  });
  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      if (!urlBotId || !event.metaKey || event.key !== 'n') return;
      event.preventDefault();
      navigate(`/bots/${urlBotId}/new`);
    };
    window.addEventListener('keydown', onKeyDown);
    return () => window.removeEventListener('keydown', onKeyDown);
  }, [urlBotId, navigate]);
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
  return (
    <div className="flex h-screen">
      {inDesktopApp && <div data-tauri-drag-region className="fixed inset-x-0 top-0 z-10 h-3" />}
      {floating && (
        <div aria-hidden="true" onClick={closeSidebar} className="fixed inset-0 z-30 bg-scrim" />
      )}
      {(narrow ? floating : !hidden) && (
        <Sidebar
          now={now}
          openBotId={botId}
          width={width}
          onWidth={narrow ? undefined : setWidth}
        />
      )}
      {narrow && (
        <button
          type="button"
          aria-label="Sidebar"
          aria-expanded={floating}
          onClick={(event) => (floating ? closeSidebar() : openSidebar(event.currentTarget))}
          className="fixed top-3.5 left-3 z-20 flex size-8 items-center justify-center rounded-lg text-muted"
        >
          <svg
            aria-hidden="true"
            width="18"
            height="18"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            strokeWidth="1.8"
            strokeLinecap="round"
          >
            <rect x="4" y="5" width="16" height="14" rx="3" />
            <path d="M10 5v14" />
          </svg>
        </button>
      )}
      {switching && <Switcher onClose={() => setSwitching(false)} />}
      <main className="flex min-w-0 flex-1 flex-col">
        <Outlet />
      </main>
    </div>
  );
}
