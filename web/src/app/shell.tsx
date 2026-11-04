import { Outlet, useNavigate } from 'react-router';
import { Button } from '@/components/button';
import { BotList } from '@/modules/bots/components/bot-list';

export function Shell() {
  const navigate = useNavigate();
  return (
    <div className="flex h-screen">
      <nav className="flex w-64 flex-col gap-3 border-r border-line bg-card p-3">
        <Button onClick={() => navigate('/bots/new')}>New Bot</Button>
        <BotList />
      </nav>
      <main className="flex min-w-0 flex-1 flex-col">
        <Outlet />
      </main>
    </div>
  );
}
