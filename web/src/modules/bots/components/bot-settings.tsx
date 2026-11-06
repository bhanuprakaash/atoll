import { useEffect, useRef, useState, type ReactNode } from 'react';
import { Field } from '@/components/field';
import { Segmented } from '@/components/segmented';
import { Switch } from '@/components/switch';
import { TextArea, TextField } from '@/components/text-field';
import { usePinBot, useUpdateBot, type Bot, type NewBot } from '@/modules/bots/api/bots';
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
  const update = useUpdateBot(bot.id);
  const pin = usePinBot(bot.id);
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
        </div>
      </section>
    </aside>
  );
}
