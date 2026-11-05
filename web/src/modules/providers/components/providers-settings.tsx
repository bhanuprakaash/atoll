import { useEffect, useRef, useState } from 'react';
import { BrandLogo } from '@/components/brand-logo';
import type { CreatureLook } from '@/components/creature';
import {
  Box,
  outlineButton,
  RowText,
  rowClass,
  SectionHead,
  solidButton,
  Tag,
  Tile,
} from '@/components/settings-ui';
import {
  type Provider,
  useProviders,
  useRefreshProviders,
  useSignIn,
  useSignInState,
} from '@/modules/providers/api/providers';

type Bot = { id: string; name: string; look: CreatureLook; provider: Provider['name'] };

function useProviderSignIn(provider: Provider) {
  const [active, setActive] = useState(false);
  const opened = useRef(false);
  const signIn = useSignIn();
  const state = useSignInState(provider.name, active);
  useEffect(() => {
    if (state.data?.status === 'waiting' && state.data.url && !opened.current) {
      opened.current = true;
      window.open(state.data.url, '_blank');
    }
    if (state.data?.status === 'failed' || state.data?.status === 'signed_in') setActive(false);
  }, [state.data]);
  function start() {
    opened.current = false;
    signIn.mutate(provider.name, {
      onSuccess: () => setActive(true),
    });
  }
  const failed = state.data?.status === 'failed' || signIn.isError;
  return {
    start,
    busy: signIn.isPending || active,
    error: failed ? state.data?.error || signIn.error?.message : '',
  };
}

const tileClass = (name: Provider['name']) => (name === 'claude' ? 'bg-claude-bg' : 'bg-tile');
const tone = (p: Provider) => (!p.found ? 'plain' : p.signed_in ? 'ok' : 'warn');
const status = (p: Provider) =>
  !p.found ? 'Not found' : p.signed_in ? 'Signed in' : 'Not signed in';

function ProviderRow({ provider, bots }: { provider: Provider; bots: Bot[] }) {
  const { start, busy, error } = useProviderSignIn(provider);
  const users = bots.filter((b) => b.provider === provider.name).map((b) => b.name);
  const detail = [
    provider.found && provider.version,
    provider.found && provider.signed_in && users.length > 0 && `used by ${users.join(', ')}`,
    provider.found &&
      !provider.signed_in &&
      users.length > 0 &&
      `${users.join(', ')} can’t run until you sign in`,
    provider.found && provider.models.map((m) => m.name).join(', '),
  ]
    .filter(Boolean)
    .join(' · ');
  return (
    <section role="group" aria-label={provider.label} className={rowClass}>
      <Tile className={tileClass(provider.name)}>
        <BrandLogo brand={provider.name} className="size-[18px]" />
      </Tile>
      <RowText title={provider.label} detail={detail}>
        {provider.found && (
          <i className="block truncate font-mono text-[12.5px] not-italic text-muted">
            {provider.path}
          </i>
        )}
        {error && <i className="block text-[12.5px] not-italic text-danger">{error}</i>}
      </RowText>
      <Tag tone={tone(provider)}>{status(provider)}</Tag>
      {provider.found && !provider.signed_in && (
        <button type="button" className={solidButton} onClick={start} disabled={busy}>
          Sign in
        </button>
      )}
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
      <Box>
        {providers.map((provider) => (
          <ProviderRow key={provider.name} provider={provider} bots={bots} />
        ))}
      </Box>
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
  const { start, busy, error } = useProviderSignIn(provider);
  return (
    <section
      role="group"
      aria-label={provider.label}
      className="flex items-center gap-3.5 rounded-[20px] border border-line bg-box p-4"
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
              onClick={() => refresh.mutate()}
            >
              check again
            </button>
          </span>
        </span>
      </div>
      <p className="text-center text-[12.5px] text-muted">
        {ready} of {providers.length} ready
      </p>
    </div>
  );
}
