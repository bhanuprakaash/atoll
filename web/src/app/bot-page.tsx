import { Navigate, Outlet, useNavigate, useParams } from 'react-router';
import { Button } from '@/components/button';
import { useBots } from '@/modules/bots/api/bots';
import { useCreateThread, useThreads } from '@/modules/threads/api/threads';

export function BotPage() {
  const { botId = '' } = useParams();
  const navigate = useNavigate();
  const { data: bots = [] } = useBots();
  const createThread = useCreateThread(botId);

  return (
    <>
      <header className="flex items-center justify-between border-b border-line px-4 py-3">
        <h1 className="font-medium">{bots.find((b) => b.id === botId)?.name}</h1>
        <Button
          disabled={createThread.isPending}
          onClick={() =>
            createThread.mutate(undefined, {
              onSuccess: (thread) => navigate(`/bots/${botId}/threads/${thread.id}`),
            })
          }
        >
          New thread
        </Button>
      </header>
      <Outlet />
    </>
  );
}

export function LatestThread() {
  const { botId = '' } = useParams();
  const { data: threads } = useThreads(botId);
  const latest = threads?.[0];
  return latest ? <Navigate to={`/bots/${botId}/threads/${latest.id}`} replace /> : null;
}
