export type Metric = { label: string; value: string | number; delta?: string };

const text = (v: unknown): v is string => typeof v === 'string' && v !== '';

export function parseMetrics(source: string): Metric[] | undefined {
  let data: unknown;
  try {
    data = JSON.parse(source);
  } catch {
    return undefined;
  }
  if (!Array.isArray(data) || data.length < 2 || data.length > 4) return undefined;
  const fits = data.every(
    (m) =>
      m &&
      typeof m === 'object' &&
      text(m.label) &&
      (text(m.value) || typeof m.value === 'number') &&
      (m.delta === undefined || text(m.delta)),
  );
  return fits ? data : undefined;
}

export function parseKv(source: string): [string, string][] | undefined {
  const pairs: [string, string][] = [];
  for (const line of source.split('\n')) {
    if (!line.trim()) continue;
    const at = line.indexOf(': ');
    if (at < 1 || !line.slice(0, at).trim()) return undefined;
    pairs.push([line.slice(0, at).trim(), line.slice(at + 2).trim()]);
  }
  return pairs.length > 0 ? pairs : undefined;
}

export function Metrics({ items }: { items: Metric[] }) {
  return (
    <div
      data-component="metrics"
      role="list"
      className="grid grid-cols-[repeat(auto-fit,minmax(8rem,1fr))] gap-2"
    >
      {items.map((m, i) => (
        <div key={i} role="listitem" className="rounded-xl border border-line bg-box px-3.5 py-3">
          <div className="text-xs text-muted">{m.label}</div>
          <div className="mt-1 text-2xl font-medium tracking-tight">{m.value}</div>
          {m.delta && <div className="mt-0.5 text-xs text-muted">{m.delta}</div>}
        </div>
      ))}
    </div>
  );
}

export function KeyValue({ pairs }: { pairs: [string, string][] }) {
  return (
    <dl
      data-component="kv"
      className="grid grid-cols-[max-content_minmax(0,1fr)] gap-x-5 gap-y-1.5 rounded-xl border border-line bg-box px-3.5 py-3"
    >
      {pairs.map(([k, v], i) => (
        <div key={i} className="contents">
          <dt className="text-muted">{k}</dt>
          <dd>{v}</dd>
        </div>
      ))}
    </dl>
  );
}
