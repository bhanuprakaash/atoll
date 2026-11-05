import { useEffect, useRef, useState } from 'react';
import { Button } from '@/components/button';
import {
  type Provider,
  useProviders,
  useRefreshProviders,
  useSignIn,
  useSignInState,
} from '@/modules/providers/api/providers';

function ProviderCard({ provider }: { provider: Provider }) {
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
  return (
    <section
      role="group"
      aria-label={provider.label}
      className="rounded-card border border-line bg-card p-4"
    >
      <h2 className="font-medium">{provider.label}</h2>
      <p className="text-sm text-muted">
        {!provider.found ? 'Not found' : provider.signed_in ? 'Signed in' : 'Not signed in'}
      </p>
      {provider.found && (
        <>
          <p className="text-sm">{provider.path}</p>
          <p className="text-sm">{provider.version}</p>
          <p className="text-sm text-muted">{provider.models.map((m) => m.name).join(', ')}</p>
        </>
      )}
      {provider.found && !provider.signed_in && (
        <Button onClick={start} disabled={signIn.isPending || active}>
          Sign in
        </Button>
      )}
      {(state.data?.status === 'failed' || signIn.isError) && (
        <p className="text-danger">{state.data?.error || signIn.error?.message}</p>
      )}
    </section>
  );
}
export function ProvidersSettings() {
  const { data: providers = [], error } = useProviders();
  const refresh = useRefreshProviders();
  return (
    <div className="flex flex-col gap-4">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-medium">Providers</h1>
        <Button onClick={() => refresh.mutate()}>Refresh</Button>
      </div>
      {error && <p className="text-danger">{error.message}</p>}
      {providers.map((provider) => (
        <ProviderCard key={provider.name} provider={provider} />
      ))}
    </div>
  );
}
