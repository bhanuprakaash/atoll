import { NavLink } from 'react-router';
import { useBots } from '@/modules/bots/api/bots';

export function BotList() {
  const { data: bots = [] } = useBots();
  return (
    <ul className="flex flex-col gap-1">
      {bots.map((bot) => (
        <li key={bot.id}>
          <NavLink
            to={`/bots/${bot.id}`}
            className={({ isActive }) =>
              `block rounded-control px-3 py-1.5 text-sm ${isActive ? 'bg-line' : ''}`
            }
          >
            {bot.name}
          </NavLink>
        </li>
      ))}
    </ul>
  );
}
