import { useEffect, useState } from 'react';
import { DiffView } from '@/components/diff-view';
import { ErrorNotice } from '@/components/error-notice';
import { Markdown } from '@/components/markdown';
import { pullNumber } from '@/components/pr-chip';
import { ApiError } from '@/lib/api-client';
import { usePullRequest, type PullRequestDetail } from '@/modules/threads/api/pull-request';

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
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key !== 'Escape' || e.defaultPrevented) return;
      e.preventDefault();
      onClose();
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [onClose]);
  const n = pullNumber(url);
  return (
    <aside
      aria-label="Pull request"
      className="flex min-h-0 min-w-0 max-w-130 flex-[1_1_520px] flex-col border-l border-line bg-bg"
    >
      <div className="flex flex-none items-center gap-2 px-6 pt-5 pb-3 text-[13.5px]">
        {pr && <span className="text-muted">{pr.repo}</span>}
        <a href={url} target="_blank" rel="noreferrer" className="text-ink hover:underline">
          #{n}
          <span aria-hidden="true"> ↗</span>
        </a>
        <span className="flex-1" />
        <button
          type="button"
          aria-label="Close"
          onClick={onClose}
          className="-mr-2 flex size-9 items-center justify-center rounded-[10px] text-muted hover:bg-tile hover:text-ink"
        >
          <svg
            aria-hidden="true"
            width="16"
            height="16"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            strokeWidth="1.8"
            strokeLinecap="round"
          >
            <path d="M6 6l12 12M18 6L6 18" />
          </svg>
        </button>
      </div>
      <div className="min-h-0 flex-1 overflow-auto">
        {pr ? (
          <Details pr={pr} />
        ) : error ? (
          <div className="px-6 py-2">
            <Failure error={error} url={url} retry={() => void refetch()} />
          </div>
        ) : (
          <p className="px-6 py-2 text-[13.5px] text-muted">Loading PR #{n}…</p>
        )}
      </div>
      {pr && (
        <div className="flex flex-none items-center gap-3 border-t border-line px-6 py-3">
          <button
            type="button"
            disabled={isFetching}
            onClick={() => void refetch()}
            className={action}
          >
            Reload
          </button>
        </div>
      )}
    </aside>
  );
}
