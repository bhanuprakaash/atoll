import { useMutation } from '@tanstack/react-query';
import { api } from '@/lib/api-client';

export function useAnswerApproval(approvalId: string) {
  return useMutation({
    mutationFn: (answer: { approve: boolean; in_thread?: boolean }) =>
      api<void>(`/approvals/${approvalId}`, { method: 'POST', body: answer }),
  });
}
