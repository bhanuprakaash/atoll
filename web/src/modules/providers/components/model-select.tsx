import { useId, useState } from 'react';
import { controlClass } from '@/components/field';
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
  const [open, setOpen] = useState(false);
  const [search, setSearch] = useState('');
  const id = useId();
  const selected = providers.find((p) => p.name === provider);
  const selectedModel = selected?.models.find((m) => m.id === model);
  return (
    <div className="relative flex flex-col gap-1">
      <label id={id} className="text-sm">
        Model
      </label>
      <button
        type="button"
        role="combobox"
        aria-labelledby={id}
        aria-expanded={open}
        aria-controls={`${id}-list`}
        className={`${controlClass} flex justify-between text-left`}
        onClick={() => setOpen(!open)}
      >
        <span>
          {selectedModel ? `${selected?.label} · ${selectedModel.name}` : 'Choose a model'}
        </span>
        <span aria-hidden="true">⌄</span>
      </button>
      {open && (
        <div className="absolute top-full z-30 mt-1 w-full rounded-control border border-line bg-card p-2 shadow-xl">
          <input
            aria-label="Search models"
            className={controlClass}
            placeholder="Search models"
            value={search}
            onChange={(e) => setSearch(e.target.value)}
          />
          <div id={`${id}-list`} role="listbox" className="max-h-64 overflow-auto">
            {providers
              .filter((p) => p.signed_in)
              .map((p) => {
                const models = p.models.filter((m) =>
                  `${p.label} ${m.name}`.toLowerCase().includes(search.toLowerCase()),
                );
                return (
                  models.length > 0 && (
                    <div key={p.name} role="group" aria-label={p.label}>
                      <div className="flex justify-between px-3 pt-3 pb-1 text-xs text-muted">
                        <span>{p.label}</span>
                        <span>Ready</span>
                      </div>
                      {models.map((m) => (
                        <button
                          key={m.id}
                          type="button"
                          role="option"
                          aria-selected={p.name === provider && m.id === model}
                          className="block w-full rounded-control px-3 py-2 text-left text-sm hover:bg-composer"
                          onClick={() => {
                            onChange(p.name, m.id);
                            setOpen(false);
                            setSearch('');
                          }}
                        >
                          {m.name}
                        </button>
                      ))}
                    </div>
                  )
                );
              })}
          </div>
        </div>
      )}
    </div>
  );
}
