import { useMutation, useQuery } from '@tanstack/react-query';
import { api } from '@/lib/api-client';

export type Connection = {
  id: string;
  kind: 'gitlab';
  instance_url: string;
  status: 'pending' | 'connected';
};

export type DeviceFlow = {
  id: string;
  user_code: string;
  verification_uri: string;
  interval_ms: number;
};

export function useConnections() {
  return useQuery({
    queryKey: ['connections'],
    queryFn: () => api<Connection[]>('/connections'),
  });
}

export function useStartGitlab() {
  return useMutation({
    mutationFn: (instanceUrl: string) =>
      api<DeviceFlow>('/connections/gitlab/start', {
        method: 'POST',
        body: { instance_url: instanceUrl },
      }),
  });
}

export const pollConnection = (id: string) =>
  api<{ status: Connection['status'] }>(`/connections/${id}/poll`, { method: 'POST' });
