import { useEffect, useState } from 'react';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { api } from '@/lib/api-client';
import { Link } from 'react-router';
import { ChoiceList } from '@/modules/bots/components/choice-list';

type Connection = { id: string; instance_url: string; status: 'pending' | 'connected' };
type Flow = { id: string; user_code: string; verification_uri: string; interval_ms: number };

export function ConnectionList({
  connections,
  selected,
  onChange,
}: {
  connections: Connection[];
  selected: string[];
  onChange: (ids: string[]) => void;
}) {
  const [flow, setFlow] = useState<Flow | null>(null);
  const [error, setError] = useState('');
  const client = useQueryClient();
  const start = useMutation({
    mutationFn: (instance_url: string) =>
      api<Flow>('/connections/gitlab/start', { method: 'POST', body: { instance_url } }),
  });
  useEffect(() => {
    if (!flow) return;
    const timer = setInterval(() => {
      void api<{ status: string }>(`/connections/${flow.id}/poll`, { method: 'POST' })
        .then(({ status }) => {
          if (status !== 'connected') return;
          setFlow(null);
          void client.invalidateQueries({ queryKey: ['connections'] });
        })
        .catch((err: Error) => {
          setError(err.message);
          setFlow(null);
        });
    }, flow.interval_ms);
    return () => clearInterval(timer);
  }, [flow, client]);
  return (
    <>
      <ChoiceList
        title="Connections"
        singular="connection"
        choices={connections.map((c) => ({
          id: c.id,
          name: `GitLab · ${c.instance_url.replace(/^https?:\/\//, '')}`,
          brand: 'gitlab' as const,
        }))}
        selected={selected}
        onChange={onChange}
        empty={
          <>
            No connections yet.{' '}
            <Link to="/settings/connections" className="text-ink underline underline-offset-2">
              Connect GitLab in Settings
            </Link>
          </>
        }
      />
      {connections
        .filter((c) => selected.includes(c.id) && c.status !== 'connected')
        .map((c) => (
          <button
            key={c.id}
            type="button"
            className="self-start rounded-full border border-outline px-3 py-1 text-[13px] hover:bg-tile"
            disabled={start.isPending || !!flow}
            onClick={() => {
              setError('');
              start.mutate(c.instance_url, {
                onSuccess: (started) => {
                  onChange(selected.map((id) => (id === c.id ? started.id : id)));
                  setFlow(started);
                  void client.invalidateQueries({ queryKey: ['connections'] });
                },
              });
            }}
          >
            Connect GitLab · {c.instance_url}
          </button>
        ))}
      {flow && (
        <p className="rounded-xl bg-field p-3 text-[13px] text-muted">
          Open{' '}
          <a className="underline" href={flow.verification_uri} target="_blank" rel="noreferrer">
            {flow.verification_uri}
          </a>{' '}
          and enter <span className="font-mono">{flow.user_code}</span>. Waiting for approval…
        </p>
      )}
      {(error || start.isError) && (
        <p className="text-sm text-danger">{error || start.error?.message}</p>
      )}
    </>
  );
}
