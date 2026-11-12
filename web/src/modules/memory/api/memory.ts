import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api } from '@/lib/api-client';

export type MemoryItem = {
  id: string;
  kind: 'decision' | 'convention' | 'finding' | 'fact' | 'lesson';
  body: string;
  why: string;
  scope: 'user' | 'repo' | 'bot';
  scope_id: string;
  status: 'candidate' | 'accepted' | 'retired';
  source_bot_id: string;
  source_thread_id: string;
  source_run_id: string;
  replaces: string[];
  replaced_by: string;
  created_at: string;
  retired_at: string | null;
  ticket: string;
  retired_reason: string;
};

export function useMemory() {
  return useQuery({ queryKey: ['memory'], queryFn: () => api<MemoryItem[]>('/memory') });
}

export function useRetireMemory(id: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: () => api<void>(`/memory/${id}/retire`, { method: 'POST' }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['memory'] }),
  });
}
