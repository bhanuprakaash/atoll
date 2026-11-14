import { useEffect, useState } from 'react';
import { useRun } from '@/modules/threads/api/runs';
import { Creature, type CreatureLook } from '@/components/creature';
import { ApprovalCard } from '@/modules/threads/components/approval-card';
import type { LivePart, LiveRun as Live } from '@/modules/threads/hooks/use-thread-events';

export const chatText = 'text-[15px] leading-[1.6] tracking-normal';

const fenceNames: Record<string, string> = { 'reef-metrics': 'Metrics', 'reef-kv': 'Details' };

// openFence names the reef- component whose fence the text has not closed yet.
function openFence(text: string) {
  let open: string | undefined;
  for (const line of text.split('\n')) {
    const fence = /^ {0,3}```(.*)$/.exec(line);
    if (fence && !open) open = fence[1].trim();
    else if (fence && !fence[1].trim()) open = undefined;
  }
  return open && fenceNames[open];
}

function statusText(parts: LivePart[]) {
  for (const part of [...parts].reverse()) {
    const fence = 'text' in part ? openFence(part.text) : undefined;
    const text =
      'step' in part ? part.step : fence ? `${fence}…` : part.text.trim().split('\n').pop()?.trim();
    if (text) return text;
  }
  return 'Thinking';
}

export function LiveRun({
  id,
  run,
  answered,
  look,
}: {
  id: string;
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
  return (
    <div className="flex flex-col gap-2">
      {!(answered || (run.finished && run.settled)) && (
        <div className="flex items-center gap-3 text-sm">
          <span className="relative size-8 flex-none">
            <Creature look={look} state="working" kind={kind} size={32} />
          </span>
          <span role="status" className="tide min-w-0 truncate">
            {statusText(run.parts)}
          </span>
          {data?.started_at && (
            <span className="flex-none text-xs text-label tabular-nums">
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
