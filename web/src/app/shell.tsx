import { useQuery } from '@tanstack/react-query';
import { NavLink, Outlet, useNavigate } from 'react-router';
import { api } from '@/lib/api-client';
import { BotList } from '@/modules/bots/components/bot-list';

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
        className="flex w-72 flex-col border-r border-line bg-card px-2.5 pt-4 pb-3"
      >
        <div data-tauri-drag-region className="flex h-8 items-center justify-between px-2 pb-0.5">
          {!inDesktopApp && <span>Reef</span>}
          <button
            type="button"
            aria-label="New Bot"
            onClick={() => navigate('/bots/new')}
            className="ml-auto flex size-8 items-center justify-center rounded-control text-muted"
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
        <div className="mt-3.5">
          <BotList />
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
      <main className="flex min-w-0 flex-1 flex-col">
        <Outlet />
      </main>
    </div>
  );
}
