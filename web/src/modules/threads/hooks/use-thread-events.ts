import { useEffect, useRef, useState } from 'react';
import { useQueryClient } from '@tanstack/react-query';
import type { ToolKind } from '@/components/creature';
import { setLost } from '@/lib/api-client';
import { getRun } from '@/modules/threads/api/runs';
import { getThread, markOpened } from '@/modules/threads/api/threads';
import { toolLine } from '@/modules/threads/utils/tool-line';

export type LivePart = { text: string } | { step: string };
export type LiveRun = {
  parts: LivePart[];
  tool?: { kind: ToolKind; target: string; name: string; verb: string };
  approvals: { id: string; tool: string; input: unknown; allow_in_thread?: boolean }[];
  finished?: { status: string; error: string; blocked?: string };
  settled?: boolean;
};
type Data<T> = T & { run_id: string };
const empty: LiveRun = { parts: [], approvals: [] };
const withApproval = (run: LiveRun, a: LiveRun['approvals'][number]): LiveRun =>
  run.approvals.some((x) => x.id === a.id) ? run : { ...run, approvals: [...run.approvals, a] };

export function useThreadEvents(threadId: string) {
  const queryClient = useQueryClient();
  const [runs, setRuns] = useState<Record<string, LiveRun>>({});
  const [connected, setConnected] = useState(true);
  const [mr, setMr] = useState<{ url: string; state: string; title: string } | null>(null);
  const [worktreeRemoved, setWorktreeRemoved] = useState(false);
  const runsRef = useRef(runs);
  runsRef.current = runs;
  const closedApprovals = useRef(new Set<string>());
  useEffect(() => {
    setMr(null);
    setWorktreeRemoved(false);
    setRuns({});
    closedApprovals.current.clear();
    let reconnecting = false;
    const opened = () =>
      markOpened(threadId)
        .then(() => queryClient.invalidateQueries({ queryKey: ['bots'] }))
        .catch(() => {});
    void opened();
    const source = new EventSource(`/api/threads/${threadId}/events`);
    const refresh = () => {
      // Invalidating reuses a first read still on its way, which may say the run is running.
      void queryClient
        .cancelQueries({ queryKey: ['run'] })
        .then(() => queryClient.invalidateQueries({ queryKey: ['run'] }));
      const [messages] = [
        ['messages', threadId],
        ['thread', threadId],
        ['threads'],
        ['bots'],
        ['memory'],
        ['changes', threadId],
      ].map((queryKey) => queryClient.invalidateQueries({ queryKey }));
      return messages;
    };
    const settle = (id: string, refreshed: Promise<void>) =>
      refreshed.finally(() =>
        setRuns((prev) => (prev[id] ? { ...prev, [id]: { ...prev[id], settled: true } } : prev)),
      );
    const on = <T>(type: string, change: (data: Data<T>, run: LiveRun) => LiveRun) =>
      source.addEventListener(type, (e) => {
        const data: Data<T> = JSON.parse((e as MessageEvent<string>).data);
        setRuns((prev) => ({ ...prev, [data.run_id]: change(data, prev[data.run_id] ?? empty) }));
      });
    source.onopen = () => {
      setConnected(true);
      setLost(false);
      const refreshed = refresh();
      if (reconnecting) {
        setRuns((prev) =>
          Object.fromEntries(Object.entries(prev).map(([id, run]) => [id, { ...run, parts: [] }])),
        );
        for (const [id, run] of Object.entries(runsRef.current)) {
          if (!run.finished)
            void getRun(id)
              .then((result) => {
                if (result.status !== 'running' && result.status !== 'waiting') {
                  setRuns((prev) => ({
                    ...prev,
                    [id]: { ...prev[id], finished: { status: result.status, error: result.error } },
                  }));
                  void settle(id, refreshed);
                }
              })
              .catch(() => {});
        }
      }
      reconnecting = false;
      void getThread(threadId)
        .then(({ active_run, approvals }) => {
          if (active_run)
            setRuns((prev) => ({
              ...prev,
              [active_run.id]: (approvals ?? [])
                .filter((a) => !closedApprovals.current.has(a.id))
                .reduce(withApproval, prev[active_run.id] ?? empty),
            }));
        })
        .catch(() => {});
    };
    source.onerror = () => {
      reconnecting = true;
      setConnected(false);
      if (source.readyState === EventSource.CONNECTING) setLost(true);
    };
    on<{ text: string }>('text', (d, run) => {
      const last = run.parts.at(-1);
      return {
        ...run,
        parts:
          last && 'text' in last
            ? [...run.parts.slice(0, -1), { text: last.text + d.text }]
            : [...run.parts, { text: d.text }],
      };
    });
    on<{ kind: ToolKind; target: string; name: string; verb: string }>('tool_call', (d, run) => {
      const tool = { kind: d.kind, target: d.target, name: d.name, verb: d.verb };
      return {
        ...run,
        tool,
        parts: [...run.parts, { step: d.name.endsWith('finish_run') ? '' : toolLine(tool) }],
      };
    });
    on<LiveRun['approvals'][number]>('approval', (d, run) => withApproval(run, d));
    on<{ id: string }>('approval_closed', (d, run) => {
      closedApprovals.current.add(d.id);
      return { ...run, approvals: run.approvals.filter((x) => x.id !== d.id) };
    });
    on<{ status: string; error: string; blocked?: string }>('run_finished', (d, run) => ({
      ...run,
      finished: d,
    }));
    source.addEventListener('run_finished', (e) => {
      void settle((JSON.parse((e as MessageEvent<string>).data) as Data<object>).run_id, refresh());
      void opened();
    });
    for (const type of ['platform_changed', 'memory_saved', 'worktree_created', 'helper_started'])
      source.addEventListener(type, refresh);
    source.addEventListener('mr_updated', (e) => {
      setMr(JSON.parse((e as MessageEvent<string>).data));
      refresh();
    });
    source.addEventListener('worktree_removed', () => {
      setWorktreeRemoved(true);
      refresh();
    });
    return () => source.close();
  }, [threadId, queryClient]);
  return {
    runs,
    connected,
    mr,
    worktreeRemoved,
    begin: (id: string) => setRuns((prev) => ({ ...prev, [id]: prev[id] ?? empty })),
  };
}
