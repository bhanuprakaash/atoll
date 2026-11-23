import { useEffect } from 'react';

export function ReplacePin({
  pins,
  onPick,
  onClose,
}: {
  pins: { id: string; title: string }[];
  onPick: (id: string) => void;
  onClose: () => void;
}) {
  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key !== 'Escape') return;
      event.preventDefault();
      onClose();
    };
    window.addEventListener('keydown', onKeyDown, true);
    return () => window.removeEventListener('keydown', onKeyDown, true);
  }, [onClose]);
  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-scrim p-4"
      onMouseDown={(event) => event.target === event.currentTarget && onClose()}
    >
      <section
        role="dialog"
        aria-modal="true"
        aria-label="Replace which pin?"
        className="w-full max-w-100 rounded-[22px] border border-composer-line bg-popover p-5 shadow-[0_18px_48px_rgba(0,0,0,.55)]"
      >
        <h2 className="mb-1 text-base">Replace which pin?</h2>
        <p className="mb-3 text-[13px] text-muted">
          Three threads can stay pinned. Pick the one to unpin.
        </p>
        <div className="flex flex-col gap-1">
          {pins.map((t) => (
            <button
              key={t.id}
              type="button"
              onClick={() => onPick(t.id)}
              className="h-9 truncate rounded-control bg-tile px-3 text-left text-[13.5px]"
            >
              {t.title || 'Empty thread'}
            </button>
          ))}
        </div>
      </section>
    </div>
  );
}
