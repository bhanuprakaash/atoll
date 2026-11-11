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
  useSignInAgain,
  useStartGithub,
  useStartGitlab,
  type Connection,
  type DeviceFlow,
} from '@/modules/connections/api/connections';
import { ErrorNotice } from '@/components/error-notice';
import { ApiError } from '@/lib/api-client';

type Bot = { id: string; name: string; look: CreatureLook; connections: string[] };

export function ConnectionsSettings({ bots = [] }: { bots?: Bot[] }) {
  const queryClient = useQueryClient();
  const [instance, setInstance] = useState('https://gitlab.com');
  const [adding, setAdding] = useState(false);
  const [flow, setFlow] = useState<DeviceFlow | null>(null);
  const [pollFailure, setPollFailure] = useState<{ id: string; error: unknown } | null>(null);
  const { data: connections = [] } = useConnections();
  const start = useStartGitlab();
  const startGithub = useStartGithub();
  const signInAgain = useSignInAgain();
  const gitlab = connections.filter((c) => c.kind === 'gitlab');
  const github = connections.find((c) => c.kind === 'github');
  const githubUsers = bots.filter((b) => github && b.connections?.includes(github.id));
  const showForm = adding || !!flow || gitlab.length === 0;

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
        (err: unknown) => {
          setFlow(null);
          setPollFailure({ id: flow.id, error: err });
          void queryClient.invalidateQueries({ queryKey: ['connections'] });
        },
      );
    }, flow.interval_ms);
    return () => clearInterval(timer);
  }, [flow, queryClient]);

  const begin = () => {
    setPollFailure(null);
    start.mutate(instance, {
      onSuccess: (started) => {
        setFlow(started);
        void queryClient.invalidateQueries({ queryKey: ['connections'] });
      },
    });
  };
  const submit = (e: FormEvent) => {
    e.preventDefault();
    begin();
  };
  const beginGithub = () => {
    setPollFailure(null);
    startGithub.mutate(undefined, {
      onSuccess: (started) => {
        window.open(started.verification_uri, '_blank');
        setFlow(started);
        void queryClient.invalidateQueries({ queryKey: ['connections'] });
      },
    });
  };
  const restart = (c: Pick<Connection, 'kind' | 'id'>) => {
    setPollFailure(null);
    signInAgain.mutate(c, {
      onSuccess: (started) => {
        setFlow(started);
        void queryClient.invalidateQueries({ queryKey: ['connections'] });
      },
    });
  };

  const restartFailed = () =>
    pollFailure &&
    restart({ id: pollFailure.id, kind: github?.id === pollFailure.id ? 'github' : 'gitlab' });

  const statusTag = (c: Connection) => (
    <Tag tone={c.status === 'connected' ? 'ok' : 'warn'}>
      {c.status === 'connected'
        ? 'Connected'
        : c.status === 'needs_sign_in'
          ? 'Needs sign-in'
          : 'Pending'}
    </Tag>
  );

  // The notice of a failed sign-in already offers Sign in again: one button, not two.
  const noticeSignsIn =
    pollFailure?.error instanceof ApiError &&
    ['keychain', 'signed_out'].includes(pollFailure.error.code);
  const signInAgainButton = (c: Connection) =>
    !(noticeSignsIn && pollFailure?.id === c.id) &&
    (c.status === 'needs_sign_in' || (c.kind === 'github' && c.status === 'pending' && !flow)) && (
      <button
        type="button"
        disabled={signInAgain.isPending || !!flow}
        onClick={() => restart(c)}
        className={solidButton}
      >
        Sign in again
      </button>
    );

  return (
    <>
      <SectionHead title="Connections" lead="Sign in once. Each Bot picks which ones it gets." />
      <Box>
        {gitlab.map((c) => {
          const users = bots.filter((b) => b.connections?.includes(c.id));
          return (
            <div key={c.id} className={rowClass}>
              <Tile className="bg-gitlab-bg">
                <BrandLogo brand="gitlab" className="size-[22px]" />
              </Tile>
              <RowText title="GitLab" detail={c.instance_url.replace(/^https?:\/\//, '')} />
              {users.length > 0 && <UsedBy bots={users} />}
              {statusTag(c)}
              {signInAgainButton(c)}
            </div>
          );
        })}
        {gitlab.length === 0 && (
          <div className={rowClass}>
            <Tile className="bg-gitlab-bg">
              <BrandLogo brand="gitlab" className="size-[22px]" />
            </Tile>
            <RowText title="GitLab" detail="Not connected. Sign in below." />
            <Tag tone="warn">Not connected</Tag>
          </div>
        )}
        <div className={rowClass}>
          <Tile className="bg-line">
            <BrandLogo brand="github" className="size-5" />
          </Tile>
          <RowText title="GitHub" detail="Bots get gh, git and GitHub tools." />
          {githubUsers.length > 0 && <UsedBy bots={githubUsers} />}
          {github ? statusTag(github) : <Tag tone="warn">Not connected</Tag>}
          {github && signInAgainButton(github)}
          {!github && (
            <button
              type="button"
              disabled={startGithub.isPending || !!flow}
              onClick={beginGithub}
              className={solidButton}
            >
              Connect GitHub
            </button>
          )}
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
          {start.isError && <ErrorNotice error={start.error} retry={begin} />}
          {signInAgain.isError && (
            <ErrorNotice
              error={signInAgain.error}
              retry={() => signInAgain.variables && restart(signInAgain.variables)}
            />
          )}
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
      {startGithub.isError && <ErrorNotice error={startGithub.error} retry={beginGithub} />}
      {pollFailure && (
        <ErrorNotice error={pollFailure.error} signIn={restartFailed} retry={restartFailed} />
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
