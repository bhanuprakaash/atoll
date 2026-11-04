import type { ButtonHTMLAttributes } from 'react';

export function Button({ className = '', ...props }: ButtonHTMLAttributes<HTMLButtonElement>) {
  return (
    <button
      type="button"
      className={`rounded-control bg-accent px-3 py-1.5 text-sm font-medium text-accent-ink disabled:opacity-50 ${className}`}
      {...props}
    />
  );
}
