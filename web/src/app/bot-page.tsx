import { useState } from 'react';
import { Navigate, Outlet, useNavigate, useParams } from 'react-router';
import { Button } from '@/components/button';
import { controlClass } from '@/components/field';
import { useBots } from '@/modules/bots/api/bots';
import { useRepos } from '@/modules/repos/api/repos';
import { useCreateThread, useThreads } from '@/modules/threads/api/threads';

export function BotPage() {
  const { botId = '' } = useParams();
  const navigate = useNavigate();
  const [repoId, setRepoId] = useState('');
  const { data: bots = [] } = useBots();
  const { data: repos = [] } = useRepos();
  const createThread = useCreateThread(botId);

  return (
    <>
      <header className="flex items-center justify-between border-b border-line px-4 py-3">
        <h1 className="font-medium">{bots.find((b) => b.id === botId)?.name}</h1>
        <div className="flex items-center gap-2">
          {createThread.isError && (
            <p className="text-sm text-danger">{createThread.error.message}</p>
          )}
          <select
            aria-label="Repo"
            className={`${controlClass} w-auto`}
            value={repoId}
            onChange={(e) => setRepoId(e.target.value)}
          >
            <option value="">No repo</option>
            {repos.map((repo) => (
              <option key={repo.id} value={repo.id}>
                {repo.name}
              </option>
            ))}
          </select>
          <Button
            disabled={createThread.isPending}
            onClick={() =>
              createThread.mutate(repoId || undefined, {
                onSuccess: (thread) => navigate(`/bots/${botId}/threads/${thread.id}`),
              })
            }
          >
            New thread
          </Button>
        </div>
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
