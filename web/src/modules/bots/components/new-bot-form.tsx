import { useState, type FormEvent, type ReactNode } from 'react';
import { Link } from 'react-router';
import { Button } from '@/components/button';
import type { CreatureLook } from '@/components/creature';
import { Field } from '@/components/field';
import { Segmented } from '@/components/segmented';
import { TextArea, TextField } from '@/components/text-field';
import { useCreateBot, type Bot, type Provider } from '@/modules/bots/api/bots';
import { BotPreview, CreatureSidebar } from '@/modules/bots/components/bot-preview';
import { ChoiceList } from '@/modules/bots/components/choice-list';
import { ConnectionList } from '@/modules/bots/components/connection-list';
import { CreatureCarousel } from '@/modules/bots/components/creature-picker';
import { ErrorNotice } from '@/components/error-notice';

export type ModelControl = (
  provider: Provider,
  model: string,
  onChange: (provider: Provider, model: string) => void,
) => ReactNode;
export type Choices = {
  repos: { id: string; name: string }[];
  skills: { id: string; name: string }[];
  connections: {
    id: string;
    kind: 'gitlab' | 'github';
    instance_url: string;
    status: 'pending' | 'connected' | 'needs_sign_in';
  }[];
  reconnect: (connection: Choices['connections'][number]) => ReactNode;
};

export const repoAccess = [
  { value: 'all', label: 'All repos' },
  { value: 'only', label: 'Only these' },
];

export function NewBotForm({
  repos,
  connections,
  reconnect,
  skills,
  onCreated,
  modelControl,
  chiefId,
}: Choices & { onCreated: (bot: Bot) => void; modelControl: ModelControl; chiefId?: string }) {
  const [name, setName] = useState('');
  const [persona, setPersona] = useState('');
  const [provider, setProvider] = useState<Provider>('claude');
  const [model, setModel] = useState('');
  const [look, setLook] = useState<CreatureLook>('jellyfish');
  const [onlyThese, setOnlyThese] = useState(false);
  const [repoIds, setRepoIds] = useState<string[]>([]);
  const [connectionIds, setConnectionIds] = useState<string[]>([]);
  const [skillIds, setSkillIds] = useState<string[]>([]);
  const create = useCreateBot();
  const submit = (e: FormEvent) => {
    e.preventDefault();
    create.mutate(
      {
        name,
        persona,
        provider,
        model,
        look,
        repo_access: onlyThese ? repoIds : 'all',
        connections: connectionIds,
        skills: skillIds,
      },
      { onSuccess: onCreated },
    );
  };
  return (
    <div className="flex h-full min-h-0 flex-col">
      <div
        data-tauri-drag-region="deep"
        className="flex flex-none items-center justify-between border-b border-line px-8 py-5"
      >
        <h1 className="text-base">New Bot</h1>
        <div className="flex items-center gap-4">
          <Link to={chiefId ? `/bots/${chiefId}` : '/'} className="text-sm text-muted">
            Cancel
          </Link>
          <Button type="submit" form="new-bot" disabled={create.isPending || !model}>
            Create Bot
          </Button>
        </div>
      </div>
      <div className="flex min-h-0 flex-1 items-start justify-center gap-12 overflow-auto px-6 py-9">
        <form
          id="new-bot"
          onSubmit={submit}
          className="flex w-full max-w-[600px] min-w-0 flex-col gap-7"
        >
          {chiefId && (
            <Link className="text-sm text-muted hover:text-ink" to={`/bots/${chiefId}`}>
              Rather describe it? Ask Chief to set it up.
            </Link>
          )}
          <section className="flex flex-col gap-5">
            <h2 className="text-xs tracking-wide text-label">Basics</h2>
            <Field label="Name">
              {(id) => (
                <TextField
                  id={id}
                  placeholder="Name your Bot"
                  value={name}
                  onChange={(e) => setName(e.target.value)}
                  required
                />
              )}
            </Field>
            <div className="flex flex-col gap-2">
              <span className="text-xs tracking-wide text-label">Look</span>
              <CreatureCarousel value={look} onChange={setLook} />
            </div>
          </section>
          <section className="flex flex-col gap-5 border-t border-line pt-7">
            <h2 className="text-xs tracking-wide text-label">Brain</h2>
            {modelControl(provider, model, (p, m) => {
              setProvider(p);
              setModel(m);
            })}
            <Field label="Persona">
              {(id) => (
                <TextArea
                  id={id}
                  rows={4}
                  value={persona}
                  onChange={(e) => setPersona(e.target.value)}
                />
              )}
            </Field>
            <p className="-mt-3 text-xs text-muted">
              Describe how this Bot should work and what it should know.
            </p>
          </section>
          <section className="flex flex-col gap-5 border-t border-line pt-7">
            <h2 className="text-xs tracking-wide text-label">Access</h2>
            <ChoiceList
              title="Skills"
              singular="skill"
              choices={skills}
              selected={skillIds}
              onChange={setSkillIds}
            />
            <Segmented
              label="Repo access"
              value={onlyThese ? 'only' : 'all'}
              onChange={(value) => setOnlyThese(value === 'only')}
              choices={repoAccess}
            />
            {onlyThese && (
              <ChoiceList
                title="Repos"
                singular="repo"
                choices={repos}
                selected={repoIds}
                onChange={setRepoIds}
              />
            )}
            <ConnectionList
              connections={connections}
              selected={connectionIds}
              onChange={setConnectionIds}
              reconnect={reconnect}
            />
          </section>
          {create.isError && (
            <ErrorNotice
              error={create.error}
              retry={() =>
                create.variables && create.mutate(create.variables, { onSuccess: onCreated })
              }
            />
          )}
        </form>
        <section
          aria-label="Preview"
          className="sticky top-9 hidden h-fit w-[340px] flex-none min-[1200px]:block"
        >
          <h2 className="mb-4 text-xs tracking-wide text-label">Preview</h2>
          <BotPreview
            name={name}
            persona={persona}
            provider={provider}
            model={model}
            look={look}
            skills={skills.filter((skill) => skillIds.includes(skill.id))}
            connections={connections.filter((connection) => connectionIds.includes(connection.id))}
            repos={onlyThese ? repos.filter((repo) => repoIds.includes(repo.id)) : 'all'}
          />
          <h2 className="mt-6 mb-3 text-xs tracking-wide text-label">In the sidebar</h2>
          <div className="flex items-center gap-3 rounded-card border border-line bg-card p-2.5">
            <CreatureSidebar look={look} name={name} />
          </div>
        </section>
      </div>
    </div>
  );
}
