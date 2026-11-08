import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api } from '@/lib/api-client';

export type Group = { id: string; name: string; folded: boolean };

export function useGroups() {
  return useQuery({
    queryKey: ['groups'],
    queryFn: () => api<Group[]>('/groups'),
  });
}

function useGroupMutation<T, R = void>(request: (input: T) => Promise<R>) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: request,
    onSuccess: () =>
      Promise.all([
        queryClient.invalidateQueries({ queryKey: ['groups'] }),
        queryClient.invalidateQueries({ queryKey: ['bots'] }),
      ]),
  });
}

export const useCreateGroup = () =>
  useGroupMutation((name: string) => api<Group>('/groups', { method: 'POST', body: { name } }));

export const useUpdateGroup = () =>
  useGroupMutation(({ id, ...changes }: { id: string; name?: string; folded?: boolean }) =>
    api<void>(`/groups/${id}`, { method: 'PATCH', body: changes }),
  );

export const useMoveGroup = () =>
  useGroupMutation(({ id, up }: { id: string; up: boolean }) =>
    api<void>(`/groups/${id}/move`, { method: 'POST', body: { up } }),
  );

export const useDeleteGroup = () =>
  useGroupMutation((id: string) => api<void>(`/groups/${id}`, { method: 'DELETE' }));

export const useSetBotGroup = () =>
  useGroupMutation(({ botId, groupId }: { botId: string; groupId: string | null }) =>
    api<void>(`/bots/${botId}/group`, { method: 'PUT', body: { group_id: groupId } }),
  );
