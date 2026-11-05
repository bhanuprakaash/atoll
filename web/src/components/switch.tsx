import type { ButtonHTMLAttributes } from 'react';

export function Switch({
  checked,
  onChange,
  ...props
}: {
  checked: boolean;
  onChange: (checked: boolean) => void;
} & Omit<ButtonHTMLAttributes<HTMLButtonElement>, 'onChange' | 'role' | 'type'>) {
  return (
    <button
      type="button"
      role="switch"
      aria-checked={checked}
      onClick={() => onChange(!checked)}
      className={`relative h-6 w-10 flex-none rounded-full transition-colors focus-visible:outline-2 focus-visible:outline-muted ${checked ? 'bg-accent' : 'bg-segment'}`}
      {...props}
    >
      <span
        aria-hidden="true"
        className={`absolute top-0.5 left-0.5 size-5 rounded-full transition-transform ${checked ? 'translate-x-4 bg-accent-ink' : 'bg-muted'}`}
      />
    </button>
  );
}
