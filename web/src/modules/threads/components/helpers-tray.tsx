import { useEffect, useRef, useState, type KeyboardEvent } from 'react';
import { Link, useLocation, useNavigate } from 'react-router';
import { Creature, type CreatureLook } from '@/components/creature';
import { quiet } from '@/components/button';
import { useStopRun } from '@/modules/threads/api/runs';
import type { Thread } from '@/modules/threads/api/threads';
import { ApprovalCard } from '@/modules/threads/components/approval-card';
import { HelperCreature } from '@/modules/threads/components/helper-creature';
import {
  ciWord,
  elapsed,
  helperState,
  isLiveHelper,
  type HelperState,
} from '@/modules/threads/utils/helper-state';
import { mrRef } from '@/modules/threads/utils/mr-ref';

type Bots = Record<string, { name: string; look: CreatureLook }>;

const ended = ['done', 'failed', 'stopped'];
const reportedWord: Record<string, string> = {
  done: 'Reported',
  failed: 'Failed',
  stopped: 'Stopped',
};
const ciStyle: Record<string, string> = {
  running: 'text-muted',
  passed: 'text-ok',
  failed: 'text-danger',
};
const dot: Record<string, string> = { merged: 'bg-whale', closed: 'bg-muted' };

function useNow(on: boolean) {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    if (!on) return;
    setNow(Date.now());
    const timer = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(timer);
  }, [on]);
  return now;
}

function Pull({ helper }: { helper: Thread }) {
  const ref = mrRef(helper.mr_url);
  if (!ref) return null;
  const word = helper.mr_state === 'opened' ? ciWord(helper) : helper.mr_state;
  return (
    <span className="flex flex-none items-center gap-1.5">
      <a
        href={helper.mr_url}
        target="_blank"
        rel="noreferrer"
        title={helper.mr_title || ref}
        className="flex items-center gap-1.5 text-ink/80 hover:text-ink"
      >
        <span
          aria-hidden="true"
          className={`size-1.5 rounded-full ${dot[helper.mr_state] ?? 'bg-ok'}`}
        />
        {ref}
      </a>{' '}
      {word && <span className={ciStyle[word] ?? 'text-muted'}>{word}</span>}
    </span>
  );
}

function Row({
  helper,
  bot,
  state,
  now,
}: {
  helper: Thread;
  bot?: Bots[string];
  state: HelperState;
  now: number;
}) {
  const navigate = useNavigate();
  const stop = useStopRun();
  const href = `/bots/${helper.bot_id}/threads/${helper.id}`;
  const live = isLiveHelper(helper);
  return (
    <li
      tabIndex={-1}
      onKeyDown={(e) => e.key === 'Enter' && e.target === e.currentTarget && navigate(href)}
      className="group flex items-center gap-3 rounded-xl px-3 py-2 outline-none focus-visible:bg-selected hover:bg-selected"
    >
      <HelperCreature name={bot?.name ?? 'Helper'} creature={bot?.look} state={state} />{' '}
      <span className="flex-none">{bot?.name ?? 'Helper'}</span>{' '}
      <span className="min-w-0 flex-1 truncate text-muted" title={helper.title}>
        {helper.title}
      </span>{' '}
      {live ? (
        <>
          <span className="max-w-48 flex-none truncate text-muted">
            {state === 'needs_you' ? 'Needs you' : helper.step || 'Working'}
          </span>{' '}
          <span className="w-10 flex-none text-right text-muted tabular-nums">
            {elapsed(helper.created_at, now)}
          </span>
        </>
      ) : (
        <span className="flex-none text-muted">{reportedWord[helper.run_status]}</span>
      )}{' '}
      <Pull helper={helper} />{' '}
      <Link to={href} className={quiet}>
        Open
      </Link>{' '}
      {live && (
        <button
          type="button"
          disabled={stop.isPending}
          onClick={() => stop.mutate(helper.run_id)}
          className={`${quiet} opacity-0 group-focus-within:opacity-100 group-hover:opacity-100`}
        >
          Stop
        </button>
      )}
    </li>
  );
}

// HelpersTray is the one line attached to the top of the composer: what the helpers of this boss
// thread are doing, the approval that needs the person, and a popover with one row per helper.
// It sits in the composer's form, which must be positioned.
export function HelpersTray({
  boss,
  helpers,
  bots,
  reports,
  seen,
}: {
  boss: string;
  helpers: Thread[];
  bots: Bots;
  reports: Map<string, string>;
  seen: (helperId: string, reportId: string) => boolean;
}) {
  const [open, setOpen] = useState(false);
  const [lit, setLit] = useState(false);
  const section = useRef<HTMLElement>(null);
  const location = useLocation();
  const show = useRef<HTMLButtonElement>(null);
  const dialog = useRef<HTMLDivElement>(null);
  const rows = helpers.filter((h) => {
    if (h.closed) return false;
    if (isLiveHelper(h)) return true;
    const report = reports.get(h.id);
    return ended.includes(h.run_status) && !(report && seen(h.id, report));
  });
  const shown = rows.length > 0;
  const fromSidebar = shown && (location.state as { helpers?: boolean } | null)?.helpers === true;
  useEffect(() => {
    if (!fromSidebar) return;
    section.current?.scrollIntoView({ block: 'nearest' });
    setLit(true);
    const timer = setTimeout(() => setLit(false), 1500);
    return () => clearTimeout(timer);
  }, [fromSidebar, location.key]);
  const isOpen = open && shown;
  const now = useNow(isOpen);

  useEffect(() => {
    if (!shown) return;
    const onKey = (e: globalThis.KeyboardEvent) => {
      if ((e.ctrlKey || e.metaKey) && e.key === '.') {
        e.preventDefault();
        setOpen((current) => !current);
      } else if (e.key === 'Escape' && isOpen) {
        e.preventDefault();
        setOpen(false);
        show.current?.focus();
      }
    };
    const onDown = (e: PointerEvent) => {
      const target = e.target as Node;
      if (isOpen && !dialog.current?.contains(target) && !show.current?.contains(target))
        setOpen(false);
    };
    window.addEventListener('keydown', onKey);
    document.addEventListener('pointerdown', onDown);
    return () => {
      window.removeEventListener('keydown', onKey);
      document.removeEventListener('pointerdown', onDown);
    };
  }, [shown, isOpen]);
  useEffect(() => {
    if (isOpen) dialog.current?.focus({ preventScroll: true });
  }, [isOpen]);

  if (!shown) return null;
  const working = rows.filter(isLiveHelper).length;
  const reported = rows.length - working;
  const waiting = rows.filter((h) => h.approval);
  const asking = waiting[0];
  const askingBot = asking && bots[asking.bot_id];
  const move = (e: KeyboardEvent) => {
    const step = e.key === 'ArrowDown' ? 1 : e.key === 'ArrowUp' ? -1 : 0;
    if (!step) return;
    e.preventDefault();
    const items = [...dialog.current!.querySelectorAll<HTMLElement>('li')];
    const at = items.indexOf(document.activeElement as HTMLElement);
    const next =
      at < 0 ? (step > 0 ? 0 : items.length - 1) : (at + step + items.length) % items.length;
    items[next]?.focus();
  };
  return (
    <>
      <section
        ref={section}
        aria-label="Helpers"
        className={`${lit ? 'bg-selected' : ''} -mt-1.5 -mr-2.5 -ml-4.5 border-b border-composer-line text-[13px]`}
      >
        <div className="flex h-8 items-center gap-2.5 px-4.5">
          <span className="flex items-center -space-x-1">
            {rows.map((h) => (
              <HelperCreature
                key={h.id}
                name={bots[h.bot_id]?.name ?? 'Helper'}
                creature={bots[h.bot_id]?.look}
                state={helperState(h)}
              />
            ))}
          </span>
          <span className="text-ink">Working with {boss}</span>
          <span className="text-muted">
            {[working && `${working} working`, reported && `${reported} reported`]
              .filter(Boolean)
              .join(' · ')}
          </span>
          <button
            ref={show}
            type="button"
            aria-haspopup="dialog"
            aria-expanded={isOpen}
            onClick={() => setOpen(!isOpen)}
            className={`ml-auto ${quiet}`}
          >
            Show
          </button>
        </div>
        {asking?.approval && (
          <div className="flex items-center gap-3 px-4.5 pb-2">
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
              <span className="flex-none text-muted">{waiting.length - 1} more</span>
            )}
          </div>
        )}
      </section>
      {isOpen && (
        <div
          ref={dialog}
          role="dialog"
          aria-label="Helper tasks"
          tabIndex={-1}
          onKeyDown={move}
          className="absolute inset-x-0 bottom-full z-20 mb-2 max-h-[min(24rem,60vh)] overflow-y-auto rounded-2xl border border-outline bg-popover p-1.5 text-[13px] shadow-[0_12px_32px_rgba(0,0,0,.5)] outline-none"
        >
          <ul>
            {rows.map((h) => (
              <Row key={h.id} helper={h} bot={bots[h.bot_id]} state={helperState(h)} now={now} />
            ))}
          </ul>
        </div>
      )}
    </>
  );
}
