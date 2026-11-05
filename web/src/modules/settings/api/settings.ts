import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api } from '@/lib/api-client';

export type Settings = { user_name: string; worktree_root: string; data_dir: string };
export function useSettings() {
  return useQuery({ queryKey: ['settings'], queryFn: () => api<Settings>('/settings') });
}
export function useSaveSettings() {
  const client = useQueryClient();
  return useMutation({
    mutationFn: (settings: Settings) =>
      api<Settings>('/settings', { method: 'PUT', body: settings }),
    onSuccess: () => client.invalidateQueries({ queryKey: ['settings'] }),
  });
}
