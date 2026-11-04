import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api } from '@/lib/api-client';

export type Thread = { id: string; bot_id: string; created_at: string };

export function useThreads(botId: string) {
  return useQuery({
    queryKey: ['threads', botId],
    queryFn: () => api<Thread[]>(`/bots/${botId}/threads`),
  });
}

export function useCreateThread(botId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: () => api<Thread>(`/bots/${botId}/threads`, { method: 'POST' }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['threads', botId] }),
  });
}
