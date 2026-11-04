import { useState, type FormEvent } from 'react';
import { Button } from '@/components/button';
import { controlClass, Field } from '@/components/field';
import { type Skill, useSaveSkill, useSkills } from '@/modules/skills/api/skills';

function SkillForm({ skill }: { skill?: Skill }) {
  const [name, setName] = useState(skill?.name ?? '');
  const [content, setContent] = useState(skill?.content ?? '');
  const save = useSaveSkill(skill?.id);

  const submit = (e: FormEvent) => {
    e.preventDefault();
    save.mutate(
      { name, content },
      {
        onSuccess: () => {
          if (!skill) {
            setName('');
            setContent('');
          }
        },
      },
    );
  };

  return (
    <form
      onSubmit={submit}
      aria-label={skill ? `Skill ${skill.name}` : 'New skill'}
      className="flex flex-col gap-4 rounded-card border border-line bg-card p-3"
    >
      <Field label="Skill name">
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
      <Field label="Skill content">
        {(id) => (
          <textarea
            id={id}
            className={`${controlClass} font-mono`}
            rows={6}
            value={content}
            onChange={(e) => setContent(e.target.value)}
            required
          />
        )}
      </Field>
      {save.isError && <p className="text-sm text-danger">{save.error.message}</p>}
      <Button type="submit" disabled={save.isPending} className="self-start">
        {skill ? 'Save skill' : 'Create skill'}
      </Button>
    </form>
  );
}

export function SkillsSettings() {
  const { data: skills = [] } = useSkills();
  return (
    <div className="flex max-w-2xl flex-col gap-6 p-6">
      <SkillForm />
      {skills.map((skill) => (
        <SkillForm key={skill.id} skill={skill} />
      ))}
    </div>
  );
}
