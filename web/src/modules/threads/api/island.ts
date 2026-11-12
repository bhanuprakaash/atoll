import { useMutation } from '@tanstack/react-query';
import { api } from '@/lib/api-client';

export function useMarkIslandSeen() {
  return useMutation({ mutationFn: () => api<void>('/island/seen', { method: 'POST' }) });
}
