import { useQuery } from '@tanstack/react-query';
import { NavLink, Outlet, useNavigate } from 'react-router';
import { api } from '@/lib/api-client';
import { BotList } from '@/modules/bots/components/bot-list';

export function Shell() {
  const navigate = useNavigate();
  const { data: settings } = useQuery({
    queryKey: ['settings'],
    queryFn: () => api<{ user_name: string }>('/settings'),
  });
  return (
    <div className="flex h-screen">
      <nav aria-label="Bots" className="flex w-72 flex-col border-r border-line bg-card px-3 py-4">
        <div className="flex items-center justify-between px-3 pb-4">
          <span>Reef</span>
          <button
            type="button"
            aria-label="New Bot"
            onClick={() => navigate('/bots/new')}
            className="rounded-control px-2 text-xl text-muted"
          >
            +
          </button>
        </div>
        <BotList />
        <NavLink to="/settings" className="mt-auto rounded-control px-3 py-3 text-sm">
          <span>{settings?.user_name || 'User'}</span>
          <span className="block text-xs text-muted">Settings</span>
        </NavLink>
      </nav>
      <main className="flex min-w-0 flex-1 flex-col">
        <Outlet />
      </main>
    </div>
  );
}
