import { useState, type FormEvent } from 'react';
import { Button } from '@/components/button';
import { controlClass, Field } from '@/components/field';
import { useRegisterRepo, useRepos } from '@/modules/repos/api/repos';

export function ReposSettings() {
  const [name, setName] = useState('');
  const [path, setPath] = useState('');
  const { data: repos = [] } = useRepos();
  const register = useRegisterRepo();

  const submit = (e: FormEvent) => {
    e.preventDefault();
    register.mutate(
      { name, path },
      {
        onSuccess: () => {
          setName('');
          setPath('');
        },
      },
    );
  };

  return (
    <div className="flex max-w-md flex-col gap-6 p-6">
      <form onSubmit={submit} className="flex flex-col gap-4">
        <Field label="Repo name">
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
        <Field label="Local path">
          {(id) => (
            <input
              id={id}
              className={controlClass}
              value={path}
              onChange={(e) => setPath(e.target.value)}
              required
            />
          )}
        </Field>
        {register.isError && <p className="text-sm text-danger">{register.error.message}</p>}
        <Button type="submit" disabled={register.isPending} className="self-start">
          Register repo
        </Button>
      </form>
      <ul className="flex flex-col gap-2">
        {repos.map((repo) => (
          <li key={repo.id} className="rounded-card border border-line bg-card px-3 py-2">
            <p className="text-sm font-medium">{repo.name}</p>
            <p className="font-mono text-xs text-muted">{repo.path}</p>
          </li>
        ))}
      </ul>
    </div>
  );
}
