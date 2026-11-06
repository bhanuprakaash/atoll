import { useEffect, useState, type FormEvent } from 'react';
import { Field } from '@/components/field';
import {
  outlineButton,
  RowText,
  rowClass,
  SectionHead,
  solidButton,
  Tile,
} from '@/components/settings-ui';
import { TextArea, TextField } from '@/components/text-field';
import {
  useConnectMcpServer,
  useMcpServers,
  useRemoveMcpServer,
  useSaveMcpServer,
  type McpServer,
} from '@/modules/mcp-servers/api/mcp-servers';

const listClass = 'divide-y divide-line overflow-hidden rounded-[18px] border border-line bg-box';

const lines = (text: string) => text.split('\n').filter((line) => line.trim() !== '');

function pairs(text: string, separator: string) {
  const out: Record<string, string> = {};
  for (const line of lines(text)) {
    const at = line.indexOf(separator);
    const name = (at < 0 ? line : line.slice(0, at)).trim();
    out[name] = at < 0 ? '' : line.slice(at + 1).trimStart();
  }
  return out;
}

function ServerIcon() {
  return (
    <svg
      width="16"
      height="16"
      viewBox="0 0 24 24"
      fill="none"
      stroke="var(--color-ink)"
      strokeWidth="1.8"
      strokeLinecap="round"
      strokeLinejoin="round"
    >
      <path d="M4 5h16v6H4zM4 13h16v6H4zM8 8h.01M8 16h.01" />
    </svg>
  );
}

const withoutSlash = (url: string) => url.trim().replace(/\/$/, '');

function ServerDialog({
  kind,
  server,
  githubURL,
  onClose,
}: {
  kind: 'local' | 'remote';
  server?: McpServer;
  githubURL?: string;
  onClose: () => void;
}) {
  const [asking, setAsking] = useState(false);
  const [name, setName] = useState(server?.name ?? '');
  const [command, setCommand] = useState(server?.command ?? '');
  const [args, setArgs] = useState((server?.args ?? []).join('\n'));
  const [env, setEnv] = useState((server?.env_names ?? []).map((n) => `${n}=`).join('\n'));
  const [url, setUrl] = useState(server?.url ?? '');
  const [headers, setHeaders] = useState(
    (server?.header_names ?? []).map((n) => `${n}: `).join('\n'),
  );
  const save = useSaveMcpServer(server?.id);
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => e.key === 'Escape' && onClose();
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [onClose]);
  const add = () =>
    save.mutate(
      kind === 'local'
        ? { name, command, args: lines(args), env: pairs(env, '=') }
        : { name, url, headers: pairs(headers, ':') },
      { onSuccess: onClose },
    );
  const submit = (e: FormEvent) => {
    e.preventDefault();
    if (!server && githubURL && withoutSlash(url) === withoutSlash(githubURL)) setAsking(true);
    else add();
  };
  const title = server ? `Edit ${server.name}` : kind === 'local' ? 'Add local' : 'Add by URL';
  return (
    <div
      className="fixed inset-0 z-30 flex items-center justify-center bg-scrim p-4"
      onMouseDown={(e) => e.target === e.currentTarget && onClose()}
    >
      <section
        role="dialog"
        aria-modal="true"
        aria-label={title}
        className="w-full max-w-110 rounded-[22px] border border-composer-line bg-popover p-6 shadow-[0_18px_48px_rgba(0,0,0,.55)]"
      >
        <h2 className="mb-5 text-base">{title}</h2>
        <form onSubmit={submit} className="flex flex-col gap-4">
          <Field label="Name">
            {(id) => (
              <TextField
                id={id}
                size="sm"
                autoFocus
                value={name}
                onChange={(e) => setName(e.target.value)}
                required
              />
            )}
          </Field>
          {kind === 'local' ? (
            <>
              <Field label="Command">
                {(id) => (
                  <TextField
                    id={id}
                    size="sm"
                    className="font-mono text-[13px]"
                    value={command}
                    onChange={(e) => setCommand(e.target.value)}
                    required
                  />
                )}
              </Field>
              <Field label="Args">
                {(id) => (
                  <TextArea
                    id={id}
                    rows={3}
                    className="font-mono text-[13px]"
                    placeholder="One argument per line"
                    value={args}
                    onChange={(e) => setArgs(e.target.value)}
                  />
                )}
              </Field>
              <Field label="Env">
                {(id) => (
                  <TextArea
                    id={id}
                    rows={3}
                    className="font-mono text-[13px]"
                    placeholder="NAME=value, one per line"
                    value={env}
                    onChange={(e) => setEnv(e.target.value)}
                  />
                )}
              </Field>
            </>
          ) : (
            <>
              <Field label="URL">
                {(id) => (
                  <TextField
                    id={id}
                    size="sm"
                    className="font-mono text-[13px]"
                    placeholder="https://example.com/mcp"
                    value={url}
                    onChange={(e) => setUrl(e.target.value)}
                    required
                  />
                )}
              </Field>
              <Field label="Headers">
                {(id) => (
                  <TextArea
                    id={id}
                    rows={3}
                    className="font-mono text-[13px]"
                    placeholder="Name: value, one per line"
                    value={headers}
                    onChange={(e) => setHeaders(e.target.value)}
                  />
                )}
              </Field>
            </>
          )}
          {asking && (
            <p className="text-sm text-muted">
              GitHub has a connection. Use it instead, or add this server as well.
            </p>
          )}
          {save.isError && <p className="text-sm text-danger">{save.error.message}</p>}
          <div className="flex justify-end gap-2">
            <button type="button" className={outlineButton} onClick={onClose}>
              Cancel
            </button>
            {asking ? (
              <>
                <button type="button" className={outlineButton} onClick={add}>
                  Add anyway
                </button>
                <button type="button" className={solidButton} onClick={onClose}>
                  Use connection
                </button>
              </>
            ) : (
              <button type="submit" disabled={save.isPending} className={solidButton}>
                {server ? 'Save' : 'Add'}
              </button>
            )}
          </div>
        </form>
      </section>
    </div>
  );
}

const statusText = {
  not_connected: 'Not connected',
  connected: 'Connected',
  needs_sign_in: 'Needs sign-in',
  needs_app: 'This server needs a registered app',
};

function SignInStatus({ server, allowURL }: { server: McpServer; allowURL?: string }) {
  return (
    server.status && (
      <i className="block text-[12.5px] not-italic text-muted">
        {statusText[server.status]}
        {allowURL && server.status !== 'connected' && (
          <>
            {' · '}
            <a href={allowURL} target="_blank" rel="noreferrer" className="underline">
              Open the Allow page
            </a>
          </>
        )}
      </i>
    )
  );
}

function SignInButton({ server, onClick }: { server: McpServer; onClick: () => void }) {
  const label =
    server.status === 'not_connected'
      ? 'Connect'
      : server.status === 'needs_sign_in'
        ? 'Sign in again'
        : undefined;
  return (
    label && (
      <button type="button" className={solidButton} onClick={onClick}>
        {label}
      </button>
    )
  );
}

function runs(s: McpServer) {
  return s.kind === 'local' ? [s.command, ...(s.args ?? [])].join(' ') : s.url;
}

export function McpServersSettings() {
  const [dialog, setDialog] = useState<{ kind: 'local' | 'remote'; server?: McpServer }>();
  const [waiting, setWaiting] = useState<McpServer>();
  const [allow, setAllow] = useState<{ id: string; url: string }>();
  const connect = useConnectMcpServer();
  const remove = useRemoveMcpServer();
  const { data: servers = [] } = useMcpServers(waiting);
  const signIn = (s: McpServer) => {
    setWaiting(s);
    setAllow(undefined);
    connect.mutate(s.id, {
      onSuccess: ({ authorize_url }) => {
        setAllow({ id: s.id, url: authorize_url });
        window.open(authorize_url, '_blank');
      },
    });
  };
  const catalog = servers.filter((s) => s.kind === 'catalog');
  const fromConnections = servers.filter((s) => s.kind === 'connection');
  const own = servers.filter((s) => s.kind === 'local' || s.kind === 'remote');
  return (
    <>
      <SectionHead
        title="MCP servers"
        lead="Tools a Bot can use. Add a server here, then switch it on in a Bot's settings."
      >
        <button
          type="button"
          className={outlineButton}
          onClick={() => setDialog({ kind: 'local' })}
        >
          Add local
        </button>
        <button type="button" className={solidButton} onClick={() => setDialog({ kind: 'remote' })}>
          Add by URL
        </button>
      </SectionHead>
      <h3 className="-mb-2 text-xs tracking-wide text-label">Catalog</h3>
      <ul aria-label="Catalog" className={listClass}>
        {catalog.map((s) => (
          <li key={s.id} className={rowClass}>
            <Tile>
              <ServerIcon />
            </Tile>
            <RowText title={s.name} detail={runs(s)} mono>
              <SignInStatus server={s} allowURL={allow?.id === s.id ? allow.url : undefined} />
            </RowText>
            <SignInButton server={s} onClick={() => signIn(s)} />
            {(s.status === 'connected' || s.status === 'needs_sign_in') && (
              <button
                type="button"
                className={outlineButton}
                disabled={remove.isPending}
                onClick={() => remove.mutate(s.id)}
              >
                Sign out
              </button>
            )}
          </li>
        ))}
      </ul>
      {fromConnections.length > 0 && (
        <>
          <h3 className="-mb-2 text-xs tracking-wide text-label">From connections</h3>
          <ul aria-label="From connections" className={listClass}>
            {fromConnections.map((s) => (
              <li key={s.id} className={rowClass}>
                <Tile>
                  <ServerIcon />
                </Tile>
                <RowText title={s.name} detail={runs(s)} mono>
                  <i className="block text-[12.5px] not-italic text-muted">
                    from GitHub connection
                  </i>
                </RowText>
              </li>
            ))}
          </ul>
        </>
      )}
      <h3 className="-mb-2 text-xs tracking-wide text-label">Your servers</h3>
      {own.length > 0 ? (
        <ul aria-label="Your servers" className={listClass}>
          {own.map((s) => {
            const names = [...(s.env_names ?? []), ...(s.header_names ?? [])];
            return (
              <li key={s.id} className={rowClass}>
                <Tile>
                  <ServerIcon />
                </Tile>
                <RowText title={s.name} detail={runs(s)} mono>
                  {names.length > 0 && (
                    <i className="block truncate font-mono text-[12.5px] not-italic text-muted">
                      {s.kind === 'local' ? 'Env' : 'Headers'}: {names.join(', ')}
                    </i>
                  )}
                  <SignInStatus server={s} allowURL={allow?.id === s.id ? allow.url : undefined} />
                </RowText>
                <SignInButton server={s} onClick={() => signIn(s)} />
                <button
                  type="button"
                  className={outlineButton}
                  onClick={() =>
                    setDialog({ kind: s.kind === 'local' ? 'local' : 'remote', server: s })
                  }
                >
                  Edit
                </button>
                <button
                  type="button"
                  className={outlineButton}
                  disabled={remove.isPending}
                  onClick={() => remove.mutate(s.id)}
                >
                  Remove
                </button>
              </li>
            );
          })}
        </ul>
      ) : (
        <p className="text-[13.5px] text-muted">No servers of your own yet.</p>
      )}
      {connect.isError && <p className="text-sm text-danger">{connect.error.message}</p>}
      {remove.isError && <p className="text-sm text-danger">{remove.error.message}</p>}
      {dialog && (
        <ServerDialog
          key={dialog.server?.id ?? dialog.kind}
          kind={dialog.kind}
          server={dialog.server}
          githubURL={fromConnections[0]?.url}
          onClose={() => setDialog(undefined)}
        />
      )}
    </>
  );
}
