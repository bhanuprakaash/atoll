import { Fragment, useEffect, useLayoutEffect, useRef, useState, type ReactNode } from 'react';
import { Link, useNavigate } from 'react-router';
import { BrandLogo } from '@/components/brand-logo';
import { Creature, type CreatureLook } from '@/components/creature';
import { Markdown } from '@/components/markdown';
import { Select } from '@/components/select';
import {
  useMessages,
  useSendMessage,
  type Message,
  type Run,
} from '@/modules/threads/api/messages';
import { useRun } from '@/modules/threads/api/runs';
import {
  useCloseThread,
  useCreateThreadWithMessage,
  useThread,
} from '@/modules/threads/api/threads';
import { Composer } from '@/modules/threads/components/composer';
import { LiveRun } from '@/modules/threads/components/live-run';
import { useThreadEvents } from '@/modules/threads/hooks/use-thread-events';

function duration(run: Run) {
  const seconds =
    Math.max(0, Math.floor((Date.parse(run.finished_at) - Date.parse(run.started_at)) / 1000)) || 0;
  return seconds >= 60 ? `${Math.floor(seconds / 60)}m ${seconds % 60}s` : `${seconds}s`;
}
function RunResult({
  id,
  retry,
  signedOut,
}: {
  id: string;
  retry: () => void;
  signedOut?: (provider: Run['provider']) => ReactNode;
}) {
  const { data: run } = useRun(id);
  if (!run || run.status === 'running' || run.status === 'waiting') return null;
  const tools = run.tools ?? {};
  const blocked = run.report?.blocked?.trim();
  const parts = [
    tools.read ? `read ${tools.read} ${tools.read === 1 ? 'file' : 'files'}` : '',
    tools.edit ? `edited ${tools.edit}` : '',
    tools.command ? `ran ${tools.command} ${tools.command === 1 ? 'command' : 'commands'}` : '',
  ].filter(Boolean);
  return (
    <div className="text-sm text-muted">
      {run.status === 'done' && (parts.length > 0 || duration(run) !== '0s') && (
        <p>
          Worked {duration(run)}
          {parts.length > 0 && ` · ${parts.join(', ')}`}
        </p>
      )}
      {blocked && !['none', 'nothing', 'nothing.'].includes(blocked.toLowerCase()) && (
        <p className="flex items-center gap-2.5 text-ink/80">
          <span aria-hidden="true" className="size-2 flex-none rounded-full bg-blocked" />
          Blocked: {blocked}
        </p>
      )}
      {run.status === 'stopped' && (
        <p className="text-[13px]">Stopped by you after {duration(run)}</p>
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
            onClick={retry}
            className="h-8 rounded-full border border-outline px-4 text-[13.5px] text-ink"
          >
            Retry
          </button>
        </div>
      )}
    </div>
  );
}
function messageTime(createdAt: string) {
  const date = new Date(createdAt);
  const today = new Date();
  const startOfToday = new Date(today.getFullYear(), today.getMonth(), today.getDate());
  const startOfMessage = new Date(date.getFullYear(), date.getMonth(), date.getDate());
  const daysAgo = Math.round((startOfToday.getTime() - startOfMessage.getTime()) / 86_400_000);
  const time = `${date.getHours()}:${String(date.getMinutes()).padStart(2, '0')}`;
  if (daysAgo === 0) return time;
  if (daysAgo === 1) return `Yesterday ${time}`;
  if (daysAgo < 7) return `${date.toLocaleDateString([], { weekday: 'short' })} ${time}`;
  return `${date.getDate()} ${date.toLocaleDateString([], { month: 'short' })} ${time}`;
}

function MessageLine({
  message,
  retry,
  signedOut,
}: {
  message: Message;
  retry: () => void;
  signedOut?: (provider: Run['provider']) => ReactNode;
}) {
  if (message.role === 'user')
    return (
      <div data-testid="message" className="flex max-w-[75%] self-end flex-col items-end gap-1">
        <article aria-label="You">
          <div className="rounded-[22px] bg-accent px-[18px] py-2.5 text-[15px] leading-normal whitespace-pre-wrap text-accent-ink [overflow-wrap:anywhere]">
            {message.text}
          </div>
        </article>
        <time
          className="pr-3 text-xs text-muted"
          dateTime={message.created_at}
          data-testid="message-time"
        >
          {messageTime(message.created_at)}
        </time>
      </div>
    );
  return (
    <div data-testid="message" className="flex max-w-[85%] flex-col gap-2">
      <RunResult id={message.run_id} retry={retry} signedOut={signedOut} />
      <div className="text-[15px] leading-normal">
        <Markdown>{message.text}</Markdown>
      </div>
      <time className="text-xs text-muted" dateTime={message.created_at} data-testid="message-time">
        {messageTime(message.created_at)}
      </time>
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
  sendFirst: (text: string) => void;
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
            (setup.connections?.length ?? 0) > 0 && 'GitLab',
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
  const sendFirst = (text: string) => {
    if (create.isPending) return;
    create.mutate(
      { repoId, text },
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
  const close = useCloseThread(threadId);
  const create = useCreateThreadWithMessage(botId);
  const send = useSendMessage(threadId);
  const { runs, connected, mr, worktreeRemoved, begin } = useThreadEvents(threadId);
  const activeId =
    Object.entries(runs).find(([, run]) => !run.finished)?.[0] ??
    (['running', 'waiting'].includes(thread?.run_status ?? '') ? thread?.run_id : undefined);
  const log = useRef<HTMLDivElement>(null);
  const previousHeight = useRef<number | null>(null);
  const shownThread = useRef('');
  const wasAtLatest = useRef(true);
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
    if (messages.length > 50 && hasNextPage && !isFetching) void fetchNextPage();
  }, [fetchNextPage, hasNextPage, isFetching, messages.length]);
  const answered = new Set(messages.filter((m) => m.role === 'bot').map((m) => m.run_id));
  const lastUser = [...messages].reverse().find((m) => m.role === 'user');
  const retry = () => {
    if (lastUser) send.mutate(lastUser.text, { onSuccess: ({ run }) => begin(run.id) });
  };
  const sendFirst = (text: string) => {
    if (repoId && !thread?.repo_id)
      create.mutate(
        { repoId, text },
        {
          onSuccess: (result) =>
            navigate(`/bots/${botId}/threads/${result.thread?.id ?? result.id}`),
        },
      );
    else send.mutate(text, { onSuccess: ({ run }) => begin(run.id) });
  };
  const mrUrl = mr?.url ?? thread?.mr_url;
  const mrState = mr?.state ?? thread?.mr_state;
  const iid = mrUrl?.match(/(?:merge_requests|merge-requests)\/(\d+)/)?.[1];
  const mrChip = mrUrl && (
    <a
      href={mrUrl}
      target="_blank"
      rel="noreferrer"
      className="flex h-7 items-center gap-1.5 rounded-full bg-selected pr-1 pl-2.5 text-ink"
    >
      !{iid ?? 'MR'} {mr?.title ?? thread?.mr_title}
      <span className="rounded-full bg-segment px-2 py-px text-[11.5px]">
        {{ merged: 'Merged', closed: 'Closed' }[mrState ?? ''] ?? 'Open'}
      </span>
    </a>
  );
  const chip = 'flex h-7 items-center gap-1.5 rounded-full bg-selected px-2.5';
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
            <circle cx="6" cy="5" r="2" />
            <circle cx="6" cy="19" r="2" />
            <circle cx="18" cy="7" r="2" />
            <path d="M6 7v10M18 9c0 5-6 4-12 8" />
          </svg>
          <span className="font-mono">{thread.branch}</span>
        </span>
      )}
      {mrChip}
    </>
  );
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
      {!empty && (
        <div className="flex justify-end px-4 py-2 text-xs text-muted">
          {thread?.closed ? (
            'Closed'
          ) : (
            <button type="button" disabled={close.isPending} onClick={() => close.mutate()}>
              Close thread
            </button>
          )}
        </div>
      )}
      <div
        ref={log}
        role="log"
        aria-label="Messages"
        onScroll={() => {
          const element = log.current;
          if (!element) return;
          const atLatest = element.scrollTop + element.clientHeight >= element.scrollHeight - 1;
          wasAtLatest.current = atLatest;
          setAwayFromLatest(!atLatest);
          loadOlder();
        }}
        className={`flex min-h-0 flex-1 flex-col overflow-y-auto px-7 [mask-image:linear-gradient(to_bottom,transparent,black_1.5rem)] ${empty ? 'justify-center' : ''}`}
      >
        <div className="mx-auto flex w-full max-w-190 flex-col gap-4.5 pt-6 pb-4">
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
                <MessageLine message={m} retry={retry} signedOut={signedOut} />
                {thread?.branch && m.role === 'user' && m.id === firstUserId && (
                  <p className="text-sm text-muted">
                    Working on <span className="font-mono text-ink/80">{thread.branch}</span>
                  </p>
                )}
                {m.role === 'bot' &&
                  messages[index + 1]?.run_id !== m.run_id &&
                  afterRun?.(m.run_id)}
              </Fragment>
            );
          })}
          {Object.entries(runs).map(([id, run]) => (
            <div key={id}>
              <LiveRun id={id} run={run} look={look} />
              {run.finished && !answered.has(id) && (
                <>
                  <RunResult id={id} retry={retry} signedOut={signedOut} />
                  {afterRun?.(id)}
                </>
              )}
            </div>
          ))}
          {thread?.run_id && !activeId && !answered.has(thread.run_id) && !runs[thread.run_id] && (
            <>
              <RunResult id={thread.run_id} retry={retry} signedOut={signedOut} />
              {afterRun?.(thread.run_id)}
            </>
          )}
          {mrChip}
          {lostRepo && (
            <p className="text-sm text-muted">
              {botName} no longer has access to {lostRepo}
            </p>
          )}
          {(worktreeRemoved || thread?.worktree_removed) && (
            <p className="text-sm text-muted">
              {iid && mrState === 'merged' ? `!${iid} merged · ` : ''}worktree cleaned up
            </p>
          )}
          {thread?.closed && (
            <p className="self-center rounded-full border border-line px-4 py-2 text-sm text-muted">
              Closed{' '}
              {thread.closed_at &&
                (new Date(thread.closed_at).toDateString() === new Date().toDateString()
                  ? 'today'
                  : new Date(thread.closed_at).toLocaleDateString())}{' '}
              {thread.closed_at &&
                new Date(thread.closed_at).toLocaleTimeString([], {
                  hour: '2-digit',
                  minute: '2-digit',
                })}{' '}
              {thread.snapshot_ref && ' · unfinished work saved'}
            </p>
          )}
          {!connected && <p className="text-sm text-muted">Reconnecting</p>}
        </div>
      </div>
      {!empty && (
        <div className="mx-auto w-full max-w-204 px-7 pb-4.5">
          {awayFromLatest && (
            <div className="mb-2 flex justify-center">
              <button
                type="button"
                aria-label="Jump to latest"
                data-working={activeId ? 'true' : undefined}
                onClick={scrollToLatest}
                className="flex size-10 items-center justify-center rounded-full border border-outline bg-card shadow-sm"
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
          {thread?.closed ? (
            <div className="flex items-center gap-4 rounded-3xl border border-line bg-card p-4 text-sm">
              <div className="flex-1">
                This thread is closed{' '}
                {thread.branch && (
                  <span className="block font-mono text-xs text-muted">
                    {[
                      thread.branch,
                      iid && `!${iid} ${mrState || 'open'}`,
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
            />
          )}
        </div>
      )}
    </div>
  );
}
