import { Button } from '@/components/button';
import { type PlatformChange, useChanges, useUndoChange } from '@/modules/platform/api/changes';

function Change({ change, botName }: { change: PlatformChange; botName?: string }) {
  const undo = useUndoChange(change.id);
  const verb =
    change.action === 'edit'
      ? 'Edited'
      : change.entity === 'repo'
        ? 'Registered repo'
        : `Created ${change.entity}`;
  return (
    <li
      role="region"
      aria-label="Platform change"
      className="flex items-center gap-3 text-sm text-muted"
    >
      <div className="flex flex-1 flex-col gap-0.5">
        <p className={change.undone ? 'line-through' : ''}>
          {verb} {change.after?.name ?? change.before?.name}
        </p>
        <p className="text-xs text-muted">{botName}</p>
        {undo.isError && <p className="text-danger">{undo.error.message}</p>}
      </div>
      {change.undone ? (
        <span className="text-muted">Undone</span>
      ) : (
        <Button
          className="border border-line bg-transparent text-muted"
          disabled={undo.isPending}
          onClick={() => undo.mutate()}
        >
          Undo
        </Button>
      )}
    </li>
  );
}

export function PlatformChanges({
  threadId,
  botNames,
}: {
  threadId: string;
  botNames: Record<string, string>;
}) {
  const { data: changes = [] } = useChanges(threadId);
  if (changes.length === 0) return null;

  return (
    <div className="py-2">
      <ul className="flex flex-col gap-2">
        {changes.map((change) => (
          <Change key={change.id} change={change} botName={botNames[change.source_bot_id]} />
        ))}
      </ul>
    </div>
  );
}
