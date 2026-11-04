import { ApprovalCard } from '@/modules/threads/components/approval-card';
import type { LiveRun as Live } from '@/modules/threads/hooks/use-thread-events';

export function LiveRun({ run }: { run: Live }) {
  return (
    <div className="flex flex-col gap-2">
      {run.tools.map((tool) => (
        <p key={tool.id} className="font-mono text-xs text-muted">
          {tool.name}
        </p>
      ))}
      {run.text && <p className="whitespace-pre-wrap text-sm">{run.text}</p>}
      {run.finished?.error && <p className="text-sm text-danger">{run.finished.error}</p>}
      {!run.finished && run.approvals.map((a) => <ApprovalCard key={a.id} {...a} />)}
    </div>
  );
}
