import { useSearchParams } from 'react-router';
import { useEffect, useRef, useState } from 'react';
import type { CreatureLook } from '@/components/creature';
import { Creature } from '@/components/creature';
import { Markdown } from '@/components/markdown';
import {
  Box,
  RowText,
  rowClass,
  SectionHead,
  solidButton,
  Tile,
  UsedBy,
} from '@/components/settings-ui';
import { TextArea, TextField } from '@/components/text-field';
import { type Skill, useSaveSkill, useSkills } from '@/modules/skills/api/skills';

type Bot = { id: string; name: string; look: CreatureLook; skills: string[] };

const labelClass = 'text-xs tracking-wide text-label';

function split(content: string) {
  const m = content.match(/^---\n([\s\S]*?)\n?---\n?([\s\S]*)$/);
  return m ? { head: m[1], body: m[2] } : { head: '', body: content };
}
const join = (head: string, body: string) => (head ? `---\n${head}\n---\n${body}` : body);
const descriptionLine = /^description:.*$/m;
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

function SkillEditor({ skill, bots, close }: { skill: Skill; bots: Bot[]; close: () => void }) {
  const [name, setName] = useState(skill.name);
  const [content, setContent] = useState(skill.content);
  const [preview, setPreview] = useState(false);
  const save = useSaveSkill(skill.id);
  const { head, body } = split(content);
  const description = head.match(/^description:\s*(.*)$/m)?.[1] ?? '';
  const users = bots.filter((b) => b.skills?.includes(skill.id));
  const dirty = name !== skill.name || content !== skill.content;
  function setDescription(value: string) {
    const line = `description: ${value}`;
    setContent(
      join(
        descriptionLine.test(head)
          ? head.replace(descriptionLine, line)
          : head
            ? `${line}\n${head}`
            : line,
        body,
      ),
    );
  }
  return (
    <aside
      role="dialog"
      aria-label={`Edit ${skill.name}`}
      className="fixed inset-y-0 right-0 z-10 flex w-140 max-w-full flex-col border-l border-line bg-card"
    >
      <div className="flex h-16 items-center gap-2.5 border-b border-line pl-5 pr-3">
        <Tile className="bg-tile">
          <FileIcon size={16} />
        </Tile>
        <h2 className="font-mono text-[15px] font-normal">{skill.name}</h2>
        <button
          type="button"
          aria-label="Close"
          onClick={close}
          className="ml-auto flex size-9 items-center justify-center rounded-[10px] text-muted"
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
        <label className="flex flex-col gap-2">
          <span className={labelClass}>Name</span>
          <TextField
            className="font-mono"
            size="sm"
            value={name}
            onChange={(e) => setName(e.target.value)}
          />
        </label>
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
        <div className="flex flex-col gap-2">
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
          <span className="text-danger">{save.error.message}</span>
        ) : dirty ? (
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
          disabled={!dirty || save.isPending}
          onClick={() => save.mutate({ name, content })}
        >
          Save skill
        </button>
      </div>
    </aside>
  );
}

export function SkillsSettings({ bots = [] }: { bots?: Bot[] }) {
  const { data: skills = [] } = useSkills();
  const [selected, setSelected] = useState<string | null>(null);
  const skill = skills.find((s) => s.id === selected);
  const create = useSaveSkill();
  const [params, setParams] = useSearchParams();
  const wantsNew = params.has('new');
  const startedNew = useRef(false);
  useEffect(() => {
    if (!wantsNew || startedNew.current) return;
    startedNew.current = true;
    setParams({}, { replace: true });
    create.mutate(
      { name: 'new-skill', content: '---\ndescription: \n---\n' },
      { onSuccess: (s) => setSelected(s.id) },
    );
  }, [wantsNew]);
  return (
    <>
      <SectionHead
        title="Skills"
        lead="One library. Each Bot picks its own; runs get read-only copies."
      >
        <button
          type="button"
          className={solidButton}
          disabled={create.isPending}
          onClick={() =>
            create.mutate(
              { name: 'new-skill', content: '---\ndescription: \n---\n' },
              { onSuccess: (s) => setSelected(s.id) },
            )
          }
        >
          New skill
        </button>
      </SectionHead>
      {create.isError && <p className="text-danger">{create.error.message}</p>}
      {skills.length === 0 ? (
        <div className="rounded-[18px] border border-dashed border-outline px-6 py-10 text-center">
          <p>No skills yet</p>
          <p className="mt-1 text-[13px] text-muted">
            A skill is a SKILL.md your Bots read when it applies. Create one with New skill.
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
                onClick={() => setSelected(s.id)}
              >
                <Tile>
                  <FileIcon size={16} />
                </Tile>
                <RowText
                  title={s.name}
                  monoTitle
                  detail={split(s.content).head.match(/^description:\s*(.*)$/m)?.[1]}
                />
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
        <SkillEditor key={skill.id} skill={skill} bots={bots} close={() => setSelected(null)} />
      )}
    </>
  );
}
