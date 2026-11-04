import { useState } from 'react';
import { Button } from '@/components/button';
import { useAnswerApproval } from '@/modules/threads/api/approvals';

export function ApprovalCard({ id, tool, input }: { id: string; tool: string; input: unknown }) {
  const [answered, setAnswered] = useState(false);
  const answer = useAnswerApproval(id);
  if (answered) return null;

  const respond = (approve: boolean) =>
    answer.mutate(approve, { onSuccess: () => setAnswered(true) });

  return (
    <div className="flex flex-col gap-2 rounded-card border border-line bg-card p-3">
      <p className="text-sm font-medium">{tool}</p>
      <pre className="overflow-x-auto font-mono text-xs text-muted">
        {JSON.stringify(input, null, 2)}
      </pre>
      {answer.isError && <p className="text-sm text-danger">{answer.error.message}</p>}
      <div className="flex gap-2">
        <Button disabled={answer.isPending} onClick={() => respond(true)}>
          Approve
        </Button>
        <Button disabled={answer.isPending} onClick={() => respond(false)} className="bg-danger">
          Reject
        </Button>
      </div>
    </div>
  );
}
