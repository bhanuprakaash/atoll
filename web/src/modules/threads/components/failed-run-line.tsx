import { useState, type ReactNode } from 'react';
import { Link } from 'react-router';
import { quiet } from '@/components/button';
import type { Run } from '@/modules/threads/api/messages';
import { useRun } from '@/modules/threads/api/runs';
import { messageTime } from '@/utils/time';

const signedOutError = /sign.?in|authenticat|not logged in|login/i;
const connectionSignIn = /needs sign-in/i;

function reason(error: string) {
  if (connectionSignIn.test(error)) return `${error.replace(/\.$/, '')}.`;
  if (/keyring|keychain/i.test(error)) return "Reef couldn't reach the system keychain.";
  if (signedOutError.test(error)) return "the provider isn't signed in.";
  return 'something went wrong.';
}

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
  const [details, setDetails] = useState(false);
  if (run.error_kind === 'rate_limited') {
    return (
      <p className="text-ink/80">
        {botName} hit its {run.provider === 'codex' ? 'Codex' : 'Claude'} limit;{' '}
        {run.resets_at ? `back at ${messageTime(run.resets_at)}` : 'try again later'}
      </p>
    );
  }
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
      {run.error_kind === 'signed_out' && signedOut ? (
        signedOut(run.provider)
      ) : (
        <span className="min-w-60 flex-1 text-ink/80 [overflow-wrap:anywhere]">
          {replied
            ? `Run failed${run.error ? `: ${run.error}` : '.'}`
            : `Didn't run: ${reason(run.error)}`}
          {signedOutError.test(run.error) && !connectionSignIn.test(run.error) && (
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
          {!replied && run.error && !connectionSignIn.test(run.error) && (
            <>
              {' '}
              <button
                type="button"
                aria-expanded={details}
                onClick={() => setDetails(!details)}
                className={quiet}
              >
                Details
              </button>
              {details && <span className="mt-1 block text-[12.5px] text-muted">{run.error}</span>}
            </>
          )}
        </span>
      )}
      {newest && (
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
