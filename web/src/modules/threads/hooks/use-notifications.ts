import { useCallback, useEffect, useRef, useState } from 'react';
import { useMatch } from 'react-router';
import { useDismissEarlier, useMarkIslandSeen } from '@/modules/threads/api/island';
import { mrRef } from '@/modules/threads/utils/mr-ref';

type About = {
  thread_id: string;
  parent_thread_id: string;
  bot_id: string;
  bot_name: string;
  title: string;
};
type Notice<T> = About & { data: T };
export type ApprovalNotice = About & { id: string; tool: string; input: unknown };
export type EarlierNotice = About & {
  id: number;
  at: number;
  text: string;
  detail?: string;
  type: string;
  target: string;
};
export type Notifications = {
  needs: ApprovalNotice[];
  earlier: EarlierNotice[];
  drop?: ApprovalNotice;
  widened?: EarlierNotice;
  dismiss: () => void;
  markSeen: () => void;
  dismissEarlier: (notice: EarlierNotice) => void;
  markAllRead: () => void;
  markAllReadError: unknown;
};

type MrUpdate = { url: string; state: string; title: string };
type Change = {
  change: { id: string; entity: string; action: string; after: { name?: string } | null };
};
type Remembered = About & {
  type: 'run_finished' | 'mr_updated' | 'platform_changed';
  at: string;
  data: { status: string } & MrUpdate & Change;
};

const widenedMs = 4000;

const finishedText = (notice: About, status: string) =>
  `${notice.bot_name} ${status === 'failed' ? 'failed' : 'finished'} ${notice.title}`;

const mrText = (notice: About, url: string) =>
  `${notice.bot_name} opened ${mrRef(url) ?? 'a merge request'}`;

function changeText(notice: About, { entity, action, after }: Change['change']) {
  const verb = action === 'edit' ? 'edited' : entity === 'repo' ? 'registered' : 'created';
  const what = entity === 'bot' ? 'Bot' : entity;
  return `${notice.bot_name} ${verb} ${what} ${after?.name ?? ''}`.trim();
}

const isRun = (n: { type: string; thread_id: string }, threadId: string) =>
  n.type === 'run_finished' && n.thread_id === threadId;

const isOn = (notice: About, threadId?: string) =>
  notice.thread_id === threadId || notice.parent_thread_id === threadId;

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
    const add = (notice: About, type: string, target: string, text: string, detail?: string) => {
      if (isOn(notice, screen.current)) return undefined;
      const item = { ...notice, id: seq.current++, at: Date.now(), text, detail, type, target };
      setEarlier((prev) => [
        item,
        ...prev.filter((n) => type !== 'run_finished' || !isRun(n, notice.thread_id)),
      ]);
      return item;
    };
    source.onopen = () => setApprovals([]);
    source.addEventListener('earlier', (e) => {
      const items: Remembered[] = JSON.parse((e as MessageEvent<string>).data);
      setEarlier(
        items
          .filter((n) => !isOn(n, screen.current))
          .map((n) => ({ ...n, at: Date.parse(n.at) }))
          .flatMap((n) => {
            const id = seq.current++;
            if (n.type === 'run_finished')
              return [{ ...n, id, target: n.thread_id, text: finishedText(n, n.data.status) }];
            if (n.type === 'mr_updated')
              return [
                {
                  ...n,
                  id,
                  target: n.thread_id,
                  text: mrText(n, n.data.url),
                  detail: n.data.title,
                },
              ];
            if (n.data.change.entity === 'allow') return [];
            return [{ ...n, id, target: n.data.change.id, text: changeText(n, n.data.change) }];
          }),
      );
    });
    on<{ id: string; tool: string; input: unknown }>('approval', ({ data, ...about }) =>
      setApprovals((prev) =>
        prev.some((a) => a.id === data.id) ? prev : [{ ...about, ...data }, ...prev],
      ),
    );
    on<unknown>('thread_read', ({ thread_id }) =>
      setEarlier((prev) => prev.filter((n) => !isRun(n, thread_id))),
    );
    on<{ id: string }>('approval_closed', ({ data }) =>
      setApprovals((prev) => prev.filter((a) => a.id !== data.id)),
    );
    on<{ status: string }>('run_finished', (notice) => {
      const item = add(
        notice,
        'run_finished',
        notice.thread_id,
        finishedText(notice, notice.data.status),
      );
      if (!item) return;
      setWidened(item);
      window.clearTimeout(timer);
      timer = window.setTimeout(() => setWidened(undefined), widenedMs);
    });
    on<MrUpdate>('mr_updated', (notice) => {
      if (notice.data.state === 'opened')
        add(
          notice,
          'mr_updated',
          notice.thread_id,
          mrText(notice, notice.data.url),
          notice.data.title,
        );
    });
    on<Change>('platform_changed', (notice) => {
      if (notice.data.change.entity !== 'allow')
        add(
          notice,
          'platform_changed',
          notice.data.change.id,
          changeText(notice, notice.data.change),
        );
    });
    return () => {
      source.close();
      window.clearTimeout(timer);
    };
  }, []);
  const needs = approvals.filter((a) => a.thread_id !== onScreen);
  const shown = useRef(needs);
  shown.current = needs;
  const dismiss = useCallback(
    () => setDismissed((prev) => [...prev, ...shown.current.map((a) => a.id)]),
    [],
  );
  const { mutate: markSeen } = useMarkIslandSeen();
  const { mutate: postAllRead, error: markAllReadError } = useMarkIslandSeen();
  const markAllRead = useCallback(() => {
    setEarlier([]);
    postAllRead();
  }, [postAllRead]);
  const { mutate: postDismiss } = useDismissEarlier();
  const dismissEarlier = useCallback(
    (notice: EarlierNotice) => {
      setEarlier((prev) => prev.filter((n) => n.id !== notice.id));
      postDismiss({ type: notice.type, id: notice.target });
    },
    [postDismiss],
  );
  return {
    needs,
    earlier,
    drop: needs.find((a) => a.parent_thread_id !== onScreen && !dismissed.includes(a.id)),
    widened: widened && !isOn(widened, onScreen) ? widened : undefined,
    dismiss,
    markSeen,
    dismissEarlier,
    markAllRead,
    markAllReadError,
  };
}
