import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api } from '@/lib/api-client';
import type { Run } from '@/modules/threads/api/messages';

export const getRun = (runId: string) => api<Run>(`/runs/${runId}`);
export function useRun(runId: string) {
  return useQuery({ queryKey: ['run', runId], queryFn: () => getRun(runId), enabled: !!runId });
}
export function useStopRun() {
  return useMutation({
    mutationFn: (id: string) => api<void>(`/runs/${id}/stop`, { method: 'POST' }),
  });
}
export function useContinueRun(threadId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => api<{ run: Run }>(`/runs/${id}/continue`, { method: 'POST' }),
    onSuccess: () =>
      Promise.all(
        [['messages', threadId], ['thread', threadId], ['bots']].map((queryKey) =>
          queryClient.invalidateQueries({ queryKey }),
        ),
      ),
  });
}
