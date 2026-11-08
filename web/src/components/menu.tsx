import { useEffect, useRef, useState, type KeyboardEvent } from 'react';

export type MenuItem = {
  label: string;
  onSelect?: () => void;
  disabled?: boolean;
  items?: MenuItem[];
};

const width = 220;
const rowHeight = 32;

export function Menu({
  at,
  items,
  onClose,
}: {
  at: { x: number; y: number };
  items: MenuItem[];
  onClose: () => void;
}) {
  const menu = useRef<HTMLDivElement>(null);
  const [sub, setSub] = useState<{ at: { x: number; y: number }; items: MenuItem[] } | null>(null);
  const enabled = () => [...menu.current!.querySelectorAll<HTMLElement>('button:enabled')];
  useEffect(() => {
    enabled()[0]?.focus();
  }, []);
  const onKeyDown = (event: KeyboardEvent) => {
    if (event.key === 'Escape') onClose();
    const step = event.key === 'ArrowDown' ? 1 : event.key === 'ArrowUp' ? -1 : 0;
    if (!step) return;
    event.preventDefault();
    const buttons = enabled();
    const next = buttons.indexOf(document.activeElement as HTMLElement) + step;
    buttons[(next + buttons.length) % buttons.length]?.focus();
  };
  return (
    <>
      <div
        className="fixed inset-0 z-40"
        onClick={onClose}
        onContextMenu={(event) => {
          event.preventDefault();
          onClose();
        }}
      />
      <div
        ref={menu}
        role="menu"
        onKeyDown={onKeyDown}
        style={{
          left: Math.min(at.x, window.innerWidth - width - 8),
          top: Math.min(at.y, window.innerHeight - items.length * rowHeight - 16),
          width,
        }}
        className="fixed z-50 rounded-card border border-outline bg-composer p-1.25 shadow-[0_12px_32px_rgba(0,0,0,.5)]"
      >
        {items.map((item) => (
          <button
            key={item.label}
            type="button"
            role="menuitem"
            disabled={item.disabled}
            onClick={(event) => {
              if (item.items) {
                const box = event.currentTarget.getBoundingClientRect();
                setSub({ at: { x: box.right + 4, y: box.top - 6 }, items: item.items });
                return;
              }
              item.onSelect?.();
              onClose();
            }}
            className="flex h-8 w-full items-center rounded-control px-2.5 text-left text-[13.5px] focus-visible:bg-tile enabled:hover:bg-tile disabled:text-label"
          >
            {item.label}
          </button>
        ))}
      </div>
      {sub && <Menu at={sub.at} items={sub.items} onClose={onClose} />}
    </>
  );
}
