import { useState } from 'react';
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

export function ApprovalCard({ id, tool, input }: { id: string; tool: string; input: unknown }) {
  const [answered, setAnswered] = useState<boolean | null>(null);
  const [showDiff, setShowDiff] = useState(false);
  const answer = useAnswerApproval(id);
  const target = approvalTarget(input);
  const edit = /edit|write|filechange/i.test(tool);
  const command = /bash|command/i.test(tool);
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
  const respond = (approve: boolean) =>
    answer.mutate(approve, { onSuccess: () => setAnswered(approve) });
  return (
    <div className="flex flex-col gap-2 text-sm">
      <div className="flex flex-wrap items-center gap-3">
        <span className="min-w-60 flex-1 text-ink/80">
          {answered === null
            ? edit
              ? 'Wants to edit'
              : command
                ? 'Wants to run'
                : `Wants to use ${tool}`
            : answered
              ? edit
                ? 'Allowed editing'
                : 'Allowed'
              : edit
                ? 'Denied editing'
                : 'Denied'}{' '}
          <span className="font-mono text-[0.88em] text-ink">{target}</span>
          {edit && (added > 0 || removed > 0) && (
            <>
              {' '}
              <span className="text-[13px] text-ok">+{added}</span>{' '}
              <span className="text-[13px] text-danger">−{removed}</span>
            </>
          )}
          {edit && (
            <>
              {' '}
              <button
                type="button"
                onClick={() => setShowDiff(!showDiff)}
                className="text-[13px] text-muted underline decoration-outline underline-offset-3"
              >
                {showDiff ? 'Hide diff' : 'Show diff'}
              </button>
            </>
          )}
        </span>
        {answered === null && (
          <span className="flex gap-2">
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
              className="h-8 rounded-full border border-outline px-4 text-[13.5px] text-ink"
            >
              Deny
            </button>
          </span>
        )}
      </div>
      {showDiff && (
        <pre className="rounded-2xl border border-line bg-well p-3 font-mono text-xs whitespace-pre-wrap text-ink/80">
          {diff || 'No diff available'}
        </pre>
      )}
      {answer.isError && <span className="text-danger">Could not answer approval</span>}
    </div>
  );
}
