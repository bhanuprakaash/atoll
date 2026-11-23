import { useEffect, useLayoutEffect, useRef, useState, type KeyboardEvent } from 'react';
import { Link, useNavigate } from 'react-router';
import type { Appearance } from '@/components/creature';
import { openOnClick, PullIcon, pullNumber } from '@/components/pr-chip';
import { useStopRun } from '@/modules/threads/api/runs';
import type { PullRequest, Thread } from '@/modules/threads/api/threads';
import { HelperCreature } from '@/modules/threads/components/helper-creature';
import {
  ciWord,
  elapsed,
  helperState,
  isLiveHelper,
  type HelperState,
} from '@/modules/threads/utils/helper-state';
import { headPR, mrRef } from '@/modules/threads/utils/mr-ref';

type Bots = Record<string, { name: string; look: Appearance }>;

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

const icons = {
  repo: <path d="M3 7a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z" />,
  worktree: <path d="M4 6h16M4 12h16M4 18h10" />,
  branch: (
    <>
      <circle cx="6" cy="5" r="2" />
      <circle cx="6" cy="19" r="2" />
      <circle cx="18" cy="7" r="2" />
      <path d="M6 7v10M18 9c0 5-6 4-12 8" />
    </>
  ),
};
const Icon = ({ name }: { name: keyof typeof icons }) => (
  <svg
    aria-hidden="true"
    width="15"
    height="15"
    viewBox="0 0 24 24"
    fill="none"
    stroke="currentColor"
    strokeWidth="1.8"
    strokeLinecap="round"
    strokeLinejoin="round"
    className="flex-none text-muted"
  >
    {icons[name]}
  </svg>
);

const rowClass =
  'group flex min-h-8 items-center gap-2.5 rounded-lg px-2.5 py-1.5 hover:bg-selected focus-within:bg-selected';

const middle = (path: string) => {
  const home = path.replace(/^\/(?:Users|home)\/[^/]+/, '~');
  return home.length > 34 ? `${home.slice(0, 14)}…${home.slice(-18)}` : home;
};

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
      className={`flex-none text-xs text-muted opacity-0 group-focus-within:opacity-100 group-hover:opacity-100 hover:text-ink focus-visible:opacity-100`}
    >
      {copied ? 'Copied' : 'Copy'}
    </button>
  );
}

function Line({
  icon,
  value,
  shown = value,
  what,
  mono,
}: {
  icon: keyof typeof icons;
  value: string;
  shown?: string;
  what: string;
  mono?: boolean;
}) {
  return (
    <div className={rowClass}>
      <Icon name={icon} />
      <span
        title={value}
        className={`min-w-0 flex-1 truncate ${mono ? 'font-mono text-[12.5px]' : ''}`}
      >
        {shown}
      </span>
      <Copy value={value} what={what} />
    </div>
  );
}

function Section({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <section className="border-t border-line px-1.5 py-2 first:border-t-0">
      <h3 className="px-2.5 pt-1.5 pb-1 text-xs font-normal text-label">{title}</h3>
      {children}
    </section>
  );
}

// Ref is how a PR is named: PR 7 with the pull request icon on GitHub, !7 on GitLab.
const Ref = ({ url }: { url: string }) => {
  const n = pullNumber(url);
  if (!n) return <>{mrRef(url)}</>;
  return (
    <>
      <PullIcon className="text-muted" />
      PR {n}
    </>
  );
};

function Pull({ pr, onOpen }: { pr?: PullRequest; onOpen?: (url: string) => void }) {
  const ref = mrRef(pr?.url);
  if (!pr || !ref) return null;
  const word = pr.state === 'opened' ? ciWord(pr) : pr.state;
  return (
    <span className="flex flex-none items-center gap-2">
      <a
        href={pr.url}
        target="_blank"
        rel="noreferrer"
        title={pr.title || ref}
        onClick={pullNumber(pr.url) ? openOnClick(pr.url, onOpen) : undefined}
        className="inline-flex items-center gap-1.25 text-ink"
      >
        <Dot state={pr.state} />
        <Ref url={pr.url} />
      </a>{' '}
      {word && <span className={`text-xs ${ciStyle[word] ?? 'text-muted'}`}>{word}</span>}
    </span>
  );
}

function Row({
  helper,
  bot,
  state,
  now,
  onOpenPR,
}: {
  helper: Thread;
  bot?: Bots[string];
  state: HelperState;
  now: number;
  onOpenPR?: (threadId: string, url: string) => void;
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
      className={`${rowClass} cursor-pointer items-start outline-none focus-visible:bg-selected`}
    >
      <span className="pt-px">
        <HelperCreature name={bot?.name ?? 'Helper'} creature={bot?.look} state={state} />
      </span>
      <div className="min-w-0 flex-1">
        <div className="flex items-baseline gap-2">
          <Link to={href} className="flex min-w-0 flex-1 items-baseline gap-1.5">
            <span className="flex-none">{bot?.name ?? 'Helper'}</span>{' '}
            <span className="truncate text-muted" title={helper.title}>
              {helper.title}
            </span>
          </Link>{' '}
          <span className="flex-none text-xs text-muted tabular-nums">
            {elapsed(helper.created_at, end)}
          </span>
        </div>{' '}
        <div className="mt-px flex items-center gap-2 text-[12.5px] text-muted">
          <span
            className={`min-w-0 flex-1 truncate ${state === 'needs_you' && live ? 'text-ink' : ''}`}
          >
            {!live
              ? 'Done'
              : state === 'needs_you'
                ? 'Needs you'
                : [helper.wake, helper.step].filter(Boolean).join(' · ') || 'Working'}
          </span>{' '}
          <Pull pr={headPR(helper.prs)} onOpen={onOpenPR && ((url) => onOpenPR(helper.id, url))} />{' '}
          {live && (
            <button
              type="button"
              disabled={stop.isPending}
              onClick={() => stop.mutate(helper.run_id)}
              className="flex-none rounded-full border border-outline px-2.25 py-px text-xs text-ink opacity-0 group-focus-within:opacity-100 group-hover:opacity-100 disabled:opacity-50"
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

const FOLDED = 3;

function Fold({ hidden, open, onToggle }: { hidden: number; open: boolean; onToggle: () => void }) {
  if (hidden <= 0 && !open) return null;
  return (
    <button
      type="button"
      onClick={onToggle}
      className="px-2.5 py-1 text-xs text-muted hover:text-ink"
    >
      {open ? 'Show less' : `Show ${hidden} more`}
    </button>
  );
}

const newest = (a: Thread, b: Thread) => Date.parse(b.created_at) - Date.parse(a.created_at);

// ThreadPanel is the card of the thread's status: its workspace, its pull request and its helpers.
// It sits over the chat and stops above the composer.
export function ThreadPanel({
  thread,
  repoName,
  bots,
  onOpenPR,
}: {
  thread: Thread;
  repoName?: string;
  bots: Bots;
  onOpenPR?: (threadId: string, url: string) => void;
}) {
  const card = useRef<HTMLDivElement>(null);
  const [allDone, setAllDone] = useState(false);
  const [allPRs, setAllPRs] = useState(false);
  const helpers = [...(thread.helpers ?? [])].sort(newest);
  const newestFirst = [...thread.prs].reverse();
  const openPRs = newestFirst.filter((p) => p.state === 'opened');
  const donePRs = newestFirst.filter((p) => p.state !== 'opened');
  const prs = [...openPRs, ...(allPRs ? donePRs : donePRs.slice(0, FOLDED))];
  const running = helpers.filter(isLiveHelper);
  const done = helpers.filter((h) => !isLiveHelper(h));
  const shownDone = allDone ? done : done.slice(0, FOLDED);
  const now = useNow(running.length > 0);
  const repo = repoName ?? thread.repo_id;

  useEffect(() => card.current?.focus({ preventScroll: true }), []);
  useLayoutEffect(() => {
    const el = card.current;
    const form = document.querySelector('[data-composer]')?.closest('form');
    if (!el || !form?.parentElement) return;
    const fit = () => {
      el.style.maxHeight = `${Math.min(480, Math.max(120, form.getBoundingClientRect().top - el.getBoundingClientRect().top - 8))}px`;
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
    const items = [...card.current!.querySelectorAll<HTMLElement>('li[tabindex]')];
    const at = items.indexOf(document.activeElement as HTMLElement);
    const next =
      at < 0 ? (step > 0 ? 0 : items.length - 1) : (at + step + items.length) % items.length;
    items[next]?.focus();
  };
  const group = (list: Thread[]) => (
    <ul>
      {list.map((h) => (
        <Row
          key={h.id}
          helper={h}
          bot={bots[h.bot_id]}
          state={helperState(h)}
          now={now}
          onOpenPR={onOpenPR}
        />
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
      className="absolute top-28 right-4 z-20 w-[min(340px,calc(100%-2rem))] overflow-y-auto rounded-card border border-outline bg-popover text-[13.5px] shadow-[0_12px_32px_rgba(0,0,0,.45)] outline-none"
    >
      <Section title="Workspace">
        <div>
          {repo ? (
            <Line icon="repo" value={repo} what="repo" />
          ) : (
            <span className="px-2.5 text-muted">No repo</span>
          )}
          {thread.worktree && (
            <Line
              icon="worktree"
              value={thread.worktree}
              shown={middle(thread.worktree)}
              what="worktree path"
              mono
            />
          )}
          {thread.branch ? (
            <Line icon="branch" value={thread.branch} what="branch" mono />
          ) : (
            repo && <span className="px-2.5 text-muted">No branch yet</span>
          )}
        </div>
      </Section>
      {prs.length > 0 && (
        <Section title="Pull requests">
          <ul>
            {prs.map((pr) => (
              <li key={pr.url}>
                <a
                  href={pr.url}
                  target="_blank"
                  rel="noreferrer"
                  onClick={
                    pullNumber(pr.url)
                      ? openOnClick(pr.url, onOpenPR && ((url) => onOpenPR(thread.id, url)))
                      : undefined
                  }
                  className={rowClass}
                >
                  <Dot state={pr.state} />
                  <span className="flex flex-none items-center gap-1.25 text-ink">
                    <Ref url={pr.url} />
                  </span>{' '}
                  <span className="min-w-0 flex-1 truncate text-muted">{pr.title}</span>{' '}
                  {pr.state === 'opened' && (
                    <span className={`text-xs ${ciStyle[ciWord(pr)] ?? 'text-muted'}`}>
                      {ciWord(pr)}
                    </span>
                  )}
                </a>
              </li>
            ))}
          </ul>
          <Fold
            hidden={donePRs.length - FOLDED}
            open={allPRs}
            onToggle={() => setAllPRs(!allPRs)}
          />
        </Section>
      )}
      {helpers.length > 0 && (
        <Section title="Helpers">
          {group(running)}
          {done.length > 0 && (
            <>
              <h4 className="px-2.5 pt-2 pb-0.5 text-[11.5px] font-normal text-label">Done</h4>
              {group(shownDone)}
              <Fold
                hidden={done.length - FOLDED}
                open={allDone}
                onToggle={() => setAllDone(!allDone)}
              />
            </>
          )}
        </Section>
      )}
    </div>
  );
}
