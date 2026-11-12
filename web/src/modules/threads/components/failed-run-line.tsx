import { type ReactNode } from 'react';
import { ErrorNotice } from '@/components/error-notice';
import type { Run } from '@/modules/threads/api/messages';
import { useRun } from '@/modules/threads/api/runs';
import { messageTime } from '@/utils/time';

const sentence = (text: string) => `${text.replace(/\.$/, '')}.`;

export function FailedLine({
  run,
  botName,
  replied,
  newest,
  retry,
  signedOut,
}: {
  run: Run;
  botName: string;
  replied: boolean;
  newest: boolean;
  retry: (runId: string) => void;
  signedOut?: (provider: Run['provider']) => ReactNode;
}) {
  if (run.error_kind === 'rate_limited') {
    return (
      <p className="text-ink/80">
        {botName} hit its {run.provider === 'codex' ? 'Codex' : 'Claude'} limit;{' '}
        {run.resets_at ? `back at ${messageTime(run.resets_at)}` : 'try again later'}
      </p>
    );
  }
  if (run.error_kind === 'secret_store' || (run.error_kind === 'signed_out' && signedOut)) {
    return (
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
        {run.error_kind === 'secret_store' ? (
          <span className="min-w-60 flex-1 text-ink/80">{run.error}</span>
        ) : (
          signedOut?.(run.provider)
        )}
        {newest && run.error_kind !== 'secret_store' && (
          <button
            type="button"
            onClick={() => retry(run.id)}
            className="h-8 rounded-full border border-outline px-4 text-[13.5px] text-ink"
          >
            Retry
          </button>
        )}
      </div>
    );
  }
  const kind = run.error_kind;
  const plain = kind === 'provider_off' || kind === 'needs_sign_in';
  const settings =
    kind === 'provider_off' || kind === 'signed_out'
      ? '/settings/providers'
      : kind === 'needs_sign_in'
        ? '/settings/connections'
        : '';
  const reason = plain
    ? sentence(run.error)
    : kind === 'signed_out'
      ? "the provider isn't signed in."
      : 'something went wrong.';
  return (
    <ErrorNotice
      title={`${replied ? 'Run failed' : "Didn't run"}: ${reason}`}
      details={run.error_detail || (plain ? '' : run.error)}
      actions={[
        ...(settings ? [{ label: 'Open settings', to: settings }] : []),
        ...(newest ? [{ label: 'Retry', onClick: () => retry(run.id) }] : []),
      ]}
    />
  );
}

export function FailedRun({
  id,
  botName,
  newest,
  retry,
  signedOut,
}: {
  id: string;
  botName: string;
  newest: boolean;
  retry: (runId: string) => void;
  signedOut?: (provider: Run['provider']) => ReactNode;
}) {
  const { data: run } = useRun(id);
  if (run?.status !== 'failed') return null;
  return (
    <FailedLine
      run={run}
      botName={botName}
      replied={false}
      newest={newest}
      retry={retry}
      signedOut={signedOut}
    />
  );
}
