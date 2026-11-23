import { useBots, type Bot } from '@/modules/bots/api/bots';
import { ThreadSection } from '@/modules/bots/components/thread-tree';
import { nowSections, threadNote, workersByBoss } from '@/modules/bots/utils/thread-tree';
import { relativeTime } from '@/utils/time';

export function NowPage() {
  const { data: bots = [] } = useBots();
  const workers = workersByBoss(bots);
  const owners = new Map<string, { bot: Bot; note: string }>();
  for (const bot of bots)
    for (const t of bot.threads ?? [])
      owners.set(t.id, {
        bot,
        note: threadNote(t, workers.get(t.id)) || relativeTime(t.last_activity),
      });
  const sections = nowSections(
    bots.flatMap((bot) => bot.threads ?? []),
    undefined,
    workers,
  );
  return (
    <section aria-label="Now" className="min-h-0 flex-1 overflow-y-auto px-5 pt-18 pb-10">
      <div className="mx-auto flex max-w-150 flex-col">
        <h1 className="px-2 pb-1 text-base font-normal">Now</h1>
        {sections.map(({ label, threads }) => (
          <ThreadSection key={label} label={label} threads={threads} owners={owners} />
        ))}
        {sections.length === 0 && <p className="py-3 text-sm text-muted">Nothing needs you</p>}
      </div>
    </section>
  );
}
