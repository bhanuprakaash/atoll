import { useEffect, useState, type FormEvent } from 'react';
import {
  Box,
  outlineButton,
  RowText,
  rowClass,
  SectionHead,
  solidButton,
  Tag,
  Tile,
} from '@/components/settings-ui';
import { TextField } from '@/components/text-field';
import { usePickRepoFolder, useRegisterRepo, useRepos } from '@/modules/repos/api/repos';

function RepoForm({ onDone, onCancel }: { onDone?: () => void; onCancel?: () => void }) {
  const [name, setName] = useState('');
  const [path, setPath] = useState('');
  const register = useRegisterRepo();
  const picker = usePickRepoFolder();
  const pick = () =>
    picker.mutate(undefined, {
      onSuccess: (result) => {
        if (!result) return;
        setPath(result.path);
        setName((current) => current || result.path.split('/').filter(Boolean).at(-1) || '');
      },
    });
  const submit = (e: FormEvent) => {
    e.preventDefault();
    register.mutate(
      { name, path },
      {
        onSuccess: () => {
          setName('');
          setPath('');
          onDone?.();
        },
      },
    );
  };
  return (
    <form onSubmit={submit} className="flex flex-col gap-4">
      <label className="flex flex-col gap-2">
        <span className="text-xs tracking-wide text-label">Repo name</span>
        <TextField
          size="sm"
          autoFocus={!!onCancel}
          placeholder="atoll"
          value={name}
          onChange={(e) => setName(e.target.value)}
          pattern="[A-Za-z0-9._\-]+"
          title="Letters, digits, dot, dash and underscore"
          required
        />
      </label>
      <label className="flex flex-col gap-2">
        <span className="text-xs tracking-wide text-label">Local path</span>
        <span className="flex gap-2">
          <TextField
            size="sm"
            className="font-mono text-[13px]"
            placeholder="~/code/atoll"
            value={path}
            onChange={(e) => setPath(e.target.value)}
            required
          />
          <button
            type="button"
            className={outlineButton}
            disabled={picker.isPending}
            onClick={pick}
          >
            Choose…
          </button>
        </span>
      </label>
      {picker.isError && <p className="text-sm text-danger">{picker.error.message}</p>}
      {register.isError && <p className="text-sm text-danger">{register.error.message}</p>}
      <div className="flex justify-end gap-2">
        {onCancel && (
          <button type="button" className={outlineButton} onClick={onCancel}>
            Cancel
          </button>
        )}
        <button type="submit" disabled={register.isPending} className={solidButton}>
          Register repo
        </button>
      </div>
    </form>
  );
}

function AddRepoDialog({ onClose }: { onClose: () => void }) {
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => e.key === 'Escape' && onClose();
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [onClose]);
  return (
    <div
      className="fixed inset-0 z-30 flex items-center justify-center bg-scrim p-4"
      onMouseDown={(e) => e.target === e.currentTarget && onClose()}
    >
      <section
        role="dialog"
        aria-modal="true"
        aria-label="Add repo"
        className="w-full max-w-110 rounded-[22px] border border-composer-line bg-popover p-6 shadow-[0_18px_48px_rgba(0,0,0,.55)]"
      >
        <h2 className="mb-1 text-base">Add repo</h2>
        <p className="mb-5 text-[13px] text-muted">Pick a folder with a git repo in it.</p>
        <RepoForm onDone={onClose} onCancel={onClose} />
      </section>
    </div>
  );
}

export function ReposSettings() {
  const [adding, setAdding] = useState(false);
  const { data: repos = [] } = useRepos();
  return (
    <>
      <SectionHead
        title="Repos"
        lead="Register a repo once. Each thread works in one; edits get their own worktree."
      >
        {repos.length > 0 && (
          <button type="button" className={solidButton} onClick={() => setAdding(true)}>
            Add repo
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
              {repo.host === 'github' && <Tag>GitHub</Tag>}
            </div>
          ))}
        </Box>
      )}
      {repos.length === 0 && (
        <div className="rounded-[20px] border border-sidebar-line bg-box p-5">
          <RepoForm />
        </div>
      )}
      {adding && <AddRepoDialog onClose={() => setAdding(false)} />}
    </>
  );
}
