import { useMessages } from '@/modules/threads/api/messages';
import { Composer } from '@/modules/threads/components/composer';
import { LiveRun } from '@/modules/threads/components/live-run';
import { useThreadEvents } from '@/modules/threads/hooks/use-thread-events';

export function ThreadView({ threadId }: { threadId: string }) {
  const { data: messages = [] } = useMessages(threadId);
  const { runs, connected, running, begin } = useThreadEvents(threadId);
  const answered = new Set(messages.filter((m) => m.role === 'bot').map((m) => m.run_id));

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="flex flex-1 flex-col gap-3 overflow-y-auto p-4">
        {messages.map((m) => (
          <p
            key={m.id}
            className={`max-w-prose whitespace-pre-wrap rounded-card px-3 py-2 text-sm ${
              m.role === 'user' ? 'self-end bg-accent text-accent-ink' : 'self-start bg-card'
            }`}
          >
            {m.text}
          </p>
        ))}
        {Object.entries(runs)
          .filter(([runId]) => !answered.has(runId))
          .map(([runId, run]) => (
            <LiveRun key={runId} run={run} />
          ))}
      </div>
      <Composer threadId={threadId} busy={!connected || running} onSent={(run) => begin(run.id)} />
    </div>
  );
}
