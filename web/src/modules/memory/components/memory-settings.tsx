import { useState } from 'react';
import { Box, RowText, rowClass, SectionHead, Tag } from '@/components/settings-ui';
import { TextField } from '@/components/text-field';
import { type MemoryItem, useMemory, useRetireMemory } from '@/modules/memory/api/memory';

const scopes = [
  ['all', 'All'],
  ['user', 'You'],
  ['repo', 'Repos'],
  ['bot', 'Bots'],
] as const;

const pill = 'h-[30px] rounded-full px-3 text-[13px]';

function day(iso: string) {
  const d = new Date(iso);
  return d.toDateString() === new Date().toDateString()
    ? 'today'
    : d.toLocaleDateString('en', { month: 'short', day: 'numeric' });
}

function MemoryRow({
  item,
  botNames,
  repoNames,
}: {
  item: MemoryItem;
  botNames: Record<string, string>;
  repoNames: Record<string, string>;
}) {
  const retire = useRetireMemory(item.id);
  const scope =
    item.scope === 'user'
      ? 'you'
      : item.scope === 'repo'
        ? `repo ${repoNames[item.scope_id] ?? ''}`.trim()
        : `bot ${botNames[item.scope_id] ?? ''}`.trim();
  return (
    <div className={rowClass}>
      <RowText
        title={item.body}
        detail={[scope, botNames[item.source_bot_id], day(item.created_at)]
          .filter(Boolean)
          .join(' · ')}
      >
        {retire.isError && (
          <i className="block text-[12.5px] not-italic text-danger">{retire.error.message}</i>
        )}
      </RowText>
      <Tag>{item.kind}</Tag>
      {item.status === 'retired' ? (
        <Tag>Retired</Tag>
      ) : (
        <button
          type="button"
          className="h-7 flex-none rounded-full border border-outline px-2.5 text-xs text-ink disabled:opacity-50"
          disabled={retire.isPending}
          onClick={() => retire.mutate()}
        >
          Retire
        </button>
      )}
    </div>
  );
}

export function MemorySettings({
  botNames = {},
  repoNames = {},
}: {
  botNames?: Record<string, string>;
  repoNames?: Record<string, string>;
}) {
  const { data: items = [] } = useMemory();
  const [query, setQuery] = useState('');
  const [scope, setScope] = useState<(typeof scopes)[number][0]>('all');
  const [retired, setRetired] = useState(false);
  const q = query.trim().toLowerCase();
  const shown = items.filter(
    (item) =>
      (retired || item.status !== 'retired') &&
      (scope === 'all' || item.scope === scope) &&
      (!q || `${item.body} ${item.why} ${item.kind}`.toLowerCase().includes(q)),
  );
  return (
    <>
      <SectionHead
        title="Memory"
        lead="What your Bots have learned. Nothing is deleted; retire what is wrong."
      />
      <label className="flex h-[42px] items-center gap-2 rounded-[14px] border border-line bg-box px-3.5 text-muted">
        <svg
          width="16"
          height="16"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          strokeWidth="1.8"
          strokeLinecap="round"
          aria-hidden="true"
        >
          <circle cx="11" cy="11" r="7" />
          <path d="M20 20l-3.5-3.5" />
        </svg>
        <TextField
          size="bare"
          width="flex-1"
          aria-label="Search memory"
          placeholder="Search memory"
          value={query}
          onChange={(e) => setQuery(e.target.value)}
        />
      </label>
      <div className="flex flex-wrap items-center gap-1.5">
        {scopes.map(([id, label]) => (
          <button
            key={id}
            type="button"
            aria-pressed={scope === id}
            onClick={() => setScope(id)}
            className={`${pill} ${scope === id ? 'bg-accent text-accent-ink' : 'border border-outline text-ink'}`}
          >
            {label}
          </button>
        ))}
        <span aria-hidden="true" className="mx-1 h-5 w-px bg-outline" />
        <button
          type="button"
          aria-pressed={retired}
          onClick={() => setRetired(!retired)}
          className={`${pill} border ${retired ? 'border-muted text-ink' : 'border-outline text-muted'}`}
        >
          Show retired
        </button>
      </div>
      {shown.length === 0 ? (
        <p className="rounded-[18px] border border-dashed border-outline px-6 py-10 text-center text-[13px] text-muted">
          {items.length === 0 ? 'Nothing remembered yet.' : 'No memory matches.'}
        </p>
      ) : (
        <Box>
          {shown.map((item) => (
            <MemoryRow key={item.id} item={item} botNames={botNames} repoNames={repoNames} />
          ))}
        </Box>
      )}
    </>
  );
}
