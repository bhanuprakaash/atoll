import { useQuery } from '@tanstack/react-query';
import { api } from '@/lib/api-client';

export type ThreadFile = {
  path?: string;
  text?: string;
  branch?: string;
  commit?: string;
  matches?: string[];
};

// useThreadFile reads a file of the thread's checkout as it is now: when the panel opens and on
// Reload only.
export function useThreadFile(threadId: string, path: string, enabled: boolean) {
  return useQuery({
    queryKey: ['thread-file', threadId, path],
    queryFn: () => api<ThreadFile>(`/threads/${threadId}/file?path=${encodeURIComponent(path)}`),
    enabled,
    retry: false,
    staleTime: Infinity,
    refetchOnWindowFocus: false,
    refetchOnReconnect: false,
  });
}
