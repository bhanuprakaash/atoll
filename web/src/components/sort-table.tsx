import {
  Children,
  cloneElement,
  isValidElement,
  type ReactElement,
  type ReactNode,
  useState,
} from 'react';
import type { ExtraProps } from 'react-markdown';

type Node = NonNullable<ExtraProps['node']>;
type El = ReactElement<{
  children?: ReactNode;
  className?: string;
  style?: { textAlign?: 'left' | 'right' | 'center' };
}>;
type Sort = { col: number; dir: 'ascending' | 'descending' } | null;

const _number = /^[+\-−]?\$?\d[\d,]*(\.\d+)?%?$/;
const toNumber = (s: string) => Number(s.replace(/[,$%+]/g, '').replace('−', '-'));

function text(node: Node['children'][number]): string {
  if (node.type === 'text') return node.value;
  return node.type === 'element' ? node.children.map(text).join('') : '';
}
const rows = (node: Node | undefined) =>
  (node?.children ?? []).filter((c) => c.type === 'element' && c.tagName === 'tr') as Node[];
const section = (node: Node, name: string) =>
  node.children.find((c) => c.type === 'element' && c.tagName === name) as Node | undefined;
const cells = (tr?: Node) =>
  (tr?.children ?? []).filter((c) => c.type === 'element').map((c) => text(c).trim());
const els = (node: ReactNode) => Children.toArray(node).filter(isValidElement) as El[];

function Arrow({ dir }: { dir: 'ascending' | 'descending' | 'none' }) {
  return (
    <svg aria-hidden="true" viewBox="0 0 8 12" className="h-3 w-2 flex-none" fill="currentColor">
      <path d="M4 0 7 4.5H1Z" className={dir === 'ascending' ? '' : 'opacity-30'} />
      <path d="M4 12 1 7.5h6Z" className={dir === 'descending' ? '' : 'opacity-30'} />
    </svg>
  );
}

export function SortTable({ node, children }: { node: Node; children?: ReactNode }) {
  const [sort, setSort] = useState<Sort>(null);
  const head = cells(rows(section(node, 'thead'))[0]);
  const body = rows(section(node, 'tbody')).map(cells);
  const [thead, tbody] = els(children);
  const headCells = els(els(thead?.props.children)[0]?.props.children);
  const bodyRows = els(tbody?.props.children);
  const numeric = head.map(
    (_, c) => body.length > 0 && body.every((r) => _number.test(r[c] ?? '')),
  );
  const order = body.map((_, i) => i);
  if (sort) {
    const { col, dir } = sort;
    const key = (i: number) => (numeric[col] ? toNumber(body[i][col]) : (body[i][col] ?? ''));
    order.sort(
      (a, b) => (key(a) < key(b) ? -1 : key(a) > key(b) ? 1 : 0) * (dir === 'ascending' ? 1 : -1),
    );
  }
  const align = (c: number, el?: El) => ({
    textAlign: numeric[c] ? ('right' as const) : (el?.props.style?.textAlign ?? 'left'),
  });
  const click = (col: number) =>
    setSort(
      sort?.col !== col
        ? { col, dir: 'ascending' }
        : sort.dir === 'ascending'
          ? { col, dir: 'descending' }
          : null,
    );
  return (
    <div
      data-component="table"
      className="max-h-96 max-w-full overflow-auto rounded-xl border border-line"
    >
      <table className="w-full border-collapse text-[13px]">
        <thead>
          <tr>
            {headCells.map((th, c) => {
              const dir = sort?.col === c ? sort.dir : 'none';
              return (
                <th
                  key={c}
                  aria-sort={dir}
                  style={align(c, th)}
                  className="sticky top-0 bg-box px-3 py-2 font-medium text-muted"
                >
                  <button
                    type="button"
                    onClick={() => click(c)}
                    className={`inline-flex items-center gap-1.5 hover:text-ink ${numeric[c] ? 'flex-row-reverse' : ''}`}
                  >
                    {th.props.children}
                    <Arrow dir={dir} />
                  </button>
                </th>
              );
            })}
          </tr>
        </thead>
        <tbody>
          {order.map((i) => (
            <tr key={bodyRows[i]?.key ?? i} className="border-t border-line">
              {els(bodyRows[i]?.props.children).map((td, c) =>
                cloneElement(td, { key: c, className: 'px-3 py-2', style: align(c, td) }),
              )}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
