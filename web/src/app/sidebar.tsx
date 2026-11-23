import { useEffect, useRef, useState } from 'react';
import { NavLink, useNavigate } from 'react-router';
import { Menu } from '@/components/menu';
import { BotList } from '@/modules/bots/components/bot-list';
import { useSettings } from '@/modules/settings/api/settings';
import { inDesktopApp, useAppUpdate } from '@/lib/app-update';

const WIDTH_DEFAULT = 260;
const WIDTH_MIN = 240;
const WIDTH_MAX = 420;
const WIDTH_STEP = 16;
const WIDTH_KEY = 'atoll.sidebarWidth';
const DRAWER_GUTTER = 48;

function clampWidth(width: number) {
  return Math.min(WIDTH_MAX, Math.max(WIDTH_MIN, Math.round(width)));
}

export function useSidebarWidth() {
  const [width, setWidth] = useState(() => {
    try {
      const stored = Number(localStorage.getItem(WIDTH_KEY));
      return stored ? clampWidth(stored) : WIDTH_DEFAULT;
    } catch {
      return WIDTH_DEFAULT;
    }
  });
  useEffect(() => {
    try {
      localStorage.setItem(WIDTH_KEY, String(width));
    } catch {
      // storage unavailable: the width lasts for this session only
    }
  }, [width]);
  return [width, (next: number) => setWidth(clampWidth(next))] as const;
}

function initials(name: string) {
  const words = name.split(/\s+/).filter(Boolean);
  return words
    .slice(0, 2)
    .map((word) => word[0].toUpperCase())
    .join('');
}

// Sidebar is the one column of Bots. Wide, it sits beside the chat and resizes on its right edge;
// with onWidth unset it floats over the chat.
export function Sidebar({
  now,
  openBotId,
  width,
  onWidth,
}: {
  now: boolean;
  openBotId?: string;
  width: number;
  onWidth?: (width: number) => void;
}) {
  const navigate = useNavigate();
  const resizeStart = useRef({ x: 0, width: 0 });
  const [newMenu, setNewMenu] = useState<{ x: number; y: number } | null>(null);
  const [creatingGroup, setCreatingGroup] = useState<{ botId?: string } | null>(null);
  const { data: settings } = useSettings();
  const userName = settings?.user_name || 'User';
  const updateAvailable = Boolean(useAppUpdate().data?.available);
  return (
    <nav
      aria-label="Bots"
      style={{ width: onWidth ? width : `min(${width}px, calc(100vw - ${DRAWER_GUTTER}px))` }}
      className={`flex flex-none flex-col border-r border-line bg-card pt-4 pb-3 ${onWidth ? 'relative' : 'fixed inset-y-0 left-0 z-40'}`}
    >
      {inDesktopApp && <div data-tauri-drag-region="deep" className="h-8" />}
      <BotList
        now={now}
        openBotId={openBotId}
        creatingGroup={creatingGroup}
        onCreatingGroup={setCreatingGroup}
      />
      <div className="mt-2 flex flex-none items-center gap-1 border-t border-line px-3 pt-3">
        <NavLink
          to="/settings"
          aria-label={updateAvailable ? 'Settings, Update available' : 'Settings'}
          className={({ isActive }) =>
            `relative flex min-w-0 flex-1 items-center gap-2.5 rounded-lg px-1 py-1 ${isActive ? 'bg-selected' : 'hover:bg-field'}`
          }
        >
          <span
            aria-hidden="true"
            className="flex size-7 flex-none items-center justify-center rounded-full bg-composer text-xs text-muted"
          >
            {initials(userName)}
          </span>
          <span aria-hidden="true" className="min-w-0 truncate text-sm">
            {userName}
          </span>
          {updateAvailable && (
            <span aria-hidden="true" className="size-2 flex-none rounded-full bg-ok" />
          )}
        </NavLink>
        <button
          type="button"
          aria-label="New"
          aria-haspopup="menu"
          aria-expanded={newMenu !== null}
          onClick={(event) => {
            const box = event.currentTarget.getBoundingClientRect();
            setNewMenu({ x: box.right + 4, y: box.top });
          }}
          className="flex size-8 flex-none items-center justify-center rounded-control text-muted hover:text-ink"
        >
          <svg
            aria-hidden="true"
            width="18"
            height="18"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            strokeWidth="1.6"
            strokeLinecap="round"
          >
            <path d="M12 4v16M4 12h16" />
          </svg>
        </button>
      </div>
      {newMenu && (
        <Menu
          at={newMenu}
          onClose={() => setNewMenu(null)}
          items={[
            { label: 'New Bot', onSelect: () => navigate('/bots/new') },
            { label: 'New group', onSelect: () => setCreatingGroup({}) },
          ]}
        />
      )}
      {onWidth && (
        <div
          role="separator"
          aria-orientation="vertical"
          aria-label="Resize sidebar"
          aria-valuenow={width}
          aria-valuemin={WIDTH_MIN}
          aria-valuemax={WIDTH_MAX}
          tabIndex={0}
          onPointerDown={(event) => {
            event.currentTarget.setPointerCapture(event.pointerId);
            resizeStart.current = { x: event.clientX, width };
          }}
          onPointerMove={(event) => {
            if (event.currentTarget.hasPointerCapture(event.pointerId))
              onWidth(resizeStart.current.width + event.clientX - resizeStart.current.x);
          }}
          onDoubleClick={() => onWidth(WIDTH_DEFAULT)}
          onKeyDown={(event) => {
            const step = { ArrowRight: WIDTH_STEP, ArrowLeft: -WIDTH_STEP }[event.key];
            if (!step) return;
            event.preventDefault();
            onWidth(width + step);
          }}
          className="group absolute top-3 -right-1 bottom-0 z-20 w-1.5 cursor-col-resize outline-none"
        >
          <span className="absolute inset-y-0 right-[3px] w-px group-hover:bg-muted group-focus-visible:bg-muted group-active:bg-muted" />
        </div>
      )}
    </nav>
  );
}
