import { useEffect, useRef, useState, type ReactNode } from 'react';
import { Link } from 'react-router';
import { BrandLogo, type Brand } from '@/components/brand-logo';

type Choice = { id: string; name: string; brand?: Brand; content?: string; path?: string };

const detailOf = (c: Choice) =>
  c.path ?? c.content?.match(/^description:\s*(.*)$/m)?.[1] ?? undefined;

function Icon({ d, size = 16 }: { d: string; size?: number }) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.8"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
    >
      <path d={d} />
    </svg>
  );
}

const icons = {
  skill: 'M4 4h10l6 6v10H4zM14 4v6h6M8 14h8M8 17h5',
  repo: 'M3 7h6l2 2h10v10H3z',
  plus: 'M12 5v14M5 12h14',
  close: 'M6 6l12 12M18 6L6 18',
  search: 'M11 4a7 7 0 1 0 0 14 7 7 0 0 0 0-14zM20 20l-4-4',
  check: 'M5 12l5 5 9-10',
};

function Tile({ choice, singular }: { choice: Choice; singular: string }) {
  return (
    <span className="flex size-[34px] flex-none items-center justify-center rounded-[10px] bg-tile text-ink/80">
      {choice.brand ? (
        <BrandLogo brand={choice.brand} className="size-4" />
      ) : (
        <Icon d={singular === 'skill' ? icons.skill : icons.repo} />
      )}
    </span>
  );
}

function Text({ choice, mono }: { choice: Choice; mono: boolean }) {
  const detail = detailOf(choice);
  return (
    <span className="min-w-0 flex-1">
      <span className={`block truncate ${mono ? 'font-mono text-[13px]' : 'text-sm'}`}>
        {choice.name}
      </span>
      {detail && (
        <span
          className={`block truncate text-[12.5px] text-label ${choice.path ? 'font-mono text-xs' : ''}`}
        >
          {detail}
        </span>
      )}
    </span>
  );
}

export function ChoiceList({
  title,
  singular,
  choices,
  selected,
  onChange,
  empty,
}: {
  title: string;
  singular: string;
  choices: Choice[];
  selected: string[];
  onChange: (ids: string[]) => void;
  empty?: ReactNode;
}) {
  const [open, setOpen] = useState(false);
  const [search, setSearch] = useState('');
  const dialog = useRef<HTMLDivElement>(null);
  const mono = singular !== 'connection' && singular !== 'Bot';
  const close = () => {
    setOpen(false);
    setSearch('');
  };
  useEffect(() => {
    if (!open) return;
    const onKey = (e: KeyboardEvent) => e.key === 'Escape' && close();
    const onDown = (e: MouseEvent) => !dialog.current?.contains(e.target as Node) && close();
    window.addEventListener('keydown', onKey);
    window.addEventListener('mousedown', onDown);
    return () => {
      window.removeEventListener('keydown', onKey);
      window.removeEventListener('mousedown', onDown);
    };
  }, [open]);
  const chosen = choices.filter((c) => selected.includes(c.id));
  const shown = choices.filter((c) => c.name.toLowerCase().includes(search.toLowerCase()));
  const toggle = (id: string) =>
    onChange(selected.includes(id) ? selected.filter((x) => x !== id) : [...selected, id]);
  return (
    <section className="flex flex-col gap-2">
      <h3 className="text-xs tracking-wide text-label">{title}</h3>
      <div className="overflow-hidden rounded-2xl bg-field">
        {chosen.map((c) => (
          <div
            key={c.id}
            className="flex items-center gap-3 border-b border-composer-line py-2.5 pr-2 pl-3"
          >
            <Tile choice={c} singular={singular} />
            <Text choice={c} mono={mono} />
            <button
              type="button"
              aria-label={`Remove ${c.name}`}
              onClick={() => onChange(selected.filter((id) => id !== c.id))}
              className="flex size-[30px] flex-none items-center justify-center rounded-full text-muted hover:bg-tile hover:text-ink"
            >
              <Icon d={icons.close} size={14} />
            </button>
          </div>
        ))}
        <button
          type="button"
          aria-expanded={open}
          onClick={() => (open ? close() : setOpen(true))}
          className="flex w-full items-center gap-3 px-3 py-2.5 text-left text-[13.5px] text-muted hover:text-ink"
        >
          <span className="flex size-[34px] flex-none items-center justify-center rounded-[10px] border border-dashed border-segment">
            <Icon d={icons.plus} />
          </span>
          Add {singular}
        </button>
      </div>
      {open && (
        <div className="pointer-events-none fixed inset-0 z-30 flex items-center justify-center bg-scrim p-4">
          <div
            ref={dialog}
            role="dialog"
            aria-modal="true"
            aria-label={`Add ${singular}`}
            className="pointer-events-auto flex max-h-[min(560px,85vh)] w-full max-w-110 flex-col rounded-[22px] border border-composer-line bg-popover shadow-[0_18px_48px_rgba(0,0,0,.55)]"
          >
            <div className="flex items-center justify-between px-5 pt-4 pb-3">
              <h2 className="text-base">Add {singular}s</h2>
              <button
                type="button"
                aria-label="Close"
                onClick={close}
                className="-mr-2 flex size-9 items-center justify-center rounded-[10px] text-muted hover:bg-tile hover:text-ink"
              >
                <Icon d={icons.close} size={18} />
              </button>
            </div>
            <label className="mx-5 mb-2 flex h-10 items-center gap-2 rounded-xl bg-field px-3 text-label">
              <Icon d={icons.search} size={14} />
              <input
                autoFocus
                aria-label={`Search ${title}`}
                placeholder={`Search ${singular}s`}
                value={search}
                onChange={(e) => setSearch(e.target.value)}
                className="min-w-0 flex-1 bg-transparent text-[13.5px] text-ink outline-none placeholder:text-label"
              />
            </label>
            <div
              role="menu"
              className="flex min-h-0 flex-1 flex-col gap-0.5 overflow-auto px-3 py-1"
            >
              {shown.map((c) => {
                const on = selected.includes(c.id);
                return (
                  <button
                    type="button"
                    role="menuitem"
                    key={c.id}
                    aria-pressed={on}
                    className={`flex w-full items-center gap-3 rounded-xl px-2.5 py-2 text-left ${on ? 'bg-tile' : 'hover:bg-field'}`}
                    onClick={() => toggle(c.id)}
                  >
                    <Tile choice={c} singular={singular} />
                    <Text choice={c} mono={mono} />
                    <span
                      className={`flex size-5 flex-none items-center justify-center rounded-full ${on ? 'bg-accent text-accent-ink' : 'border border-segment'}`}
                    >
                      {on && <Icon d={icons.check} size={12} />}
                    </span>
                  </button>
                );
              })}
              {shown.length === 0 && (
                <p className="px-2.5 py-3 text-[13px] text-label">
                  {search ? `No ${singular}s match` : (empty ?? `No ${singular}s yet`)}
                </p>
              )}
            </div>
            <div className="flex items-center gap-3 border-t border-composer-line px-5 py-3">
              {singular === 'skill' && (
                <Link
                  to="/settings/skills?new=1"
                  className="flex items-center gap-1.5 text-[13.5px] text-muted hover:text-ink"
                >
                  <Icon d={icons.plus} size={14} />
                  Write a new skill
                </Link>
              )}
              <span className="ml-auto text-[12.5px] text-label">{selected.length} added</span>
              <button
                type="button"
                onClick={close}
                className="h-8 rounded-full bg-accent px-4 text-[13px] text-accent-ink"
              >
                Done
              </button>
            </div>
          </div>
        </div>
      )}
    </section>
  );
}
