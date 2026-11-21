import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api } from '@/lib/api-client';

export type PullRequestDetail = {
  url: string;
  repo: string;
  number: number;
  title: string;
  author: string;
  base: string;
  head: string;
  head_sha: string;
  state: 'open' | 'draft' | 'merged' | 'closed';
  body: string;
  checks: { name: string; result: 'passed' | 'failed' | 'pending' }[];
  files: { path: string; additions: number; deletions: number; patch?: string }[];
  merge_method: 'merge' | 'squash' | 'rebase' | '';
  mergeable: boolean | null;
};

// usePullRequest reads a GitHub pull request through Reef with the thread Bot's connection. It
// reads when the panel opens and on Reload only: no polling.
export function usePullRequest(threadId: string, url: string) {
  return useQuery({
    queryKey: ['pull-request', threadId, url],
    queryFn: () => api<PullRequestDetail>(`/threads/${threadId}/pr?url=${encodeURIComponent(url)}`),
    retry: false,
    refetchOnWindowFocus: false,
    refetchOnReconnect: false,
  });
}

// useMergePR merges at the head sha the panel loaded, then reads the PR and the thread again.
export function useMergePR(threadId: string, url: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (sha: string) =>
      api<{ state: 'merged' }>(`/threads/${threadId}/pr/merge`, {
        method: 'POST',
        body: { url, sha },
      }),
    onSuccess: () =>
      Promise.all(
        [['pull-request', threadId, url], ['thread', threadId], ['bots']].map((queryKey) =>
          queryClient.invalidateQueries({ queryKey }),
        ),
      ),
  });
}
