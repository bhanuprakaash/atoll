import { useEffect, useRef, useState } from 'react';
import { useQueryClient } from '@tanstack/react-query';
import type { ToolKind } from '@/components/creature';
import { getRun } from '@/modules/threads/api/runs';
import { getThread } from '@/modules/threads/api/threads';

export type LiveRun = {
  text: string;
  tool?: { kind: ToolKind; target: string };
  approvals: { id: string; tool: string; input: unknown }[];
  finished?: { status: string; error: string; blocked?: string };
};
type Data<T> = T & { run_id: string };
const empty: LiveRun = { text: '', approvals: [] };
const withApproval = (run: LiveRun, a: { id: string; tool: string; input: unknown }): LiveRun =>
  run.approvals.some((x) => x.id === a.id) ? run : { ...run, approvals: [...run.approvals, a] };

export function useThreadEvents(threadId: string) {
  const queryClient = useQueryClient();
  const [runs, setRuns] = useState<Record<string, LiveRun>>({});
  const [connected, setConnected] = useState(true);
  const [mr, setMr] = useState<{ url: string; state: string } | null>(null);
  const [worktreeRemoved, setWorktreeRemoved] = useState(false);
  const runsRef = useRef(runs);
  runsRef.current = runs;
  useEffect(() => {
    let reconnecting = false;
    const source = new EventSource(`/api/threads/${threadId}/events`);
    const refresh = () => {
      for (const queryKey of [
        ['messages', threadId],
        ['thread', threadId],
        ['threads'],
        ['bots'],
        ['run'],
        ['memory'],
        ['changes', threadId],
      ])
        void queryClient.invalidateQueries({ queryKey });
    };
    const on = <T>(type: string, change: (data: Data<T>, run: LiveRun) => LiveRun) =>
      source.addEventListener(type, (e) => {
        const data: Data<T> = JSON.parse((e as MessageEvent<string>).data);
        setRuns((prev) => ({ ...prev, [data.run_id]: change(data, prev[data.run_id] ?? empty) }));
      });
    source.onopen = () => {
      setConnected(true);
      refresh();
      if (reconnecting) {
        for (const [id, run] of Object.entries(runsRef.current)) {
          if (!run.finished)
            void getRun(id)
              .then((result) => {
                if (result.status !== 'running' && result.status !== 'waiting')
                  setRuns((prev) => ({
                    ...prev,
                    [id]: { ...prev[id], finished: { status: result.status, error: result.error } },
                  }));
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
              [active_run.id]: (approvals ?? []).reduce(withApproval, prev[active_run.id] ?? empty),
            }));
        })
        .catch(() => {});
    };
    source.onerror = () => {
      reconnecting = true;
      setConnected(false);
    };
    on<{ text: string }>('text', (d, run) => ({ ...run, text: run.text + d.text }));
    on<{ kind: ToolKind; target: string }>('tool_call', (d, run) => ({
      ...run,
      tool: { kind: d.kind, target: d.target },
    }));
    on<{ id: string; tool: string; input: unknown }>('approval', (d, run) => withApproval(run, d));
    on<{ status: string; error: string; blocked?: string }>('run_finished', (d, run) => ({
      ...run,
      finished: d,
    }));
    source.addEventListener('run_finished', refresh);
    for (const type of ['platform_changed', 'memory_saved', 'worktree_created'])
      source.addEventListener(type, refresh);
    source.addEventListener('mr_updated', (e) =>
      setMr(JSON.parse((e as MessageEvent<string>).data)),
    );
    source.addEventListener('worktree_removed', () => setWorktreeRemoved(true));
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
