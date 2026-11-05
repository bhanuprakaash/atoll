import { Link } from 'react-router';
import { Creature } from '@/components/creature';
import {
  type PlatformChange,
  useChanges,
  useUndoChange,
  useChangeNames,
} from '@/modules/platform/api/changes';

function Change({ change, botName }: { change: PlatformChange; botName?: string }) {
  const undo = useUndoChange(change.id);
  const names = useChangeNames();
  const nameOf = (id: string, list: { id: string; name: string }[]) =>
    list.find((item) => item.id === id)?.name ?? id;
  const connectionNames = names.connections.map((c) => ({
    id: c.id,
    name: c.kind === 'gitlab' ? 'GitLab' : c.kind,
  }));
  const fields = [
    'name',
    'provider',
    'model',
    'persona',
    'look',
    'skills',
    'connections',
    'repo_access',
    'content',
  ] as const;
  const display = (value: unknown, key: string) => {
    if (!Array.isArray(value)) return String(value ?? '');
    const list =
      key === 'skills'
        ? names.skills
        : key === 'connections'
          ? connectionNames
          : key === 'repo_access'
            ? names.repos
            : [];
    return value.map((id: string) => nameOf(id, list)).join(', ');
  };
  const attached =
    change.entity === 'bot' && change.action === 'edit'
      ? (change.after?.connections ?? []).filter(
          (id) => !(change.before?.connections ?? []).includes(id),
        )
      : [];
  const attachmentOnly =
    attached.length > 0 &&
    fields
      .filter((key) => key !== 'connections')
      .every((key) => JSON.stringify(change.before?.[key]) === JSON.stringify(change.after?.[key]));
  const edits =
    change.action === 'edit' && !attachmentOnly
      ? fields
          .filter(
            (key) => JSON.stringify(change.before?.[key]) !== JSON.stringify(change.after?.[key]),
          )
          .map(
            (key) =>
              `${key.charAt(0).toUpperCase() + key.slice(1).replace('_', ' ')} ${display(change.before?.[key], key)} → ${display(change.after?.[key], key)}`,
          )
      : [];

  const verb =
    change.action === 'edit'
      ? 'Edited'
      : change.entity === 'repo'
        ? 'Registered repo'
        : `Created ${change.entity}`;
  const name = change.after?.name ?? change.before?.name;
  const by = (
    <span className="text-xs text-muted">
      {botName ?? (change.source_bot_id === 'chief' ? 'Chief' : '')}
    </span>
  );
  const undoControl = change.undone ? (
    <span className="text-[13px] text-muted">Undone</span>
  ) : (
    <button
      type="button"
      disabled={undo.isPending}
      onClick={() => undo.mutate()}
      className="text-[13px] text-muted underline decoration-outline underline-offset-3"
    >
      Undo
    </button>
  );

  if (change.entity === 'bot' && change.action === 'create' && change.after) {
    const after = change.after;
    const chips = [
      ...(after.skills ?? []).map((id) => nameOf(id, names.skills)),
      ...(after.connections ?? []).map((id) => nameOf(id, connectionNames)),
      after.repo_access === 'all' || !after.repo_access
        ? 'All repos'
        : after.repo_access.length === 0
          ? 'No repos'
          : `${after.repo_access.map((id) => nameOf(id, names.repos)).join(', ')} only`,
    ];
    return (
      <li
        role="region"
        aria-label="Platform change"
        className={`flex w-110 max-w-full flex-col gap-4.5 rounded-[28px] border border-sidebar-line bg-box p-5.5 ${change.undone ? 'opacity-60' : ''}`}
      >
        <div className="flex items-center gap-4">
          <Creature look={after.look} size={56} />
          <div className="min-w-0">
            <div className="text-xs tracking-wide text-label">NEW BOT</div>
            <div className={`text-[22px] tracking-tight ${change.undone ? 'line-through' : ''}`}>
              {after.name}
            </div>
            <div className="mt-1 inline-block rounded-full bg-selected px-2.5 py-0.5 text-xs text-ink/80">
              {[
                ({ claude: 'Claude Code', codex: 'Codex' } as Record<string, string>)[
                  after.provider ?? ''
                ] ?? after.provider,
                after.model,
              ]
                .filter(Boolean)
                .join(' · ')}
            </div>
          </div>
        </div>
        {after.persona && <p className="text-sm text-ink/80">{after.persona}</p>}
        <div className="flex flex-wrap gap-1.5 text-[12.5px]">
          {chips.map((chip) => (
            <span key={chip} className="rounded-full bg-selected px-2.5 py-1 text-ink/80">
              {chip}
            </span>
          ))}
        </div>
        <div className="flex items-center gap-2">
          {!change.undone && (
            <Link
              to={`/bots/${change.entity_id}`}
              className="flex h-8 items-center rounded-full bg-accent px-4 text-[13.5px] text-accent-ink"
            >
              Open {after.name}
            </Link>
          )}
          {change.undone ? (
            <span className="text-[13px] text-muted">Undone</span>
          ) : (
            <button
              type="button"
              disabled={undo.isPending}
              onClick={() => undo.mutate()}
              className="h-8 rounded-full border border-outline px-4 text-[13.5px]"
            >
              Undo
            </button>
          )}
          <span className="ml-auto">{by}</span>
        </div>
        {undo.isError && <p className="text-sm text-danger">{undo.error.message}</p>}
      </li>
    );
  }

  return (
    <li
      role="region"
      aria-label="Platform change"
      className={`flex items-center gap-2.5 text-sm ${change.undone ? 'text-muted' : 'text-ink/80'}`}
    >
      <span className={`flex-1 ${change.undone ? 'line-through' : ''}`}>
        {attachmentOnly ? (
          `Attached ${attached.map((id) => nameOf(id, connectionNames)).join(', ')} to ${name}`
        ) : (
          <>
            {verb} {name}
            {change.after?.path && (
              <span className="font-mono text-muted"> {change.after.path}</span>
            )}
            {edits.length > 0 && <span className="text-muted"> · {edits.join(' · ')}</span>}
          </>
        )}
      </span>
      {by}
      {undoControl}
      {undo.isError && <span className="text-danger">{undo.error.message}</span>}
    </li>
  );
}

export function PlatformChanges({
  threadId,
  runId,
  botNames,
}: {
  threadId: string;
  runId: string;
  botNames: Record<string, string>;
}) {
  const { data: all = [] } = useChanges(threadId);
  const changes = all.filter((change) => change.source_run_id === runId);
  if (changes.length === 0) return null;

  return (
    <div>
      <ul className="flex flex-col gap-2">
        {changes.map((change) => (
          <Change key={change.id} change={change} botName={botNames[change.source_bot_id]} />
        ))}
      </ul>
    </div>
  );
}
