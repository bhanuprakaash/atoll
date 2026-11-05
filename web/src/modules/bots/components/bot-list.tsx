import { NavLink } from 'react-router';
import { Creature } from '@/components/creature';
import { useBots } from '@/modules/bots/api/bots';

function relativeTime(value: string) {
  const seconds = Math.floor((Date.now() - Date.parse(value)) / 1000);
  if (!Number.isFinite(seconds)) return '';
  if (seconds < 60) return 'now';
  if (seconds < 3600) return `${Math.floor(seconds / 60)}m`;
  if (seconds < 86400) return `${Math.floor(seconds / 3600)}h`;
  return `${Math.floor(seconds / 86400)}d`;
}

export function BotList() {
  const { data: bots = [] } = useBots();
  const ordered = [...bots].sort((a, b) => Number(b.id === 'chief') - Number(a.id === 'chief'));
  return (
    <ul className="flex flex-col gap-1">
      {ordered.map((bot) => (
        <li key={bot.id}>
          <NavLink
            to={`/bots/${bot.id}`}
            className={({ isActive }) =>
              `flex items-center gap-3 rounded-control px-3 py-2 ${isActive ? 'bg-composer' : ''}`
            }
          >
            <Creature look={bot.look} state={bot.state} size={32} />
            <span className="min-w-0 flex-1">
              <span className="flex justify-between gap-2 text-sm">
                <span className="truncate">{bot.name}</span>
                <span className="text-xs text-muted">{relativeTime(bot.last_activity)}</span>
              </span>
              <span className="block truncate text-xs text-muted">{bot.activity}</span>
            </span>
          </NavLink>
        </li>
      ))}
    </ul>
  );
}
