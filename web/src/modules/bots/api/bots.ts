import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api } from '@/lib/api-client';

export type Provider = 'claude' | 'codex';

export type Bot = { id: string; name: string; persona: string; provider: Provider };

export function useBots() {
  return useQuery({ queryKey: ['bots'], queryFn: () => api<Bot[]>('/bots') });
}

export function useCreateBot() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (bot: Omit<Bot, 'id'>) => api<Bot>('/bots', { method: 'POST', body: bot }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['bots'] }),
  });
}
