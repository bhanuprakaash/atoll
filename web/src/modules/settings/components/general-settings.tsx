import { useState } from 'react';
import { Box, RowText, rowClass, SectionHead } from '@/components/settings-ui';
import { type Settings, useSaveSettings, useSettings } from '@/modules/settings/api/settings';

const inputClass =
  'h-9 rounded-xl border border-outline bg-bg px-3 text-sm text-ink outline-none focus:border-muted';

function GeneralForm({ settings }: { settings: Settings }) {
  const [name, setName] = useState(settings.user_name);
  const [root, setRoot] = useState(settings.worktree_root);
  const save = useSaveSettings();
  function persist() {
    if (name !== settings.user_name || root !== settings.worktree_root)
      save.mutate({ ...settings, user_name: name, worktree_root: root });
  }
  return (
    <>
      <SectionHead title="General" lead="Who you are to your Bots, and where their work goes." />
      <Box>
        <label className={rowClass}>
          <RowText title="Your name" detail="Bots use it when they talk to you" />
          <input
            className={`${inputClass} w-55`}
            value={name}
            onChange={(e) => setName(e.target.value)}
            onBlur={persist}
          />
        </label>
        <label className={rowClass}>
          <RowText title="Worktree folder" detail="Where Bots make a worktree for each edit" />
          <input
            className={`${inputClass} w-80 font-mono text-[13px]`}
            value={root}
            onChange={(e) => setRoot(e.target.value)}
            onBlur={persist}
          />
        </label>
        <div className={rowClass}>
          <RowText title="App data" detail={settings.data_dir} mono />
        </div>
      </Box>
      {save.isError && <p className="text-danger">{save.error.message}</p>}
    </>
  );
}
export function GeneralSettings() {
  const { data } = useSettings();
  return data && <GeneralForm settings={data} />;
}
