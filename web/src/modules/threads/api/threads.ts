import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api, ApiError } from '@/lib/api-client';
import type { ImageInput, Message, Run } from '@/modules/threads/api/messages';

export type Thread = {
  id: string;
  bot_id: string;
  parent_thread_id: string;
  parent_bot_id: string;
  helpers: Thread[];
  repo_id: string;
  branch: string;
  worktree: string;
  mr_url: string;
  mr_state: string;
  mr_title: string;
  worktree_removed: boolean;
  snapshot_ref: string;
  closed_at: string | null;
  updated_at: string;
  verb: string;
  step: string;
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
  return useQuery({
    queryKey: ['thread', threadId],
    queryFn: () => getThread(threadId),
    refetchInterval: 2000,
    retry: (failures, error) =>
      !(error instanceof ApiError && error.status === 404) && failures < 3,
  });
}

export function useCloseThread() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (threadId: string) => api<void>(`/threads/${threadId}/close`, { method: 'POST' }),
    onSuccess: (_, threadId) =>
      Promise.all(
        [['thread', threadId], ['threads'], ['bots']].map((queryKey) =>
          queryClient.invalidateQueries({ queryKey }),
        ),
      ),
  });
}

export function useRenameThread() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ id, title }: { id: string; title: string }) =>
      api<Thread>(`/threads/${id}`, { method: 'PATCH', body: { title } }),
    onSuccess: (_, { id }) =>
      Promise.all(
        [['thread', id], ['threads'], ['bots']].map((queryKey) =>
          queryClient.invalidateQueries({ queryKey }),
        ),
      ),
  });
}

export function usePinThread() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ id, pinned, replace }: { id: string; pinned: boolean; replace?: string }) =>
      api<void>(`/threads/${id}/pin`, {
        method: pinned ? 'POST' : 'DELETE',
        body: replace ? { replace } : undefined,
      }),
    onSuccess: () =>
      Promise.all(
        [['threads'], ['bots']].map((queryKey) => queryClient.invalidateQueries({ queryKey })),
      ),
  });
}

export const markOpened = (threadId: string) =>
  api<void>(`/threads/${threadId}/opened`, { method: 'POST' });

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
    mutationFn: ({
      repoId,
      text,
      images,
    }: {
      repoId: string;
      text: string;
      images?: ImageInput[];
    }) =>
      api<Thread & { thread?: Thread; message: Message; run: Run }>(`/bots/${botId}/threads`, {
        method: 'POST',
        body: { text, images, ...(repoId ? { repo_id: repoId } : {}) },
      }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['threads', botId] }),
  });
}
