import { Fragment, useState, type ReactNode } from 'react';
import { Link, useNavigate } from 'react-router';
import { BrandLogo } from '@/components/brand-logo';
import { Creature, type CreatureLook } from '@/components/creature';
import { Markdown } from '@/components/markdown';
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
function RunResult({ id, retry }: { id: string; retry: () => void }) {
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
        <p>Blocked: {blocked}</p>
      )}
      {run.status === 'stopped' && <p>Stopped by you after {duration(run)}</p>}
      {run.status === 'failed' && (
        <p>
          Run failed{run.error ? `: ${run.error}` : '.'}{' '}
          {/sign.?in|authenticat/i.test(run.error) && (
            <Link className="underline" to="/settings/providers">
              Sign in
            </Link>
          )}{' '}
          <button type="button" className="underline" onClick={retry}>
            Retry
          </button>
        </p>
      )}
    </div>
  );
}
function MessageLine({ message, retry }: { message: Message; retry: () => void }) {
  if (message.role === 'user')
    return (
      <article
        aria-label="You"
        className="max-w-prose self-end whitespace-pre-wrap rounded-3xl bg-accent px-[18px] py-2.5 text-sm text-accent-ink"
      >
        {message.text}
      </article>
    );
  return (
    <div className="flex max-w-prose flex-col gap-2">
      <RunResult id={message.run_id} retry={retry} />
      <Markdown>{message.text}</Markdown>
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
    <div className="mx-auto flex w-full max-w-xl flex-col items-center gap-4">
      <Creature look={look} size={88} />
      <h2 className="text-[28px] tracking-tight">What should {botName} work on?</h2>
      {setup && (
        <div className="flex items-center gap-2 text-sm text-muted">
          <BrandLogo brand={setup.provider} className="size-4" />
          {setup.model} · {setup.skills?.length ?? 0} skills
          {(setup.connections?.length ?? 0) > 0 && ' · GitLab'}
        </div>
      )}
      <label className="text-sm text-muted">
        Repo{' '}
        <select
          aria-label="Repo"
          value={repoId}
          onChange={(e) => setRepoId(e.target.value)}
          className="rounded-card border border-line bg-card p-2"
        >
          <option value="">No repo</option>
          {repos.map((repo) => (
            <option key={repo.id} value={repo.id}>
              {repo.name}
            </option>
          ))}
        </select>
      </label>
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
  memory,
}: {
  threadId: string;
  botId: string;
  botName: string;
  look?: CreatureLook;
  bot?: { provider: 'claude' | 'codex'; model: string; skills: string[]; connections: string[] };
  repos: { id: string; name: string }[];
  repoName?: string;
  memory?: ReactNode;
}) {
  const navigate = useNavigate();
  const [repoId, setRepoId] = useState('');
  const { data: messages = [] } = useMessages(threadId);
  const { data: thread } = useThread(threadId);
  const close = useCloseThread(threadId);
  const create = useCreateThreadWithMessage(botId);
  const send = useSendMessage(threadId);
  const { runs, connected, mr, worktreeRemoved, begin } = useThreadEvents(threadId);
  const activeId =
    Object.entries(runs).find(([, run]) => !run.finished)?.[0] ??
    (['running', 'waiting'].includes(thread?.run_status ?? '') ? thread?.run_id : undefined);
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
      className="self-start rounded-full border border-line px-2 py-1"
    >
      !{iid ?? 'MR'} {mr?.title ?? thread?.mr_title} ·{' '}
      {{ merged: 'Merged', closed: 'Closed' }[mrState ?? ''] ?? 'Open'}
    </a>
  );
  const context = (
    <>
      {(repoName || thread?.repo_id) && (
        <span className="rounded-full border border-line px-2 py-1">
          {repoName ?? thread?.repo_id}
        </span>
      )}
      {thread?.branch && (
        <span className="rounded-full border border-line px-2 py-1">{thread.branch}</span>
      )}
      {mrChip}
    </>
  );
  const empty = messages.length === 0 && !activeId;
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
        role="log"
        aria-label="Messages"
        className={`flex min-h-0 flex-1 flex-col gap-4 overflow-y-auto p-5 ${empty ? 'justify-center' : ''}`}
      >
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
        {thread?.branch && (
          <p className="text-sm text-muted">
            Working on <span className="font-mono">{thread.branch}</span>
          </p>
        )}
        {messages.map((m, index) => {
          const day = new Date(m.created_at);
          const previous = index > 0 ? new Date(messages[index - 1].created_at) : null;
          const valid = Number.isFinite(day.getTime()) && day.getFullYear() > 1;
          const showDay = valid && (!previous || day.toDateString() !== previous.toDateString());
          return (
            <Fragment key={m.id}>
              {showDay && (
                <div className="flex items-center gap-3 text-xs text-muted">
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
              <MessageLine message={m} retry={retry} />
            </Fragment>
          );
        })}
        {Object.entries(runs).map(([id, run]) => (
          <div key={id}>
            <LiveRun id={id} run={run} look={look} />
            {run.finished && !answered.has(id) && <RunResult id={id} retry={retry} />}
          </div>
        ))}
        {thread?.run_id && !activeId && !answered.has(thread.run_id) && !runs[thread.run_id] && (
          <RunResult id={thread.run_id} retry={retry} />
        )}
        {mrChip}
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
      {!empty && (
        <div className="mx-auto w-full max-w-3xl p-4">
          {memory}
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
