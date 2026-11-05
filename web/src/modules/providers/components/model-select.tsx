import { useId } from 'react';
import { BrandLogo } from '@/components/brand-logo';
import { Select } from '@/components/select';
import type { Provider } from '@/modules/providers/api/providers';

const key = (provider: string, model: string) => `${provider}:${model}`;

export function ModelSelect({
  providers,
  provider,
  model,
  onChange,
}: {
  providers: Provider[];
  provider: Provider['name'];
  model: string;
  onChange: (provider: Provider['name'], model: string) => void;
}) {
  const id = useId();
  const groups = providers
    .filter((p) => p.signed_in)
    .map((p) => ({
      label: p.label,
      meta: 'Ready',
      logo: (
        <span
          aria-hidden="true"
          className={`flex size-[26px] flex-none items-center justify-center rounded-lg ${p.name === 'claude' ? 'bg-claude-bg' : 'bg-tile'}`}
        >
          <BrandLogo brand={p.name} className="size-4" />
        </span>
      ),
      options: p.models.map((m) => ({ value: key(p.name, m.id), label: m.name })),
    }));
  return (
    <div className="flex flex-col gap-2">
      <label id={id} className="text-xs tracking-wide text-muted">
        Model
      </label>
      <Select
        labelledBy={id}
        value={key(provider, model)}
        groups={groups}
        placeholder="Choose a model"
        search="Search models"
        onChange={(value) => {
          const at = value.indexOf(':');
          onChange(value.slice(0, at) as Provider['name'], value.slice(at + 1));
        }}
      />
    </div>
  );
}
