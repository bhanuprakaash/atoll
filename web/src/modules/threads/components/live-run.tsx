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
  platform: 'Updating',
  work: 'Working',
  other: 'Working',
};
export function LiveRun({ run, look }: { run: Live; look?: CreatureLook }) {
  const kind = run.tool?.kind ?? 'none';
  return (
    <div className="flex flex-col gap-2">
      {!run.finished && (
        <div className="flex items-center gap-2 text-sm text-muted">
          <Creature look={look} state="working" kind={kind} size={24} />
          <span role="status">
            {verbs[kind] ?? 'Working'}
            {run.tool?.target ? ` ${run.tool.target}` : ''}
          </span>
        </div>
      )}
      {run.approvals.map((a) => (
        <ApprovalCard key={a.id} {...a} />
      ))}
    </div>
  );
}
