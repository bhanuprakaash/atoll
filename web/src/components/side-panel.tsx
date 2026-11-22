import { useEffect, useState, type ReactNode, type PointerEvent } from 'react';

const _key = 'side-panel-width';
const _default = 520;
const _min = 360;
const _chat = 480;

// clampWidth keeps the panel within 360px and 60% of the window, and the chat at 480px or more.
function clampWidth(w: number) {
  const max = Math.min(window.innerWidth * 0.6, window.innerWidth - _chat);
  return Math.round(Math.max(_min, Math.min(w, max)));
}

function savedWidth() {
  try {
    const w = Number(localStorage.getItem(_key));
    return w > 0 ? w : _default;
  } catch {
    return _default;
  }
}

// SidePanel docks on the right of the bot page: one panel at a time, resized from its left edge,
// its width shared by every panel and kept. Esc and × close it.
export function SidePanel({
  label,
  header,
  actions,
  footer,
  onClose,
  children,
}: {
  label: string;
  header: ReactNode;
  actions?: ReactNode;
  footer?: ReactNode;
  onClose: () => void;
  children: ReactNode;
}) {
  const [width, setWidth] = useState(savedWidth);
  const [, setWindowWidth] = useState(window.innerWidth);
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key !== 'Escape' || e.defaultPrevented) return;
      e.preventDefault();
      onClose();
    };
    const onResize = () => setWindowWidth(window.innerWidth);
    window.addEventListener('keydown', onKey);
    window.addEventListener('resize', onResize);
    return () => {
      window.removeEventListener('keydown', onKey);
      window.removeEventListener('resize', onResize);
    };
  }, [onClose]);
  const drag = (e: PointerEvent<HTMLDivElement>) => {
    e.preventDefault();
    const handle = e.currentTarget;
    handle.setPointerCapture(e.pointerId);
    const right = handle.parentElement!.getBoundingClientRect().right;
    let next = width;
    const move = (ev: globalThis.PointerEvent) => {
      next = clampWidth(right - ev.clientX);
      setWidth(next);
    };
    const up = () => {
      handle.removeEventListener('pointermove', move);
      handle.removeEventListener('pointerup', up);
      try {
        localStorage.setItem(_key, String(next));
      } catch {
        // the width is kept only for this page
      }
    };
    handle.addEventListener('pointermove', move);
    handle.addEventListener('pointerup', up);
  };
  return (
    <aside
      aria-label={label}
      style={{ width: clampWidth(width) }}
      className="relative flex min-h-0 flex-none flex-col border-l border-line bg-bg"
    >
      <div
        role="separator"
        aria-orientation="vertical"
        aria-label="Resize panel"
        onPointerDown={drag}
        className="absolute inset-y-0 -left-1 z-10 w-2 cursor-col-resize hover:bg-line"
      />
      <div className="flex flex-none items-center gap-2 px-6 pt-5 pb-3 text-[13.5px]">
        {header}
        <span className="flex-1" />
        {actions}
        <button
          type="button"
          aria-label="Close"
          onClick={onClose}
          className="-mr-2 flex size-9 flex-none items-center justify-center rounded-[10px] text-muted hover:bg-tile hover:text-ink"
        >
          <svg
            aria-hidden="true"
            width="16"
            height="16"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            strokeWidth="1.8"
            strokeLinecap="round"
          >
            <path d="M6 6l12 12M18 6L6 18" />
          </svg>
        </button>
      </div>
      <div className="min-h-0 flex-1 overflow-auto">{children}</div>
      {footer}
    </aside>
  );
}
