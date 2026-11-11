import { Fragment, useEffect, useLayoutEffect, useRef, useState, type ReactNode } from 'react';
import { Link, useNavigate } from 'react-router';
import { BrandLogo } from '@/components/brand-logo';
import { quiet } from '@/components/button';
import { Creature, type CreatureLook } from '@/components/creature';
import { Markdown } from '@/components/markdown';
import { ZoomImage } from '@/components/zoom-image';
import { Select } from '@/components/select';
import {
  fetchAttachment,
  useMessages,
  useSendMessage,
  type ImageInput,
  type Message,
  type Run,
} from '@/modules/threads/api/messages';
import { useRun, useStopRun } from '@/modules/threads/api/runs';
import { useCreateThreadWithMessage, useThread } from '@/modules/threads/api/threads';
import { Composer } from '@/modules/threads/components/composer';
import { HelpersTray } from '@/modules/threads/components/helpers-tray';
import { LiveRun } from '@/modules/threads/components/live-run';
import { useSeenReports } from '@/modules/threads/hooks/use-seen-reports';
import { useThreadEvents } from '@/modules/threads/hooks/use-thread-events';
import { mrRef } from '@/modules/threads/utils/mr-ref';
import { clockTime } from '@/utils/time';

const chatText = 'text-base leading-[1.65] tracking-normal';

function duration(run: Run) {
  const seconds =
    Math.max(0, Math.floor((Date.parse(run.finished_at) - Date.parse(run.started_at)) / 1000)) || 0;
  return seconds >= 60 ? `${Math.floor(seconds / 60)}m ${seconds % 60}s` : `${seconds}s`;
}
function RunResult({
  id,
  time,
  retry,
  signedOut,
}: {
  id: string;
  time?: string;
  retry: (runId: string) => void;
  signedOut?: (provider: Run['provider']) => ReactNode;
}) {
  const { data: run } = useRun(id);
  if (!run)
    return (
      time && (
        <p className="text-xs text-muted">
          <time dateTime={time} data-testid="message-time">
            {messageTime(time)}
          </time>
        </p>
      )
    );
  if (run.status === 'running' || run.status === 'waiting') return null;
  const tools = run.tools ?? {};
  const blocked = run.report?.blocked?.trim();
  const parts = [
    tools.read ? `read ${tools.read} ${tools.read === 1 ? 'file' : 'files'}` : '',
    tools.edit ? `edited ${tools.edit}` : '',
    tools.command ? `ran ${tools.command} ${tools.command === 1 ? 'command' : 'commands'}` : '',
  ].filter(Boolean);
  const at = time ?? run.finished_at;
  const footer = [
    run.status === 'done' &&
      (parts.length > 0 || duration(run) !== '0s') &&
      `Worked ${duration(run)}`,
    run.status === 'done' && parts.join(', '),
    run.status === 'stopped' && `Stopped by you after ${duration(run)}`,
  ].filter(Boolean);
  return (
    <div className="flex flex-col gap-6 text-sm text-muted">
      {blocked && !['none', 'nothing', 'nothing.'].includes(blocked.toLowerCase()) && (
        <p className="relative text-ink/80">
          <span
            aria-hidden="true"
            className="absolute top-[0.5lh] -left-4.5 size-2 -translate-y-1/2 rounded-full bg-blocked"
          />
          <span className="line-clamp-2">Blocked: {blocked}</span>
        </p>
      )}
      {run.status === 'failed' && (
        <div className="flex flex-wrap items-center gap-3">
          <svg
            aria-hidden="true"
            width="16"
            height="16"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            strokeWidth="1.8"
            strokeLinecap="round"
            className="flex-none text-danger"
          >
            <circle cx="12" cy="12" r="9" />
            <path d="M12 7.5v5M12 16v.5" />
          </svg>
          {run.error_kind === 'signed_out' && signedOut ? (
            signedOut(run.provider)
          ) : (
            <span className="min-w-60 flex-1 text-ink/80 [overflow-wrap:anywhere]">
              Run failed{run.error ? `: ${run.error}` : '.'}
              {/sign.?in|authenticat|not logged in|login/i.test(run.error) && (
                <>
                  {' '}
                  <Link
                    className="text-ink underline decoration-outline underline-offset-3"
                    to="/settings/providers"
                  >
                    Sign in
                  </Link>
                </>
              )}
            </span>
          )}
          <button
            type="button"
            onClick={() => retry(id)}
            className="h-8 rounded-full border border-outline px-4 text-[13.5px] text-ink"
          >
            Retry
          </button>
        </div>
      )}
      <p className="text-xs">
        <time dateTime={at} data-testid={time ? 'message-time' : undefined}>
          {messageTime(at)}
        </time>
        {footer.map((part) => ` · ${part}`).join('')}
      </p>
    </div>
  );
}
function messageTime(createdAt: string) {
  const date = new Date(createdAt);
  const today = new Date();
  const startOfToday = new Date(today.getFullYear(), today.getMonth(), today.getDate());
  const startOfMessage = new Date(date.getFullYear(), date.getMonth(), date.getDate());
  const daysAgo = Math.round((startOfToday.getTime() - startOfMessage.getTime()) / 86_400_000);
  const time = clockTime(createdAt);
  if (daysAgo === 0) return time;
  if (daysAgo === 1) return `Yesterday ${time}`;
  if (daysAgo < 7) return `${date.toLocaleDateString([], { weekday: 'short' })} ${time}`;
  return `${date.getDate()} ${date.toLocaleDateString([], { month: 'short' })} ${time}`;
}

// helperReport is a helper's message in its boss's thread as the user reads it: the prose, and the
// helper thread it came from. The Done/Tried/Blocked/Next lines stay in the helper thread.
function helperReport(text: string) {
  const head = /^.+ reported back from thread (\S+)\.\n/.exec(text);
  if (head) {
    const rest = text.slice(head[0].length);
    const cut = rest.search(/\n\n(Done|Tried|Blocked|Next): |\n\nMerge \S+ into your branch\.$/);
    return { prose: (cut < 0 ? rest : rest.slice(0, cut)).trim(), threadId: head[1] };
  }
  const tail = /\n\(thread (\S+)\)$/.exec(text);
  if (tail) return { prose: text.slice(0, tail.index), threadId: tail[1] };
  return { prose: text, threadId: '' };
}

function Clamp({
  children,
  className = '',
  toggle = quiet,
}: {
  children: ReactNode;
  className?: string;
  toggle?: string;
}) {
  const box = useRef<HTMLDivElement>(null);
  const [over, setOver] = useState(false);
  const [open, setOpen] = useState(false);
  useLayoutEffect(() => {
    const element = box.current;
    if (element && !open) setOver(element.scrollHeight > element.clientHeight + 1);
  });
  return (
    <>
      <div
        ref={box}
        className={`${className} ${open ? '' : 'max-h-[8lh] overflow-hidden'} ${over && !open ? '[mask-image:linear-gradient(to_bottom,black_65%,transparent)]' : ''}`}
      >
        {children}
      </div>
      {over && (
        <button type="button" onClick={() => setOpen(!open)} className={toggle}>
          {open ? 'Show less' : 'Show more'}
        </button>
      )}
    </>
  );
}

const names = (list: { name: string; href: string }[]) => {
  const byName = new Map<string, { href: string; count: number }>();
  for (const { name, href } of list)
    byName.set(name, { href, count: (byName.get(name)?.count ?? 0) + 1 });
  const items = [...byName].map(([name, { href, count }]) => (
    <Fragment key={name}>
      <Link to={href} className="text-ink/80 hover:text-ink">
        {name}
      </Link>
      {count > 1 && ` ×${count}`}
    </Fragment>
  ));
  return items.map((item, i) => (
    <Fragment key={i}>
      {i > 0 && (i === items.length - 1 ? ' and ' : ', ')}
      {item}
    </Fragment>
  ));
};

type Bots = Record<string, { name: string; look: CreatureLook }>;

function MessageLine({
  message,
  threadId,
  from,
  brief,
  footer,
}: {
  message: Message;
  threadId: string;
  from?: Bots[string];
  brief: boolean;
  footer?: ReactNode;
}) {
  const time = (
    <time className="text-xs text-muted" dateTime={message.created_at} data-testid="message-time">
      {messageTime(message.created_at)}
    </time>
  );
  if (message.kind) {
    return (
      <p data-testid="message" className="text-sm text-muted">
        Reef: {message.text} {time}
      </p>
    );
  }
  if (message.from_bot_id) {
    const report = brief ? { prose: message.text, threadId: '' } : helperReport(message.text);
    const text = (
      <div className={chatText}>
        <Markdown threadId={threadId}>{report.prose}</Markdown>
      </div>
    );
    return (
      <article
        aria-label={from?.name ?? 'Helper'}
        data-testid="message"
        data-report-for={brief ? undefined : report.threadId || undefined}
        data-report-id={message.id}
        className="flex max-w-[85%] flex-col items-start gap-2"
      >
        <span className="flex items-center gap-2 text-sm">
          <Creature look={from?.look} size={24} />
          {from?.name ?? 'Helper'}
        </span>
        {brief ? (
          <Clamp className={`self-stretch ${chatText}`}>
            <Markdown threadId={threadId}>{report.prose}</Markdown>
          </Clamp>
        ) : (
          text
        )}
        {report.threadId && (
          <Link to={`/bots/${message.from_bot_id}/threads/${report.threadId}`} className={quiet}>
            Open thread
          </Link>
        )}
        {time}
      </article>
    );
  }
  if (message.role === 'user')
    return (
      <div data-testid="message" className="flex max-w-[75%] flex-col items-end gap-2 self-end">
        <article aria-label="You" className="flex flex-col items-end gap-1.5">
          {message.attachments.length > 0 && (
            <div className="flex flex-wrap justify-end gap-1.5">
              {message.attachments.map((a) => (
                <ZoomImage
                  key={a.id}
                  src={`/api/attachments/${a.id}`}
                  alt="Attachment"
                  testId="message-attachment"
                  className="max-h-40 max-w-60 rounded-2xl object-cover"
                />
              ))}
            </div>
          )}
          {message.text && (
            <div
              className={`rounded-[22px] bg-accent px-[18px] py-2.5 ${chatText} whitespace-pre-wrap text-accent-ink [overflow-wrap:anywhere]`}
            >
              <Clamp toggle="text-[13px] text-accent-ink/60 hover:text-accent-ink">
                {message.text}
              </Clamp>
            </div>
          )}
        </article>
        <span className="pr-[18px]">{time}</span>
      </div>
    );
  return (
    <div data-testid="message" className="flex max-w-[85%] flex-col gap-6">
      <div className={chatText}>
        <Markdown threadId={threadId}>{message.text}</Markdown>
      </div>
      {footer}
    </div>
  );
}
function EmptyThread({
  botName,
  look,
  setup,
  repos,
  repoId,
  setRepoId,
  threadId,
  disabled,
  context,
  sendFirst,
  onSent,
}: {
  botName: string;
  look?: CreatureLook;
  setup?: { provider: 'claude' | 'codex'; model: string; skills: string[]; connections: string[] };
  repos: { id: string; name: string }[];
  repoId: string;
  setRepoId: (id: string) => void;
  threadId: string;
  disabled: boolean;
  context?: ReactNode;
  sendFirst: (text: string, images?: ImageInput[]) => void;
  onSent: (run: Run) => void;
}) {
  return (
    <div className="mx-auto flex w-full max-w-160 flex-col items-center gap-4">
      <Creature look={look} size={88} />
      <h2 className="text-[28px] tracking-tight">What should {botName} work on?</h2>
      {setup && (
        <div className="flex items-center gap-2 text-sm text-muted">
          <BrandLogo brand={setup.provider} className="size-4" />
          {[
            setup.model || { claude: 'Claude Code', codex: 'Codex' }[setup.provider],
            (setup.skills?.length ?? 0) > 0 &&
              `${setup.skills.length} skill${setup.skills.length === 1 ? '' : 's'}`,
            ...new Set(setup.connections),
          ]
            .filter(Boolean)
            .join(' · ')}
        </div>
      )}
      {repos.length > 0 && (
        <div className="flex items-center gap-2 text-sm text-muted">
          Repo
          <div className="w-56">
            <Select
              ariaLabel="Repo"
              value={repoId}
              onChange={setRepoId}
              groups={[
                {
                  options: [
                    { value: '', label: 'No repo' },
                    ...repos.map((repo) => ({ value: repo.id, label: repo.name })),
                  ],
                },
              ]}
              className="h-9 rounded-xl border border-outline bg-box px-3 text-sm"
            />
          </div>
        </div>
      )}
      <div className="w-full">
        <Composer
          threadId={threadId}
          botName={botName}
          disabled={disabled}
          onSent={onSent}
          context={context}
          onFirstSend={sendFirst}
        />
      </div>
      <div role="group" aria-label="Starters" className="flex flex-wrap justify-center gap-2">
        {['Explore this repo', 'What should we work on next?', 'Help me investigate an issue'].map(
          (text) => (
            <button
              type="button"
              key={text}
              disabled={disabled}
              onClick={() => sendFirst(text)}
              className="rounded-full border border-line px-3 py-2 text-sm"
            >
              {text}
            </button>
          ),
        )}
      </div>
    </div>
  );
}

export function DraftThread({
  botId,
  botName,
  look,
  bot,
  repos,
}: {
  botId: string;
  botName: string;
  look?: CreatureLook;
  bot?: { provider: 'claude' | 'codex'; model: string; skills: string[]; connections: string[] };
  repos: { id: string; name: string }[];
}) {
  const navigate = useNavigate();
  const [repoId, setRepoId] = useState('');
  const create = useCreateThreadWithMessage(botId);
  const sendFirst = (text: string, images?: ImageInput[]) => {
    if (create.isPending) return;
    create.mutate(
      { repoId, text, images },
      {
        onSuccess: (result) =>
          navigate(`/bots/${botId}/threads/${result.thread?.id ?? result.id}`, { replace: true }),
      },
    );
  };
  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div
        role="log"
        aria-label="Messages"
        className="flex min-h-0 flex-1 flex-col justify-center overflow-y-auto p-5"
      >
        <EmptyThread
          botName={botName}
          look={look}
          setup={bot}
          repos={repos}
          repoId={repoId}
          setRepoId={setRepoId}
          threadId=""
          disabled={create.isPending}
          sendFirst={sendFirst}
          onSent={() => {}}
        />
        {create.isError && (
          <p className="text-center text-sm text-danger">{create.error.message}</p>
        )}
      </div>
    </div>
  );
}

export function ThreadView({
  threadId,
  botId,
  botName,
  look,
  bots,
  bot,
  repos,
  repoName,
  lostRepo,
  afterRun,
  signedOut,
}: {
  threadId: string;
  botId: string;
  botName: string;
  look?: CreatureLook;
  bots: Bots;
  bot?: { provider: 'claude' | 'codex'; model: string; skills: string[]; connections: string[] };
  repos: { id: string; name: string }[];
  repoName?: string;
  lostRepo?: string;
  afterRun?: (runId: string) => ReactNode;
  signedOut?: (provider: Run['provider']) => ReactNode;
}) {
  const navigate = useNavigate();
  const [repoId, setRepoId] = useState('');
  const messageQuery = useMessages(threadId);
  const messages = messageQuery.data?.pages.flat() ?? [];
  const { fetchNextPage, hasNextPage, isFetching } = messageQuery;
  const { data: thread } = useThread(threadId);
  const create = useCreateThreadWithMessage(botId);
  const send = useSendMessage(threadId);
  const stop = useStopRun();
  const { runs, connected, mr, worktreeRemoved, begin } = useThreadEvents(threadId);
  const activeId =
    Object.entries(runs).find(([, run]) => !run.finished)?.[0] ??
    (['running', 'waiting'].includes(thread?.run_status ?? '') ? thread?.run_id : undefined);
  const log = useRef<HTMLDivElement>(null);
  const previousHeight = useRef<number | null>(null);
  const shownThread = useRef('');
  const content = useRef<HTMLDivElement>(null);
  const wasAtLatest = useRef(true);
  const lastTop = useRef(0);
  const [awayFromLatest, setAwayFromLatest] = useState(false);
  useLayoutEffect(() => {
    const element = log.current;
    if (!element) return;
    if (shownThread.current !== threadId) {
      shownThread.current = threadId;
      wasAtLatest.current = true;
      setAwayFromLatest(false);
    }
    if (previousHeight.current !== null && element.scrollHeight > previousHeight.current) {
      element.scrollTop += element.scrollHeight - previousHeight.current;
      previousHeight.current = null;
    } else if (wasAtLatest.current && messages.length > 0) {
      element.scrollTop = element.scrollHeight;
    }
    const atLatest = element.scrollTop + element.clientHeight >= element.scrollHeight - 1;
    wasAtLatest.current = atLatest;
    setAwayFromLatest(!atLatest);
  }, [messages, threadId]);
  useEffect(() => {
    const element = log.current;
    const inner = content.current;
    if (!element || !inner) return;
    const observer = new ResizeObserver(() => {
      if (wasAtLatest.current && previousHeight.current === null) {
        element.scrollTop = element.scrollHeight;
      }
    });
    observer.observe(inner);
    return () => observer.disconnect();
  }, []);
  useEffect(() => {
    if (messages.length > 50 && hasNextPage && !isFetching) void fetchNextPage();
  }, [fetchNextPage, hasNextPage, isFetching, messages.length]);
  const answered = new Set(messages.filter((m) => m.role === 'bot').map((m) => m.run_id));
  const retry = async (runId: string) => {
    const message = messages.find((m) => m.role === 'user' && m.run_id === runId);
    if (!message) return;
    const images = await Promise.all(
      message.attachments.map(async (a) => {
        const blob = await fetchAttachment(a.id);
        const url = await new Promise<string>((resolve) => {
          const reader = new FileReader();
          reader.onload = () => resolve(reader.result as string);
          reader.readAsDataURL(blob);
        });
        return { mime: a.mime, data: url.slice(url.indexOf(',') + 1) };
      }),
    );
    send.mutate({ text: message.text, images }, { onSuccess: ({ run }) => begin(run.id) });
  };
  const sendFirst = (text: string, images?: ImageInput[]) => {
    if (repoId && !thread?.repo_id)
      create.mutate(
        { repoId, text, images },
        {
          onSuccess: (result) =>
            navigate(`/bots/${botId}/threads/${result.thread?.id ?? result.id}`),
        },
      );
    else send.mutate({ text, images }, { onSuccess: ({ run }) => begin(run.id) });
  };
  const mrUrl = mr?.url ?? thread?.mr_url;
  const mrState = mr?.state ?? thread?.mr_state;
  const ref = mrRef(mrUrl);
  const chip = 'flex h-7 flex-none items-center gap-1.5 rounded-full bg-selected px-2.5';
  const mrChip = mrUrl && (
    <a
      href={mrUrl}
      target="_blank"
      rel="noreferrer"
      title={(mr?.title ?? thread?.mr_title) || ref || mrUrl}
      className={`${chip} text-ink`}
    >
      <span
        aria-hidden="true"
        className={`size-1.5 rounded-full ${{ merged: 'bg-whale', closed: 'bg-danger' }[mrState ?? ''] ?? 'bg-ok'}`}
      />
      {ref ?? '!MR'}
    </a>
  );
  const context = (
    <>
      <span className={chip}>
        <svg
          aria-hidden="true"
          width="13"
          height="13"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          strokeWidth="1.8"
          strokeLinecap="round"
          strokeLinejoin="round"
        >
          <path d="M3 7a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z" />
        </svg>
        {repoName ?? (thread?.repo_id || 'No repo')}
      </span>
      {thread?.branch && (
        <span className={`${chip} min-w-0 flex-initial`} title={thread.branch}>
          <svg
            aria-hidden="true"
            width="13"
            height="13"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            strokeWidth="1.8"
            strokeLinecap="round"
            strokeLinejoin="round"
          >
            <circle cx="6" cy="5" r="2" />
            <circle cx="6" cy="19" r="2" />
            <circle cx="18" cy="7" r="2" />
            <path d="M6 7v10M18 9c0 5-6 4-12 8" />
          </svg>
          <span className="truncate font-mono">{thread.branch}</span>
        </span>
      )}
      {mrChip}
    </>
  );
  const helpers = thread?.helpers ?? [];
  const handoffs = new Map<string, { name: string; href: string }[]>();
  for (const h of helpers) {
    const anchor = [...messages]
      .reverse()
      .find((m) => Date.parse(m.created_at) <= Date.parse(h.created_at));
    if (anchor)
      handoffs.set(anchor.id, [
        ...(handoffs.get(anchor.id) ?? []),
        { name: bots[h.bot_id]?.name ?? 'Helper', href: `/bots/${h.bot_id}/threads/${h.id}` },
      ]);
  }
  const reports = new Map<string, string>();
  for (const m of messages)
    if (m.from_bot_id && m.from_bot_id !== thread?.parent_bot_id) {
      const helper = helperReport(m.text).threadId;
      if (helper) reports.set(helper, m.id);
    }
  const seen = useSeenReports(log, [...reports.values()].join());
  const boss =
    thread?.parent_thread_id && thread.parent_bot_id && !thread.closed
      ? (bots[thread.parent_bot_id]?.name ?? 'Its boss')
      : '';
  const empty = messages.length === 0 && !activeId;
  const firstUserId = messages.find((m) => m.role === 'user')?.id;
  const loadOlder = () => {
    const element = log.current;
    if (
      !element ||
      element.scrollTop > 0 ||
      !messageQuery.hasPreviousPage ||
      messageQuery.isFetchingPreviousPage
    )
      return;
    previousHeight.current = element.scrollHeight;
    void messageQuery.fetchOlder().then((loaded) => {
      if (!loaded) previousHeight.current = null;
    });
  };
  const scrollToLatest = () => {
    log.current?.scrollTo({ top: log.current.scrollHeight });
    setAwayFromLatest(false);
  };
  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div
        ref={log}
        role="log"
        aria-label="Messages"
        onScroll={() => {
          const element = log.current;
          if (!element) return;
          const atLatest = element.scrollTop + element.clientHeight >= element.scrollHeight - 1;
          if (atLatest) wasAtLatest.current = true;
          else if (element.scrollTop < lastTop.current) wasAtLatest.current = false;
          lastTop.current = element.scrollTop;
          setAwayFromLatest(!wasAtLatest.current);
          loadOlder();
        }}
        className={`flex min-h-0 flex-1 flex-col overflow-y-auto px-7 ${empty ? 'justify-center' : ''}`}
      >
        <div ref={content} className="mx-auto flex w-full max-w-190 flex-col gap-4.5 pt-6 pb-4">
          {empty && (
            <EmptyThread
              botName={botName}
              look={look}
              setup={bot}
              repos={repos}
              repoId={repoId}
              setRepoId={setRepoId}
              threadId={threadId}
              disabled={!connected}
              context={context}
              sendFirst={sendFirst}
              onSent={(run) => begin(run.id)}
            />
          )}
          {messages.map((m, index) => {
            const day = new Date(m.created_at);
            const previous = index > 0 ? new Date(messages[index - 1].created_at) : null;
            const valid = Number.isFinite(day.getTime()) && day.getFullYear() > 1;
            const showDay = valid && (!previous || day.toDateString() !== previous.toDateString());
            return (
              <Fragment key={m.id}>
                {showDay && (
                  <div className="flex items-center gap-3 text-xs text-label">
                    <span className="h-px flex-1 bg-line" />
                    {day.toDateString() === new Date().toDateString()
                      ? 'Today'
                      : day.toLocaleDateString([], {
                          month: 'short',
                          day: 'numeric',
                          year: 'numeric',
                        })}
                    <span className="h-px flex-1 bg-line" />
                  </div>
                )}
                <MessageLine
                  message={m}
                  threadId={threadId}
                  from={bots[m.from_bot_id ?? '']}
                  brief={!!m.from_bot_id && m.from_bot_id === thread?.parent_bot_id}
                  footer={
                    m.role === 'bot' &&
                    messages[index + 1]?.run_id !== m.run_id && (
                      <>
                        {afterRun?.(m.run_id)}
                        <RunResult
                          id={m.run_id}
                          time={m.created_at}
                          retry={retry}
                          signedOut={signedOut}
                        />
                      </>
                    )
                  }
                />
                {handoffs.has(m.id) && (
                  <p className="text-sm text-muted">Handed off to {names(handoffs.get(m.id)!)}</p>
                )}
                {thread?.branch && m.role === 'user' && m.id === firstUserId && (
                  <p className="text-sm text-muted">
                    Working on <span className="font-mono text-ink/80">{thread.branch}</span>
                  </p>
                )}
              </Fragment>
            );
          })}
          {Object.entries(runs).map(([id, run]) => (
            <div key={id} className="flex flex-col gap-6">
              <LiveRun id={id} run={run} look={look} />
              {run.finished && !answered.has(id) && (
                <>
                  {afterRun?.(id)}
                  <RunResult id={id} retry={retry} signedOut={signedOut} />
                </>
              )}
            </div>
          ))}
          {thread?.run_id && !activeId && !answered.has(thread.run_id) && !runs[thread.run_id] && (
            <div className="flex flex-col gap-6">
              {afterRun?.(thread.run_id)}
              <RunResult id={thread.run_id} retry={retry} signedOut={signedOut} />
            </div>
          )}
          {lostRepo && (
            <p className="text-sm text-muted">
              {botName} no longer has access to {lostRepo}
            </p>
          )}
          {(worktreeRemoved || thread?.worktree_removed) && (
            <p className="text-sm text-muted">
              {ref && mrState === 'merged' ? `${ref} merged · ` : ''}worktree cleaned up
            </p>
          )}
          {thread?.closed && (
            <p className="self-center rounded-full border border-line px-4 py-2 text-sm text-muted">
              Closed{' '}
              {thread.closed_at &&
                (new Date(thread.closed_at).toDateString() === new Date().toDateString()
                  ? 'today'
                  : new Date(thread.closed_at).toLocaleDateString())}{' '}
              {thread.closed_at && clockTime(thread.closed_at)}{' '}
              {thread.snapshot_ref && ' · unfinished work saved'}
            </p>
          )}
          {!connected && <p className="text-sm text-muted">Reconnecting</p>}
        </div>
      </div>
      {!empty && (
        <div className="relative mx-auto w-full max-w-204 px-7 pb-4.5">
          {awayFromLatest && (
            <div className="pointer-events-none absolute inset-x-0 bottom-full mb-2 flex justify-center">
              <button
                type="button"
                aria-label="Jump to latest"
                data-working={activeId ? 'true' : undefined}
                onClick={scrollToLatest}
                className="pointer-events-auto flex size-10 items-center justify-center rounded-full border border-outline bg-card shadow-sm"
              >
                {activeId ? (
                  <Creature look={look} state="working" size={28} />
                ) : (
                  <svg aria-hidden="true" width="18" height="18" viewBox="0 0 24 24" fill="none">
                    <path
                      d="m7 10 5 5 5-5"
                      stroke="currentColor"
                      strokeWidth="1.8"
                      strokeLinecap="round"
                      strokeLinejoin="round"
                    />
                  </svg>
                )}
              </button>
            </div>
          )}
          {boss ? (
            <div className="flex items-center gap-4 rounded-3xl border border-line bg-card p-4 text-sm">
              <p className="flex-1">{`${boss} is handling this thread. Talk to ${boss} to change it.`}</p>
              <Link
                to={`/bots/${thread?.parent_bot_id}/threads/${thread?.parent_thread_id}`}
                className="rounded-full border border-outline px-4 py-2"
              >
                {`Open ${boss}'s thread`}
              </Link>
              {activeId && (
                <button
                  type="button"
                  disabled={stop.isPending}
                  onClick={() => stop.mutate(activeId)}
                  className="rounded-full bg-accent px-4 py-2 text-accent-ink"
                >
                  Stop
                </button>
              )}
            </div>
          ) : thread?.closed ? (
            <div className="flex items-center gap-4 rounded-3xl border border-line bg-card p-4 text-sm">
              <div className="flex-1">
                This thread is closed{' '}
                {thread.branch && (
                  <span className="block font-mono text-xs text-muted">
                    {[
                      thread.branch,
                      ref && `${ref} ${mrState || 'open'}`,
                      (worktreeRemoved || thread.worktree_removed) && 'worktree cleaned up',
                    ]
                      .filter(Boolean)
                      .join(' · ')}
                  </span>
                )}
              </div>
              <button
                type="button"
                className="rounded-full bg-accent px-4 py-2 text-accent-ink"
                onClick={() => navigate(`/bots/${botId}/new`)}
              >
                New thread with {botName}
              </button>
            </div>
          ) : (
            <Composer
              threadId={threadId}
              botName={botName}
              runId={activeId}
              disabled={!connected || !!thread?.closed}
              onSent={(run) => begin(run.id)}
              context={context}
              tray={<HelpersTray helpers={helpers} bots={bots} reports={reports} seen={seen} />}
            />
          )}
        </div>
      )}
    </div>
  );
}
