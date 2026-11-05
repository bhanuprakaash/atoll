import { useEffect, useRef } from 'react';
import { Navigate, Outlet, useNavigate, useParams } from 'react-router';
import { Creature } from '@/components/creature';
import { useBots, type Bot } from '@/modules/bots/api/bots';
import { useCreateThread, useThreads } from '@/modules/threads/api/threads';

export function BotHeader({
  bot,
  title,
  waiting = 0,
  onThreads,
  onSettings,
}: {
  bot?: Bot;
  title?: string;
  waiting?: number;
  onThreads?: () => void;
  onSettings?: () => void;
}) {
  return (
    <header className="flex min-h-16 items-center gap-3 border-b border-line px-5 py-2">
      {bot && <Creature look={bot.look} state={bot.state} size={30} />}
      <div className="min-w-0 flex-1">
        <h1>{bot?.name}</h1>
        <div className="truncate text-xs text-muted">{title}</div>
      </div>
      <button
        type="button"
        aria-label={waiting ? `Threads, ${waiting} waiting` : 'Threads'}
        onClick={onThreads}
        className="flex items-center gap-2 rounded-full border border-line px-3 py-2 text-sm"
      >
        Threads
        {waiting > 0 && (
          <span aria-hidden="true" className="rounded-full bg-danger px-2 text-xs">
            {waiting}
          </span>
        )}
      </button>
      <button
        type="button"
        aria-label="Bot settings"
        onClick={onSettings}
        className="rounded-full p-2 text-muted"
      >
        <svg
          aria-hidden="true"
          width="18"
          height="18"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          strokeWidth="1.8"
        >
          <path d="M4 7h10M18 7h2M4 17h4M12 17h8" />
          <circle cx="16" cy="7" r="2" />
          <circle cx="10" cy="17" r="2" />
        </svg>
      </button>
    </header>
  );
}

export function BotPage() {
  const { botId = '', threadId = '' } = useParams();
  const { data: bots = [] } = useBots();
  const { data: threads = [] } = useThreads(botId);
  return (
    <>
      <BotHeader
        bot={bots.find((bot) => bot.id === botId)}
        title={threads.find((thread) => thread.id === threadId)?.title}
        waiting={threads.filter((thread) => thread.approval).length}
      />
      <Outlet />
    </>
  );
}

export function LatestThread() {
  const { botId = '' } = useParams();
  const navigate = useNavigate();
  const { data: threads } = useThreads(botId);
  const create = useCreateThread(botId);
  const creatingFor = useRef('');
  useEffect(() => {
    if (
      threads &&
      threads.length === 0 &&
      creatingFor.current !== botId &&
      !create.isPending &&
      !create.isError
    ) {
      creatingFor.current = botId;
      create.mutate(undefined, {
        onSuccess: (thread) => navigate(`/bots/${botId}/threads/${thread.id}`, { replace: true }),
      });
    }
  }, [threads, create, botId, navigate]);
  const latest = threads?.[0];
  return latest ? <Navigate to={`/bots/${botId}/threads/${latest.id}`} replace /> : null;
}
