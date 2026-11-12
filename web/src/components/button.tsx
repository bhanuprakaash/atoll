import type { ButtonHTMLAttributes } from 'react';

export function Button({ className = '', ...props }: ButtonHTMLAttributes<HTMLButtonElement>) {
  return (
    <button
      type="button"
      className={`rounded-full bg-accent px-3 py-1.5 text-[13.5px] text-accent-ink disabled:opacity-50 ${className}`}
      {...props}
    />
  );
}

// refuse turns a refusing control red on hover and keyboard focus only.
export const refuse =
  'hover:border-danger hover:text-danger focus-visible:border-danger focus-visible:text-danger';

// quiet is the one style of a message's inline actions: Undo, Open, Retire, Details.
export const quiet = 'text-[13px] text-muted hover:text-ink disabled:opacity-50';
