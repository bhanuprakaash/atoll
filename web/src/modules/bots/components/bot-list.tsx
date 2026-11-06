import { NavLink } from 'react-router';
import { Creature } from '@/components/creature';
import { useBots, usePinBot, type Bot } from '@/modules/bots/api/bots';
import { relativeTime } from '@/utils/time';

function PinButton({ bot }: { bot: Bot }) {
  const pin = usePinBot(bot.id);
  const label = `${bot.pinned ? 'Unpin' : 'Pin'} ${bot.name}`;
  return (
    <button
      type="button"
      aria-label={label}
      title={label}
      onClick={(e) => {
        e.preventDefault();
        pin.mutate(!bot.pinned);
      }}
      className={`flex size-5 flex-none items-center justify-center rounded text-muted hover:text-ink focus-visible:opacity-100 group-hover:opacity-100 ${bot.pinned ? '' : 'opacity-0'}`}
    >
      <svg
        aria-hidden="true"
        width="13"
        height="13"
        viewBox="0 0 24 24"
        fill={bot.pinned ? 'currentColor' : 'none'}
        stroke="currentColor"
        strokeWidth="1.8"
        strokeLinecap="round"
        strokeLinejoin="round"
      >
        <path d="M9 4h6l-1 6 3 3H7l3-3-1-6zM12 13v7" />
      </svg>
    </button>
  );
}

export function BotList({ search }: { search: string }) {
  const { data: bots = [] } = useBots();
  const ordered = bots
    .filter((bot) => bot.name.toLowerCase().includes(search.toLowerCase()))
    .sort((a, b) => Number(b.id === 'chief') - Number(a.id === 'chief'));
  return (
    <ul className="flex flex-col gap-0.5">
      {search && ordered.length === 0 && (
        <li className="px-2.5 py-2 text-sm text-muted">No Bots match</li>
      )}
      {ordered.map((bot) => (
        <li key={bot.id}>
          <NavLink
            to={`/bots/${bot.id}`}
            className={({ isActive }) =>
              `group flex items-center gap-3 rounded-card p-2.5 ${isActive ? 'bg-selected' : ''}`
            }
          >
            <Creature look={bot.look} state={bot.state} size={44} />
            <span className="flex min-w-0 flex-1 flex-col gap-0.5">
              <span className="flex justify-between gap-2 text-[15px]">
                <span className="truncate">{bot.name}</span>
                <span className="flex flex-none items-center gap-1.5">
                  <span className="text-xs text-muted">{relativeTime(bot.last_activity)}</span>
                  {bot.id !== 'chief' && <PinButton bot={bot} />}
                </span>
              </span>
              <span className="truncate text-[13px] text-muted">{bot.activity}</span>
            </span>
          </NavLink>
        </li>
      ))}
    </ul>
  );
}
