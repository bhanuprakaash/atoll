import { useState, type FormEvent } from 'react';
import {
  Box,
  outlineButton,
  RowText,
  rowClass,
  SectionHead,
  solidButton,
  Tile,
} from '@/components/settings-ui';
import { TextField } from '@/components/text-field';
import { usePickRepoFolder, useRegisterRepo, useRepos } from '@/modules/repos/api/repos';

export function ReposSettings() {
  const [name, setName] = useState('');
  const [path, setPath] = useState('');
  const [adding, setAdding] = useState(false);
  const { data: repos = [] } = useRepos();
  const register = useRegisterRepo();
  const picker = usePickRepoFolder();
  const showForm = adding || repos.length === 0;

  const submit = (e: FormEvent) => {
    e.preventDefault();
    register.mutate(
      { name, path },
      {
        onSuccess: () => {
          setName('');
          setPath('');
          setAdding(false);
        },
      },
    );
  };

  return (
    <>
      <SectionHead
        title="Repos"
        lead="Register a repo once. Each thread works in one; edits get their own worktree."
      >
        {repos.length > 0 && (
          <button
            type="button"
            className={adding ? outlineButton : solidButton}
            onClick={() => setAdding(!adding)}
          >
            {adding ? 'Cancel' : 'Add repo'}
          </button>
        )}
      </SectionHead>
      {repos.length > 0 && (
        <Box>
          {repos.map((repo) => (
            <div key={repo.id} className={rowClass}>
              <Tile>
                <svg
                  width="16"
                  height="16"
                  viewBox="0 0 24 24"
                  fill="none"
                  stroke="var(--color-ink)"
                  strokeWidth="1.8"
                  strokeLinecap="round"
                  strokeLinejoin="round"
                >
                  <path d="M3 7h6l2 2h10v10H3z" />
                </svg>
              </Tile>
              <RowText title={repo.name} detail={repo.path} mono />
            </div>
          ))}
        </Box>
      )}
      {showForm && (
        <form
          onSubmit={submit}
          className="flex flex-col gap-3 rounded-[20px] border border-sidebar-line bg-box p-4"
        >
          <div className="flex flex-col gap-3 sm:flex-row sm:items-end">
            <label className="flex min-w-0 flex-1 flex-col gap-2">
              <span className="text-xs text-muted">Repo name</span>
              <TextField
                size="sm"
                value={name}
                onChange={(e) => setName(e.target.value)}
                required
              />
            </label>
            <label className="flex min-w-0 flex-1 flex-col gap-2">
              <span className="text-xs text-muted">Local path</span>
              <span className="flex gap-2">
                <TextField
                  size="sm"
                  className="font-mono text-[13px]"
                  value={path}
                  onChange={(e) => setPath(e.target.value)}
                  required
                />
                <button
                  type="button"
                  className={outlineButton}
                  disabled={picker.isPending}
                  onClick={() =>
                    picker.mutate(undefined, {
                      onSuccess: (result) => {
                        if (!result) return;
                        setPath(result.path);
                        setName(
                          (current) =>
                            current || result.path.split('/').filter(Boolean).at(-1) || '',
                        );
                      },
                    })
                  }
                >
                  Choose…
                </button>
              </span>
            </label>
          </div>
          {picker.isError && <p className="text-sm text-danger">{picker.error.message}</p>}
          {register.isError && <p className="text-sm text-danger">{register.error.message}</p>}
          <button
            type="submit"
            disabled={register.isPending}
            className={`${solidButton} self-start`}
          >
            Register repo
          </button>
        </form>
      )}
    </>
  );
}
