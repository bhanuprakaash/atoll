import { useState } from 'react';
import { Link } from 'react-router';
import { quiet } from '@/components/button';
import { ApiError } from '@/lib/api-client';

const retryNext = 'Try again. If it keeps happening, copy the details into a bug report.';

// What a person does next, by the server's code (docs/design.md, App and UI: Errors).
const nextStep: Record<string, string> = {
  keychain: 'Unlock the keychain or restart Reef, then try again.',
  signed_out: 'Sign in again.',
  provider_off: 'Open Settings > Providers.',
  busy: 'Wait for it to finish, or stop it.',
  closed: 'Start a new thread to keep going.',
  not_found: 'It was deleted or moved. Go back and pick another.',
  exists: 'Pick another name.',
  invalid: 'Fix it and try again.',
  conflict: 'Change it and try again.',
  forbidden: 'Change it and try again.',
  unsupported: 'Type the repo path instead.',
  unreachable: 'Check the address and your connection, then try again.',
  internal: retryNext,
};

const button =
  'h-8 rounded-full border border-outline px-4 text-[13.5px] text-ink disabled:opacity-50';

// An action is a button, or a link when it goes somewhere.
type Action = { label: string; onClick?: () => void; to?: string };

// ErrorNotice is every failure a person can cause: a plain title, what to do next, an action
// when there is one, and the raw text behind Details. Give it the error of a request, or a title
// and details of your own. A server that is not answering is the app's Reconnecting notice, so a
// request it dropped shows nothing here. Only this file reads an error's message.
export function ErrorNotice({
  error,
  title,
  details,
  next: nextLine,
  actions,
  retry,
  signIn,
  newThread,
  className = '',
}: {
  error?: unknown;
  title?: string;
  details?: string;
  next?: string;
  actions?: Action[];
  retry?: () => void;
  signIn?: () => void;
  newThread?: () => void;
  className?: string;
}) {
  const [open, setOpen] = useState(false);
  const api = error instanceof ApiError ? error : undefined;
  if (api?.code === 'offline') return null;
  const code = api ? (api.code in nextStep ? api.code : 'internal') : error ? 'internal' : '';
  const line = nextLine ?? (title ? undefined : nextStep[code]);
  const raw =
    details ??
    (api
      ? `${api.code} · ${api.status} · ${api.method} ${api.path}`
      : error instanceof Error
        ? error.message
        : '');
  const heading = title ?? (api ? api.message : 'Something went wrong on our side.');

  let acts = actions;
  if (!acts) {
    if (code === 'provider_off') acts = [{ label: 'Open settings', to: '/settings/providers' }];
    else if (code === 'closed' && newThread) acts = [{ label: 'New thread', onClick: newThread }];
    else if ((code === 'keychain' || code === 'signed_out') && signIn)
      acts = [{ label: 'Sign in again', onClick: signIn }];
    else if (retry && ['keychain', 'unreachable', 'internal'].includes(code))
      acts = [{ label: 'Try again', onClick: retry }];
  }

  return (
    <div role="alert" className={`flex flex-col gap-2 text-sm ${className}`}>
      <div className="flex items-start gap-2.5">
        <svg
          aria-hidden="true"
          width="16"
          height="16"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          strokeWidth="1.8"
          strokeLinecap="round"
          className="mt-0.5 flex-none text-danger"
        >
          <circle cx="12" cy="12" r="9" />
          <path d="M12 7.5v5M12 16v.5" />
        </svg>
        <div className="min-w-0 flex-1 [overflow-wrap:anywhere]">
          <p className="text-ink">{heading}</p>
          {line && <p className="text-muted">{line}</p>}
        </div>
      </div>
      <div className="flex flex-wrap items-center gap-3 pl-6.5">
        {acts?.map((act) =>
          act.to ? (
            <Link key={act.label} to={act.to} className={`${button} inline-flex items-center`}>
              {act.label}
            </Link>
          ) : (
            <button key={act.label} type="button" onClick={act.onClick} className={button}>
              {act.label}
            </button>
          ),
        )}
        {raw && (
          <button
            type="button"
            aria-expanded={open}
            onClick={() => setOpen(!open)}
            className={quiet}
          >
            Details
          </button>
        )}
      </div>
      {open && (
        <p className="pl-6.5 font-mono text-xs text-muted [overflow-wrap:anywhere]">{raw}</p>
      )}
    </div>
  );
}
