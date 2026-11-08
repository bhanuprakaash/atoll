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

// quiet is the one style of a message's inline actions: Undo, Open, Retire, Details.
export const quiet = 'text-[13px] text-muted hover:text-ink disabled:opacity-50';
