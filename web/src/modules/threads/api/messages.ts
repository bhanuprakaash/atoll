import {
  useInfiniteQuery,
  useMutation,
  useQueryClient,
  type InfiniteData,
} from '@tanstack/react-query';
import { api } from '@/lib/api-client';

export type Message = {
  id: string;
  role: 'user' | 'bot';
  text: string;
  run_id: string;
  created_at: string;
};

export type Run = {
  id: string;
  thread_id: string;
  status: 'running' | 'waiting' | 'done' | 'failed' | 'stopped';
  started_at: string;
  finished_at: string;
  tools: Record<string, number>;
  report?: { blocked: string } | null;
  error: string;
};

export function useMessages(threadId: string) {
  return useInfiniteQuery<Message[], Error, InfiniteData<Message[]>, [string, string], string>({
    queryKey: ['messages', threadId],
    initialPageParam: '',
    queryFn: ({ pageParam }) =>
      api<Message[]>(
        `/threads/${threadId}/messages${pageParam ? `?before=${encodeURIComponent(pageParam)}` : ''}`,
      ),
    getPreviousPageParam: (page) => page[0]?.id,
    getNextPageParam: () => undefined,
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
