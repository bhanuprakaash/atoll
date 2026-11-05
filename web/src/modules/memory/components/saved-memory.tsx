import { useState } from 'react';
import { type MemoryItem, useMemory, useRetireMemory } from '@/modules/memory/api/memory';

function SavedItem({ item, botName }: { item: MemoryItem; botName?: string }) {
  const retire = useRetireMemory(item.id);
  const [expanded, setExpanded] = useState(false);
  const retired = item.status === 'retired';
  return (
    <li className="flex flex-col gap-1">
      <div
        className={`flex max-w-full items-center gap-2.5 self-start rounded-full border bg-box py-1.5 pr-1.5 pl-3 text-[13.5px] ${expanded ? 'border-segment' : 'border-sidebar-line'} ${retired ? 'text-muted' : ''}`}
      >
        <svg
          aria-hidden="true"
          width="15"
          height="15"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          strokeWidth="1.8"
          strokeLinecap="round"
          strokeLinejoin="round"
          className="flex-none text-muted"
        >
          <path d="M6 3h12v18l-6-4-6 4z" />
        </svg>
        <span className={`min-w-0 ${expanded ? '' : 'truncate'} ${retired ? 'line-through' : ''}`}>
          {item.body}
        </span>
        <span className="flex-none rounded-full bg-selected px-2.5 py-0.5 text-xs text-muted">
          {item.kind}
        </span>
        <button
          type="button"
          onClick={() => setExpanded(!expanded)}
          aria-expanded={expanded}
          className="flex-none text-xs text-muted underline decoration-outline underline-offset-3"
        >
          Details
        </button>
        {retired ? (
          <span className="flex-none rounded-full bg-selected px-2.5 py-0.5 text-xs text-muted">
            Retired
          </span>
        ) : (
          <button
            type="button"
            disabled={retire.isPending}
            onClick={() => retire.mutate()}
            className="h-7 flex-none rounded-full border border-outline px-3 text-xs"
          >
            Retire
          </button>
        )}
      </div>
      <p className="pl-3 text-[13px] text-muted">
        <span>{item.why}</span>
        {botName && (
          <>
            {' · '}
            <span>{botName}</span>
          </>
        )}
      </p>
      {expanded && (
        <dl className="grid grid-cols-[auto_1fr] gap-x-3 gap-y-0.5 pl-3 text-[13px] text-muted">
          <dt>Kind</dt>
          <dd className="text-ink/80">{item.kind}</dd>
          <dt>For</dt>
          <dd className="text-ink/80">
            {item.scope}
            {item.scope_id ? ` · ${item.scope_id}` : ''}
          </dd>
          {(item.replaces?.length ?? 0) > 0 && (
            <>
              <dt>Replaces</dt>
              <dd className="text-ink/80">{item.replaces.join(', ')}</dd>
            </>
          )}
        </dl>
      )}
      {retire.isError && <p className="text-sm text-danger">{retire.error.message}</p>}
    </li>
  );
}

export function SavedMemory({
  runId,
  botNames,
}: {
  runId: string;
  botNames: Record<string, string>;
}) {
  const { data: items = [] } = useMemory();
  const saved = items.filter((item) => item.source_run_id === runId);
  if (saved.length === 0) return null;

  return (
    <section aria-label="Saved memory">
      <ul className="flex flex-col gap-2">
        {saved.map((item) => (
          <SavedItem key={item.id} item={item} botName={botNames[item.source_bot_id]} />
        ))}
      </ul>
    </section>
  );
}
