import { useQuery } from '@tanstack/react-query';
import { useEffect, useState } from 'react';
import { NavLink, Outlet, useMatch, useNavigate } from 'react-router';
import { api } from '@/lib/api-client';
import { BotList } from '@/modules/bots/components/bot-list';
import { Menu } from '@/components/menu';
import { useNotifications } from '@/modules/threads/hooks/use-notifications';
import { TextField } from '@/components/text-field';
import { useCloseThread, useRenameThread } from '@/modules/threads/api/threads';

const inDesktopApp = '__TAURI_INTERNALS__' in window;

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
  const notifications = useNotifications();
  const [newMenu, setNewMenu] = useState<{ x: number; y: number } | null>(null);
  const close = useCloseThread();
  const rename = useRenameThread();
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
  const { data: settings } = useQuery({
    queryKey: ['settings'],
    queryFn: () => api<{ user_name: string }>('/settings'),
  });
  const userName = settings?.user_name || 'User';
  return (
    <div className="flex h-screen">
      {inDesktopApp && <div data-tauri-drag-region className="fixed inset-x-0 top-0 z-10 h-3" />}
      <nav
        aria-label="Bots"
        className="flex w-[300px] flex-none flex-col border-r border-sidebar-line bg-card px-2.5 pt-4 pb-3"
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
        <div className="mt-3.5 min-h-0 overflow-y-auto">
          <BotList
            search={search}
            open={{ botId, threadId }}
            onMore={() => undefined}
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
      </nav>
      {newMenu && (
        <Menu
          at={newMenu}
          onClose={() => setNewMenu(null)}
          items={[{ label: 'New Bot', onSelect: () => navigate('/bots/new') }]}
        />
      )}
      <main className="flex min-w-0 flex-1 flex-col">
        <Outlet context={notifications} />
      </main>
    </div>
  );
}
