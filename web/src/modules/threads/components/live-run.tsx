import { useEffect, useState } from 'react';
import { useRun } from '@/modules/threads/api/runs';
import { Creature, type CreatureLook, type ToolKind } from '@/components/creature';
import { ApprovalCard } from '@/modules/threads/components/approval-card';
import type { LiveRun as Live } from '@/modules/threads/hooks/use-thread-events';

const verbs: Record<ToolKind, string> = {
  none: 'Thinking',
  search: 'Searching',
  read: 'Reading',
  edit: 'Editing',
  command: 'Running',
  memory: 'Remembering',
  platform: 'Setting up',
  work: 'Working',
  other: 'Working',
};
export function LiveRun({ id, run, look }: { id: string; run: Live; look?: CreatureLook }) {
  const { data } = useRun(id);
  const [now, setNow] = useState(Date.now);
  useEffect(() => {
    if (run.finished) return;
    const timer = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(timer);
  }, [run.finished]);
  const seconds = data?.started_at
    ? Math.max(0, Math.floor((now - Date.parse(data.started_at)) / 1000))
    : 0;
  const kind = run.tool?.kind ?? 'none';
  return (
    <div className="flex flex-col gap-2">
      {!run.finished && (
        <div className="flex items-center gap-2 text-sm text-muted">
          <Creature look={look} state="working" kind={kind} size={24} />
          <span role="status" className="motion-safe:animate-pulse">
            {verbs[kind] ?? 'Working'}
            {run.tool?.target ? ` ${run.tool.target}` : ''}
          </span>
          {data?.started_at && (
            <span className="text-xs">
              {Math.floor(seconds / 60)}:{String(seconds % 60).padStart(2, '0')}
            </span>
          )}
        </div>
      )}
      {run.approvals.map((a) => (
        <ApprovalCard key={a.id} {...a} />
      ))}
    </div>
  );
}
