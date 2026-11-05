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
  const details = input && typeof input === 'object' ? (input as Record<string, unknown>) : {};
  const diff = [details.old_string, details.new_string, details.content]
    .filter((value): value is string => typeof value === 'string')
    .join('\n');
  const respond = (approve: boolean) =>
    answer.mutate(approve, { onSuccess: () => setAnswered(approve) });
  return (
    <div className="text-sm text-muted">
      <span>
        {edit ? 'Change' : 'Run'} {target}
      </span>{' '}
      {edit && (
        <button type="button" onClick={() => setShowDiff(!showDiff)} className="underline">
          Show diff
        </button>
      )}{' '}
      {answered === null ? (
        <>
          <button
            type="button"
            disabled={answer.isPending}
            onClick={() => respond(true)}
            className="underline"
          >
            Allow
          </button>
          {' · '}
          <button
            type="button"
            disabled={answer.isPending}
            onClick={() => respond(false)}
            className="underline"
          >
            Deny
          </button>
        </>
      ) : answered ? (
        'Allowed'
      ) : (
        'Denied'
      )}
      {showDiff && <pre className="whitespace-pre-wrap">{diff || 'No diff available'}</pre>}
      {answer.isError && <span className="text-danger"> Could not answer approval</span>}
    </div>
  );
}
