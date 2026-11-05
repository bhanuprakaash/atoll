import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api } from '@/lib/api-client';

export type Skill = { id: string; name: string; content: string };

export function useSkills() {
  return useQuery({ queryKey: ['skills'], queryFn: () => api<Skill[]>('/skills') });
}

export function useSaveSkill(id?: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (skill: Omit<Skill, 'id'>) =>
      id
        ? api<Skill>(`/skills/${id}`, { method: 'PUT', body: skill })
        : api<Skill>('/skills', { method: 'POST', body: skill }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['skills'] }),
  });
}
