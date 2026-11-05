import { useEffect, useState, type FormEvent } from 'react';
import { useQueryClient } from '@tanstack/react-query';
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
import {
  pollConnection,
  useConnections,
  useStartGitlab,
  type DeviceFlow,
} from '@/modules/connections/api/connections';

type Bot = { id: string; name: string; look: CreatureLook; connections: string[] };

function GitLabIcon() {
  return (
    <svg width="22" height="22" viewBox="0 0 24 24" aria-hidden="true">
      <path d="M12 21L3 14l2-9 3 6h8l3-6 2 9z" fill="var(--color-gitlab)" />
      <path d="M12 21l-4-10h8z" fill="var(--color-gitlab-dark)" />
      <path d="M3 14l5-3 4 10z" fill="var(--color-gitlab-light)" />
      <path d="M21 14l-5-3-4 10z" fill="var(--color-gitlab-light)" />
    </svg>
  );
}

function GitHubIcon() {
  return (
    <svg
      width="20"
      height="20"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.6"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
    >
      <path d="M9 19c-4 1.3-4-2-6-2.5M15 21v-3.5c0-1 .1-1.4-.5-2 2.8-.3 5.5-1.4 5.5-6a4.6 4.6 0 0 0-1.3-3.2 4.2 4.2 0 0 0-.1-3.2s-1-.3-3.3 1.3a11.4 11.4 0 0 0-6 0C6.9 2.8 5.9 3.1 5.9 3.1a4.2 4.2 0 0 0-.1 3.2A4.6 4.6 0 0 0 4.5 9.5c0 4.6 2.7 5.7 5.5 6-.6.6-.6 1.2-.5 2V21" />
    </svg>
  );
}

function SlackIcon() {
  return (
    <svg width="20" height="20" viewBox="0 0 24 24" fill="currentColor" aria-hidden="true">
      <rect x="10" y="2" width="4" height="9" rx="2" />
      <rect x="10" y="13" width="4" height="9" rx="2" />
      <rect x="2" y="10" width="9" height="4" rx="2" />
      <rect x="13" y="10" width="9" height="4" rx="2" />
    </svg>
  );
}

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
                <GitLabIcon />
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
            <GitHubIcon />
          </Tile>
          <RowText title="GitHub" detail="Coming later" />
          <Tag>Later</Tag>
        </div>
        <div className={`${rowClass} text-muted`}>
          <Tile className="bg-line">
            <SlackIcon />
          </Tile>
          <RowText title="Slack" detail="Coming later" />
          <Tag>Later</Tag>
        </div>
      </Box>
      {showForm ? (
        <form onSubmit={submit} className="flex flex-col gap-3">
          <label className="flex flex-col gap-2">
            <span className="text-xs text-muted">GitLab instance URL</span>
            <input
              className="h-9 rounded-xl border border-outline bg-bg px-3 text-sm text-ink outline-none focus:border-muted"
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
