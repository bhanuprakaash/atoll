import { useState } from 'react';
import { controlClass, Field } from '@/components/field';
import { type Settings, useSaveSettings, useSettings } from '@/modules/settings/api/settings';

function GeneralForm({ settings }: { settings: Settings }) {
  const [name, setName] = useState(settings.user_name);
  const [root, setRoot] = useState(settings.worktree_root);
  const save = useSaveSettings();
  function persist() {
    if (name !== settings.user_name || root !== settings.worktree_root)
      save.mutate({ user_name: name, worktree_root: root });
  }
  return (
    <div className="flex max-w-xl flex-col gap-5 p-6">
      <h1 className="text-2xl font-medium">General</h1>
      <Field label="Your name">
        {(id) => (
          <input
            id={id}
            className={controlClass}
            value={name}
            onChange={(e) => setName(e.target.value)}
            onBlur={persist}
          />
        )}
      </Field>
      <Field label="Worktree folder">
        {(id) => (
          <input
            id={id}
            className={controlClass}
            value={root}
            onChange={(e) => setRoot(e.target.value)}
            onBlur={persist}
          />
        )}
      </Field>
      {save.isError && <p className="text-danger">{save.error.message}</p>}
    </div>
  );
}
export function GeneralSettings() {
  const { data } = useSettings();
  return data && <GeneralForm settings={data} />;
}
