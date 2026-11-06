import { useEffect, useRef, useState, type ReactNode } from 'react';
import { useNavigate } from 'react-router';
import { Field } from '@/components/field';
import { Segmented } from '@/components/segmented';
import { outlineButton, solidButton } from '@/components/settings-ui';
import { Switch } from '@/components/switch';
import { TextArea, TextField } from '@/components/text-field';
import { useDeleteBot, useUpdateBot, type Bot, type NewBot } from '@/modules/bots/api/bots';
import { ChoiceList } from '@/modules/bots/components/choice-list';
import { ConnectionList } from '@/modules/bots/components/connection-list';
import { CreatureCarousel } from '@/modules/bots/components/creature-picker';
import {
  repoAccess as repoAccessChoices,
  type Choices,
  type ModelControl,
} from '@/modules/bots/components/new-bot-form';

export function BotSettings({
  bot,
  onClose,
  modelControl,
  repos,
  skills,
  connections,
  memory,
}: Choices & { bot: Bot; onClose: () => void; modelControl: ModelControl; memory?: ReactNode }) {
  const [name, setName] = useState(bot.name);
  const [persona, setPersona] = useState(bot.persona);
  const [look, setLook] = useState(bot.look);
  const [provider, setProvider] = useState(bot.provider);
  const [model, setModel] = useState(bot.model);
  const [repoAccess, setRepoAccess] = useState(bot.repo_access);
  const [skillIds, setSkillIds] = useState(bot.skills ?? []);
  const [platform, setPlatform] = useState(bot.can_manage_platform);
  const [connectionIds, setConnectionIds] = useState(bot.connections ?? []);
  const [confirming, setConfirming] = useState(false);
  const update = useUpdateBot(bot.id);
  const timers = useRef<Record<string, ReturnType<typeof setTimeout>>>({});
  const mutate = update.mutate;
  const pending = useRef<Record<string, Partial<NewBot>>>({});
  const saveText = (key: 'name' | 'persona', value: string) => {
    clearTimeout(timers.current[key]);
    pending.current[key] = { [key]: value };
    timers.current[key] = setTimeout(() => {
      mutate(pending.current[key]);
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
      <section role="dialog" aria-label="Bot settings" className="p-7">
        <div className="mb-6 flex items-center justify-between">
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
          <CreatureCarousel
            value={look}
            onChange={(value) => {
              setLook(value);
              mutate({ look: value });
            }}
          />
          <Field label="Name">
            {(id) => (
              <TextField
                id={id}
                value={name}
                onChange={(e) => {
                  setName(e.target.value);
                  saveText('name', e.target.value);
                }}
              />
            )}
          </Field>
          {modelControl(provider, model, (p, m) => {
            setProvider(p);
            setModel(m);
            mutate({ provider: p, model: m });
          })}
          <Field label="Persona">
            {(id) => (
              <TextArea
                id={id}
                rows={4}
                value={persona}
                onChange={(e) => {
                  setPersona(e.target.value);
                  saveText('persona', e.target.value);
                }}
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
            selected={skillIds}
            onChange={(value) => {
              setSkillIds(value);
              mutate({ skills: value });
            }}
          />
          <Segmented
            label="Repo access"
            value={repoAccess === 'all' ? 'all' : 'only'}
            onChange={(choice) => {
              const value = choice === 'all' ? 'all' : [];
              setRepoAccess(value);
              mutate({ repo_access: value });
            }}
            choices={repoAccessChoices}
          />
          {repoAccess !== 'all' && (
            <ChoiceList
              title="Repos"
              singular="repo"
              choices={repos}
              selected={repoAccess}
              onChange={(value) => {
                setRepoAccess(value);
                mutate({ repo_access: value });
              }}
            />
          )}
          <ConnectionList
            connections={connections}
            selected={connectionIds}
            onChange={(value) => {
              setConnectionIds(value);
              mutate({ connections: value });
            }}
          />
          {memory}
          {bot.can_manage_platform && (
            <label className="flex items-center justify-between border-t border-line pt-4 text-sm">
              Manage platform
              <Switch
                checked={platform}
                onChange={(value) => {
                  setPlatform(value);
                  mutate({ can_manage_platform: value });
                }}
              />
            </label>
          )}
          {update.isError ? (
            <p className="text-danger">{update.error.message}</p>
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
        {remove.isError && <p className="mb-3 text-sm text-danger">{remove.error.message}</p>}
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
