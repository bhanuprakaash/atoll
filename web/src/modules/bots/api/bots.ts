import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import type { CreatureLook, CreatureState } from '@/components/creature';
import { api } from '@/lib/api-client';
import type { Waiting } from '@/types/waiting';

export type Provider = 'claude' | 'codex';

export type SidebarThread = {
  id: string;
  title: string;
  state: 'needs_you' | 'waiting' | 'running' | 'new' | '';
  pinned: boolean;
  helper?: boolean;
  waiting?: Waiting[];
  branch: string;
  mr_url: string;
  last_activity: string;
};

export type Bot = {
  id: string;
  name: string;
  persona: string;
  look: CreatureLook;
  model: string;
  mode: 'auto' | 'ask' | 'full';
  state: CreatureState;
  pinned: boolean;
  activity: string;
  last_activity: string;
  repo_access: 'all' | string[];
  connections: string[];
  provider: Provider;
  skills: string[];
  mcp_servers: string[];
  delegates: string[];
  can_manage_platform: boolean;
  group_id: string;
  threads?: SidebarThread[];
};

export type NewBot = Pick<
  Bot,
  'name' | 'persona' | 'provider' | 'skills' | 'repo_access' | 'connections' | 'look' | 'model'
>;

export function useBots() {
  return useQuery({
    queryKey: ['bots'],
    queryFn: () => api<Bot[]>('/bots'),
    refetchInterval: 2000,
  });
}

export function useCreateBot() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (bot: NewBot) => api<Bot>('/bots', { method: 'POST', body: bot }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['bots'] }),
  });
}

export function useDeleteBot(id: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: () => api<void>(`/bots/${id}`, { method: 'DELETE' }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['bots'] }),
  });
}

export function usePinBot(id: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (pinned: boolean) =>
      api<void>(`/bots/${id}/pin`, { method: pinned ? 'POST' : 'DELETE' }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['bots'] }),
  });
}

export function useUpdateBot(id: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (
      changes: Partial<
        NewBot & Pick<Bot, 'can_manage_platform' | 'mcp_servers' | 'mode' | 'delegates'>
      >,
    ) => api<Bot>(`/bots/${id}`, { method: 'PUT', body: changes }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['bots'] }),
  });
}
