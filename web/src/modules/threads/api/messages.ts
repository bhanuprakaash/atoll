import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api } from '@/lib/api-client';

export type Message = { id: string; role: 'user' | 'bot'; text: string; run_id: string };

export type Run = {
  id: string;
  thread_id: string;
  status: 'running' | 'waiting' | 'done' | 'failed';
  error: string;
};

export function useMessages(threadId: string) {
  return useQuery({
    queryKey: ['messages', threadId],
    queryFn: () => api<Message[]>(`/threads/${threadId}/messages`),
  });
}

export function useSendMessage(threadId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (text: string) =>
      api<{ message: Message; run: Run }>(`/threads/${threadId}/messages`, {
        method: 'POST',
        body: { text },
      }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['messages', threadId] }),
  });
}
