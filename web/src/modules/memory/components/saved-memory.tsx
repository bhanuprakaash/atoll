import { Button } from '@/components/button';
import { type MemoryItem, useMemory, useRetireMemory } from '@/modules/memory/api/memory';

function SavedItem({ item, botName }: { item: MemoryItem; botName?: string }) {
  const retire = useRetireMemory(item.id);
  return (
    <li className="flex items-start gap-3 rounded-card border border-line bg-card p-3 text-sm">
      <div className="flex flex-1 flex-col gap-0.5">
        <p className="font-medium">{item.body}</p>
        <p className="text-muted">{item.why}</p>
        <p className="text-xs text-muted">{botName}</p>
        {retire.isError && <p className="text-danger">{retire.error.message}</p>}
      </div>
      {item.status === 'retired' ? (
        <span className="text-muted">Retired</span>
      ) : (
        <Button disabled={retire.isPending} onClick={() => retire.mutate()}>
          Retire
        </Button>
      )}
    </li>
  );
}

export function SavedMemory({
  threadId,
  botNames,
}: {
  threadId: string;
  botNames: Record<string, string>;
}) {
  const { data: items = [] } = useMemory();

  const saved = items.filter((item) => item.source_thread_id === threadId);
  if (saved.length === 0) return null;

  return (
    <section aria-label="Saved memory" className="border-t border-line p-4">
      <ul className="flex flex-col gap-2">
        {saved.map((item) => (
          <SavedItem key={item.id} item={item} botName={botNames[item.source_bot_id]} />
        ))}
      </ul>
    </section>
  );
}
