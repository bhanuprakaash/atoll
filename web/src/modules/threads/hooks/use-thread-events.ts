import { useEffect, useRef, useState } from 'react';
import { useQueryClient } from '@tanstack/react-query';
import { getRun } from '@/modules/threads/api/runs';
import { getThread } from '@/modules/threads/api/threads';

export type LiveRun = {
  text: string;
  tools: { id: string; name: string }[];
  approvals: { id: string; tool: string; input: unknown }[];
  finished?: { status: string; error: string };
};

type Data<T> = T & { run_id: string };

const empty: LiveRun = { text: '', tools: [], approvals: [] };

const withApproval = (run: LiveRun, a: { id: string; tool: string; input: unknown }): LiveRun =>
  run.approvals.some((x) => x.id === a.id)
    ? run
    : { ...run, approvals: [...run.approvals, { id: a.id, tool: a.tool, input: a.input }] };

export function useThreadEvents(threadId: string) {
  const queryClient = useQueryClient();
  const [runs, setRuns] = useState<Record<string, LiveRun>>({});
  const [connected, setConnected] = useState(false);
  const [mr, setMr] = useState<{ url: string; state: string } | null>(null);
  const [worktreeRemoved, setWorktreeRemoved] = useState(false);
  const runsRef = useRef(runs);
  useEffect(() => {
    runsRef.current = runs;
  });

  const update = (runId: string, change: (run: LiveRun) => LiveRun) =>
    setRuns((prev) => ({ ...prev, [runId]: change(prev[runId] ?? empty) }));

  useEffect(() => {
    const source = new EventSource(`/api/threads/${threadId}/events`);
    const on = <T>(type: string, change: (data: Data<T>, run: LiveRun) => LiveRun) =>
      source.addEventListener(type, (e) => {
        const data: Data<T> = JSON.parse((e as MessageEvent<string>).data);
        setRuns((prev) => ({ ...prev, [data.run_id]: change(data, prev[data.run_id] ?? empty) }));
      });

    source.onopen = () => {
      setConnected(true);
      void getThread(threadId).then(({ active_run, approvals }) => {
        if (!active_run) return;
        setRuns((prev) => ({
          ...prev,
          [active_run.id]: approvals.reduce(withApproval, prev[active_run.id] ?? empty),
        }));
      });
      for (const [runId, run] of Object.entries(runsRef.current)) {
        if (run.finished) continue;
        void getRun(runId).then((ended) => {
          if (ended.status !== 'done' && ended.status !== 'failed') return;
          setRuns((prev) => ({
            ...prev,
            [runId]: {
              ...(prev[runId] ?? empty),
              finished: { status: ended.status, error: ended.error },
            },
          }));
          void queryClient.invalidateQueries({ queryKey: ['messages', threadId] });
        });
      }
    };
    source.onerror = () => setConnected(false);
    on<{ text: string }>('text', (d, run) => ({ ...run, text: run.text + d.text }));
    on<{ id: string; name: string }>('tool_call', (d, run) => ({
      ...run,
      tools: [...run.tools, { id: d.id, name: d.name }],
    }));
    on<{ id: string; tool: string; input: unknown }>('approval', (d, run) => withApproval(run, d));
    on<{ status: string; error: string }>('run_finished', (d, run) => ({
      ...run,
      finished: { status: d.status, error: d.error },
    }));
    source.addEventListener('run_finished', () => {
      void queryClient.invalidateQueries({ queryKey: ['messages', threadId] });
      void queryClient.invalidateQueries({ queryKey: ['memory'] });
    });
    source.addEventListener('memory_saved', () => {
      void queryClient.invalidateQueries({ queryKey: ['memory'] });
    });
    source.addEventListener('worktree_created', () => {
      void queryClient.invalidateQueries({ queryKey: ['thread', threadId] });
    });
    source.addEventListener('mr_updated', (e) => {
      setMr(JSON.parse((e as MessageEvent<string>).data));
    });
    source.addEventListener('worktree_removed', () => setWorktreeRemoved(true));
    return () => source.close();
  }, [threadId, queryClient]);

  return {
    runs,
    connected,
    mr,
    worktreeRemoved,
    running: Object.values(runs).some((run) => !run.finished),
    begin: (runId: string) => update(runId, (run) => run),
  };
}
