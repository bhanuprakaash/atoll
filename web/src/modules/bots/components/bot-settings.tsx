import { useEffect, useRef, useState, type ReactNode } from 'react';
import { useNavigate } from 'react-router';
import { Field } from '@/components/field';
import { Segmented } from '@/components/segmented';
import { ServerLogo } from '@/components/server-logo';
import { outlineButton, solidButton } from '@/components/settings-ui';
import { Switch } from '@/components/switch';
import { TextArea, TextField } from '@/components/text-field';
import { useDeleteBot, usePinBot, useUpdateBot, type Bot } from '@/modules/bots/api/bots';
import { ChoiceList } from '@/modules/bots/components/choice-list';
import { ConnectionList } from '@/modules/bots/components/connection-list';
import { CreatureCarousel } from '@/modules/bots/components/creature-picker';
import {
  repoAccess as repoAccessChoices,
  type Choices,
  type ModelControl,
} from '@/modules/bots/components/new-bot-form';
import { ErrorNotice } from '@/components/error-notice';

const modes = [
  { value: 'auto', label: 'Auto' },
  { value: 'ask', label: 'Ask' },
  { value: 'full', label: 'Full access' },
];

type Change = Parameters<ReturnType<typeof useUpdateBot>['mutate']>[0];

export function BotSettings({
  bot,
  onClose,
  modelControl,
  repos,
  skills,
  connections,
  reconnect,
  mcpServers,
  bots,
  memory,
}: Choices & {
  bot: Bot;
  onClose: () => void;
  modelControl: ModelControl;
  mcpServers: { id: string; name: string }[];
  bots: { id: string; name: string }[];
  memory?: ReactNode;
}) {
  const [confirming, setConfirming] = useState(false);
  const update = useUpdateBot(bot.id);
  const pin = usePinBot(bot.id);
  const timers = useRef<Record<string, ReturnType<typeof setTimeout>>>({});
  const mutate = update.mutate;
  const [draft, setDraft] = useState<Change>({});
  const current = { ...bot, ...draft };
  const send = (change: Change) => {
    const settled = () =>
      setDraft((d) =>
        Object.fromEntries(Object.entries(d).filter(([k, v]) => v !== change[k as keyof Change])),
      );
    update.mutateAsync(change).then(settled, settled);
  };
  const save = (change: Change) => {
    setDraft((d) => ({ ...d, ...change }));
    send(change);
  };
  const pending = useRef<Record<string, Change>>({});
  const saveText = (key: 'name' | 'persona', value: string) => {
    setDraft((d) => ({ ...d, [key]: value }));
    clearTimeout(timers.current[key]);
    pending.current[key] = { [key]: value };
    timers.current[key] = setTimeout(() => {
      send(pending.current[key]);
      delete pending.current[key];
    }, 400);
  };
  useEffect(() => {
    const active = timers.current;
    const changes = pending.current;
    return () => {
      Object.values(active).forEach(clearTimeout);
      Object.values(changes).forEach((change) => mutate(change));
    };
  }, [mutate]);
  return (
    <aside
      aria-label="Bot settings"
      className="min-h-0 min-w-0 max-w-105 flex-[1_1_400px] overflow-auto border-l border-line bg-bg"
    >
      <section role="dialog" aria-label="Bot settings" aria-hidden={confirming} className="p-7">
        <div className="sticky top-0 z-10 -mx-7 -mt-7 flex items-center justify-between bg-bg px-7 pb-6 pt-7">
          <h2 className="text-lg">Bot settings</h2>
          <button
            type="button"
            aria-label="Close"
            onClick={onClose}
            className="-mr-2 flex size-9 items-center justify-center rounded-[10px] text-muted hover:bg-tile hover:text-ink"
          >
            <svg
              width="18"
              height="18"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              strokeWidth="1.8"
              strokeLinecap="round"
              aria-hidden="true"
            >
              <path d="M6 6l12 12M18 6L6 18" />
            </svg>
          </button>
        </div>
        <div className="flex flex-col gap-6">
          <CreatureCarousel value={current.look} onChange={(value) => save({ look: value })} />
          <Field label="Name">
            {(id) => (
              <TextField
                id={id}
                value={current.name}
                onChange={(e) => saveText('name', e.target.value)}
              />
            )}
          </Field>
          {modelControl(current.provider, current.model, (p, m) => save({ provider: p, model: m }))}
          <Field label="Persona">
            {(id) => (
              <TextArea
                id={id}
                rows={4}
                value={current.persona}
                onChange={(e) => saveText('persona', e.target.value)}
              />
            )}
          </Field>
          <p className="-mt-4 text-xs text-label">
            Describe how this Bot should work and what it should know.
          </p>
          <ChoiceList
            title="Skills"
            singular="skill"
            choices={skills}
            selected={current.skills ?? []}
            onChange={(value) => save({ skills: value })}
          />
          <fieldset className="m-0 flex min-w-0 flex-col gap-2 border-0 p-0">
            <legend className="mb-2 p-0 text-xs tracking-wide text-label">MCP servers</legend>
            <div className="overflow-hidden rounded-2xl bg-field">
              {mcpServers.map((server) => (
                <label
                  key={server.id}
                  className="flex items-center justify-between gap-3 border-b border-composer-line px-3 py-2.5 text-sm last:border-b-0"
                >
                  <span className="flex min-w-0 items-center gap-2.5">
                    <ServerLogo id={server.id} />
                    {server.name}
                  </span>
                  <Switch
                    aria-label={server.name}
                    checked={(current.mcp_servers ?? []).includes(server.id)}
                    onChange={(on) => {
                      const ids = current.mcp_servers ?? [];
                      save({
                        mcp_servers: on
                          ? [...ids, server.id]
                          : ids.filter((id) => id !== server.id),
                      });
                    }}
                  />
                </label>
              ))}
            </div>
          </fieldset>
          {bot.id !== 'chief' && (
            <>
              <Segmented
                label="Repo access"
                value={current.repo_access === 'all' ? 'all' : 'only'}
                onChange={(choice) => save({ repo_access: choice === 'all' ? 'all' : [] })}
                choices={repoAccessChoices}
              />
              {current.repo_access !== 'all' && (
                <ChoiceList
                  title="Repos"
                  singular="repo"
                  choices={repos}
                  selected={current.repo_access}
                  onChange={(value) => save({ repo_access: value })}
                />
              )}
            </>
          )}
          <Segmented
            label="Mode"
            value={current.mode}
            onChange={(value) => save({ mode: value as Bot['mode'] })}
            choices={modes}
          />
          {current.mode === 'full' && (
            <p className="-mt-4 text-xs text-label">
              Full access: this Bot never asks, even to delete files or push. Use it for Bots that
              run in a VM.
            </p>
          )}
          <ConnectionList
            connections={connections}
            selected={current.connections ?? []}
            onChange={(value) => save({ connections: value })}
            reconnect={reconnect}
          />
          <ChoiceList
            title="Can hand work to"
            singular="Bot"
            choices={bots.filter((other) => other.id !== bot.id)}
            selected={current.delegates ?? []}
            onChange={(value) => save({ delegates: value })}
          />
          <p className="-mt-4 text-xs text-label">
            {bot.name} can start up to 3 helper threads at once with these Bots
          </p>
          {memory}
          {bot.id !== 'chief' && (
            <button
              type="button"
              aria-pressed={bot.pinned}
              onClick={() => pin.mutate(!bot.pinned)}
              className={`self-start rounded-full border border-outline px-3 py-1.5 text-[13.5px] ${bot.pinned ? 'bg-selected' : 'hover:bg-line'}`}
            >
              Pin to sidebar
            </button>
          )}
          {bot.can_manage_platform && (
            <label className="flex items-center justify-between border-t border-line pt-4 text-sm">
              Manage platform
              <Switch
                checked={current.can_manage_platform}
                onChange={(value) => save({ can_manage_platform: value })}
              />
            </label>
          )}
          {update.isError ? (
            <ErrorNotice
              error={update.error}
              retry={() => update.variables && send(update.variables)}
            />
          ) : (
            update.isSuccess && <p className="text-sm text-muted">Saved</p>
          )}
          {bot.id !== 'chief' && (
            <button
              type="button"
              className={`${outlineButton} self-start text-danger`}
              onClick={() => setConfirming(true)}
            >
              Delete Bot
            </button>
          )}
        </div>
      </section>
      {confirming && <DeleteDialog bot={bot} onClose={() => setConfirming(false)} />}
    </aside>
  );
}

function DeleteDialog({ bot, onClose }: { bot: Bot; onClose: () => void }) {
  const navigate = useNavigate();
  const remove = useDeleteBot(bot.id);
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => e.key === 'Escape' && onClose();
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [onClose]);
  return (
    <div
      className="fixed inset-0 z-30 flex items-center justify-center bg-scrim p-4"
      onMouseDown={(e) => e.target === e.currentTarget && onClose()}
    >
      <section
        role="dialog"
        aria-modal="true"
        aria-label="Delete Bot"
        className="w-full max-w-110 rounded-[22px] border border-composer-line bg-popover p-6 shadow-[0_18px_48px_rgba(0,0,0,.55)]"
      >
        <h2 className="mb-1 text-base">Delete {bot.name}?</h2>
        <p className="mb-5 text-[13px] text-muted">
          Its threads and runs are deleted. Memory it saved stays.
        </p>
        {remove.isError && (
          <ErrorNotice
            error={remove.error}
            retry={() => remove.mutate(undefined, { onSuccess: () => navigate('/bots/chief') })}
            className="mb-3"
          />
        )}
        <div className="flex justify-end gap-2">
          <button type="button" className={outlineButton} onClick={onClose}>
            Cancel
          </button>
          <button
            type="button"
            className={solidButton}
            disabled={remove.isPending}
            onClick={() => remove.mutate(undefined, { onSuccess: () => navigate('/bots/chief') })}
          >
            Delete
          </button>
        </div>
      </section>
    </div>
  );
}
