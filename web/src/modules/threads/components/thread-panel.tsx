import { useEffect, useLayoutEffect, useRef, useState, type KeyboardEvent } from 'react';
import { Link, useNavigate } from 'react-router';
import { quiet } from '@/components/button';
import type { CreatureLook } from '@/components/creature';
import { useStopRun } from '@/modules/threads/api/runs';
import type { Thread } from '@/modules/threads/api/threads';
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

const ciStyle: Record<string, string> = {
  running: 'text-muted',
  passed: 'text-ok',
  failed: 'text-danger',
};
const dots: Record<string, string> = { merged: 'bg-whale', closed: 'bg-muted' };
const Dot = ({ state }: { state: string }) => (
  <span
    aria-hidden="true"
    className={`size-1.5 flex-none rounded-full ${dots[state] ?? 'bg-ok'}`}
  />
);

const middle = (path: string) =>
  path.length > 34 ? `${path.slice(0, 14)}…${path.slice(-18)}` : path;

function Copy({ value, what }: { value: string; what: string }) {
  const [copied, setCopied] = useState(false);
  useEffect(() => {
    if (!copied) return;
    const timer = setTimeout(() => setCopied(false), 1500);
    return () => clearTimeout(timer);
  }, [copied]);
  return (
    <button
      type="button"
      aria-label={`Copy ${what}`}
      onClick={() => void navigator.clipboard.writeText(value).then(() => setCopied(true))}
      className={`flex-none ${quiet}`}
    >
      {copied ? 'Copied' : 'Copy'}
    </button>
  );
}

function Line({
  value,
  shown = value,
  what,
  mono,
}: {
  value: string;
  shown?: string;
  what: string;
  mono?: boolean;
}) {
  return (
    <div className="flex items-center gap-2">
      <span title={value} className={`min-w-0 flex-1 truncate ${mono ? 'font-mono' : ''}`}>
        {shown}
      </span>
      <Copy value={value} what={what} />
    </div>
  );
}

function Section({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <section className="border-b border-line px-4 py-3 last:border-b-0">
      <h3 className="mb-1.5 text-xs text-label">{title}</h3>
      {children}
    </section>
  );
}

function Pull({ thread }: { thread: Thread }) {
  const ref = mrRef(thread.mr_url);
  if (!ref) return null;
  const word = thread.mr_state === 'opened' ? ciWord(thread) : thread.mr_state;
  return (
    <span className="flex flex-none items-center gap-1.5">
      <a
        href={thread.mr_url}
        target="_blank"
        rel="noreferrer"
        title={thread.mr_title || ref}
        className="flex items-center gap-1.5 text-ink/80 hover:text-ink"
      >
        <Dot state={thread.mr_state} />
        {ref}
      </a>
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
  const end = live ? now : Date.parse(helper.updated_at);
  return (
    <li
      tabIndex={-1}
      onClick={(e) => !(e.target as Element).closest('a, button') && navigate(href)}
      onKeyDown={(e) => e.key === 'Enter' && e.target === e.currentTarget && navigate(href)}
      className="group flex cursor-pointer gap-3 rounded-xl px-2 py-1.5 outline-none focus-visible:bg-selected hover:bg-selected"
    >
      <span className="pt-0.5">
        <HelperCreature name={bot?.name ?? 'Helper'} creature={bot?.look} state={state} />
      </span>
      <div className="min-w-0 flex-1">
        <div className="flex items-baseline gap-2">
          <Link to={href} className="flex min-w-0 flex-1 items-baseline gap-1.5">
            <span className="flex-none">{bot?.name ?? 'Helper'}</span>
            <span className="truncate text-muted" title={helper.title}>
              {helper.title}
            </span>
          </Link>
          <span className="flex-none text-muted tabular-nums">
            {elapsed(helper.created_at, end)}
          </span>
        </div>
        <div className="flex h-6 items-center gap-2 text-muted">
          <span className="min-w-0 truncate">
            {!live ? 'Done' : state === 'needs_you' ? 'Needs you' : helper.step || 'Working'}
          </span>
          <Pull thread={helper} />
          {live && (
            <button
              type="button"
              disabled={stop.isPending}
              onClick={() => stop.mutate(helper.run_id)}
              className={`ml-auto ${quiet} opacity-0 group-focus-within:opacity-100 group-hover:opacity-100`}
            >
              Stop
            </button>
          )}
        </div>
      </div>
    </li>
  );
}

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

const newest = (a: Thread, b: Thread) => Date.parse(b.created_at) - Date.parse(a.created_at);

// ThreadPanel is the card of the thread's status: its workspace, its pull request and its helpers.
// It sits over the chat and stops above the composer.
export function ThreadPanel({
  thread,
  repoName,
  bots,
}: {
  thread: Thread;
  repoName?: string;
  bots: Bots;
}) {
  const card = useRef<HTMLDivElement>(null);
  const helpers = [...(thread.helpers ?? [])].sort(newest);
  const running = helpers.filter(isLiveHelper);
  const done = helpers.filter((h) => !isLiveHelper(h));
  const now = useNow(running.length > 0);
  const repo = repoName ?? thread.repo_id;

  useEffect(() => card.current?.focus({ preventScroll: true }), []);
  useLayoutEffect(() => {
    const el = card.current;
    const form = document.querySelector('[data-composer]')?.closest('form');
    if (!el || !form?.parentElement) return;
    const fit = () => {
      el.style.maxHeight = `${Math.max(120, form.getBoundingClientRect().top - el.getBoundingClientRect().top - 8)}px`;
    };
    fit();
    const observer = new ResizeObserver(fit);
    observer.observe(form.parentElement);
    window.addEventListener('resize', fit);
    return () => {
      observer.disconnect();
      window.removeEventListener('resize', fit);
    };
  }, []);

  const move = (e: KeyboardEvent) => {
    const step = e.key === 'ArrowDown' ? 1 : e.key === 'ArrowUp' ? -1 : 0;
    if (!step) return;
    e.preventDefault();
    const items = [...card.current!.querySelectorAll<HTMLElement>('li')];
    const at = items.indexOf(document.activeElement as HTMLElement);
    const next =
      at < 0 ? (step > 0 ? 0 : items.length - 1) : (at + step + items.length) % items.length;
    items[next]?.focus();
  };
  const group = (list: Thread[]) => (
    <ul>
      {list.map((h) => (
        <Row key={h.id} helper={h} bot={bots[h.bot_id]} state={helperState(h)} now={now} />
      ))}
    </ul>
  );
  return (
    <div
      ref={card}
      role="dialog"
      aria-label="Thread details"
      data-thread-panel
      tabIndex={-1}
      onKeyDown={move}
      className="absolute top-19 right-4 z-20 w-[min(340px,calc(100%-2rem))] overflow-y-auto rounded-2xl border border-outline bg-popover text-[13px] shadow-[0_12px_32px_rgba(0,0,0,.5)] outline-none"
    >
      <Section title="Workspace">
        <div className="flex flex-col gap-1">
          {repo ? <Line value={repo} what="repo" /> : <span className="text-muted">No repo</span>}
          {thread.worktree && (
            <Line value={thread.worktree} shown={middle(thread.worktree)} what="worktree path" />
          )}
          {thread.branch ? (
            <Line value={thread.branch} what="branch" mono />
          ) : (
            repo && <span className="text-muted">No branch yet</span>
          )}
        </div>
      </Section>
      {mrRef(thread.mr_url) && (
        <Section title="Pull requests">
          <ul>
            <li>
              <a
                href={thread.mr_url}
                target="_blank"
                rel="noreferrer"
                className="flex items-center gap-2 rounded-xl px-2 py-1.5 hover:bg-selected"
              >
                <Dot state={thread.mr_state} />
                <span className="flex-none text-ink">{mrRef(thread.mr_url)}</span>
                <span className="min-w-0 flex-1 truncate text-muted">{thread.mr_title}</span>
                {thread.mr_state === 'opened' && (
                  <span className={ciStyle[ciWord(thread)] ?? 'text-muted'}>{ciWord(thread)}</span>
                )}
              </a>
            </li>
          </ul>
        </Section>
      )}
      {helpers.length > 0 && (
        <Section title="Helpers">
          {group(running)}
          {done.length > 0 && (
            <>
              <h4 className="mt-1.5 mb-0.5 px-2 text-xs text-label">Done</h4>
              {group(done)}
            </>
          )}
        </Section>
      )}
    </div>
  );
}
