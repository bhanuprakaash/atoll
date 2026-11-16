import { Creature, type CreatureLook } from '@/components/creature';
import type { Thread } from '@/modules/threads/api/threads';
import { ApprovalCard } from '@/modules/threads/components/approval-card';
import { isLiveHelper } from '@/modules/threads/utils/helper-state';

const plural = (n: number, word: string) => `${n} ${word}${n === 1 ? '' : 's'}`;

// HelperNotices is what a boss thread's helpers put above the composer because the next message
// answers it: the first open helper approval, and while a Stop holds reports, how many helpers
// still run and how many reports wait (a part at 0 left out).
export function HelperNotices({
  helpers,
  bots,
  held,
}: {
  helpers: Thread[];
  bots: Record<string, { name: string; look: CreatureLook }>;
  // held is the reports a Stop holds, null when none is held.
  held: number | null;
}) {
  const live = helpers.filter(isLiveHelper);
  const waiting = live.filter((h) => h.approval);
  const asking = waiting[0];
  const askingBot = asking && bots[asking.bot_id];
  const line =
    held === null
      ? ''
      : [
          live.length && `${plural(live.length, 'helper')} still running`,
          held && `${plural(held, 'report')} waiting`,
        ]
          .filter(Boolean)
          .join(' · ');
  return (
    <>
      {line && <p className="px-4.5 text-[13px] text-muted">{line}</p>}
      {asking?.approval && (
        <div className="flex items-center gap-3">
          <div className="min-w-0 flex-1">
            <ApprovalCard
              key={asking.approval.id}
              {...asking.approval}
              by={
                <span className="inline-flex items-center gap-1.5 align-middle text-ink">
                  <Creature look={askingBot?.look} size={16} />
                  {askingBot?.name ?? 'Helper'}
                </span>
              }
            />
          </div>
          {waiting.length > 1 && (
            <span className="flex-none text-[13px] text-muted">{waiting.length - 1} more</span>
          )}
        </div>
      )}
    </>
  );
}
