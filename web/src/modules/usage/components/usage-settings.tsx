import { type ReactNode, useState } from 'react';
import { ErrorNotice } from '@/components/error-notice';
import { Segmented } from '@/components/segmented';
import { SectionHead } from '@/components/settings-ui';
import { cost, modelName } from '@/utils/usage';
import { PlanLimits } from '@/modules/usage/components/plan-limits';
import { type Usage, type UsageRange, useUsage } from '@/modules/usage/api/usage';

const ranges = [
  { value: '24h', label: '24h' },
  { value: '7d', label: '7 days' },
  { value: '30d', label: '30 days' },
  { value: 'all', label: 'All' },
];
const daysBack = { '24h': 1, '7d': 6, '30d': 29, all: 0 };
const months = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec'];

const money = (usd: number | null) => (usd === null ? '—' : cost(usd));

function tokens(n: number) {
  if (n >= 999_950) return `${(n / 1e6).toFixed(1)}M`;
  return n >= 1000 ? `${(n / 1e3).toFixed(1)}k` : String(n);
}

const pad = (n: number) => String(n).padStart(2, '0');
const key = (d: Date) => `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`;

// dayList is each local day the bars draw: the range back from today, or from the earliest day
// with runs when that is older.
function dayList(range: UsageRange, found: string[]) {
  const today = new Date();
  const start = new Date(today.getFullYear(), today.getMonth(), today.getDate() - daysBack[range]);
  const [y, m, d] = (found[0] < key(start) ? found[0] : key(start)).split('-').map(Number);
  const list: Date[] = [];
  for (let day = new Date(y, m - 1, d); key(day) <= key(today); day.setDate(day.getDate() + 1))
    list.push(new Date(day));
  return list;
}

function PerDay({ range, days }: { range: UsageRange; days: Usage['days'] }) {
  const byDay = new Map(days.map((d) => [d.day, d.cost_usd]));
  const list = dayList(range, [...byDay.keys()]).map((d) => ({
    label: `${d.getDate()} ${months[d.getMonth()]}`,
    cost: byDay.get(key(d)),
  }));
  const top = Math.max(...list.map((d) => d.cost ?? 0));
  return (
    <section className="flex flex-col gap-2">
      <h3 className="text-[13px] font-normal text-muted">Per day, at API prices</h3>
      <ul aria-label="Per day, at API prices" className="flex h-20 items-end gap-1">
        {list.map(({ label, cost: usd }) => (
          <li
            key={label}
            aria-label={`${label}, ${usd === undefined ? 'no runs' : usd === null ? 'no price' : cost(usd)}`}
            className="flex h-full min-w-0 flex-1 items-end rounded-sm bg-box"
          >
            <span
              aria-hidden="true"
              className="w-full rounded-sm bg-ink/70"
              style={{ height: usd ? `${Math.max(4, (usd / top) * 100)}%` : 0 }}
            />
          </li>
        ))}
      </ul>
      <p aria-hidden="true" className="flex justify-between text-xs text-muted">
        <span>{list[0].label}</span>
        <span>{list[list.length - 1].label}</span>
      </p>
    </section>
  );
}

const cell = 'whitespace-nowrap px-3 py-2';
const num = `${cell} text-right tabular-nums`;
const rowHead = `${cell} text-left font-normal`;
const columnHead =
  'sticky top-0 bg-box px-3 py-2 text-right font-medium text-muted first:text-left';

function Table({
  label,
  columns,
  children,
}: {
  label: string;
  columns: string[];
  children: ReactNode;
}) {
  return (
    <div className="max-h-72 max-w-full overflow-auto rounded-xl border border-line">
      <table aria-label={label} className="w-full border-collapse text-[13px]">
        <thead>
          <tr>
            {columns.map((c) => (
              <th key={c} scope="col" className={columnHead}>
                {c}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>{children}</tbody>
      </table>
    </div>
  );
}

function Used({
  range,
  usage,
  colours,
}: {
  range: UsageRange;
  usage: Usage;
  colours: Record<string, string>;
}) {
  const { total, days, bots, models } = usage;
  return (
    <>
      <div role="group" aria-label="Totals" className="flex flex-wrap gap-x-10 gap-y-4">
        {[
          [money(total.cost_usd), 'at API prices · Claude only'],
          [tokens(total.tokens), 'tokens · all providers'],
          [String(total.runs), 'runs'],
        ].map(([value, note]) => (
          <div key={note}>
            <div className="text-[28px] leading-tight tracking-tight tabular-nums">{value}</div>
            <div className="text-[12.5px] text-muted">{note}</div>
          </div>
        ))}
      </div>
      <PerDay range={range} days={days} />
      <section className="flex flex-col gap-2">
        <div className="flex items-baseline justify-between text-[13px] text-muted">
          <h3 className="font-normal">By Bot · its own runs only</h3>
          <span>{bots.length === 1 ? '1 Bot' : `${bots.length} Bots`}</span>
        </div>
        <Table label="By Bot" columns={['Bot', 'Runs', 'Tokens', 'Cost']}>
          {bots.map((b) => (
            <tr key={b.id} className="border-t border-line">
              <th scope="row" className={rowHead}>
                <span className="flex items-center gap-2">
                  <span
                    aria-hidden="true"
                    className="size-2.5 flex-none rounded-full bg-muted"
                    style={
                      colours[b.id]
                        ? { background: `var(--color-bot-${colours[b.id]})` }
                        : undefined
                    }
                  />
                  {b.name}
                </span>
              </th>
              <td className={num}>{b.runs}</td>
              <td className={num}>{tokens(b.tokens)}</td>
              <td className={num}>{money(b.cost_usd)}</td>
            </tr>
          ))}
        </Table>
      </section>
      <section className="flex flex-col gap-2">
        <h3 className="text-[13px] font-normal text-muted">By model</h3>
        <Table label="By model" columns={['Model', 'Tokens', 'Cost']}>
          {models.map((m) => (
            <tr key={m.model} className="border-t border-line">
              <th scope="row" className={rowHead}>
                {modelName(m.model)}
              </th>
              <td className={num}>{tokens(m.tokens)}</td>
              <td className={num}>{money(m.cost_usd)}</td>
            </tr>
          ))}
        </Table>
      </section>
      <p className="text-xs text-muted">
        Cost is what the tokens would cost at API prices; your plan is not billed per token. Codex
        has no prices, so it shows tokens only.
      </p>
    </>
  );
}

export function UsageSettings({ colours }: { colours: Record<string, string> }) {
  const [range, setRange] = useState<UsageRange>('7d');
  const { data, error, refetch } = useUsage(range);
  return (
    <>
      <SectionHead title="Usage" lead="What your Bots used, and how much of each plan is left." />
      <PlanLimits />
      <div className="flex flex-wrap items-center justify-between gap-3">
        <h3 className="text-[15px] font-normal">What Bots used</h3>
        <Segmented
          label="Range"
          value={range}
          choices={ranges}
          onChange={(value) => setRange(value as UsageRange)}
          className="flex w-72 max-w-full rounded-full bg-box p-1"
        />
      </div>
      {error && <ErrorNotice error={error} retry={() => refetch()} />}
      {data &&
        (data.total.runs === 0 ? (
          <p className="text-[13.5px] text-muted">No runs in this range.</p>
        ) : (
          <Used range={range} usage={data} colours={colours} />
        ))}
    </>
  );
}
