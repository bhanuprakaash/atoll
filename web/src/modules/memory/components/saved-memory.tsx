import { useState } from 'react';
import { quiet } from '@/components/button';
import { Fold } from '@/components/fold';
import { type MemoryItem, useMemory, useRetireMemory } from '@/modules/memory/api/memory';

function Bookmark({ className = '' }: { className?: string }) {
  return (
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
      className={`flex-none text-muted ${className}`}
    >
      <path d="M6 3h12v18l-6-4-6 4z" />
    </svg>
  );
}

function SavedItem({ item, botName }: { item: MemoryItem; botName?: string }) {
  const retire = useRetireMemory(item.id);
  const [expanded, setExpanded] = useState(false);
  return (
    <li className="flex items-start gap-2.5 text-sm">
      <Bookmark className="mt-0.75" />
      <div className="flex min-w-0 flex-1 flex-col gap-0.5">
        <p className="line-clamp-2 text-ink/80">{item.body}</p>
        <p className="line-clamp-2 text-[13px] text-muted">
          <span>{item.kind}</span>
          {item.why && (
            <>
              {' · '}
              <span>{item.why}</span>
            </>
          )}
          {botName && (
            <>
              {' · '}
              <span>{botName}</span>
            </>
          )}
        </p>
        {expanded && (
          <dl className="grid grid-cols-[auto_1fr] gap-x-3 gap-y-0.5 text-[13px] text-muted">
            <dt>Text</dt>
            <dd className="text-ink/80">{item.body}</dd>
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
      </div>
      <button
        type="button"
        onClick={() => setExpanded(!expanded)}
        aria-expanded={expanded}
        className={`flex-none ${quiet}`}
      >
        Details
      </button>
      <button
        type="button"
        disabled={retire.isPending}
        onClick={() => retire.mutate()}
        className={`flex-none ${quiet}`}
      >
        Retire
      </button>
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
  const saved = items.filter((item) => item.source_run_id === runId && item.status !== 'retired');
  if (saved.length === 0) return null;

  const words = saved[0].body.split(/\s+/);
  const summary =
    saved.length === 1
      ? `Saved a memory: ${words.slice(0, 6).join(' ')}${words.length > 6 ? '…' : ''}`
      : `Saved ${saved.length} memories: ${saved.map((item) => `a ${item.kind}`).join(', ')}`;

  return (
    <section aria-label="Saved memory">
      <Fold icon={<Bookmark />} summary={summary}>
        <ul className="flex flex-col gap-2">
          {saved.map((item) => (
            <SavedItem key={item.id} item={item} botName={botNames[item.source_bot_id]} />
          ))}
        </ul>
      </Fold>
    </section>
  );
}
