import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api } from '@/lib/api-client';

export type Repo = { id: string; name: string; path: string };

export function useRepos() {
  return useQuery({ queryKey: ['repos'], queryFn: () => api<Repo[]>('/repos') });
}

export function useRegisterRepo() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (repo: Omit<Repo, 'id'>) => api<Repo>('/repos', { method: 'POST', body: repo }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['repos'] }),
  });
}
