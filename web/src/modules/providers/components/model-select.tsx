import { controlClass, Field } from '@/components/field';
import type { Provider } from '@/modules/providers/api/providers';

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
  return (
    <Field label="Model">
      {(id) => (
        <select
          id={id}
          className={controlClass}
          value={model}
          onChange={(e) => {
            const selected = e.currentTarget.selectedOptions[0];
            const name = selected?.parentElement?.getAttribute('data-provider') as
              Provider['name'] | null;
            if (name) onChange(name, e.target.value);
          }}
        >
          {!providers.some(
            (p) => p.name === provider && p.signed_in && p.models.some((m) => m.id === model),
          ) && <option value="">Choose a model</option>}
          {providers
            .filter((p) => p.signed_in)
            .map((p) => (
              <optgroup key={p.name} label={p.label} data-provider={p.name}>
                {p.models.map((m) => (
                  <option key={m.id} value={m.id}>
                    {m.name}
                  </option>
                ))}
              </optgroup>
            ))}
        </select>
      )}
    </Field>
  );
}
