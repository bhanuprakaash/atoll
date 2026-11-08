import { useQueryClient } from '@tanstack/react-query';
import { useEffect, useRef, useState } from 'react';
import { Link } from 'react-router';
import { BrandLogo } from '@/components/brand-logo';
import { Creature, type CreatureLook } from '@/components/creature';
import { Switch } from '@/components/switch';
import { TextField } from '@/components/text-field';
import { outlineButton, SectionHead, solidButton, Tag, Tile } from '@/components/settings-ui';
import {
  type Provider,
  useProviders,
  useRefreshProviders,
  useSignIn,
  useSignInState,
  useSubmitSignInCode,
  useUpdateProvider,
} from '@/modules/providers/api/providers';

type Bot = { id: string; name: string; look: CreatureLook; provider: Provider['name'] };

function useProviderSignIn(name: Provider['name']) {
  const [active, setActive] = useState(false);
  const opened = useRef(false);
  const client = useQueryClient();
  const signIn = useSignIn();
  const state = useSignInState(name, active);
  const submit = useSubmitSignInCode(name);
  useEffect(() => {
    if (!active) return;
    if (state.data?.status === 'waiting' && state.data.url && !opened.current) {
      opened.current = true;
      window.open(state.data.url, '_blank');
    }
    if (state.data?.status === 'failed' || state.data?.status === 'signed_in') setActive(false);
  }, [active, state.data]);
  function start() {
    opened.current = false;
    client.removeQueries({ queryKey: ['providers', name, 'sign-in'] });
    signIn.mutate(name, {
      onSuccess: () => setActive(true),
    });
  }
  const failed = state.data?.status === 'failed' || signIn.isError;
  return {
    start,
    busy: signIn.isPending || active,
    needsCode: active && !!state.data?.needs_code,
    submitCode: (code: string) => submit.mutate(code),
    error: failed ? state.data?.error || signIn.error?.message : submit.error?.message,
  };
}

function CodeForm({ onSubmit }: { onSubmit: (code: string) => void }) {
  const [code, setCode] = useState('');
  return (
    <form
      className="order-last mt-3 flex basis-full items-center gap-2"
      onSubmit={(e) => {
        e.preventDefault();
        if (code.trim()) onSubmit(code.trim());
      }}
    >
      <TextField
        size="sm"
        width="min-w-0 flex-1"
        aria-label="Code"
        value={code}
        onChange={(e) => setCode(e.target.value)}
        placeholder="Paste the code from Claude's page"
        className="font-mono"
      />
      <button type="submit" className={solidButton} disabled={!code.trim()}>
        Submit
      </button>
    </form>
  );
}

export function SignInExpired({ provider }: { provider: Provider['name'] }) {
  const { start, busy, needsCode, submitCode, error } = useProviderSignIn(provider);
  return (
    <>
      <span className="min-w-60 flex-1 text-ink/80">
        {provider === 'codex' ? 'Codex' : 'Claude'} sign-in expired
        {error && <i className="block text-[12.5px] not-italic text-danger">{error}</i>}
      </span>
      <button type="button" className={outlineButton} onClick={start} disabled={busy}>
        Sign in again
      </button>
      {needsCode && <CodeForm onSubmit={submitCode} />}
    </>
  );
}

const tileClass = (name: Provider['name']) => (name === 'claude' ? 'bg-claude-bg' : 'bg-tile');
const tone = (p: Provider) => (!p.found ? 'plain' : p.signed_in ? 'ok' : 'warn');
const status = (p: Provider) =>
  !p.found ? 'Not found' : p.signed_in ? 'Signed in' : 'Not signed in';

function PathOverride({ provider }: { provider: Provider }) {
  const [path, setPath] = useState(provider.path_override);
  const update = useUpdateProvider();
  function persist() {
    const next = path.trim();
    if (next !== provider.path_override)
      update.mutate({ name: provider.name, path_override: next });
  }
  return (
    <>
      <TextField
        size="sm"
        width="min-w-0 flex-1"
        aria-label={`${provider.label} path override`}
        className="font-mono text-[13px]"
        placeholder="Use another executable"
        value={path}
        onChange={(e) => setPath(e.target.value)}
        onBlur={persist}
        onKeyDown={(e) => {
          if (e.key === 'Enter') e.currentTarget.blur();
        }}
      />
      {update.isError && (
        <p className="basis-full text-[12.5px] text-danger">{update.error.message}</p>
      )}
    </>
  );
}

function OwnNames({ title, names }: { title: string; names: string[] }) {
  return (
    <>
      <dt className="self-start pt-0.5 text-xs text-muted">{title}</dt>
      <dd className="flex flex-wrap gap-1">
        {names.length === 0 && <span className="text-muted">None</span>}
        {names.map((name) => (
          <span key={name} className="rounded-full bg-tile px-2 py-0.5">
            {name}
          </span>
        ))}
      </dd>
    </>
  );
}

function ProviderRow({ provider, bots }: { provider: Provider; bots: Bot[] }) {
  const { start, busy, needsCode, submitCode, error } = useProviderSignIn(provider.name);
  const update = useUpdateProvider();
  const users = bots.filter((b) => b.provider === provider.name);
  const slash = provider.path.lastIndexOf('/');
  return (
    <section
      role="group"
      aria-label={provider.label}
      className="rounded-[20px] border border-sidebar-line bg-box p-4"
    >
      <div className="flex items-center gap-3">
        <Tile className={tileClass(provider.name)}>
          <BrandLogo brand={provider.name} className="size-[18px]" />
        </Tile>
        <span className="min-w-0 flex-1 text-[15px]">{provider.label}</span>
        <Tag tone={tone(provider)}>{busy ? 'Signing in' : status(provider)}</Tag>
        {provider.found && !provider.signed_in && (
          <button type="button" className={solidButton} onClick={start} disabled={busy}>
            Sign in
          </button>
        )}
        <Switch
          aria-label={`Use ${provider.label}`}
          checked={provider.enabled}
          onChange={(enabled) => update.mutate({ name: provider.name, enabled })}
        />
      </div>
      {provider.found && (
        <dl className="mt-4 grid grid-cols-[76px_minmax(0,1fr)] items-center gap-x-3 gap-y-2 text-[13px]">
          <dt className="text-xs text-muted">Version</dt>
          <dd>{provider.version}</dd>
          <dt className="text-xs text-muted">Models</dt>
          <dd className="flex flex-wrap gap-1">
            {provider.models.map((model) => (
              <span key={model.id} className="rounded-full bg-tile px-2 py-0.5">
                {model.name}
              </span>
            ))}
          </dd>
          {users.length > 0 && (
            <>
              <dt className="text-xs text-muted">Used by</dt>
              <dd className="flex flex-wrap gap-1">
                {users.map((bot) => (
                  <Link
                    key={bot.id}
                    to={`/bots/${bot.id}`}
                    className="flex items-center gap-1 rounded-full bg-tile px-2 py-0.5"
                  >
                    <Creature look={bot.look} size={20} />
                    {bot.name}
                  </Link>
                ))}
              </dd>
            </>
          )}
          <dt className="text-xs text-muted">Path</dt>
          <dd className="flex min-w-0 items-center gap-2">
            <span title={provider.path} className="flex min-w-0 flex-1 font-mono">
              <span className="truncate">{provider.path.slice(0, slash + 1)}</span>
              <span className="shrink-0">{provider.path.slice(slash + 1)}</span>
            </span>
            <button
              type="button"
              className={outlineButton}
              onClick={() => void navigator.clipboard.writeText(provider.path)}
            >
              Copy
            </button>
          </dd>
        </dl>
      )}
      <dl className="mt-2 grid grid-cols-[76px_minmax(0,1fr)] items-center gap-x-3 gap-y-2 text-[13px]">
        <dt className="text-xs text-muted">Override</dt>
        <dd className="flex min-w-0 flex-wrap items-center gap-2">
          <PathOverride key={provider.path_override} provider={provider} />
        </dd>
      </dl>
      <p className="mt-4 text-xs text-muted">Its own setup, not used by Bots</p>
      <dl className="mt-2 grid grid-cols-[76px_minmax(0,1fr)] gap-x-3 gap-y-2 text-[13px]">
        <OwnNames title="MCP servers" names={provider.own_mcp_servers} />
        <OwnNames title="Skills" names={provider.own_skills} />
      </dl>
      {!provider.enabled && (
        <p className="mt-4 rounded-xl bg-field p-3 text-[13px] text-muted">
          {provider.label} is off: Bots cannot pick it and their runs on it stop before they start.
        </p>
      )}
      {provider.found && !provider.signed_in && (
        <p className="mt-4 rounded-xl bg-field p-3 text-[13px] text-muted">
          Sign in to use {provider.label} with your Bots.
        </p>
      )}
      {needsCode && <CodeForm onSubmit={submitCode} />}
      {error && <p className="mt-2 text-[12.5px] text-danger">{error}</p>}
    </section>
  );
}

export function ProvidersSettings({ bots = [] }: { bots?: Bot[] }) {
  const { data: providers = [], error } = useProviders();
  const refresh = useRefreshProviders();
  return (
    <>
      <SectionHead
        title="Providers"
        lead="Agent CLIs Reef drives. Each signs in once, in its own folder, apart from your own setup."
      >
        <button type="button" className={outlineButton} onClick={() => refresh.mutate()}>
          Check again
        </button>
      </SectionHead>
      {error && <p className="text-danger">{error.message}</p>}
      <div className="flex flex-col gap-3">
        {providers.map((provider) => (
          <ProviderRow key={provider.name} provider={provider} bots={bots} />
        ))}
        <MissingProvider onRefresh={() => refresh.mutate()} />
      </div>
      <p className="text-[12.5px] text-muted">
        Reef finds providers on your shell PATH and in their usual install folders.
      </p>
    </>
  );
}

const cardTone = {
  ok: 'bg-ok-bg text-ok',
  warn: 'bg-warn-bg text-warn',
  plain: 'bg-tile text-muted',
};

function ProviderCard({ provider }: { provider: Provider }) {
  const { start, busy, needsCode, submitCode, error } = useProviderSignIn(provider.name);
  return (
    <section
      role="group"
      aria-label={provider.label}
      className="flex flex-wrap items-center gap-3.5 rounded-[20px] border border-line bg-box p-4"
    >
      <span
        aria-hidden="true"
        className={`flex size-11 flex-none items-center justify-center rounded-xl ${tileClass(provider.name)}`}
      >
        <BrandLogo brand={provider.name} className="size-[22px]" />
      </span>
      <span className="min-w-0 flex-1">
        <b className="block text-[15.5px] font-normal">{provider.label}</b>
        {provider.found && (
          <i className="block truncate text-[12.5px] not-italic text-muted">
            {provider.version} · <span className="font-mono">{provider.path}</span>
          </i>
        )}
        {error && <i className="block text-[12.5px] not-italic text-danger">{error}</i>}
      </span>
      <span
        className={`flex-none rounded-full px-2.5 py-1 text-[12.5px] ${cardTone[tone(provider)]}`}
      >
        {status(provider)}
      </span>
      {provider.found && !provider.signed_in && (
        <button type="button" className={solidButton} onClick={start} disabled={busy}>
          Sign in
        </button>
      )}
      {needsCode && <CodeForm onSubmit={submitCode} />}
    </section>
  );
}

export function FirstRunProviders() {
  const { data: providers = [], error } = useProviders();
  const refresh = useRefreshProviders();
  const ready = providers.filter((p) => p.signed_in).length;
  return (
    <div className="flex flex-col gap-3">
      {error && <p className="text-danger">{error.message}</p>}
      {providers.map((provider) => (
        <ProviderCard key={provider.name} provider={provider} />
      ))}
      <MissingProvider onRefresh={() => refresh.mutate()} />
      <p className="text-center text-[12.5px] text-muted">
        {ready} of {providers.length} ready
      </p>
    </div>
  );
}

function MissingProvider({ onRefresh }: { onRefresh: () => void }) {
  return (
    <div className="flex items-center gap-3.5 rounded-[20px] border border-dashed border-line p-4">
      <span
        aria-hidden="true"
        className="flex size-11 flex-none items-center justify-center rounded-xl bg-field text-muted"
      >
        <svg
          width="20"
          height="20"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          strokeWidth="1.8"
          strokeLinecap="round"
        >
          <path d="M12 5v14M5 12h14" />
        </svg>
      </span>
      <span className="min-w-0 flex-1">
        <b className="block text-[14.5px] font-normal text-ink/80">Don’t see one?</b>
        <span className="block text-[12.5px] text-muted">
          Install Claude Code or Codex, then{' '}
          <button
            type="button"
            className="text-ink/80 underline underline-offset-3"
            onClick={onRefresh}
          >
            check again
          </button>
        </span>
      </span>
    </div>
  );
}
