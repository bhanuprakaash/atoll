import { useState } from 'react';
import { useAnswerApproval } from '@/modules/threads/api/approvals';

export function approvalTarget(input: unknown): string {
  if (!input || typeof input !== 'object') return '';
  const value = input as Record<string, unknown>;
  const target = value.command ?? value.file_path ?? value.path ?? value.file ?? value.target;
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
  const diff = [details.old_string, details.new_string, details.content]
    .filter((value): value is string => typeof value === 'string')
    .join('\n');
  const respond = (approve: boolean) =>
    answer.mutate(approve, { onSuccess: () => setAnswered(approve) });
  return (
    <div className="text-sm text-muted">
      <span>
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
        <span className="font-mono">{target}</span>
      </span>{' '}
      {edit && (
        <button type="button" onClick={() => setShowDiff(!showDiff)} className="underline">
          {showDiff ? 'Hide diff' : 'Show diff'}
        </button>
      )}{' '}
      {answered === null ? (
        <>
          <button
            type="button"
            disabled={answer.isPending}
            onClick={() => respond(true)}
            className="rounded-full border border-line px-3 py-1"
          >
            Allow
          </button>
          {' · '}
          <button
            type="button"
            disabled={answer.isPending}
            onClick={() => respond(false)}
            className="rounded-full border border-line px-3 py-1"
          >
            Deny
          </button>
        </>
      ) : null}
      {showDiff && <pre className="whitespace-pre-wrap">{diff || 'No diff available'}</pre>}
      {answer.isError && <span className="text-danger"> Could not answer approval</span>}
    </div>
  );
}
