import { useEffect, useState } from 'react';
import { useRun } from '@/modules/threads/api/runs';
import { Creature, type CreatureLook } from '@/components/creature';
import { Markdown } from '@/components/markdown';
import { ApprovalCard } from '@/modules/threads/components/approval-card';
import type { LiveRun as Live } from '@/modules/threads/hooks/use-thread-events';
import { toolVerb } from '@/modules/threads/utils/tool-line';

export const chatText = 'text-base leading-[1.65] tracking-normal';

export function LiveRun({
  id,
  threadId,
  run,
  answered,
  look,
}: {
  id: string;
  threadId: string;
  run: Live;
  answered: boolean;
  look?: CreatureLook;
}) {
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
  const verb = toolVerb(run.tool);
  const lastStep = run.parts.reduce((last, part, i) => ('step' in part ? i : last), -1);
  const shown = answered || (run.finished && run.settled) ? [] : run.parts;
  return (
    <div className="flex flex-col gap-2">
      {!run.finished && (
        <div className="flex items-center gap-3 text-sm">
          <span className="relative size-8 flex-none">
            <Creature look={look} state="working" kind={kind} size={32} />
          </span>
          <span role="status" className="min-w-0 truncate">
            <span className="tide">{verb}</span>
            {run.tool?.target && <span className="text-muted"> {run.tool.target}</span>}
          </span>
          {data?.started_at && (
            <span className="flex-none text-xs text-label tabular-nums">
              {Math.floor(seconds / 60)}:{String(seconds % 60).padStart(2, '0')}
            </span>
          )}
        </div>
      )}
      {shown.length > 0 && (
        <div data-testid="live-text" className="flex flex-col gap-3">
          {shown.map((part, i) =>
            'text' in part ? (
              <div key={i} className={chatText}>
                <Markdown threadId={threadId}>{part.text}</Markdown>
              </div>
            ) : (
              part.step &&
              i !== lastStep && (
                <p key={i} data-testid="step" className="text-sm text-muted">
                  {part.step}
                </p>
              )
            ),
          )}
        </div>
      )}
      {run.approvals.map((a) => (
        <ApprovalCard key={a.id} {...a} />
      ))}
    </div>
  );
}
