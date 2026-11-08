import { useEffect, useRef, useState } from 'react';
import { useMatch } from 'react-router';
import { mrRef } from '@/modules/threads/utils/mr-ref';

type About = { thread_id: string; bot_id: string; bot_name: string; title: string };
type Notice<T> = About & { data: T };
export type ApprovalNotice = About & { id: string; tool: string; input: unknown };
export type EarlierNotice = About & { id: number; at: number; text: string; detail?: string };
export type Notifications = {
  needs: ApprovalNotice[];
  earlier: EarlierNotice[];
  drop?: ApprovalNotice;
  widened?: EarlierNotice;
  dismiss: () => void;
};

const widenedMs = 4000;

// One EventSource on /api/events for the app. The thread on screen is left out: the user sees it.
export function useNotifications(): Notifications {
  const onScreen = useMatch('/bots/:botId/threads/:threadId')?.params.threadId;
  const screen = useRef(onScreen);
  screen.current = onScreen;
  const seq = useRef(0);
  const [approvals, setApprovals] = useState<ApprovalNotice[]>([]);
  const [earlier, setEarlier] = useState<EarlierNotice[]>([]);
  const [widened, setWidened] = useState<EarlierNotice>();
  const [dismissed, setDismissed] = useState<string[]>([]);
  useEffect(() => {
    const source = new EventSource('/api/events');
    let timer: number | undefined;
    const on = <T>(type: string, handle: (notice: Notice<T>) => void) =>
      source.addEventListener(type, (e) => handle(JSON.parse((e as MessageEvent<string>).data)));
    const add = (notice: About, text: string, detail?: string) => {
      if (notice.thread_id === screen.current) return undefined;
      const item = { ...notice, id: seq.current++, at: Date.now(), text, detail };
      setEarlier((prev) => [item, ...prev]);
      return item;
    };
    source.onopen = () => setApprovals([]);
    on<{ id: string; tool: string; input: unknown }>('approval', ({ data, ...about }) =>
      setApprovals((prev) =>
        prev.some((a) => a.id === data.id) ? prev : [{ ...about, ...data }, ...prev],
      ),
    );
    on<{ id: string }>('approval_closed', ({ data }) =>
      setApprovals((prev) => prev.filter((a) => a.id !== data.id)),
    );
    on<unknown>('run_finished', (notice) => {
      const item = add(notice, `${notice.bot_name} finished ${notice.title}`);
      if (!item) return;
      setWidened(item);
      window.clearTimeout(timer);
      timer = window.setTimeout(() => setWidened(undefined), widenedMs);
    });
    on<{ url: string; state: string; title: string }>('mr_updated', (notice) => {
      if (notice.data.state === 'opened')
        add(
          notice,
          `${notice.bot_name} opened ${mrRef(notice.data.url) ?? 'a merge request'}`,
          notice.data.title,
        );
    });
    on<{ change: { entity: string; action: string; after: { name?: string } | null } }>(
      'platform_changed',
      (notice) => {
        const { entity, action, after } = notice.data.change;
        const verb = action === 'edit' ? 'edited' : entity === 'repo' ? 'registered' : 'created';
        const what = entity === 'bot' ? 'Bot' : entity;
        add(notice, `${notice.bot_name} ${verb} ${what} ${after?.name ?? ''}`.trim());
      },
    );
    return () => {
      source.close();
      window.clearTimeout(timer);
    };
  }, []);
  const needs = approvals.filter((a) => a.thread_id !== onScreen);
  return {
    needs,
    earlier,
    drop: needs.find((a) => !dismissed.includes(a.id)),
    widened: widened?.thread_id === onScreen ? undefined : widened,
    dismiss: () => setDismissed((prev) => [...prev, ...needs.map((a) => a.id)]),
  };
}
