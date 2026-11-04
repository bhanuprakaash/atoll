import type { ReactNode } from 'react';
import { Button } from '@/components/button';
import { useMessages } from '@/modules/threads/api/messages';
import { useCloseThread, useThread } from '@/modules/threads/api/threads';
import { Composer } from '@/modules/threads/components/composer';
import { LiveRun } from '@/modules/threads/components/live-run';
import { useThreadEvents } from '@/modules/threads/hooks/use-thread-events';

export function ThreadView({
  threadId,
  repoName,
  memory,
}: {
  threadId: string;
  repoName?: string;
  memory?: ReactNode;
}) {
  const { data: messages = [] } = useMessages(threadId);
  const { data: thread } = useThread(threadId);
  const close = useCloseThread(threadId);
  const { runs, connected, running, mr, worktreeRemoved, begin } = useThreadEvents(threadId);
  const answered = new Set(messages.filter((m) => m.role === 'bot').map((m) => m.run_id));
  const mrUrl = mr?.url ?? thread?.mr_url;
  const mrState = mr?.state ?? thread?.mr_state;
  const closed = !!thread?.closed;

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      {thread && (
        <div className="flex flex-wrap items-center gap-x-4 gap-y-1 border-b border-line px-4 py-2 text-sm">
          {thread.repo_id && <span>{repoName ?? thread.repo_id}</span>}
          {thread.branch && <span className="font-mono text-xs">{thread.branch}</span>}
          {mrUrl && (
            <a href={mrUrl} target="_blank" rel="noreferrer" className="text-accent underline">
              Merge request{mrState && ` (${mrState})`}
            </a>
          )}
          {(worktreeRemoved || (thread.branch && !thread.worktree)) && (
            <span className="text-muted">Worktree removed</span>
          )}
          {closed ? (
            <span className="text-muted">Closed</span>
          ) : (
            <Button disabled={close.isPending} onClick={() => close.mutate()} className="ml-auto">
              Close thread
            </Button>
          )}
          {close.isError && <span className="text-danger">{close.error.message}</span>}
        </div>
      )}
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
      {memory}
      <Composer
        threadId={threadId}
        busy={!connected || running || closed}
        onSent={(run) => begin(run.id)}
      />
    </div>
  );
}
