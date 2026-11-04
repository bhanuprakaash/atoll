import { useId, type ReactNode } from 'react';

export const controlClass =
  'w-full rounded-control border border-line bg-card px-3 py-1.5 text-sm text-ink';

export function Field({ label, children }: { label: string; children: (id: string) => ReactNode }) {
  const id = useId();
  return (
    <div className="flex flex-col gap-1">
      <label htmlFor={id} className="text-sm font-medium">
        {label}
      </label>
      {children(id)}
    </div>
  );
}
