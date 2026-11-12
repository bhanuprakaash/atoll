import { useEffect, useRef, useState, type KeyboardEvent } from 'react';
import { useNavigate } from 'react-router';
import { Creature, type CreatureLook, type CreatureState } from '@/components/creature';
import { TextField } from '@/components/text-field';
import { useBots, type Bot } from '@/modules/bots/api/bots';
import { useGroups, type Group } from '@/modules/bots/api/groups';
import { sidebarOrder } from '@/modules/bots/utils/sidebar-order';
import { mrRef } from '@/modules/threads/utils/mr-ref';

type Row = {
  key: string;
  kind: 'bot' | 'thread';
  to: string;
  look: CreatureLook;
  state: CreatureState;
  title: string;
  bot: string;
  detail: string;
  needsYou: boolean;
  at: number;
  fields: { title: string; other: string };
};

const time = (iso?: string) => (iso ? Date.parse(iso) || 0 : 0);

function allRows(bots: Bot[], groups: Group[]) {
  const { top, inGroup, ungrouped } = sidebarOrder(bots, groups);
  const botsInOrder = [...top, ...groups.flatMap((g) => inGroup(g.id)), ...ungrouped];
  const threads = bots.flatMap((bot) =>
    (bot.threads ?? []).map((t): Row => ({
      key: t.id,
      kind: 'thread',
      to: `/bots/${bot.id}/threads/${t.id}`,
      look: bot.look,
      state: bot.state,
      title: t.title || 'Empty thread',
      bot: bot.name,
      detail: [t.branch, mrRef(t.mr_url)].filter(Boolean).join(' · '),
      needsYou: t.state === 'needs_you',
      at: time(t.last_activity),
      fields: { title: t.title, other: `${bot.name} ${t.branch} ${mrRef(t.mr_url) || ''}` },
    })),
  );
  const botRows = botsInOrder.map((bot): Row => ({
    key: bot.id,
    kind: 'bot',
    to: `/bots/${bot.id}`,
    look: bot.look,
    state: bot.state,
    title: bot.name,
    bot: bot.name,
    detail: '',
    needsYou: bot.state === 'needs_you',
    at: time(bot.last_activity),
    fields: { title: bot.name, other: '' },
  }));
  return { threads, botRows };
}

const recent = (a: Row, b: Row) => Number(b.needsYou) - Number(a.needsYou) || b.at - a.at;

// rank lists what the switcher shows: with no query, the threads that need you, the other threads
// by last activity, then the Bots in the sidebar's order; with one, Bots by name, then threads
// whose title holds every word, then those that needed their branch or MR, then those that needed
// their Bot's name, each group needs you first and then by last activity.
export function rank(bots: Bot[], groups: Group[], query: string) {
  const { threads, botRows } = allRows(bots, groups);
  const words = query.toLowerCase().split(/\s+/).filter(Boolean);
  if (words.length === 0) return [...threads.sort(recent), ...botRows];
  const has = (text: string) => words.every((w) => text.toLowerCase().includes(w));
  const inTitle = (r: Row) => has(r.fields.title);
  const inDetail = (r: Row) => has(`${r.fields.title} ${r.detail}`);
  const inAny = (r: Row) => has(`${r.fields.title} ${r.fields.other}`);
  return [
    ...botRows.filter(inTitle).sort(recent),
    ...threads.filter(inTitle).sort(recent),
    ...threads.filter((r) => !inTitle(r) && inDetail(r)).sort(recent),
    ...threads.filter((r) => !inDetail(r) && inAny(r)).sort(recent),
  ];
}

export function Switcher({ onClose }: { onClose: () => void }) {
  const navigate = useNavigate();
  const { data: bots = [] } = useBots();
  const { data: groups = [] } = useGroups();
  const [query, setQuery] = useState('');
  const [selected, setSelected] = useState(0);
  const [opener] = useState(() => document.activeElement as HTMLElement | null);
  const list = useRef<HTMLUListElement>(null);
  useEffect(
    () => () => {
      if (opener?.isConnected) opener.focus();
    },
    [opener],
  );
  useEffect(() => {
    list.current?.children[selected]?.scrollIntoView({ block: 'nearest' });
  }, [selected]);
  const rows = rank(bots, groups, query);
  const open = (row?: Row) => {
    if (!row) return;
    onClose();
    navigate(row.to);
  };
  const onKeyDown = (event: KeyboardEvent) => {
    const step = { ArrowDown: 1, ArrowUp: -1 }[event.key];
    if (step) {
      event.preventDefault();
      setSelected(Math.min(rows.length - 1, Math.max(0, selected + step)));
    } else if (event.key === 'Enter' && !event.nativeEvent.isComposing) {
      event.preventDefault();
      open(rows[selected]);
    } else if (event.key === 'Escape') {
      event.preventDefault();
      onClose();
    } else if (event.key === 'Tab') event.preventDefault();
  };
  return (
    <>
      <div className="fixed inset-0 z-40" onClick={onClose} />
      <div
        role="dialog"
        aria-label="Go to"
        aria-modal="true"
        className="fixed top-[14vh] left-1/2 z-50 flex max-h-[60vh] w-[min(560px,calc(100vw-32px))] -translate-x-1/2 flex-col overflow-hidden rounded-[22px] border border-composer-line bg-composer shadow-[0_12px_32px_rgba(0,0,0,.5)]"
      >
        <TextField
          size="bare"
          autoFocus
          role="combobox"
          aria-label="Go to"
          aria-autocomplete="list"
          aria-expanded="true"
          aria-controls="go-to-list"
          aria-activedescendant={rows[selected] && `go-to-${rows[selected].key}`}
          placeholder="Go to a thread or Bot"
          value={query}
          onChange={(event) => {
            setQuery(event.target.value);
            setSelected(0);
          }}
          onKeyDown={onKeyDown}
          className="h-13 flex-none border-b border-composer-line px-4.5 text-[17px]"
        />
        {rows.length === 0 ? (
          <p className="px-4.5 py-4 text-sm text-muted">No threads or Bots match</p>
        ) : (
          <ul
            ref={list}
            id="go-to-list"
            role="listbox"
            aria-label="Results"
            className="min-h-0 overflow-y-auto p-1.5"
          >
            {rows.map((row, i) => (
              <li
                key={`${row.kind}-${row.key}`}
                id={`go-to-${row.key}`}
                role="option"
                aria-selected={i === selected}
                data-kind={row.kind}
                onClick={() => open(row)}
                onMouseMove={() => setSelected(i)}
                className={`flex items-center gap-3 rounded-card px-2.5 py-1.5 ${i === selected ? 'bg-selected' : ''}`}
              >
                <Creature look={row.look} state={row.state} size={28} />
                <span className="flex min-w-0 flex-1 flex-col">
                  <span className="flex items-baseline gap-2 text-sm">
                    <span className="min-w-0 truncate">{row.title}</span>
                    {row.kind === 'thread' && (
                      <span className="flex-none text-xs text-muted">{row.bot}</span>
                    )}
                    {row.needsYou && <span className="flex-none text-xs text-ink">Needs you</span>}
                  </span>
                  {row.detail && (
                    <span className="truncate text-[12.5px] text-muted">{row.detail}</span>
                  )}
                </span>
              </li>
            ))}
          </ul>
        )}
      </div>
    </>
  );
}
