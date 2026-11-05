import { useEffect, useId, useRef, useState, type KeyboardEvent, type ReactNode } from 'react';
import { TextField } from '@/components/text-field';

export type SelectOption = { value: string; label: string };
export type SelectGroup = {
  label?: string;
  logo?: ReactNode;
  meta?: string;
  options: SelectOption[];
};

export function Select({
  labelledBy,
  ariaLabel,
  value,
  groups,
  onChange,
  placeholder = 'Choose',
  search,
  footer,
  className = 'h-12 rounded-2xl border border-tile bg-box px-4 text-base',
}: {
  labelledBy?: string;
  ariaLabel?: string;
  value: string;
  groups: SelectGroup[];
  onChange: (value: string) => void;
  placeholder?: string;
  search?: string;
  footer?: ReactNode;
  className?: string;
}) {
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState('');
  const [active, setActive] = useState(0);
  const typed = useRef({ text: '', at: 0 });
  const root = useRef<HTMLDivElement>(null);
  const trigger = useRef<HTMLButtonElement>(null);
  const list = useRef<HTMLDivElement>(null);
  const id = useId();

  const q = query.toLowerCase();
  const shown = groups
    .map((g) => ({
      ...g,
      options: g.options.filter((o) => `${g.label ?? ''} ${o.label}`.toLowerCase().includes(q)),
    }))
    .filter((g) => g.options.length > 0);
  const flat = shown.flatMap((g) => g.options);
  const selected = groups.find((g) => g.options.some((o) => o.value === value));
  const selectedOption = selected?.options.find((o) => o.value === value);

  useEffect(() => {
    if (!open) return;
    const close = (e: PointerEvent) => {
      if (!root.current?.contains(e.target as Node)) setOpen(false);
    };
    document.addEventListener('pointerdown', close);
    return () => document.removeEventListener('pointerdown', close);
  }, [open]);

  useEffect(() => {
    if (!open) return;
    list.current?.querySelector('[data-active="true"]')?.scrollIntoView?.({ block: 'nearest' });
  }, [open, active]);

  useEffect(() => {
    if (open && !search) list.current?.focus();
  }, [open, search]);

  function show() {
    setQuery('');
    setActive(
      Math.max(
        0,
        flat.findIndex((o) => o.value === value),
      ),
    );
    setOpen(true);
  }

  function close() {
    setOpen(false);
    trigger.current?.focus();
  }

  function pick(option: SelectOption) {
    onChange(option.value);
    close();
  }

  function typeAhead(key: string) {
    const now = Date.now();
    const t = typed.current;
    t.text = now - t.at < 700 ? t.text + key : key;
    t.at = now;
    const from = t.text.length === 1 ? active + 1 : active;
    const order = [...flat.slice(from), ...flat.slice(0, from)];
    const hit = order.find((o) => o.label.toLowerCase().startsWith(t.text.toLowerCase()));
    if (hit) setActive(flat.indexOf(hit));
  }

  function keys(e: KeyboardEvent<HTMLElement>) {
    const last = flat.length - 1;
    if (e.key === 'ArrowDown') setActive(Math.min(last, active + 1));
    else if (e.key === 'ArrowUp') setActive(Math.max(0, active - 1));
    else if (e.key === 'Home' && !search) setActive(0);
    else if (e.key === 'End' && !search) setActive(last);
    else if ((e.key === 'Enter' || (e.key === ' ' && !search)) && flat[active]) pick(flat[active]);
    else if (e.key === 'Escape') close();
    else if (e.key === 'Tab') setOpen(false);
    else if (!search && e.key.length === 1 && e.key !== ' ') {
      typeAhead(e.key);
      return;
    } else return;
    e.preventDefault();
  }

  let index = -1;
  return (
    <div ref={root} className="relative">
      <button
        ref={trigger}
        type="button"
        role="combobox"
        aria-labelledby={labelledBy}
        aria-label={labelledBy ? undefined : ariaLabel}
        aria-expanded={open}
        aria-haspopup="listbox"
        aria-controls={open ? `${id}-list` : undefined}
        className={`flex w-full items-center gap-2.5 text-left text-ink focus:border-muted focus:outline-none ${className}`}
        onClick={() => (open ? setOpen(false) : show())}
        onKeyDown={(e) => {
          if (!open && (e.key === 'ArrowDown' || e.key === 'ArrowUp')) {
            e.preventDefault();
            show();
          }
        }}
      >
        {selected?.logo}
        <span className={`flex-1 truncate ${selectedOption ? '' : 'text-muted'}`}>
          {selectedOption
            ? `${selected?.label ? `${selected.label} · ` : ''}${selectedOption.label}`
            : placeholder}
        </span>
        <svg
          width="16"
          height="16"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          strokeWidth="1.8"
          strokeLinecap="round"
          strokeLinejoin="round"
          aria-hidden="true"
          className="flex-none text-muted"
        >
          <path d="M6 9l6 6 6-6" />
        </svg>
      </button>
      {open && (
        <div
          className="absolute top-full right-0 left-0 z-30 mt-1.5 flex flex-col gap-0.5 rounded-[18px] border border-outline bg-field p-1.5 shadow-2xl"
          onKeyDown={keys}
        >
          {search && (
            <label className="mb-0.5 flex h-9 items-center gap-2 rounded-xl bg-well px-2.5 text-muted">
              <svg
                width="14"
                height="14"
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
                autoFocus
                role="searchbox"
                aria-label={search}
                placeholder={search}
                value={query}
                onChange={(e) => {
                  setQuery(e.target.value);
                  setActive(0);
                }}
              />
            </label>
          )}
          <div
            ref={list}
            id={`${id}-list`}
            role="listbox"
            tabIndex={-1}
            aria-label={ariaLabel}
            aria-labelledby={ariaLabel ? undefined : labelledBy}
            aria-activedescendant={flat[active] ? `${id}-${active}` : undefined}
            className="flex max-h-64 flex-col gap-0.5 overflow-auto outline-none"
          >
            {shown.map((g, gi) => (
              <div
                key={g.label ?? gi}
                role="group"
                aria-label={g.label}
                className="flex flex-col gap-0.5"
              >
                {g.label && (
                  <div className="flex items-center gap-2.5 px-2.5 pt-2 pb-0.5 text-[12.5px] text-muted">
                    {g.logo}
                    <span className="flex-1">{g.label}</span>
                    {g.meta && <span className="text-[11.5px] text-ok">{g.meta}</span>}
                  </div>
                )}
                {g.options.map((o) => {
                  index += 1;
                  const at = index;
                  return (
                    <div
                      key={o.value}
                      id={`${id}-${at}`}
                      role="option"
                      aria-selected={o.value === value}
                      data-active={at === active}
                      className={`cursor-pointer rounded-xl px-2.5 py-2 text-[13.5px] ${g.label ? 'pl-[46px]' : ''} ${at === active ? 'bg-tile' : ''}`}
                      onMouseMove={() => setActive(at)}
                      onClick={() => pick(o)}
                    >
                      {o.label}
                    </div>
                  );
                })}
              </div>
            ))}
            {flat.length === 0 && <p className="px-2.5 py-2 text-sm text-muted">No matches</p>}
          </div>
          {footer}
        </div>
      )}
    </div>
  );
}
