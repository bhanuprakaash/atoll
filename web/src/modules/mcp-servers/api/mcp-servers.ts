import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api } from '@/lib/api-client';

export type McpServer = {
  id: string;
  name: string;
  kind: 'catalog' | 'connection' | 'local' | 'remote';
  command?: string;
  args?: string[];
  url?: string;
  env_names?: string[];
  header_names?: string[];
  status?: 'not_connected' | 'connected' | 'needs_sign_in' | 'needs_app';
};

export type McpServerInput =
  | { name: string; command: string; args: string[]; env: Record<string, string> }
  | { name: string; url: string; headers: Record<string, string> };

// useMcpServers polls every second while the signing-in server still has the status it had when
// the user clicked Connect.
export function useMcpServers(signingIn?: McpServer) {
  return useQuery({
    queryKey: ['mcp-servers'],
    queryFn: () => api<McpServer[]>('/mcp-servers'),
    refetchInterval: (query) =>
      signingIn && query.state.data?.find((s) => s.id === signingIn.id)?.status === signingIn.status
        ? 1000
        : false,
  });
}

export function useConnectMcpServer() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (id: string) =>
      api<{ authorize_url: string }>(`/mcp-servers/${id}/connect`, { method: 'POST' }),
    onError: () => queryClient.invalidateQueries({ queryKey: ['mcp-servers'] }),
  });
}

export function useSaveMcpServer(id?: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (server: McpServerInput) =>
      id
        ? api<McpServer>(`/mcp-servers/${id}`, { method: 'PUT', body: server })
        : api<McpServer>('/mcp-servers', { method: 'POST', body: server }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['mcp-servers'] }),
  });
}

export function useRemoveMcpServer() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => api<void>(`/mcp-servers/${id}`, { method: 'DELETE' }),
    onSuccess: () =>
      Promise.all([
        queryClient.invalidateQueries({ queryKey: ['mcp-servers'] }),
        queryClient.invalidateQueries({ queryKey: ['bots'] }),
      ]),
  });
}
