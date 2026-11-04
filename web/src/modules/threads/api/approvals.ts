import { useMutation } from '@tanstack/react-query';
import { api } from '@/lib/api-client';

export function useAnswerApproval(approvalId: string) {
  return useMutation({
    mutationFn: (approve: boolean) =>
      api<void>(`/approvals/${approvalId}`, { method: 'POST', body: { approve } }),
  });
}
