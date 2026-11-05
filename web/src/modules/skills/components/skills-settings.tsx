import { useState } from 'react';
import { Button } from '@/components/button';
import { controlClass, Field } from '@/components/field';
import { type Skill, useSaveSkill, useSkills } from '@/modules/skills/api/skills';

type Bot = { name: string; skills: string[] };
function SkillEditor({ skill, bots, close }: { skill: Skill; bots: Bot[]; close: () => void }) {
  const [name, setName] = useState(skill.name);
  const [content, setContent] = useState(skill.content);
  const save = useSaveSkill(skill.id);
  const description = content.match(/^description:\s*(.*)$/m)?.[1] ?? '';
  function setDescription(value: string) {
    if (/^description:.*$/m.test(content))
      setContent(content.replace(/^description:.*$/m, `description: ${value}`));
    else if (content.startsWith('---\n'))
      setContent(content.replace('---\n', `---\ndescription: ${value}\n`));
    else setContent(`---\ndescription: ${value}\n---\n${content}`);
  }
  return (
    <aside
      role="dialog"
      aria-label={`Edit ${skill.name}`}
      className="flex w-full max-w-3xl flex-col gap-4 border-l border-line bg-card p-6"
    >
      <div className="flex justify-between">
        <h2 className="text-xl">Edit skill</h2>
        <button onClick={close}>Close</button>
      </div>
      <Field label="Name">
        {(id) => (
          <input
            id={id}
            className={controlClass}
            value={name}
            onChange={(e) => setName(e.target.value)}
          />
        )}
      </Field>
      <Field label="When to use it">
        {(id) => (
          <input
            id={id}
            className={controlClass}
            value={description}
            onChange={(e) => setDescription(e.target.value)}
          />
        )}
      </Field>
      <Field label="SKILL.md">
        {(id) => (
          <textarea
            id={id}
            className={`${controlClass} font-mono`}
            rows={18}
            value={content}
            onChange={(e) => setContent(e.target.value)}
          />
        )}
      </Field>
      <div>
        <h3>Bots that use it</h3>
        <p className="text-sm text-muted">
          {bots
            .filter((b) => b.skills?.includes(skill.id))
            .map((b) => b.name)
            .join(', ') || 'No Bots yet'}
        </p>
      </div>
      {save.isError && <p className="text-danger">{save.error.message}</p>}
      <Button className="self-start" onClick={() => save.mutate({ name, content })}>
        Save skill
      </Button>
    </aside>
  );
}
export function SkillsSettings({ bots = [] }: { bots?: Bot[] }) {
  const { data: skills = [] } = useSkills();
  const [selected, setSelected] = useState<string | null>(null);
  const skill = skills.find((s) => s.id === selected);
  const create = useSaveSkill();
  return (
    <div className="flex h-full">
      <div className="min-w-64 flex-1 p-6">
        <div className="mb-4 flex items-center justify-between">
          <h1 className="text-2xl">Skills</h1>
          <Button
            disabled={create.isPending}
            onClick={() =>
              create.mutate(
                { name: 'new-skill', content: '---\ndescription: \n---\n' },
                { onSuccess: (s) => setSelected(s.id) },
              )
            }
          >
            New skill
          </Button>
        </div>
        {create.isError && <p className="text-danger">{create.error.message}</p>}
        {skills.map((s) => (
          <button
            key={s.id}
            className="block w-full border-b border-line py-3 text-left"
            onClick={() => setSelected(s.id)}
          >
            {s.name}
          </button>
        ))}
      </div>
      {skill && (
        <SkillEditor key={skill.id} skill={skill} bots={bots} close={() => setSelected(null)} />
      )}
    </div>
  );
}
