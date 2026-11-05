import { useState, type FormEvent, type ReactNode } from 'react';
import { Link } from 'react-router';
import { BrandLogo } from '@/components/brand-logo';
import { Button } from '@/components/button';
import { Creature, type CreatureLook } from '@/components/creature';
import { Field } from '@/components/field';
import { Segmented } from '@/components/segmented';
import { TextArea, TextField } from '@/components/text-field';
import { useCreateBot, type Bot, type Provider } from '@/modules/bots/api/bots';
import { ChoiceList } from '@/modules/bots/components/choice-list';
import { ConnectionList } from '@/modules/bots/components/connection-list';
import { CreaturePicker } from '@/modules/bots/components/creature-picker';

export type ModelControl = (
  provider: Provider,
  model: string,
  onChange: (provider: Provider, model: string) => void,
) => ReactNode;
export type Choices = {
  repos: { id: string; name: string }[];
  skills: { id: string; name: string }[];
  connections: { id: string; instance_url: string; status: 'pending' | 'connected' }[];
};

export const repoAccess = [
  { value: 'all', label: 'All repos' },
  { value: 'only', label: 'Only these' },
];

export function NewBotForm({
  repos,
  connections,
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
    <div className="h-full overflow-auto">
      <div className="flex items-center justify-between border-b border-line px-8 py-5">
        <h1 className="text-lg">New Bot</h1>
        <div className="flex items-center gap-4">
          <Link to={chiefId ? `/bots/${chiefId}` : '/'} className="text-sm text-muted">
            Cancel
          </Link>
          <Button type="submit" form="new-bot" disabled={create.isPending || !model}>
            Create Bot
          </Button>
        </div>
      </div>
      <div className="flex flex-wrap gap-12 p-6">
        <form
          id="new-bot"
          onSubmit={submit}
          className="flex min-w-72 max-w-xl flex-1 flex-col gap-6"
        >
          {chiefId && (
            <Link
              className="rounded-control border border-line p-4 text-sm"
              to={`/bots/${chiefId}`}
            >
              Rather describe it? Ask Chief to set it up.
            </Link>
          )}
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
          <CreaturePicker value={look} onChange={setLook} shuffle />
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
          <p className="-mt-4 text-xs text-muted">
            Describe how this Bot should work and what it should know.
          </p>
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
          />
          {create.isError && <p className="text-sm text-danger">{create.error.message}</p>}
        </form>
        <section aria-label="Preview" className="w-80 max-w-full">
          <h2 className="mb-4 text-sm text-muted">Preview</h2>
          <div className="flex flex-col items-center gap-3 rounded-card border border-line bg-card p-7 text-center">
            <Creature look={look} size={112} />
            <h3 className="text-2xl">{name || 'Your Bot'}</h3>
            {model && (
              <p className="flex items-center gap-1.5 rounded-full bg-composer px-3 py-1 text-xs">
                <BrandLogo brand={provider} className="size-3.5" />
                {provider === 'claude' ? 'Claude Code' : 'Codex'} · {model}
              </p>
            )}
            <p className="text-sm text-muted">{persona || 'A little about your Bot'}</p>
            <div className="flex flex-wrap justify-center gap-2 text-xs text-muted">
              {skills
                .filter((s) => skillIds.includes(s.id))
                .map((s) => (
                  <span key={s.id} className="rounded-full bg-composer px-3 py-1">
                    {s.name}
                  </span>
                ))}
            </div>
          </div>
          <h2 className="mt-6 mb-3 text-sm text-muted">In the sidebar</h2>
          <div className="flex items-center gap-3 rounded-control border border-line p-3">
            <Creature look={look} size={36} />
            <div>
              <div>{name || 'Your Bot'}</div>
              <div className="text-sm text-muted">New Bot</div>
            </div>
          </div>
        </section>
      </div>
    </div>
  );
}
