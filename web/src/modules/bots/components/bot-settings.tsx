import { useEffect, useRef, useState } from 'react';
import { Creature } from '@/components/creature';
import { Field } from '@/components/field';
import { Segmented } from '@/components/segmented';
import { Switch } from '@/components/switch';
import { TextArea, TextField } from '@/components/text-field';
import { useUpdateBot, type Bot, type NewBot } from '@/modules/bots/api/bots';
import { ChoiceList } from '@/modules/bots/components/choice-list';
import { CreaturePicker } from '@/modules/bots/components/creature-picker';
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
}: Choices & { bot: Bot; onClose: () => void; modelControl: ModelControl }) {
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
    <div className="fixed inset-0 z-20 flex justify-end bg-bg/70">
      <section
        role="dialog"
        aria-modal="true"
        aria-label="Bot settings"
        className="h-full w-full max-w-lg overflow-auto border-l border-line bg-card p-7"
      >
        <div className="mb-6 flex items-center justify-between">
          <h2 className="text-lg">Bot settings</h2>
          <button type="button" onClick={onClose}>
            Close
          </button>
        </div>
        <div className="flex flex-col gap-6">
          <div className="flex justify-center py-3">
            <Creature look={look} size={104} />
          </div>
          <CreaturePicker
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
          <p className="-mt-4 text-xs text-muted">
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
          <ChoiceList
            title="Connections"
            singular="connection"
            choices={connections.map((c) => ({
              id: c.id,
              name: `GitLab · ${c.instance_url}`,
              brand: 'gitlab' as const,
            }))}
            selected={connectionIds}
            onChange={(value) => {
              setConnectionIds(value);
              mutate({ connections: value });
            }}
          />
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
    </div>
  );
}
