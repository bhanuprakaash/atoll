import { useBlocker, useSearchParams } from 'react-router';
import { useEffect, useRef, useState } from 'react';
import type { CreatureLook } from '@/components/creature';
import { Creature } from '@/components/creature';
import { Markdown } from '@/components/markdown';
import {
  Box,
  outlineButton,
  RowText,
  rowClass,
  SectionHead,
  solidButton,
  Tile,
  UsedBy,
} from '@/components/settings-ui';
import { TextArea, TextField } from '@/components/text-field';
import { join, setSkillDescription, skillDescription, split } from '@/utils/skill-description';
import {
  type Reimport,
  type Skill,
  type SkillFile,
  useReimportSkill,
  useSaveSkill,
  useSkills,
} from '@/modules/skills/api/skills';
import { ErrorNotice } from '@/components/error-notice';

type Bot = { id: string; name: string; look: CreatureLook; skills: string[] };

const labelClass = 'text-xs tracking-wide text-label';

const modes = [
  ['Edit', false],
  ['Preview', true],
] as const;

function FileIcon({ size }: { size: number }) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 24 24"
      fill="none"
      stroke="var(--color-ink)"
      strokeWidth="1.8"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
    >
      <path d="M4 4h10l6 6v10H4z" />
      <path d="M14 4v6h6" />
      <path d="M8 14h8M8 17h5" />
    </svg>
  );
}

const draft: Skill = {
  id: '',
  name: '',
  content: '---\ndescription: \n---\n',
  source: '',
  files: [],
};

type Draft = Pick<Skill, 'name' | 'content' | 'files'>;
const toDraft = ({ name, content, files }: Skill): Draft => ({ name, content, files });
const same = (a: Draft, b: Draft) => JSON.stringify(a) === JSON.stringify(b);
const skillMd = 'SKILL.md';

function pathProblem(path: string, files: SkillFile[]) {
  const parts = path.split('/');
  if (path.startsWith('/') || parts.includes('..')) {
    return 'Use a path inside the skill: no .. and no leading /.';
  }
  if (!path || parts.some((p) => p === '' || p === '.')) {
    return 'Type a relative path such as scripts/check.';
  }
  if (path === skillMd || files.some((f) => f.path === path)) return `${path} already exists.`;
  return '';
}

function ReimportResult({
  result,
  cancel,
  confirm,
}: {
  result: Reimport;
  cancel: () => void;
  confirm: () => void;
}) {
  const lists = [
    ['Added', result.added],
    ['Changed', result.changed],
    ['Removed', result.removed],
  ] as const;
  const none = lists.every(([, paths]) => paths.length === 0);
  return (
    <section
      aria-label="Re-import"
      className="flex flex-col gap-3 rounded-2xl border border-line p-4 text-[13px]"
    >
      {none ? (
        <p>No changes</p>
      ) : (
        lists.map(
          ([title, paths]) =>
            paths.length > 0 && (
              <div key={title}>
                <h3 className={labelClass}>{title}</h3>
                <ul aria-label={title} className="font-mono">
                  {paths.map((p) => (
                    <li key={p}>{p}</li>
                  ))}
                </ul>
              </div>
            ),
        )
      )}
      {result.status === 'confirm' ? (
        <>
          <p role="alert" className="text-blocked">
            This skill was edited in Reef since its last import. Those edits will be overwritten.
          </p>
          <div className="flex justify-end gap-2">
            <button type="button" className={outlineButton} onClick={cancel}>
              Cancel
            </button>
            <button type="button" className={solidButton} onClick={confirm}>
              Confirm
            </button>
          </div>
        </>
      ) : (
        <p className="text-muted">{none ? 'Up to date with its folder.' : 'Replaced.'}</p>
      )}
    </section>
  );
}

function SkillEditor({
  skill,
  bots,
  close,
  onCreated,
  onDirty,
}: {
  skill: Skill;
  bots: Bot[];
  close: () => void;
  onCreated: (id: string) => void;
  onDirty: (dirty: boolean) => void;
}) {
  const isNew = !skill.id;
  const [base, setBase] = useState(() => toDraft(skill));
  const [edit, setEdit] = useState(base);
  const [open, setOpen] = useState(skillMd);
  const [manual, setManual] = useState<string[]>([]);
  const [adding, setAdding] = useState<string | null>(null);
  const [addError, setAddError] = useState('');
  const [preview, setPreview] = useState(false);
  const [result, setResult] = useState<Reimport | null>(null);
  const save = useSaveSkill(skill.id || undefined);
  const reimport = useReimportSkill(skill.id);
  const { name, content, files } = edit;
  const { head, body } = split(content);
  const description = skillDescription(content);
  const users = bots.filter((b) => b.skills?.includes(skill.id));
  const changed = !same(edit, base);
  const canSave = isNew ? name.trim() !== '' : changed;
  const file = files.find((f) => f.path === open);
  useEffect(() => onDirty(changed), [changed]);
  useEffect(() => () => onDirty(false), []);
  useEffect(() => {
    if (isNew || changed) return;
    const next = toDraft(skill);
    setBase(next);
    setEdit(next);
    if (open !== skillMd && !next.files.some((f) => f.path === open)) setOpen(skillMd);
  }, [skill]);
  const setContent = (value: string) => setEdit({ ...edit, content: value });
  function setFile(path: string, patch: Partial<SkillFile>) {
    setEdit({
      ...edit,
      files: files.map((f) => (f.path === path ? { ...f, ...patch } : f)),
    });
  }
  const saveSkill = () =>
    save.mutate(
      { ...edit, name: name.trim() },
      {
        onSuccess: (s) => {
          if (isNew) onCreated(s.id);
          else setBase(toDraft(s));
        },
      },
    );
  function addFile(path: string) {
    const problem = pathProblem(path, files);
    setAddError(problem);
    if (problem) return;
    setEdit({
      ...edit,
      files: [...files, { path, content: '', executable: false }].sort((a, b) =>
        a.path < b.path ? -1 : 1,
      ),
    });
    setAdding(null);
    setOpen(path);
  }
  function setDescription(value: string) {
    setContent(join(setSkillDescription(head, value), body));
  }
  const runReimport = (replace: boolean) =>
    reimport.mutate(replace, { onSuccess: (r) => setResult(r) });
  return (
    <aside
      role="dialog"
      aria-label={isNew ? 'New skill' : `Edit ${skill.name}`}
      className="fixed inset-y-0 right-0 z-10 flex w-140 max-w-full flex-col border-l border-line bg-card"
    >
      <div className="flex h-16 items-center gap-2.5 border-b border-line pl-5 pr-3">
        <Tile className="bg-tile">
          <FileIcon size={16} />
        </Tile>
        <h2 className="font-mono text-[15px] font-normal">
          {isNew ? name || 'New skill' : skill.name}
        </h2>
        {skill.source && (
          <button
            type="button"
            className={`${outlineButton} ml-auto`}
            title={`Read ${skill.source} again`}
            disabled={changed || reimport.isPending}
            onClick={() => runReimport(false)}
          >
            Re-import
          </button>
        )}
        <button
          type="button"
          aria-label="Close"
          onClick={close}
          className={`${skill.source ? '' : 'ml-auto'} flex size-9 items-center justify-center rounded-[10px] text-muted`}
        >
          <svg
            width="18"
            height="18"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            strokeWidth="1.8"
            strokeLinecap="round"
            aria-hidden="true"
          >
            <path d="M6 6l12 12M18 6L6 18" />
          </svg>
        </button>
      </div>
      <div className="flex flex-1 flex-col gap-5 overflow-auto p-5">
        {reimport.isError && (
          <ErrorNotice error={reimport.error} retry={() => runReimport(false)} />
        )}
        {result && (
          <ReimportResult
            result={result}
            cancel={() => setResult(null)}
            confirm={() => runReimport(true)}
          />
        )}
        <label className="flex flex-col gap-2">
          <span className={labelClass}>Name</span>
          <TextField
            className="font-mono"
            size="sm"
            placeholder="review-mr"
            value={name}
            onChange={(e) => setEdit({ ...edit, name: e.target.value })}
          />
        </label>
        <div className="flex flex-col gap-2">
          <div className={`flex items-center ${labelClass}`}>
            <span>Files</span>
            <button
              type="button"
              className="ml-auto text-xs tracking-normal text-ink"
              onClick={() => {
                setAdding('');
                setAddError('');
              }}
            >
              + File
            </button>
          </div>
          {adding !== null && (
            <form
              className="flex flex-col gap-1.5"
              onSubmit={(e) => {
                e.preventDefault();
                addFile(adding.trim());
              }}
            >
              <div className="flex gap-2">
                <TextField
                  aria-label="File path"
                  className="font-mono"
                  size="sm"
                  placeholder="scripts/check"
                  autoFocus
                  value={adding}
                  onChange={(e) => setAdding(e.target.value)}
                />
                <button type="submit" className={outlineButton}>
                  Add
                </button>
              </div>
              {addError && <ErrorNotice title={addError} next="Fix the path and try again." />}
            </form>
          )}
          <ul aria-label="Files" className="flex flex-col rounded-2xl border border-line py-1">
            {[skillMd, ...files.map((f) => f.path)].map((path) => {
              const script = files.find((f) => f.path === path)?.executable;
              return (
                <li key={path}>
                  <button
                    type="button"
                    aria-label={path}
                    aria-current={open === path}
                    onClick={() => setOpen(path)}
                    className={`flex h-8 w-full items-center gap-2 px-3 text-left font-mono text-[13px] ${open === path ? 'bg-line' : ''}`}
                  >
                    {path}
                    {script && <span className="ml-auto font-sans text-xs text-muted">script</span>}
                  </button>
                </li>
              );
            })}
          </ul>
        </div>
        {file ? (
          <div className="flex flex-1 flex-col gap-2">
            <div className={`flex items-center gap-3 ${labelClass}`}>
              <span className="font-mono tracking-normal text-ink">{file.path}</span>
              <label className="ml-auto flex items-center gap-1.5 tracking-normal">
                <input
                  type="checkbox"
                  className="accent-accent"
                  checked={file.executable}
                  onChange={(e) => {
                    setManual([...manual, file.path]);
                    setFile(file.path, { executable: e.target.checked });
                  }}
                />
                Script
              </label>
              <button
                type="button"
                className="tracking-normal text-danger"
                onClick={() => {
                  setEdit({ ...edit, files: files.filter((f) => f.path !== file.path) });
                  setOpen(skillMd);
                }}
              >
                Delete file
              </button>
            </div>
            <TextArea
              aria-label="Content"
              variant="code"
              value={file.content}
              onChange={(e) => {
                const value = e.target.value;
                setFile(file.path, {
                  content: value,
                  executable: manual.includes(file.path) ? file.executable : value.startsWith('#!'),
                });
              }}
            />
          </div>
        ) : (
          <>
            <label className="flex flex-col gap-2">
              <span className={labelClass}>When to use it</span>
              <TextField
                size="sm"
                value={description}
                onChange={(e) => setDescription(e.target.value)}
              />
              <span className="text-xs text-muted">
                The Bot reads this to decide when the skill applies.
              </span>
            </label>
            <div className="flex flex-1 flex-col gap-2">
              <div className={`flex items-center ${labelClass}`}>
                <label htmlFor="skill-instructions">Instructions</label>
                <span className="ml-auto flex gap-1 rounded-full bg-field p-[3px] text-xs tracking-normal">
                  {modes.map(([label, on]) => (
                    <button
                      key={label}
                      type="button"
                      aria-pressed={preview === on}
                      onClick={() => setPreview(on)}
                      className={`h-6 rounded-full px-2.5 ${preview === on ? 'bg-outline text-ink' : 'text-muted'}`}
                    >
                      {label}
                    </button>
                  ))}
                </span>
              </div>
              {preview ? (
                <div className="min-h-[420px] rounded-2xl border border-line bg-well p-4 text-[13px] leading-[1.7] text-ink/80">
                  <Markdown>{body}</Markdown>
                </div>
              ) : (
                <TextArea
                  id="skill-instructions"
                  variant="code"
                  value={body}
                  onChange={(e) => setContent(join(head, e.target.value))}
                />
              )}
            </div>
          </>
        )}
        <div className={`flex flex-col gap-2 ${isNew ? 'hidden' : ''}`}>
          <span className={labelClass}>Used by</span>
          {users.length > 0 ? (
            <ul className="flex flex-wrap gap-1.5">
              {users.map((b) => (
                <li
                  key={b.id}
                  className="flex h-8 items-center gap-1.5 rounded-full border border-outline bg-tile pl-1.5 pr-3 text-[13px]"
                >
                  <Creature look={b.look} size={22} />
                  {b.name}
                </li>
              ))}
            </ul>
          ) : (
            <p className="text-[13px] text-muted">No Bots yet</p>
          )}
        </div>
      </div>
      <div className="flex items-center gap-2 border-t border-line px-5 py-3.5 text-[12.5px] text-muted">
        {save.isError ? (
          <ErrorNotice error={save.error} retry={saveSkill} className="min-w-0 flex-1" />
        ) : isNew ? (
          'Not created yet'
        ) : changed ? (
          'Unsaved changes'
        ) : (
          <>
            <svg
              width="13"
              height="13"
              viewBox="0 0 24 24"
              fill="none"
              stroke="var(--color-ok)"
              strokeWidth="2.4"
              strokeLinecap="round"
              strokeLinejoin="round"
              aria-hidden="true"
            >
              <path d="M5 12l5 5 9-10" />
            </svg>
            Saved · next runs get this version
          </>
        )}
        <button
          type="button"
          className={`${solidButton} ml-auto`}
          disabled={!canSave || save.isPending}
          onClick={saveSkill}
        >
          {isNew ? 'Create skill' : 'Save skill'}
        </button>
      </div>
    </aside>
  );
}

function DiscardDialog({ keep, discard }: { keep: () => void; discard: () => void }) {
  return (
    <div className="fixed inset-0 z-20 flex items-center justify-center bg-bg/60">
      <div
        role="dialog"
        aria-modal="true"
        aria-labelledby="discard-title"
        className="flex w-90 flex-col gap-4 rounded-2xl border border-line bg-card p-5"
      >
        <h2 id="discard-title" className="text-[15px] font-normal">
          Discard unsaved changes?
        </h2>
        <p className="text-[13px] text-muted">Your edits to this skill are not saved.</p>
        <div className="flex justify-end gap-2">
          <button type="button" className={outlineButton} onClick={keep}>
            Keep editing
          </button>
          <button type="button" className={solidButton} onClick={discard}>
            Discard
          </button>
        </div>
      </div>
    </div>
  );
}

export function SkillsSettings({ bots = [] }: { bots?: Bot[] }) {
  const { data: skills = [] } = useSkills();
  const [selected, setSelected] = useState<string | null>(null);
  const [dirty, setDirty] = useState(false);
  const [leave, setLeave] = useState<{ go: () => void } | null>(null);
  const blocker = useBlocker(dirty);
  const skill = selected === 'new' ? draft : skills.find((s) => s.id === selected);
  const [params, setParams] = useSearchParams();
  const wantsNew = params.has('new');
  const startedNew = useRef(false);
  const guard = (go: () => void) => (dirty ? setLeave({ go }) : go());
  useEffect(() => {
    if (!wantsNew || startedNew.current) return;
    startedNew.current = true;
    setParams({}, { replace: true });
    setSelected('new');
  }, [wantsNew]);
  useEffect(() => {
    if (!dirty) return;
    function onUnload(e: BeforeUnloadEvent) {
      e.preventDefault();
    }
    window.addEventListener('beforeunload', onUnload);
    return () => window.removeEventListener('beforeunload', onUnload);
  }, [dirty]);
  return (
    <>
      <SectionHead
        title="Skills"
        lead="One library. Each Bot picks its own; runs get read-only copies."
      >
        <button
          type="button"
          className={solidButton}
          onClick={() => guard(() => setSelected('new'))}
        >
          New skill
        </button>
      </SectionHead>
      {skills.length === 0 ? (
        <div className="rounded-[18px] border border-dashed border-outline px-6 py-10 text-center">
          <p>No skills yet</p>
          <p className="mt-1 text-[13px] text-muted">
            A skill is a SKILL.md and its files, which your Bots read when it applies. Create one
            with New skill.
          </p>
        </div>
      ) : (
        <Box>
          {skills.map((s) => {
            const users = bots.filter((b) => b.skills?.includes(s.id));
            return (
              <button
                key={s.id}
                type="button"
                className={`${rowClass} w-full text-left ${s.id === selected ? 'bg-line' : ''}`}
                onClick={() => s.id !== selected && guard(() => setSelected(s.id))}
              >
                <Tile>
                  <FileIcon size={16} />
                </Tile>
                <RowText title={s.name} monoTitle detail={skillDescription(s.content)} />
                {users.length > 0 ? (
                  <UsedBy bots={users} />
                ) : (
                  <span className="text-xs text-muted">unused</span>
                )}
                <svg
                  width="14"
                  height="14"
                  viewBox="0 0 24 24"
                  fill="none"
                  stroke="var(--color-muted)"
                  strokeWidth="2"
                  strokeLinecap="round"
                  aria-hidden="true"
                >
                  <path d="M9 6l6 6-6 6" />
                </svg>
              </button>
            );
          })}
        </Box>
      )}
      {skill && (
        <SkillEditor
          key={skill.id || 'new'}
          skill={skill}
          bots={bots}
          close={() => guard(() => setSelected(null))}
          onCreated={setSelected}
          onDirty={setDirty}
        />
      )}
      {(leave || blocker.state === 'blocked') && (
        <DiscardDialog
          keep={() => {
            setLeave(null);
            if (blocker.state === 'blocked') blocker.reset();
          }}
          discard={() => {
            setLeave(null);
            setDirty(false);
            if (blocker.state === 'blocked') blocker.proceed();
            else leave?.go();
          }}
        />
      )}
    </>
  );
}
