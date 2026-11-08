import { useState } from 'react';
import { Link } from 'react-router';
import { quiet } from '@/components/button';
import { Creature, type CreatureLook } from '@/components/creature';
import { type PlatformChange, useChanges, useUndoChange } from '@/modules/platform/api/changes';

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

const fields = [
  'name',
  'provider',
  'model',
  'mode',
  'persona',
  'look',
  'skills',
  'connections',
  'repo_access',
  'delegates',
  'content',
] as const;

type Names = {
  bots: { id: string; name: string }[];
  skills: { id: string; name: string }[];
  repos: { id: string; name: string }[];
  connections: { id: string; name: string }[];
};

// By names the Bot that made a change, unless it is the thread's own Bot.
function By({
  change,
  botId,
  botName,
}: {
  change: PlatformChange;
  botId: string;
  botName?: string;
}) {
  if (change.source_bot_id === botId) return null;
  return (
    <span className="flex-none text-xs text-muted">
      {botName ?? (change.source_bot_id === 'chief' ? 'Chief' : '')}
    </span>
  );
}

function NewBotRow({
  change,
  look,
  names,
}: {
  change: PlatformChange;
  look?: CreatureLook;
  names: Names;
}) {
  const undo = useUndoChange([change.id]);
  const after = change.after ?? {};
  return (
    <div className="flex items-center gap-3 border-t border-sidebar-line px-4 py-2.5">
      <Creature look={look ?? after.look} size={30} />
      <div className={`min-w-0 flex-1 ${change.undone ? 'text-muted' : ''}`}>
        <div className={`truncate text-sm ${change.undone ? 'line-through' : ''}`}>
          {after.name}
        </div>
        <div className="truncate text-xs text-muted">
          {[
            [providerLabel(after.provider), after.model].filter(Boolean).join(' · '),
            (after.skills ?? []).map((id) => nameOf(id, names.skills)).join(', '),
            repoAccessLabel(after.repo_access, names.repos),
          ]
            .filter(Boolean)
            .join(' · ')}
        </div>
        {undo.isError && <p className="text-sm text-danger">{undo.error.message}</p>}
      </div>
      {change.undone ? (
        <span className="text-[13px] text-muted">Undone</span>
      ) : (
        <>
          <Link to={`/bots/${change.entity_id}`} className={quiet}>
            Open
          </Link>
          <button
            type="button"
            disabled={undo.isPending}
            onClick={() => undo.mutate()}
            className={quiet}
          >
            Undo
          </button>
        </>
      )}
    </div>
  );
}

function NewBots({
  changes,
  botId,
  botName,
  looks,
  names,
}: {
  changes: PlatformChange[];
  botId: string;
  botName?: string;
  looks: Record<string, CreatureLook>;
  names: Names;
}) {
  const pending = changes.filter((change) => !change.undone);
  const undo = useUndoChange(pending.map((change) => change.id));
  return (
    <li
      role="region"
      aria-label="Platform change"
      className="w-full rounded-[22px] border border-sidebar-line bg-box"
    >
      <div className="flex items-center gap-2.5 px-4 py-3 text-[13px]">
        <span className="flex-1">
          {changes.length === 1 ? 'New Bot' : `New Bots · ${changes.length}`}
        </span>
        <By change={changes[0]} botId={botId} botName={botName} />
        {changes.length > 1 && pending.length > 0 && (
          <button
            type="button"
            disabled={undo.isPending}
            onClick={() => undo.mutate()}
            className={quiet}
          >
            Undo all
          </button>
        )}
      </div>
      {changes.map((change) => (
        <NewBotRow key={change.id} change={change} look={looks[change.entity_id]} names={names} />
      ))}
      {undo.isError && (
        <p className="border-t border-sidebar-line px-4 py-2.5 text-sm text-danger">
          {undo.error.message}
        </p>
      )}
    </li>
  );
}

function Change({
  change,
  botId,
  botName,
  names,
}: {
  change: PlatformChange;
  botId: string;
  botName?: string;
  names: Names;
}) {
  const undo = useUndoChange([change.id]);
  const [open, setOpen] = useState(false);
  const display = (value: unknown, key: string) => {
    if (!Array.isArray(value)) return String(value ?? '');
    const list =
      key === 'skills'
        ? names.skills
        : key === 'connections'
          ? names.connections
          : key === 'repo_access'
            ? names.repos
            : key === 'delegates'
              ? names.bots
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
          .map((key) => ({
            label: key.replace('_', ' '),
            before: display(change.before?.[key], key),
            after: display(change.after?.[key], key),
          }))
      : [];
  const long = edits.some((e) => `${e.before}${e.after}`.length > 60 || /\n/.test(e.after));
  const name = change.after?.name ?? change.before?.name;

  return (
    <li
      role="region"
      aria-label="Platform change"
      className={`flex flex-col gap-2 text-sm ${change.undone ? 'text-muted' : 'text-ink/80'}`}
    >
      <div className="flex items-start gap-2.5">
        <span className={`line-clamp-2 flex-1 ${change.undone ? 'line-through' : ''}`}>
          {attachmentOnly ? (
            `Attached ${attached.map((id) => nameOf(id, names.connections)).join(', ')} to ${name}`
          ) : (
            <>
              {change.action === 'edit' ? past(change) : `${past(change)} ${change.entity}`} {name}
              {change.after?.path && (
                <span className="font-mono text-muted"> {change.after.path}</span>
              )}
              {edits.length > 0 && (
                <span className="text-muted">
                  {' · '}
                  {long
                    ? edits.map((e) => e.label).join(', ')
                    : edits.map((e) => `${e.label} ${e.before} → ${e.after}`).join(' · ')}
                </span>
              )}
            </>
          )}
        </span>
        <By change={change} botId={botId} botName={botName} />
        {long && (
          <button
            type="button"
            aria-expanded={open}
            onClick={() => setOpen(!open)}
            className={`flex-none ${quiet}`}
          >
            {open ? 'Hide change' : 'Show change'}
          </button>
        )}
        {change.undone ? (
          <span className="flex-none text-[13px] text-muted">Undone</span>
        ) : (
          <button
            type="button"
            disabled={undo.isPending}
            onClick={() => undo.mutate()}
            className={`flex-none ${quiet}`}
          >
            Undo
          </button>
        )}
      </div>
      {open &&
        edits.map((e) => (
          <div
            key={e.label}
            className="grid grid-cols-2 gap-3 rounded-2xl border border-sidebar-line p-3 text-[13px]"
          >
            {[
              ['Before', e.before],
              ['After', e.after],
            ].map(([label, value]) => (
              <div key={label} className="min-w-0">
                <div className="text-xs text-label">
                  {label} · {e.label}
                </div>
                <p className="whitespace-pre-wrap text-ink/80 [overflow-wrap:anywhere]">{value}</p>
              </div>
            ))}
          </div>
        ))}
      {undo.isError && <p className="text-danger">{undo.error.message}</p>}
    </li>
  );
}

function ChangeGroup({
  changes,
  botId,
  botName,
}: {
  changes: PlatformChange[];
  botId: string;
  botName?: string;
}) {
  const pending = changes.filter((change) => !change.undone);
  const undo = useUndoChange(pending.map((change) => change.id));
  return (
    <li
      role="region"
      aria-label="Platform change"
      className={`flex items-start gap-2.5 text-sm ${pending.length === 0 ? 'text-muted' : 'text-ink/80'}`}
    >
      <span className={`line-clamp-2 flex-1 ${pending.length === 0 ? 'line-through' : ''}`}>
        {past(changes[0])} {changes.length} {changes[0].entity}s{' '}
        <span className="text-muted">
          {changes.map((change) => change.after?.name ?? change.before?.name).join(', ')}
        </span>
      </span>
      <By change={changes[0]} botId={botId} botName={botName} />
      {pending.length === 0 ? (
        <span className="flex-none text-[13px] text-muted">Undone</span>
      ) : (
        <button
          type="button"
          disabled={undo.isPending}
          onClick={() => undo.mutate()}
          className={`flex-none ${quiet}`}
        >
          Undo all
        </button>
      )}
      {undo.isError && <p className="text-danger">{undo.error.message}</p>}
    </li>
  );
}

export function PlatformChanges({
  threadId,
  runId,
  botId,
  botNames,
  looks,
  names,
}: {
  threadId: string;
  runId: string;
  botId: string;
  botNames: Record<string, string>;
  looks: Record<string, CreatureLook>;
  names: Omit<Names, 'connections'> & { connections: { id: string; kind: string }[] };
}) {
  const { data: all = [] } = useChanges(threadId);
  const changes = all.filter((change) => change.source_run_id === runId);
  if (changes.length === 0) return null;
  const labelled: Names = {
    ...names,
    connections: names.connections.map((c) => ({
      id: c.id,
      name: c.kind === 'gitlab' ? 'GitLab' : c.kind === 'github' ? 'GitHub' : c.kind,
    })),
  };
  const groups = new Map<string, PlatformChange[]>();
  for (const change of changes) {
    const key = `${change.entity}/${change.action}`;
    groups.set(key, [...(groups.get(key) ?? []), change]);
  }

  return (
    <ul className="flex flex-col gap-2">
      {[...groups].map(([key, group]) => {
        const botName = botNames[group[0].source_bot_id];
        if (key === 'bot/create')
          return (
            <NewBots
              key={key}
              changes={group}
              botId={botId}
              botName={botName}
              looks={looks}
              names={labelled}
            />
          );
        return group.length === 1 ? (
          <Change key={key} change={group[0]} botId={botId} botName={botName} names={labelled} />
        ) : (
          <ChangeGroup key={key} changes={group} botId={botId} botName={botName} />
        );
      })}
    </ul>
  );
}
