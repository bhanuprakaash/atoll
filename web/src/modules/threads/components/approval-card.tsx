import { useLayoutEffect, useRef, useState, type ReactNode } from 'react';
import { quiet, refuse } from '@/components/button';
import { ErrorNotice } from '@/components/error-notice';
import { ApiError } from '@/lib/api-client';
import { useAnswerApproval } from '@/modules/threads/api/approvals';

export function approvalTarget(input: unknown): string {
  if (!input || typeof input !== 'object') return '';
  const value = input as Record<string, unknown>;
  const changes = Array.isArray(value.changes) ? value.changes : [];
  const first = changes[0] as Record<string, unknown> | undefined;
  const target =
    value.command ?? value.file_path ?? value.path ?? value.file ?? value.target ?? first?.path;
  return typeof target === 'string' ? target : '';
}

const isEdit = (tool: string) => /edit|write|filechange/i.test(tool);
const isCommand = (tool: string) => /bash|command/i.test(tool);

function approvalLead(tool: string) {
  if (isEdit(tool)) return 'Wants to edit';
  return isCommand(tool) ? 'Wants to run' : 'Wants to use';
}

export function approvalVerb(tool: string) {
  return isEdit(tool) || isCommand(tool) ? approvalLead(tool) : `Wants to use ${tool}`;
}

export function ApprovalCard({
  id,
  tool,
  input,
  by,
  allow_in_thread,
}: {
  id: string;
  tool: string;
  input: unknown;
  by?: ReactNode;
  allow_in_thread?: boolean;
}) {
  const [answered, setAnswered] = useState(false);
  const [showDiff, setShowDiff] = useState(false);
  const [open, setOpen] = useState(false);
  const [long, setLong] = useState(false);
  const code = useRef<HTMLElement>(null);
  const answer = useAnswerApproval(id);
  const edit = isEdit(tool);
  const target =
    isEdit(tool) || isCommand(tool)
      ? approvalTarget(input)
      : [tool, approvalTarget(input)].filter(Boolean).join(' ');
  const details = input && typeof input === 'object' ? (input as Record<string, unknown>) : {};
  const changes = Array.isArray(details.changes)
    ? (details.changes as Record<string, unknown>[])
    : [];
  const patches = changes
    .map((change) => change.diff)
    .filter((value): value is string => typeof value === 'string');
  const diff = [details.old_string, details.new_string, details.content, ...patches]
    .filter((value): value is string => typeof value === 'string')
    .join('\n');
  const patchLines = patches.flatMap((patch) => patch.split('\n'));
  const added = patches.length
    ? patchLines.filter((line) => line.startsWith('+') && !line.startsWith('+++')).length
    : typeof details.new_string === 'string'
      ? details.new_string.split('\n').length
      : typeof details.content === 'string'
        ? details.content.split('\n').length
        : 0;
  const removed = patches.length
    ? patchLines.filter((line) => line.startsWith('-') && !line.startsWith('---')).length
    : typeof details.old_string === 'string'
      ? details.old_string.split('\n').length
      : 0;
  const respond = (approve: boolean, in_thread?: boolean) =>
    answer.mutate(
      { approve, in_thread },
      {
        onSuccess: () => setAnswered(true),
        onError: (err) => err instanceof ApiError && err.status === 404 && setAnswered(true),
      },
    );
  useLayoutEffect(() => {
    const el = code.current;
    if (!el || open) return;
    const measure = () => setLong(el.scrollHeight > el.clientHeight + 1);
    measure();
    const observer = new ResizeObserver(measure);
    observer.observe(el);
    return () => observer.disconnect();
  }, [open, target, answered]);
  if (answered) return null;
  return (
    <div role="group" aria-label="Approval" className="flex flex-col gap-1.5 text-sm">
      <div className="flex flex-wrap items-center gap-x-3 gap-y-2">
        <span className="flex min-w-60 flex-1 items-center gap-2 text-muted">
          <span className="flex-none">
            {by ? (
              <>
                {by} {approvalLead(tool).toLowerCase()}
              </>
            ) : (
              approvalLead(tool)
            )}{' '}
          </span>
          <code
            ref={code}
            className={`min-w-0 flex-1 rounded-md bg-box px-2 py-1 font-mono text-[12.5px] leading-5 text-ink [overflow-wrap:anywhere] ${open ? 'whitespace-pre-wrap' : 'line-clamp-2'}`}
          >
            {target}
          </code>
        </span>
        <span className="flex items-center gap-2">
          <button
            type="button"
            disabled={answer.isPending}
            onClick={() => respond(true)}
            className="h-8 rounded-full bg-accent px-4 text-[13.5px] text-accent-ink"
          >
            Allow
          </button>
          <button
            type="button"
            disabled={answer.isPending}
            onClick={() => respond(false)}
            className={`h-8 rounded-full border border-outline px-4 text-[13.5px] text-ink ${refuse}`}
          >
            Deny
          </button>
          {allow_in_thread && (
            <button
              type="button"
              disabled={answer.isPending}
              onClick={() => respond(true, true)}
              className={quiet}
            >
              Allow in this thread
            </button>
          )}
        </span>
      </div>
      {(long || open || edit) && (
        <div className="flex items-center gap-3 text-[13px] text-muted">
          {long || open ? (
            <button type="button" onClick={() => setOpen(!open)} className={quiet}>
              {open ? 'Show less' : 'Show all'}
            </button>
          ) : null}
          {edit && (added > 0 || removed > 0) && (
            <span>
              <span className="text-ok">+{added}</span> −{removed}
            </span>
          )}
          {edit && (
            <button type="button" onClick={() => setShowDiff(!showDiff)} className={quiet}>
              {showDiff ? 'Hide diff' : 'Show diff'}
            </button>
          )}
        </div>
      )}
      {showDiff && (
        <pre className="rounded-2xl border border-line bg-well p-3 font-mono text-xs whitespace-pre-wrap text-ink/80">
          {diff || 'No diff available'}
        </pre>
      )}
      {answer.isError && (
        <ErrorNotice
          title="Couldn't send your answer."
          error={answer.error}
          retry={() =>
            answer.variables && respond(answer.variables.approve, answer.variables.in_thread)
          }
        />
      )}
    </div>
  );
}
