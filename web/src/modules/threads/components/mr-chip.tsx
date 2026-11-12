import { mrRef } from '@/modules/threads/utils/mr-ref';

export function MrChip({
  url,
  state,
  title,
  className = 'h-7 px-2.5',
}: {
  url: string;
  state?: string;
  title?: string;
  className?: string;
}) {
  const ref = mrRef(url);
  return (
    <a
      href={url}
      target="_blank"
      rel="noreferrer"
      title={title || ref || url}
      className={`flex flex-none items-center gap-1.5 rounded-full bg-selected text-ink ${className}`}
    >
      <span
        aria-hidden="true"
        className={`size-1.5 rounded-full ${{ merged: 'bg-whale', closed: 'bg-muted' }[state ?? ''] ?? 'bg-ok'}`}
      />
      {ref ?? '!MR'}
    </a>
  );
}
