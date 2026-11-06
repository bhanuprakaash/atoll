import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import type { CreatureLook } from '@/components/creature';
import { api } from '@/lib/api-client';

type ChangeSnapshot = {
  name?: string;
  path?: string;
  content?: string;
  look?: CreatureLook;
  provider?: 'claude' | 'codex';
  model?: string;
  persona?: string;
  skills?: string[];
  repo_access?: 'all' | string[];
  connections?: string[];
};

export type PlatformChange = {
  id: string;
  entity: 'bot' | 'skill' | 'repo';
  entity_id: string;
  action: 'create' | 'edit';
  before: ChangeSnapshot | null;
  after: ChangeSnapshot | null;
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

export function useUndoChange(ids: string[]) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async () => {
      for (const id of ids) await api<void>(`/platform/changes/${id}/undo`, { method: 'POST' });
    },
    onSuccess: async () => {
      await Promise.all(
        ['changes', 'bots', 'skills', 'repos'].map((key) =>
          queryClient.invalidateQueries({ queryKey: [key] }),
        ),
      );
    },
  });
}

export function useChangeNames() {
  const skills = useQuery({
    queryKey: ['skills'],
    queryFn: () => api<{ id: string; name: string }[]>('/skills'),
  });
  const repos = useQuery({
    queryKey: ['repos'],
    queryFn: () => api<{ id: string; name: string }[]>('/repos'),
  });
  const connections = useQuery({
    queryKey: ['connections'],
    queryFn: () => api<{ id: string; kind: string }[]>('/connections'),
  });
  return {
    skills: skills.data ?? [],
    repos: repos.data ?? [],
    connections: connections.data ?? [],
  };
}
