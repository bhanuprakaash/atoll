import { useState, type FormEvent, type ReactNode } from 'react';
import { Link } from 'react-router';
import { Button } from '@/components/button';
import { Checkbox } from '@/components/checkbox';
import { Creature, type CreatureLook } from '@/components/creature';
import { controlClass, Field } from '@/components/field';
import { useCreateBot, type Bot, type Provider } from '@/modules/bots/api/bots';
import { CreaturePicker } from '@/modules/bots/components/creature-picker';

const toggle = (list: string[], id: string) =>
  list.includes(id) ? list.filter((x) => x !== id) : [...list, id];
export type ModelControl = (
  provider: Provider,
  model: string,
  onChange: (provider: Provider, model: string) => void,
) => ReactNode;
export type Choices = {
  repos: { id: string; name: string }[];
  skills: { id: string; name: string }[];
  connections: { id: string; instance_url: string }[];
};

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
  const [menu, setMenu] = useState(false);
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
      <div className="border-b border-line px-6 py-5">
        <h1 className="text-lg">New Bot</h1>
      </div>
      <div className="flex flex-wrap gap-12 p-6">
        <form onSubmit={submit} className="flex min-w-72 max-w-xl flex-1 flex-col gap-5">
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
              <input
                id={id}
                className={controlClass}
                value={name}
                onChange={(e) => setName(e.target.value)}
                required
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
                onChange={(e) => setPersona(e.target.value)}
              />
            )}
          </Field>
          <CreaturePicker value={look} onChange={setLook} />
          {modelControl(provider, model, (p, m) => {
            setProvider(p);
            setModel(m);
          })}
          <Field label="Repo access">
            {(id) => (
              <select
                id={id}
                className={controlClass}
                value={onlyThese ? 'only' : 'all'}
                onChange={(e) => setOnlyThese(e.target.value === 'only')}
              >
                <option value="all">All</option>
                <option value="only">Only these</option>
              </select>
            )}
          </Field>
          {onlyThese &&
            repos.map((repo) => (
              <Checkbox
                key={repo.id}
                label={repo.name}
                checked={repoIds.includes(repo.id)}
                onChange={() => setRepoIds(toggle(repoIds, repo.id))}
              />
            ))}
          {skills.length > 0 && (
            <fieldset className="flex flex-col gap-1">
              <legend className="text-sm font-medium">Skills</legend>
              {skills.map((s) => (
                <Checkbox
                  key={s.id}
                  label={s.name}
                  checked={skillIds.includes(s.id)}
                  onChange={() => setSkillIds(toggle(skillIds, s.id))}
                />
              ))}
            </fieldset>
          )}
          <div>
            <div className="text-sm font-medium">Connections</div>
            {connections
              .filter((c) => connectionIds.includes(c.id))
              .map((c) => (
                <div key={c.id} className="text-sm">
                  {c.instance_url}
                </div>
              ))}
            <button
              type="button"
              className="mt-2 rounded-control border border-line px-3 py-2 text-sm"
              onClick={() => setMenu(!menu)}
            >
              Add connection
            </button>
            {menu && (
              <div role="menu" className="rounded-control border border-line bg-card p-2">
                {connections.map((c) => (
                  <button
                    type="button"
                    role="menuitem"
                    className="block p-2 text-sm"
                    key={c.id}
                    onClick={() => {
                      setConnectionIds(toggle(connectionIds, c.id));
                      setMenu(false);
                    }}
                  >
                    {connectionIds.includes(c.id) ? '✓ ' : ''}
                    GitLab · {c.instance_url}
                  </button>
                ))}
              </div>
            )}
          </div>
          {create.isError && <p className="text-sm text-danger">{create.error.message}</p>}
          <Button type="submit" disabled={create.isPending || !model} className="self-start">
            Create Bot
          </Button>
        </form>
        <section aria-label="Preview" className="w-80 max-w-full">
          <h2 className="mb-4 text-sm text-muted">Preview</h2>
          <div className="rounded-control border border-line bg-card p-6">
            <Creature look={look} size={64} />
            <h3 className="mt-3 text-lg">{name || 'Your Bot'}</h3>
            <p className="text-sm text-muted">{persona || 'A little about your Bot'}</p>
          </div>
          <div className="mt-4 flex items-center gap-3 rounded-control border border-line p-3">
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
