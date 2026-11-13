import { useMutation } from '@tanstack/react-query';
import { api } from '@/lib/api-client';

export function useMarkIslandSeen() {
  return useMutation({
    mutationFn: () => api<void>('/island/seen', { method: 'POST', keepalive: true }),
  });
}

export type Dismissal = { type: string; id: string };

export function useDismissEarlier() {
  return useMutation({
    mutationFn: (body: Dismissal) => api<void>('/island/dismiss', { method: 'POST', body }),
  });
}
