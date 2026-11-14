import { useState, type FocusEvent, type MouseEvent, type ReactNode } from 'react';
import { createPortal } from 'react-dom';

type Anchor = { right: number; top: number; height: number };

export function useTooltip() {
  const [hover, setHover] = useState<Anchor | null>(null);
  const [focus, setFocus] = useState<Anchor | null>(null);
  const rect = (event: MouseEvent | FocusEvent) => event.currentTarget.getBoundingClientRect();
  return [
    hover ?? focus,
    {
      onMouseMove: (event: MouseEvent) => {
        if (!hover) setHover(rect(event));
      },
      onMouseLeave: () => setHover(null),
      onFocus: (event: FocusEvent) => setFocus(rect(event)),
      onBlur: () => setFocus(null),
    },
  ] as const;
}

export function Tooltip({ anchor, children }: { anchor: Anchor; children: ReactNode }) {
  return createPortal(
    <div
      role="tooltip"
      style={{ left: anchor.right + 8, top: anchor.top + anchor.height / 2 }}
      className="pointer-events-none fixed z-50 flex -translate-y-1/2 flex-col rounded-control border border-outline bg-popover px-2.5 py-1.5 text-[13px] whitespace-nowrap shadow-[0_8px_24px_rgba(0,0,0,.5)]"
    >
      {children}
    </div>,
    document.body,
  );
}
