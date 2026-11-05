import { Link } from 'react-router';
import { Creature } from '@/components/creature';
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
      className={`flex items-center gap-3 text-sm text-muted ${change.entity === 'bot' && change.action === 'create' ? 'rounded-card border border-line bg-card p-4' : ''}`}
    >
      {change.entity === 'bot' && change.action === 'create' && (
        <Creature look={change.after?.look} size={52} />
      )}
      <div className="flex flex-1 flex-col gap-0.5">
        {change.entity === 'bot' && change.action === 'create' && (
          <span className="text-xs">NEW BOT</span>
        )}
        <p className={change.undone ? 'line-through' : ''}>
          {verb} <span>{change.after?.name ?? change.before?.name}</span>
        </p>
        {change.entity === 'bot' && change.action === 'create' && change.after && (
          <>
            <p>{[change.after.provider, change.after.model].filter(Boolean).join(' · ')}</p>
            {change.after.persona && <p>{change.after.persona}</p>}
            {!change.undone && (
              <Link className="underline" to={`/bots/${change.entity_id}`}>
                Open {change.after.name}
              </Link>
            )}
          </>
        )}
        {change.action === 'edit' &&
          change.before?.model &&
          change.after?.model &&
          change.before.model !== change.after.model && (
            <p>
              Model {change.before.model} → {change.after.model}
            </p>
          )}
        <p className="text-xs text-muted">
          {botName ?? (change.source_bot_id === 'chief' ? 'Chief' : '')}
        </p>
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
