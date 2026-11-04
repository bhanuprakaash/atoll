import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api } from '@/lib/api-client';

export type PlatformChange = {
  id: string;
  entity: 'bot' | 'skill' | 'repo';
  entity_id: string;
  action: 'create' | 'edit';
  before: { name?: string } | null;
  after: { name?: string } | null;
  undone: boolean;
  source_bot_id: string;
  source_thread_id: string;
  source_run_id: string;
  created_at: string;
};

export function useChanges(threadId: string) {
  return useQuery({
    queryKey: ['changes', threadId],
    queryFn: () => api<PlatformChange[]>(`/threads/${threadId}/changes`),
  });
}

export function useUndoChange(id: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: () => api<void>(`/platform/changes/${id}/undo`, { method: 'POST' }),
    onSuccess: async () => {
      await Promise.all(
        ['changes', 'bots', 'skills'].map((key) =>
          queryClient.invalidateQueries({ queryKey: [key] }),
        ),
      );
    },
  });
}
