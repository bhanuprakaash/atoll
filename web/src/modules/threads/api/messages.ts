import {
  useInfiniteQuery,
  useMutation,
  useQueryClient,
  type InfiniteData,
} from '@tanstack/react-query';
import { api, apiBlob } from '@/lib/api-client';
import type { RunUsage } from '@/utils/usage';

export type Message = {
  id: string;
  role: 'user' | 'bot';
  text: string;
  run_id: string;
  from_bot_id?: string;
  kind?: string;
  created_at: string;
  queued?: boolean;
  attachments: { id: string; mime: string }[];
  not_read: boolean;
  answered_approvals?: { id: string; tool: string; input: unknown }[];
};

export const fetchAttachment = (id: string) => apiBlob(`/attachments/${id}`);

export type ImageInput = { mime: string; data: string };

export type Run = {
  id: string;
  thread_id: string;
  status: 'running' | 'waiting' | 'done' | 'failed' | 'stopped';
  started_at: string;
  finished_at: string;
  tools: Record<string, number>;
  report?: { done: string; blocked: string } | null;
  reply: string;
  error: string;
  error_detail: string;
  error_kind:
    '' | 'signed_out' | 'secret_store' | 'rate_limited' | 'provider_off' | 'needs_sign_in';
  resets_at: string;
  provider: 'claude' | 'codex';
  usage?: RunUsage | null;
};

type MessagePageParam = { before?: string; after?: string };

export function useMessages(threadId: string) {
  const queryClient = useQueryClient();
  const query = useInfiniteQuery<
    Message[],
    Error,
    InfiniteData<Message[], MessagePageParam>,
    [string, string],
    MessagePageParam
  >({
    queryKey: ['messages', threadId],
    initialPageParam: {},
    queryFn: ({ pageParam }) =>
      api<Message[]>(
        `/threads/${threadId}/messages${
          pageParam.before
            ? `?before=${encodeURIComponent(pageParam.before)}`
            : pageParam.after
              ? `?after=${encodeURIComponent(pageParam.after)}`
              : ''
        }`,
      ),
    getPreviousPageParam: (page) =>
      page.length === 50 && page[0] ? { before: page[0].id } : undefined,
    getNextPageParam: (page, _pages, pageParam) => {
      const last = page.at(-1);
      return last && (page.length === 50 || pageParam.before) ? { after: last.id } : undefined;
    },
  });
  // An empty older page would start every refetch and cut off the pages after it.
  const fetchOlder = async () => {
    const { data } = await query.fetchPreviousPage();
    if (data?.pages[0]?.length !== 0) return true;
    queryClient.setQueryData<InfiniteData<Message[], MessagePageParam>>(
      ['messages', threadId],
      (d) => d && { pages: d.pages.slice(1), pageParams: d.pageParams.slice(1) },
    );
    return false;
  };
  return { ...query, fetchOlder };
}

export function useSendMessage(threadId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (input: { text: string; images?: ImageInput[] }) =>
      api<{ message: Message; run: Run | null; answered?: string[] }>(
        `/threads/${threadId}/messages`,
        {
          method: 'POST',
          body: input,
        },
      ),
    onSuccess: () =>
      Promise.all(
        [
          ['messages', threadId],
          ['thread', threadId],
        ].map((queryKey) => queryClient.invalidateQueries({ queryKey })),
      ),
  });
}

type QueuedChange = { id: string; text?: string; send?: boolean };

// useChangeQueued edits, removes or sends now a message that waits for the thread's run to end.
export function useChangeQueued(threadId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async ({ id, text, send }: QueuedChange) => {
      const path = `/threads/${threadId}/messages/${id}`;
      if (send) await api(`${path}/send`, { method: 'POST' });
      else if (text !== undefined) await api(path, { method: 'PATCH', body: { text } });
      else await api(path, { method: 'DELETE' });
    },
    onSettled: () =>
      Promise.all(
        [
          ['messages', threadId],
          ['thread', threadId],
        ].map((queryKey) => queryClient.invalidateQueries({ queryKey })),
      ),
  });
}
