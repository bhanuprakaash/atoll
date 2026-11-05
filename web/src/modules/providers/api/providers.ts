import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api } from '@/lib/api-client';

export type Provider = {
  name: 'claude' | 'codex';
  label: string;
  found: boolean;
  path: string;
  version: string;
  signed_in: boolean;
  models: { id: string; name: string }[];
};
export type SignInState = { status: string; url: string; error: string };

export function useProviders() {
  return useQuery({ queryKey: ['providers'], queryFn: () => api<Provider[]>('/providers') });
}

export function useRefreshProviders() {
  const client = useQueryClient();
  return useMutation({
    mutationFn: () => api<Provider[]>('/providers/refresh', { method: 'POST' }),
    onSuccess: (providers) => client.setQueryData(['providers'], providers),
  });
}

export function useSignIn() {
  return useMutation({
    mutationFn: (name: Provider['name']) =>
      api<SignInState>(`/providers/${name}/sign-in`, { method: 'POST' }),
  });
}

export function useSignInState(name: Provider['name'], enabled = true) {
  const client = useQueryClient();
  return useQuery({
    queryKey: ['providers', name, 'sign-in'],
    queryFn: async () => {
      const state = await api<SignInState>(`/providers/${name}/sign-in`);
      if (state.status === 'signed_in')
        void client.invalidateQueries({ queryKey: ['providers'], exact: true });
      return state;
    },
    enabled,
    refetchInterval: (query) =>
      ['starting', 'waiting'].includes(query.state.data?.status ?? '') ? 1000 : false,
  });
}
