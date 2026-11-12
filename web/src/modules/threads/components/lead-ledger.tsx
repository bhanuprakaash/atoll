import { useState, type ReactNode } from 'react';
import { Creature, type CreatureLook } from '@/components/creature';
import { MrChip } from '@/modules/threads/components/mr-chip';
import { clockTime } from '@/utils/time';

export type LedgerRow = {
  id: string;
  note?: ReactNode;
  time?: string;
  name?: string;
  look?: CreatureLook;
  line?: string;
  state?: string;
  reportFor?: string;
  mr?: { url: string; state: string; title: string };
  report?: ReactNode;
};

export function LeadLedger({ rows, onToggle }: { rows: LedgerRow[]; onToggle: () => void }) {
  const [open, setOpen] = useState<string[]>([]);
  const toggle = (id: string) => {
    setOpen((ids) => (ids.includes(id) ? ids.filter((i) => i !== id) : [...ids, id]));
    onToggle();
  };
  return (
    <ul aria-label="Updates" className="rounded-2xl border border-line px-3 py-1 text-[13.5px]">
      {rows.map((row) => (
        <li
          key={row.id}
          data-report-for={row.reportFor || undefined}
          data-report-id={row.id}
          className="text-muted"
        >
          {row.note ? (
            <p className="flex h-[30px] items-center">{row.note}</p>
          ) : (
            <>
              <div className="flex h-[30px] items-center gap-2.5">
                <button
                  type="button"
                  aria-expanded={open.includes(row.id)}
                  onClick={() => toggle(row.id)}
                  className="flex h-full min-w-0 flex-1 items-center gap-2.5 text-left hover:text-ink"
                >
                  <time dateTime={row.time} className="w-14 flex-none text-xs">
                    {clockTime(row.time ?? '')}
                  </time>
                  <Creature look={row.look} size={18} />
                  <span className="flex-none text-ink/80">{row.name}</span>
                  <span className="min-w-0 flex-1 truncate">{row.line}</span>
                </button>
                {row.mr && (
                  <MrChip
                    url={row.mr.url}
                    state={row.mr.state}
                    title={row.mr.title}
                    className="h-6 px-2 text-xs"
                  />
                )}
                <span className="flex flex-none items-center gap-1.5 text-xs">
                  {row.state === 'Failed' && (
                    <span aria-hidden="true" className="size-1.5 rounded-full bg-danger" />
                  )}
                  {row.state}
                </span>
              </div>
              {open.includes(row.id) && (
                <div className="flex flex-col items-start gap-2 pt-1 pb-3 text-sm leading-relaxed text-ink">
                  {row.report}
                </div>
              )}
            </>
          )}
        </li>
      ))}
    </ul>
  );
}
