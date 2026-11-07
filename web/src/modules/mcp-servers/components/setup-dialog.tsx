import { useEffect, useState, type FormEvent } from 'react';
import { Field } from '@/components/field';
import { outlineButton, solidButton } from '@/components/settings-ui';
import { TextField } from '@/components/text-field';
import {
  useMcpServerSetup,
  useSaveMcpServerSetup,
  type McpServer,
} from '@/modules/mcp-servers/api/mcp-servers';

export function SetupDialog({ server, onClose }: { server: McpServer; onClose: () => void }) {
  const [clientId, setClientId] = useState('');
  const info = useMcpServerSetup(server.id);
  const save = useSaveMcpServerSetup(server.id);
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => e.key === 'Escape' && onClose();
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [onClose]);
  const submit = (e: FormEvent) => {
    e.preventDefault();
    save.mutate(clientId.trim(), { onSuccess: onClose });
  };
  return (
    <div
      className="fixed inset-0 z-30 flex items-center justify-center bg-scrim p-4"
      onMouseDown={(e) => e.target === e.currentTarget && onClose()}
    >
      <section
        role="dialog"
        aria-modal="true"
        aria-label={`Set up ${server.name}`}
        className="w-full max-w-110 rounded-[22px] border border-composer-line bg-popover p-6 shadow-[0_18px_48px_rgba(0,0,0,.55)]"
      >
        <h2 className="mb-2 text-base">Set up {server.name}</h2>
        <p className="mb-3 text-[13.5px] text-muted">
          {server.name} lets only internal and Marketplace apps in, so each workspace creates its
          own. Reef sends no client secret: the app uses PKCE.
        </p>
        <ol aria-label="Steps" className="mb-4 list-decimal space-y-1.5 pl-5 text-[13.5px]">
          <li>
            Create an app from scratch at api.slack.com/apps, in the workspace you want to connect.
          </li>
          <li>
            Under OAuth &amp; Permissions, add this redirect URL:{' '}
            <code className="font-mono text-[12.5px]">{info.data?.redirect_uri ?? '...'}</code>
          </li>
          <li>
            Under User Token Scopes, add:{' '}
            <code className="font-mono text-[12.5px]">{info.data?.scopes.join(' ') ?? '...'}</code>
          </li>
          <li>Turn PKCE on for the app. Do not copy the client secret.</li>
          <li>Copy the Client ID from Basic Information and paste it here.</li>
        </ol>
        {info.isError && <p className="mb-3 text-sm text-danger">{info.error.message}</p>}
        <form onSubmit={submit} className="flex flex-col gap-4">
          <Field label="Client ID">
            {(id) => (
              <TextField
                id={id}
                size="sm"
                autoFocus
                className="font-mono text-[13px]"
                value={clientId}
                onChange={(e) => setClientId(e.target.value)}
                required
              />
            )}
          </Field>
          {save.isError && <p className="text-sm text-danger">{save.error.message}</p>}
          <div className="flex justify-end gap-2">
            <button type="button" className={outlineButton} onClick={onClose}>
              Cancel
            </button>
            <button type="submit" disabled={save.isPending} className={solidButton}>
              Save
            </button>
          </div>
        </form>
      </section>
    </div>
  );
}
