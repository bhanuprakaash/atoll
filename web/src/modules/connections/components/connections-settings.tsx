import { useEffect, useState, type FormEvent } from 'react';
import { useQueryClient } from '@tanstack/react-query';
import { BrandLogo } from '@/components/brand-logo';
import type { CreatureLook } from '@/components/creature';
import {
  Box,
  RowText,
  rowClass,
  SectionHead,
  solidButton,
  Tag,
  Tile,
  UsedBy,
} from '@/components/settings-ui';
import { TextField } from '@/components/text-field';
import {
  pollConnection,
  useConnections,
  useStartGitlab,
  type DeviceFlow,
} from '@/modules/connections/api/connections';

type Bot = { id: string; name: string; look: CreatureLook; connections: string[] };

export function ConnectionsSettings({ bots = [] }: { bots?: Bot[] }) {
  const queryClient = useQueryClient();
  const [instance, setInstance] = useState('https://gitlab.com');
  const [adding, setAdding] = useState(false);
  const [flow, setFlow] = useState<DeviceFlow | null>(null);
  const [pollError, setPollError] = useState('');
  const { data: connections = [] } = useConnections();
  const start = useStartGitlab();
  const showForm = adding || !!flow || !connections.some((c) => c.status === 'connected');

  useEffect(() => {
    if (!flow) return;
    const timer = setInterval(() => {
      pollConnection(flow.id).then(
        ({ status }) => {
          if (status !== 'connected') return;
          setFlow(null);
          setAdding(false);
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
    <>
      <SectionHead title="Connections" lead="Sign in once. Each Bot picks which ones it gets." />
      <Box>
        {connections.map((c) => {
          const users = bots.filter((b) => b.connections?.includes(c.id));
          return (
            <div key={c.id} className={rowClass}>
              <Tile className="bg-gitlab-bg">
                <BrandLogo brand="gitlab" className="size-[22px]" />
              </Tile>
              <RowText title="GitLab" detail={c.instance_url.replace(/^https?:\/\//, '')} />
              {users.length > 0 && <UsedBy bots={users} />}
              <Tag tone={c.status === 'connected' ? 'ok' : 'warn'}>
                {c.status === 'connected' ? 'Connected' : 'Pending'}
              </Tag>
            </div>
          );
        })}
        <div className={`${rowClass} text-muted`}>
          <Tile className="bg-line">
            <BrandLogo brand="github" className="size-5" />
          </Tile>
          <RowText title="GitHub" detail="Coming later" />
          <Tag>Later</Tag>
        </div>
        <div className={`${rowClass} text-muted`}>
          <Tile className="bg-line">
            <BrandLogo brand="slack" className="size-5" />
          </Tile>
          <RowText title="Slack" detail="Coming later" />
          <Tag>Later</Tag>
        </div>
      </Box>
      {showForm ? (
        <form onSubmit={submit} className="flex flex-col gap-3">
          <label className="flex flex-col gap-2">
            <span className="text-xs text-muted">GitLab instance URL</span>
            <TextField
              size="sm"
              value={instance}
              onChange={(e) => setInstance(e.target.value)}
              required
            />
          </label>
          {start.isError && <p className="text-sm text-danger">{start.error.message}</p>}
          {pollError && <p className="text-sm text-danger">{pollError}</p>}
          <button
            type="submit"
            disabled={start.isPending || !!flow}
            className={`${solidButton} self-start`}
          >
            Sign in to GitLab
          </button>
        </form>
      ) : (
        <button
          type="button"
          onClick={() => setAdding(true)}
          className="h-[34px] self-start rounded-full border border-dashed border-outline px-3.5 text-[13px] text-ink"
        >
          Add a GitLab instance
        </button>
      )}
      {flow && (
        <div className="flex flex-col gap-1 rounded-2xl border border-line bg-box p-4 text-sm">
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
          <p className="font-mono text-lg">{flow.user_code}</p>
          <p className="text-muted">Waiting for you to approve...</p>
        </div>
      )}
    </>
  );
}
