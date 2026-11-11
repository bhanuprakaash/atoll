import {
  useInfiniteQuery,
  useMutation,
  useQueryClient,
  type InfiniteData,
} from '@tanstack/react-query';
import { api, apiBlob } from '@/lib/api-client';

export type Message = {
  id: string;
  role: 'user' | 'bot';
  text: string;
  run_id: string;
  from_bot_id?: string;
  kind?: string;
  created_at: string;
  attachments: { id: string; mime: string }[];
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
  report?: { blocked: string } | null;
  error: string;
  error_kind: '' | 'signed_out';
  provider: 'claude' | 'codex';
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
      api<{ message: Message; run: Run }>(`/threads/${threadId}/messages`, {
        method: 'POST',
        body: input,
      }),
    onSuccess: () =>
      Promise.all(
        [['messages', threadId], ['thread', threadId], ['bots']].map((queryKey) =>
          queryClient.invalidateQueries({ queryKey }),
        ),
      ),
  });
}
