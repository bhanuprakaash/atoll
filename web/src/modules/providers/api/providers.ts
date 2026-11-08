import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api } from '@/lib/api-client';

export type Provider = {
  name: 'claude' | 'codex';
  label: string;
  found: boolean;
  path: string;
  path_override: string;
  version: string;
  enabled: boolean;
  signed_in: boolean;
  models: { id: string; name: string }[];
  own_mcp_servers: string[];
  own_skills: string[];
};
export type SignInState = { status: string; url: string; needs_code: boolean; error: string };

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

export function useUpdateProvider() {
  const client = useQueryClient();
  return useMutation({
    mutationFn: ({
      name,
      ...body
    }: { name: Provider['name'] } & Partial<Pick<Provider, 'enabled' | 'path_override'>>) =>
      api<Provider>(`/providers/${name}`, { method: 'PUT', body }),
    onSuccess: (provider) =>
      client.setQueryData<Provider[]>(['providers'], (list) =>
        list?.map((p) => (p.name === provider.name ? provider : p)),
      ),
  });
}

export function useSignIn() {
  const client = useQueryClient();
  return useMutation({
    onMutate: (name: Provider['name']) => {
      client.removeQueries({ queryKey: ['providers', name, 'sign-in'] });
    },
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
      ['starting', 'waiting'].includes(query.state.data?.status ?? '') ? 250 : false,
  });
}

export function useSubmitSignInCode(name: Provider['name']) {
  return useMutation({
    mutationFn: (code: string) =>
      api<void>(`/providers/${name}/sign-in/code`, { method: 'POST', body: { code } }),
  });
}
