import { useEffect, useRef, useState } from 'react';
import { DiffView } from '@/components/diff-view';
import { ErrorNotice } from '@/components/error-notice';
import { Markdown } from '@/components/markdown';
import { pullNumber } from '@/components/pr-chip';
import { SidePanel } from '@/components/side-panel';
import { ApiError } from '@/lib/api-client';
import {
  useMergePR,
  usePullRequest,
  type PullRequestDetail,
} from '@/modules/threads/api/pull-request';

const states: Record<PullRequestDetail['state'], [string, string]> = {
  open: ['Open', 'bg-ok-bg text-ok'],
  draft: ['Draft', 'bg-tile text-muted'],
  merged: ['Merged', 'bg-kind-bg text-kind'],
  closed: ['Closed', 'bg-tile text-muted'],
};
const words = { failed: 'failed', pending: 'running', passed: 'passed' } as const;
const tones = { failed: 'text-danger', pending: 'text-muted', passed: 'text-ok' } as const;
const action =
  'h-8 rounded-full border border-outline px-4 text-[13.5px] text-ink disabled:opacity-50';

// failedLine names the failed checks in one plain line: "2 checks failed: test-guard, check".
export function failedLine(checks: PullRequestDetail['checks']) {
  const failed = checks.filter((c) => c.result === 'failed').map((c) => c.name);
  if (failed.length === 0) return '';
  return `${failed.length} check${failed.length > 1 ? 's' : ''} failed: ${failed.join(', ')}`;
}

function Counts({ add, del }: { add: number; del: number }) {
  return (
    <span className="flex-none font-mono text-xs">
      <span className="text-ok">+{add}</span> <span className="text-muted">−{del}</span>
    </span>
  );
}

function File({ url, file }: { url: string; file: PullRequestDetail['files'][number] }) {
  const [open, setOpen] = useState(false);
  return (
    <li>
      <button
        type="button"
        aria-expanded={open}
        onClick={() => setOpen(!open)}
        className="flex w-full items-center gap-2 rounded-lg px-2 py-1.5 text-left hover:bg-selected"
      >
        <span aria-hidden="true" className={`text-muted ${open ? 'rotate-90' : ''}`}>
          ›
        </span>
        <span className="min-w-0 flex-1 truncate font-mono text-[12.5px]">{file.path}</span>
        <Counts add={file.additions} del={file.deletions} />
      </button>
      {open && (
        <div className="px-2 pb-2">
          {file.patch ? (
            <DiffView source={file.patch} />
          ) : (
            <p className="flex items-center gap-3 px-5 text-[13px] text-muted">
              Too large to show here
              <a
                href={`${url}/files`}
                target="_blank"
                rel="noreferrer"
                className="underline hover:text-ink"
              >
                Open on GitHub
              </a>
            </p>
          )}
        </div>
      )}
    </li>
  );
}

function Section({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <section className="flex flex-col gap-2 border-t border-line px-6 py-4">
      <h3 className="text-xs text-label">{title}</h3>
      {children}
    </section>
  );
}

function Details({ pr }: { pr: PullRequestDetail }) {
  const failed = failedLine(pr.checks);
  const [state, tone] = states[pr.state];
  const add = pr.files.reduce((n, f) => n + f.additions, 0);
  const del = pr.files.reduce((n, f) => n + f.deletions, 0);
  return (
    <>
      <div className="flex flex-col gap-2 px-6 pb-4">
        <h2 className="text-[17px] leading-snug text-ink">{pr.title}</h2>
        <p className="flex flex-wrap items-center gap-x-2 gap-y-1 text-[13px] text-muted">
          {pr.author && <span>{pr.author}</span>}
          <span className="font-mono text-[12.5px]">
            {pr.base} ← {pr.head}
          </span>
          <span className={`rounded-full px-2 py-px text-xs ${tone}`}>{state}</span>
        </p>
      </div>
      {pr.checks.length > 0 && (
        <Section title="Checks">
          {failed && <p className="text-[13.5px] text-ink">{failed}</p>}
          <ul aria-label="Checks" className="flex flex-col text-[13.5px]">
            {pr.checks.map((c, i) => (
              <li key={i} className="flex items-center gap-2 py-0.5">
                <span className="min-w-0 flex-1 truncate">{c.name}</span>
                <span className={`text-xs ${tones[c.result]}`}>{words[c.result]}</span>
              </li>
            ))}
          </ul>
        </Section>
      )}
      <Section title="Description">
        {pr.body.trim() ? (
          <div className="text-[13.5px]">
            <Markdown>{pr.body}</Markdown>
          </div>
        ) : (
          <p className="text-[13.5px] text-muted">No description.</p>
        )}
      </Section>
      <Section title="Files">
        {pr.files.length === 0 ? (
          <p className="text-[13.5px] text-muted">This PR has no file changes.</p>
        ) : (
          <>
            <p className="flex items-center gap-2 text-[13.5px]">
              {pr.files.length} file{pr.files.length > 1 ? 's' : ''}
              <Counts add={add} del={del} />
            </p>
            <ul className="-mx-2 flex flex-col">
              {pr.files.map((f) => (
                <File key={f.path} url={pr.url} file={f} />
              ))}
            </ul>
          </>
        )}
      </Section>
    </>
  );
}

const methods = { merge: 'Merge', squash: 'Squash and merge', rebase: 'Rebase and merge' } as const;

// Merge is the person's merge: a first click asks "Confirm merge?", which resets after 3 seconds,
// on Esc or on a click elsewhere; the second merges at the head sha the panel loaded.
function Merge({ threadId, pr }: { threadId: string; pr: PullRequestDetail }) {
  const [confirming, setConfirming] = useState(false);
  const button = useRef<HTMLButtonElement>(null);
  const merge = useMergePR(threadId, pr.url);
  const { reset } = merge;
  useEffect(() => reset(), [reset, pr.head_sha]);
  useEffect(() => {
    if (!confirming) return;
    const timer = setTimeout(() => setConfirming(false), 3000);
    const onKey = (e: KeyboardEvent) => {
      if (e.key !== 'Escape') return;
      e.preventDefault();
      setConfirming(false);
    };
    const onDown = (e: PointerEvent) => {
      if (!button.current?.contains(e.target as Node)) setConfirming(false);
    };
    window.addEventListener('keydown', onKey, true);
    window.addEventListener('pointerdown', onDown, true);
    return () => {
      clearTimeout(timer);
      window.removeEventListener('keydown', onKey, true);
      window.removeEventListener('pointerdown', onDown, true);
    };
  }, [confirming]);
  const conflicts = pr.mergeable === false;
  const code = merge.error instanceof ApiError ? merge.error.code : '';
  const label = merge.isPending
    ? 'Merging…'
    : confirming
      ? 'Confirm merge?'
      : pr.merge_method
        ? methods[pr.merge_method]
        : 'Merge';
  return (
    <div className="flex flex-col gap-2">
      {merge.error ? (
        code === 'head_moved' ? (
          <ErrorNotice error={merge.error} next="" details="" />
        ) : (
          <ErrorNotice error={merge.error} next={code === 'refused' ? '' : undefined} />
        )
      ) : conflicts ? (
        <p className="text-[13px] text-ink">Has conflicts with {pr.base}.</p>
      ) : (
        <p className="text-[13px] text-muted">
          {`Merges ${pr.head} into ${pr.base} at ${pr.head_sha.slice(0, 7)}, the commit shown here`}
        </p>
      )}
      <button
        ref={button}
        type="button"
        disabled={conflicts || !pr.merge_method || merge.isPending}
        onClick={() => {
          if (!confirming) {
            merge.reset();
            setConfirming(true);
            return;
          }
          setConfirming(false);
          merge.mutate(pr.head_sha);
        }}
        className="h-8 self-end rounded-full bg-accent px-4 text-[13.5px] text-accent-ink disabled:opacity-50"
      >
        {label}
      </button>
    </div>
  );
}

function Failure({ error, url, retry }: { error: unknown; url: string; retry: () => void }) {
  const code = error instanceof ApiError ? error.code : '';
  if (code === 'no_github' || code === 'github_sign_in')
    return (
      <ErrorNotice
        error={error}
        next=""
        details=""
        actions={[{ label: 'Settings › Connections', to: '/settings/connections' }]}
      />
    );
  const actions = [
    { label: 'Retry', onClick: retry },
    { label: 'Open on GitHub', href: url },
  ];
  if (code === 'cant_see')
    return <ErrorNotice error={error} next="" details="" actions={actions} />;
  return <ErrorNotice error={error} actions={actions} />;
}

// PRPanel is a GitHub pull request read through Reef, docked on the right of the thread.
export function PRPanel({
  threadId,
  url,
  onClose,
}: {
  threadId: string;
  url: string;
  onClose: () => void;
}) {
  const { data: pr, error, refetch, isFetching } = usePullRequest(threadId, url);
  const n = pullNumber(url);
  return (
    <SidePanel
      label="Pull request"
      onClose={onClose}
      header={
        <>
          {pr && <span className="text-muted">{pr.repo}</span>}
          <a href={url} target="_blank" rel="noreferrer" className="text-ink hover:underline">
            #{n}
            <span aria-hidden="true"> ↗</span>
          </a>
        </>
      }
      footer={
        pr && (
          <div className="flex flex-none flex-col gap-3 border-t border-line px-6 py-3">
            {(pr.state === 'open' || pr.state === 'draft') && <Merge threadId={threadId} pr={pr} />}
            <button
              type="button"
              disabled={isFetching}
              onClick={() => void refetch()}
              className={`${action} self-start`}
            >
              Reload
            </button>
          </div>
        )
      }
    >
      {pr ? (
        <Details pr={pr} />
      ) : error ? (
        <div className="px-6 py-2">
          <Failure error={error} url={url} retry={() => void refetch()} />
        </div>
      ) : (
        <p className="px-6 py-2 text-[13.5px] text-muted">Loading PR #{n}…</p>
      )}
    </SidePanel>
  );
}
