import { Link } from 'react-router';
import type { CreatureLook } from '@/components/creature';
import type { Thread } from '@/modules/threads/api/threads';
import { ApprovalCard } from '@/modules/threads/components/approval-card';
import { HelperCreature } from '@/modules/threads/components/helper-creature';
import { helperState } from '@/modules/threads/utils/helper-state';

const stateText = { needs_you: 'Needs you', done: 'Done', failed: 'Failed', stopped: 'Stopped' };

export function HelpersRegion({
  helpers,
  bots,
}: {
  helpers: Thread[];
  bots: Record<string, { name: string; look: CreatureLook }>;
}) {
  const open = helpers.filter((h) => !h.closed);
  if (open.length === 0) return null;
  return (
    <section aria-label="Helpers" className="mb-2 flex flex-col gap-2 px-4.5 text-sm">
      {open.map((h) => {
        const state = helperState(h);
        const bot = bots[h.bot_id];
        return (
          <div key={h.id} className="flex flex-col gap-2">
            <div className="flex items-center gap-2.5">
              <HelperCreature name={bot?.name ?? 'Helper'} creature={bot?.look} state={state} />
              <span>{bot?.name ?? 'Helper'}</span>
              <span
                className={`min-w-0 flex-1 truncate ${state === 'needs_you' ? 'text-needs-ink' : 'text-muted'}`}
              >
                {state === 'working' ? h.step || 'Working' : stateText[state]}
              </span>
              <Link
                to={`/bots/${h.bot_id}/threads/${h.id}`}
                className="text-muted underline decoration-outline underline-offset-3"
              >
                Open
              </Link>
            </div>
            {h.approval && <ApprovalCard key={h.approval.id} {...h.approval} />}
          </div>
        );
      })}
    </section>
  );
}
