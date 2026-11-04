import { useState, type FormEvent } from 'react';
import { Button } from '@/components/button';
import { Checkbox } from '@/components/checkbox';
import { controlClass, Field } from '@/components/field';
import { useCreateBot, type Bot, type Provider } from '@/modules/bots/api/bots';

const toggle = (list: string[], id: string) =>
  list.includes(id) ? list.filter((x) => x !== id) : [...list, id];

export function NewBotForm({
  repos,
  connections,
  onCreated,
}: {
  repos: { id: string; name: string }[];
  connections: { id: string; instance_url: string }[];
  onCreated: (bot: Bot) => void;
}) {
  const [name, setName] = useState('');
  const [persona, setPersona] = useState('');
  const [provider, setProvider] = useState<Provider>('claude');
  const [onlyThese, setOnlyThese] = useState(false);
  const [repoIds, setRepoIds] = useState<string[]>([]);
  const [connectionIds, setConnectionIds] = useState<string[]>([]);
  const create = useCreateBot();

  const submit = (e: FormEvent) => {
    e.preventDefault();
    create.mutate(
      {
        name,
        persona,
        provider,
        repo_access: onlyThese ? repoIds : 'all',
        connections: connectionIds,
      },
      { onSuccess: onCreated },
    );
  };

  return (
    <form onSubmit={submit} className="flex max-w-md flex-col gap-4 p-6">
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
      <Field label="Model">
        {(id) => (
          <select
            id={id}
            className={controlClass}
            value={provider}
            onChange={(e) => setProvider(e.target.value as Provider)}
          >
            <option value="claude">Claude Code</option>
            <option value="codex">Codex</option>
          </select>
        )}
      </Field>
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
      {connections.length > 0 && (
        <fieldset className="flex flex-col gap-1">
          <legend className="text-sm font-medium">Connections</legend>
          {connections.map((c) => (
            <Checkbox
              key={c.id}
              label={c.instance_url}
              checked={connectionIds.includes(c.id)}
              onChange={() => setConnectionIds(toggle(connectionIds, c.id))}
            />
          ))}
        </fieldset>
      )}
      {create.isError && <p className="text-sm text-danger">{create.error.message}</p>}
      <Button type="submit" disabled={create.isPending} className="self-start">
        Create
      </Button>
    </form>
  );
}
