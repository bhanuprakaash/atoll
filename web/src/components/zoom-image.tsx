import { useEffect, useState } from 'react';
import { createPortal } from 'react-dom';

export function ZoomImage({
  src,
  alt,
  className = '',
  testId,
}: {
  src: string;
  alt: string;
  className?: string;
  testId?: string;
}) {
  const [open, setOpen] = useState(false);
  useEffect(() => {
    if (!open) return;
    const onKey = (e: KeyboardEvent) => e.key === 'Escape' && setOpen(false);
    document.addEventListener('keydown', onKey);
    return () => document.removeEventListener('keydown', onKey);
  }, [open]);
  return (
    <>
      <button type="button" onClick={() => setOpen(true)} className="block cursor-zoom-in">
        <img src={src} alt={alt} data-testid={testId} className={className} />
      </button>
      {open &&
        createPortal(
          <div
            role="dialog"
            aria-label="Image"
            onClick={(e) => e.target === e.currentTarget && setOpen(false)}
            className="fixed inset-0 z-50 flex items-center justify-center bg-scrim p-6"
          >
            <img src={src} alt={alt} className="max-h-full max-w-full object-contain" />
          </div>,
          document.body,
        )}
    </>
  );
}
