import { useEffect, useState } from 'react';
import { useQueryClient } from '@tanstack/react-query';
import { ErrorNotice } from '@/components/error-notice';
import {
  pollConnection,
  useSignInAgain,
  type Connection,
  type DeviceFlow,
} from '@/modules/connections/api/connections';

export function ReconnectButton({ connection }: { connection: Connection }) {
  const [flow, setFlow] = useState<DeviceFlow | null>(null);
  const [error, setError] = useState<unknown>(null);
  const client = useQueryClient();
  const signInAgain = useSignInAgain();
  useEffect(() => {
    if (!flow) return;
    const timer = setInterval(() => {
      pollConnection(flow.id).then(
        ({ status }) => {
          if (status !== 'connected') return;
          setFlow(null);
          void client.invalidateQueries({ queryKey: ['connections'] });
        },
        (err: unknown) => {
          setError(err);
          setFlow(null);
          void client.invalidateQueries({ queryKey: ['connections'] });
        },
      );
    }, flow.interval_ms);
    return () => clearInterval(timer);
  }, [flow, client]);
  const start = () => {
    setError(null);
    signInAgain.mutate(connection, {
      onSuccess: (started) => {
        setFlow(started);
        void client.invalidateQueries({ queryKey: ['connections'] });
      },
    });
  };
  return (
    <>
      <button
        type="button"
        className="self-start rounded-full border border-outline px-3 py-1 text-[13px] hover:bg-tile"
        disabled={signInAgain.isPending || !!flow}
        onClick={start}
      >
        Connect GitLab · {connection.instance_url}
      </button>
      {flow && (
        <p className="rounded-xl bg-field p-3 text-[13px] text-muted">
          Open{' '}
          <a className="underline" href={flow.verification_uri} target="_blank" rel="noreferrer">
            {flow.verification_uri}
          </a>{' '}
          and enter <span className="font-mono">{flow.user_code}</span>. Waiting for approval…
        </p>
      )}
      {(!!error || signInAgain.isError) && (
        <ErrorNotice error={error ?? signInAgain.error} signIn={start} retry={start} />
      )}
    </>
  );
}
