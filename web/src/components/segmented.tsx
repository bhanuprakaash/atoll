import type { KeyboardEvent, ReactNode } from 'react';

export type Choice = { value: string; label: string; content?: ReactNode };

export function Segmented({
  label,
  value,
  choices,
  onChange,
  className = 'flex max-w-80 rounded-full bg-box p-1',
  choiceClass = (on: boolean) =>
    `h-[30px] flex-1 rounded-full text-[13px] ${on ? 'bg-segment text-ink' : 'text-muted'}`,
}: {
  label: string;
  value: string;
  choices: Choice[];
  onChange: (value: string) => void;
  className?: string;
  choiceClass?: (on: boolean) => string;
}) {
  const current = Math.max(
    0,
    choices.findIndex((c) => c.value === value),
  );
  function move(e: KeyboardEvent<HTMLDivElement>) {
    const step = { ArrowRight: 1, ArrowDown: 1, ArrowLeft: -1, ArrowUp: -1 }[e.key];
    if (!step) return;
    e.preventDefault();
    const next = choices[(current + step + choices.length) % choices.length];
    onChange(next.value);
    e.currentTarget.querySelector<HTMLElement>(`[data-value="${next.value}"]`)?.focus();
  }
  return (
    <div role="radiogroup" aria-label={label} className={className} onKeyDown={move}>
      {choices.map((c, i) => (
        <button
          key={c.value}
          type="button"
          role="radio"
          aria-checked={i === current}
          aria-label={c.content ? c.label : undefined}
          data-value={c.value}
          tabIndex={i === current ? 0 : -1}
          className={choiceClass(i === current)}
          onClick={() => onChange(c.value)}
        >
          {c.content ?? c.label}
        </button>
      ))}
    </div>
  );
}
