import { useId, type ReactNode } from 'react';

export function Field({ label, children }: { label: string; children: (id: string) => ReactNode }) {
  const id = useId();
  return (
    <div className="flex flex-col gap-2">
      <label htmlFor={id} className="text-xs tracking-wide text-muted">
        {label}
      </label>
      {children(id)}
    </div>
  );
}
