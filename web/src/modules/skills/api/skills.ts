import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api } from '@/lib/api-client';

export type SkillFile = { path: string; content: string; executable: boolean };

export type Skill = {
  id: string;
  name: string;
  content: string;
  source: string;
  files: SkillFile[];
};

export type Reimport = {
  status: 'created' | 'replaced' | 'unchanged' | 'confirm';
  added: string[];
  changed: string[];
  removed: string[];
  warning?: string;
};

export function useSkills() {
  return useQuery({ queryKey: ['skills'], queryFn: () => api<Skill[]>('/skills') });
}

export function useSaveSkill(id?: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (skill: Pick<Skill, 'name' | 'content' | 'files'>) =>
      id
        ? api<Skill>(`/skills/${id}`, { method: 'PUT', body: skill })
        : api<Skill>('/skills', { method: 'POST', body: skill }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['skills'] }),
  });
}

export function useReimportSkill(id: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (replace: boolean) =>
      api<Reimport>(`/skills/${id}/reimport`, { method: 'POST', body: { replace } }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['skills'] }),
  });
}
