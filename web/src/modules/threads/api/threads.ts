import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api } from '@/lib/api-client';
import type { Message, Run } from '@/modules/threads/api/messages';

export type Thread = {
  id: string;
  bot_id: string;
  repo_id: string;
  branch: string;
  worktree: string;
  mr_url: string;
  mr_state: string;
  closed: boolean;
  created_at: string;
  title: string;
  last_activity: string;
  run_id: string;
  run_status: string;
  approval: { id: string; tool: string; input: unknown } | null;
};

export const getThread = (threadId: string) =>
  api<
    Thread & {
      active_run: Run | null;
      approvals: { run_id: string; id: string; tool: string; input: unknown }[];
    }
  >(`/threads/${threadId}`);

export function useThread(threadId: string) {
  return useQuery({ queryKey: ['thread', threadId], queryFn: () => getThread(threadId) });
}

export function useCloseThread(threadId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: () => api<void>(`/threads/${threadId}/close`, { method: 'POST' }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['thread', threadId] }),
  });
}

export function useThreads(botId: string) {
  return useQuery({
    queryKey: ['threads', botId],
    queryFn: () => api<Thread[]>(`/bots/${botId}/threads`),
    refetchInterval: 2000,
  });
}

export function useCreateThread(botId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (input?: string | { text: string }) =>
      api<Thread>(`/bots/${botId}/threads`, {
        method: 'POST',
        body: typeof input === 'string' ? { repo_id: input } : input,
      }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['threads', botId] }),
  });
}

export function useCreateThreadWithMessage(botId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ repoId, text }: { repoId: string; text: string }) =>
      api<Thread & { thread?: Thread; message: Message; run: Run }>(`/bots/${botId}/threads`, {
        method: 'POST',
        body: { repo_id: repoId, text },
      }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['threads', botId] }),
  });
}
