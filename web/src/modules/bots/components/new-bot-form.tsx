import { useState, type FormEvent } from 'react';
import { Button } from '@/components/button';
import { controlClass, Field } from '@/components/field';
import { useCreateBot, type Bot, type Provider } from '@/modules/bots/api/bots';

export function NewBotForm({ onCreated }: { onCreated: (bot: Bot) => void }) {
  const [name, setName] = useState('');
  const [persona, setPersona] = useState('');
  const [provider, setProvider] = useState<Provider>('claude');
  const create = useCreateBot();

  const submit = (e: FormEvent) => {
    e.preventDefault();
    create.mutate({ name, persona, provider }, { onSuccess: onCreated });
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
      {create.isError && <p className="text-sm text-danger">{create.error.message}</p>}
      <Button type="submit" disabled={create.isPending} className="self-start">
        Create
      </Button>
    </form>
  );
}
