import { useEffect, useRef, useState } from 'react';
import { Checkbox } from '@/components/checkbox';
import { controlClass, Field } from '@/components/field';
import { useUpdateBot, type Bot, type NewBot } from '@/modules/bots/api/bots';
import { CreaturePicker } from '@/modules/bots/components/creature-picker';
import type { Choices, ModelControl } from '@/modules/bots/components/new-bot-form';

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
  const toggle = (list: string[], id: string) =>
    list.includes(id) ? list.filter((x) => x !== id) : [...list, id];
  return (
    <div className="fixed inset-0 z-20 flex justify-end bg-bg/70">
      <section
        role="dialog"
        aria-modal="true"
        aria-label="Bot settings"
        className="h-full w-full max-w-lg overflow-auto border-l border-line bg-bg p-6"
      >
        <div className="mb-6 flex items-center justify-between">
          <h2 className="text-lg">Bot settings</h2>
          <button type="button" onClick={onClose}>
            Close
          </button>
        </div>
        <div className="flex flex-col gap-5">
          <Field label="Name">
            {(id) => (
              <input
                id={id}
                className={controlClass}
                value={name}
                onChange={(e) => {
                  setName(e.target.value);
                  saveText('name', e.target.value);
                }}
              />
            )}
          </Field>
          <Field label="Persona">
            {(id) => (
              <textarea
                id={id}
                className={controlClass}
                rows={4}
                value={persona}
                onChange={(e) => {
                  setPersona(e.target.value);
                  saveText('persona', e.target.value);
                }}
              />
            )}
          </Field>
          <CreaturePicker
            value={look}
            onChange={(value) => {
              setLook(value);
              mutate({ look: value });
            }}
          />
          {modelControl(provider, model, (p, m) => {
            setProvider(p);
            setModel(m);
            mutate({ provider: p, model: m });
          })}
          <Field label="Repo access">
            {(id) => (
              <select
                id={id}
                className={controlClass}
                value={repoAccess === 'all' ? 'all' : 'only'}
                onChange={(e) => {
                  const value = e.target.value === 'all' ? 'all' : [];
                  setRepoAccess(value);
                  mutate({ repo_access: value });
                }}
              >
                <option value="all">All</option>
                <option value="only">Only these</option>
              </select>
            )}
          </Field>
          {repoAccess !== 'all' &&
            repos.map((r) => (
              <Checkbox
                key={r.id}
                label={r.name}
                checked={repoAccess.includes(r.id)}
                onChange={() => {
                  const value = toggle(repoAccess, r.id);
                  setRepoAccess(value);
                  mutate({ repo_access: value });
                }}
              />
            ))}
          {skills.length > 0 && (
            <fieldset>
              <legend>Skills</legend>
              {skills.map((s) => (
                <Checkbox
                  key={s.id}
                  label={s.name}
                  checked={skillIds.includes(s.id)}
                  onChange={() => {
                    const value = toggle(skillIds, s.id);
                    setSkillIds(value);
                    mutate({ skills: value });
                  }}
                />
              ))}
            </fieldset>
          )}
          {connections.length > 0 && (
            <fieldset>
              <legend>Connections</legend>
              {connections.map((c) => (
                <Checkbox
                  key={c.id}
                  label={c.instance_url}
                  checked={connectionIds.includes(c.id)}
                  onChange={() => {
                    const value = toggle(connectionIds, c.id);
                    setConnectionIds(value);
                    mutate({ connections: value });
                  }}
                />
              ))}
            </fieldset>
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
