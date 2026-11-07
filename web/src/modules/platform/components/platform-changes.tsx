import { Link } from 'react-router';
import { Creature, type CreatureLook } from '@/components/creature';
import {
  type PlatformChange,
  useChanges,
  useUndoChange,
  useChangeNames,
} from '@/modules/platform/api/changes';

const providerLabel = (provider?: string) =>
  (({ claude: 'Claude Code', codex: 'Codex' }) as Record<string, string>)[provider ?? ''] ??
  provider;

const nameOf = (id: string, list: { id: string; name: string }[]) =>
  list.find((item) => item.id === id)?.name ?? id;

const past = (change: PlatformChange) =>
  change.action === 'edit' ? 'Edited' : change.entity === 'repo' ? 'Registered' : 'Created';

const repoAccessLabel = (
  access: 'all' | string[] | undefined,
  repos: { id: string; name: string }[],
) =>
  access === 'all' || !access
    ? 'All repos'
    : access.length === 0
      ? 'No repos'
      : `${access.map((id) => nameOf(id, repos)).join(', ')} only`;

function By({ change, botName }: { change: PlatformChange; botName?: string }) {
  return (
    <span className="text-xs text-muted">
      {botName ?? (change.source_bot_id === 'chief' ? 'Chief' : '')}
    </span>
  );
}

function Change({
  change,
  botName,
  look,
}: {
  change: PlatformChange;
  botName?: string;
  look?: CreatureLook;
}) {
  const undo = useUndoChange([change.id]);
  const names = useChangeNames();
  const connectionNames = names.connections.map((c) => ({
    id: c.id,
    name: c.kind === 'gitlab' ? 'GitLab' : c.kind === 'github' ? 'GitHub' : c.kind,
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

  const verb = change.action === 'edit' ? past(change) : `${past(change)} ${change.entity}`;
  const name = change.after?.name ?? change.before?.name;
  const by = <By change={change} botName={botName} />;
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
      repoAccessLabel(after.repo_access, names.repos),
    ];
    return (
      <li
        role="region"
        aria-label="Platform change"
        className={`flex w-110 max-w-full flex-col gap-4.5 rounded-[28px] border border-sidebar-line bg-box p-5.5 ${change.undone ? 'opacity-60' : ''}`}
      >
        <div className="flex items-center gap-4">
          <Creature look={look ?? after.look} size={56} />
          <div className="min-w-0">
            <div className="text-xs tracking-wide text-label">NEW BOT</div>
            <div className={`text-[22px] tracking-tight ${change.undone ? 'line-through' : ''}`}>
              {after.name}
            </div>
            <div className="mt-1 inline-block rounded-full bg-selected px-2.5 py-0.5 text-xs text-ink/80">
              {[providerLabel(after.provider), after.model].filter(Boolean).join(' · ')}
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

function ChangeGroup({
  changes,
  botName,
  looks,
}: {
  changes: PlatformChange[];
  botName?: string;
  looks: Record<string, CreatureLook>;
}) {
  const pending = changes.filter((change) => !change.undone);
  const undo = useUndoChange(pending.map((change) => change.id));
  const names = useChangeNames();
  const { entity } = changes[0];
  const by = <By change={changes[0]} botName={botName} />;
  const undoAll =
    pending.length === 0 ? (
      <span className="text-[13px] text-muted">Undone</span>
    ) : (
      <button
        type="button"
        disabled={undo.isPending}
        onClick={() => undo.mutate()}
        className="text-[13px] text-muted underline decoration-outline underline-offset-3"
      >
        Undo all
      </button>
    );
  const error = undo.isError && <p className="text-sm text-danger">{undo.error.message}</p>;

  if (entity === 'bot' && changes[0].action === 'create') {
    return (
      <li
        role="region"
        aria-label="Platform change"
        className="w-110 max-w-full rounded-[22px] border border-sidebar-line bg-box"
      >
        <div className="flex items-center gap-2.5 px-4 py-3 text-[13px]">
          <span className="flex-1">New Bots · {changes.length}</span>
          {by}
          {undoAll}
        </div>
        {changes.map((change) => (
          <div
            key={change.id}
            className="flex items-center gap-3 border-t border-sidebar-line px-4 py-2.5"
          >
            <Creature look={looks[change.entity_id] ?? change.after?.look} size={30} />
            <div className="min-w-0 flex-1">
              <div className={`truncate text-sm ${change.undone ? 'line-through' : ''}`}>
                {change.after?.name}
              </div>
              <div className="truncate text-xs text-muted">
                {[
                  providerLabel(change.after?.provider),
                  change.after?.model,
                  (change.after?.skills ?? []).map((id) => nameOf(id, names.skills)).join(', '),
                  repoAccessLabel(change.after?.repo_access, names.repos),
                ]
                  .filter(Boolean)
                  .join(' · ')}
              </div>
            </div>
            {!change.undone && (
              <Link
                to={`/bots/${change.entity_id}`}
                className="text-[13px] text-muted underline decoration-outline underline-offset-3"
              >
                Open
              </Link>
            )}
          </div>
        ))}
        {error && <div className="border-t border-sidebar-line px-4 py-2.5">{error}</div>}
      </li>
    );
  }

  return (
    <li
      role="region"
      aria-label="Platform change"
      className={`flex items-center gap-2.5 text-sm ${pending.length === 0 ? 'text-muted' : 'text-ink/80'}`}
    >
      <span className={`flex-1 ${pending.length === 0 ? 'line-through' : ''}`}>
        {past(changes[0])} {changes.length} {entity}s{' '}
        <span className="text-muted">
          {changes.map((change) => change.after?.name ?? change.before?.name).join(', ')}
        </span>
      </span>
      {by}
      {undoAll}
      {error}
    </li>
  );
}

export function PlatformChanges({
  threadId,
  runId,
  botNames,
  looks,
}: {
  threadId: string;
  runId: string;
  botNames: Record<string, string>;
  looks: Record<string, CreatureLook>;
}) {
  const { data: all = [] } = useChanges(threadId);
  const changes = all.filter((change) => change.source_run_id === runId);
  if (changes.length === 0) return null;
  const groups = new Map<string, PlatformChange[]>();
  for (const change of changes) {
    const key = `${change.entity}/${change.action}`;
    groups.set(key, [...(groups.get(key) ?? []), change]);
  }

  return (
    <div>
      <ul className="flex flex-col gap-2">
        {[...groups].map(([key, group]) =>
          group.length === 1 ? (
            <Change
              key={key}
              change={group[0]}
              botName={botNames[group[0].source_bot_id]}
              look={looks[group[0].entity_id]}
            />
          ) : (
            <ChangeGroup
              key={key}
              changes={group}
              botName={botNames[group[0].source_bot_id]}
              looks={looks}
            />
          ),
        )}
      </ul>
    </div>
  );
}
