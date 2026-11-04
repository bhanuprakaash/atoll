import { useEffect, useState, type FormEvent } from 'react';
import { useQueryClient } from '@tanstack/react-query';
import { Button } from '@/components/button';
import { controlClass, Field } from '@/components/field';
import {
  pollConnection,
  useConnections,
  useStartGitlab,
  type DeviceFlow,
} from '@/modules/connections/api/connections';

export function ConnectionsSettings() {
  const queryClient = useQueryClient();
  const [instance, setInstance] = useState('https://gitlab.com');
  const [flow, setFlow] = useState<DeviceFlow | null>(null);
  const [pollError, setPollError] = useState('');
  const { data: connections = [] } = useConnections();
  const start = useStartGitlab();

  useEffect(() => {
    if (!flow) return;
    const timer = setInterval(() => {
      pollConnection(flow.id).then(
        ({ status }) => {
          if (status !== 'connected') return;
          setFlow(null);
          void queryClient.invalidateQueries({ queryKey: ['connections'] });
        },
        (err: Error) => {
          setFlow(null);
          setPollError(err.message);
        },
      );
    }, flow.interval_ms);
    return () => clearInterval(timer);
  }, [flow, queryClient]);

  const submit = (e: FormEvent) => {
    e.preventDefault();
    setPollError('');
    start.mutate(instance, {
      onSuccess: (started) => {
        setFlow(started);
        void queryClient.invalidateQueries({ queryKey: ['connections'] });
      },
    });
  };

  return (
    <div className="flex max-w-md flex-col gap-6 p-6">
      <form onSubmit={submit} className="flex flex-col gap-4">
        <Field label="GitLab instance URL">
          {(id) => (
            <input
              id={id}
              className={controlClass}
              value={instance}
              onChange={(e) => setInstance(e.target.value)}
              required
            />
          )}
        </Field>
        {start.isError && <p className="text-sm text-danger">{start.error.message}</p>}
        {pollError && <p className="text-sm text-danger">{pollError}</p>}
        <Button type="submit" disabled={start.isPending || !!flow} className="self-start">
          Sign in to GitLab
        </Button>
      </form>
      {flow && (
        <div className="flex flex-col gap-1 rounded-card border border-line bg-card p-3 text-sm">
          <p>
            Open{' '}
            <a
              href={flow.verification_uri}
              target="_blank"
              rel="noreferrer"
              className="text-accent underline"
            >
              {flow.verification_uri}
            </a>{' '}
            and enter this code:
          </p>
          <p className="font-mono text-lg font-medium">{flow.user_code}</p>
          <p className="text-muted">Waiting for you to approve...</p>
        </div>
      )}
      <ul className="flex flex-col gap-2">
        {connections.map((c) => (
          <li
            key={c.id}
            className="flex justify-between rounded-card border border-line bg-card px-3 py-2 text-sm"
          >
            <span>{c.instance_url}</span>
            <span className="text-muted">{c.status}</span>
          </li>
        ))}
      </ul>
    </div>
  );
}
