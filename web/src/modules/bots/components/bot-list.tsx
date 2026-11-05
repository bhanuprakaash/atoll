import { NavLink } from 'react-router';
import { Creature } from '@/components/creature';
import { useBots } from '@/modules/bots/api/bots';
import { relativeTime } from '@/utils/time';

export function BotList() {
  const { data: bots = [] } = useBots();
  const ordered = [...bots].sort((a, b) => Number(b.id === 'chief') - Number(a.id === 'chief'));
  return (
    <ul className="flex flex-col gap-0.5">
      {ordered.map((bot) => (
        <li key={bot.id}>
          <NavLink
            to={`/bots/${bot.id}`}
            className={({ isActive }) =>
              `flex items-center gap-3 rounded-card p-2.5 ${isActive ? 'bg-selected' : ''}`
            }
          >
            <Creature look={bot.look} state={bot.state} size={44} />
            <span className="flex min-w-0 flex-1 flex-col gap-0.5">
              <span className="flex justify-between gap-2 text-[15px]">
                <span className="truncate">{bot.name}</span>
                <span className="text-xs text-muted">{relativeTime(bot.last_activity)}</span>
              </span>
              <span className="truncate text-[13px] text-muted">{bot.activity}</span>
            </span>
          </NavLink>
        </li>
      ))}
    </ul>
  );
}
