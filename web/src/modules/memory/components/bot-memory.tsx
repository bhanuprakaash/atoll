import { Link } from 'react-router';
import { useMemory, useRetireMemory, type MemoryItem } from '@/modules/memory/api/memory';

function MemoryRow({ item }: { item: MemoryItem }) {
  const retire = useRetireMemory(item.id);
  return (
    <li className="flex items-start gap-2 text-sm">
      <span className="line-clamp-2 min-w-0 flex-1">{item.body}</span>
      {item.status === 'retired' ? (
        <span className="text-muted">Retired</span>
      ) : (
        <button type="button" disabled={retire.isPending} onClick={() => retire.mutate()}>
          Retire
        </button>
      )}
      {retire.isError && <span className="text-danger">{retire.error.message}</span>}
    </li>
  );
}

export function BotMemory({ botId }: { botId: string }) {
  const { data: items = [] } = useMemory();
  const relevant = items.filter(
    (item) => item.source_bot_id === botId || (item.scope === 'bot' && item.scope_id === botId),
  );
  return (
    <section className="border-t border-line pt-4">
      <div className="flex justify-between">
        <h3>Memory</h3>
        <Link to="/settings/memory" className="text-sm text-muted">
          See all
        </Link>
      </div>
      {relevant.length > 0 ? (
        <ul className="mt-3 flex flex-col gap-2">
          {relevant.map((item) => (
            <MemoryRow key={item.id} item={item} />
          ))}
        </ul>
      ) : (
        <p className="mt-2 text-sm text-muted">No memories yet</p>
      )}
    </section>
  );
}
