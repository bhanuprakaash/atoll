import { useState } from 'react';
import { BrandLogo, type Brand } from '@/components/brand-logo';
import { TextField } from '@/components/text-field';

export function ChoiceList({
  title,
  singular,
  choices,
  selected,
  onChange,
}: {
  title: string;
  singular: string;
  choices: { id: string; name: string; brand?: Brand }[];
  selected: string[];
  onChange: (ids: string[]) => void;
}) {
  const [open, setOpen] = useState(false);
  const [search, setSearch] = useState('');
  return (
    <section>
      <h3 className="mb-2 text-sm">{title}</h3>
      <div className="border-t border-line">
        {choices
          .filter((c) => selected.includes(c.id))
          .map((c) => (
            <div
              key={c.id}
              className="flex items-center justify-between border-b border-line py-3 text-sm"
            >
              <span className="flex items-center gap-2.5">
                {c.brand && <BrandLogo brand={c.brand} className="size-4" />}
                {c.name}
              </span>
              <button
                type="button"
                className="text-muted hover:text-ink"
                aria-label={`Remove ${c.name}`}
                onClick={() => onChange(selected.filter((id) => id !== c.id))}
              >
                ×
              </button>
            </div>
          ))}
        <button
          type="button"
          className="py-3 text-sm text-muted hover:text-ink"
          onClick={() => setOpen(!open)}
        >
          + Add {singular}
        </button>
        {open && (
          <div role="menu" className="rounded-control border border-line bg-card p-2">
            <TextField
              size="sm"
              aria-label={`Search ${title}`}
              placeholder={`Search ${title.toLowerCase()}`}
              value={search}
              onChange={(e) => setSearch(e.target.value)}
            />
            {choices
              .filter(
                (c) =>
                  !selected.includes(c.id) && c.name.toLowerCase().includes(search.toLowerCase()),
              )
              .map((c) => (
                <button
                  type="button"
                  role="menuitem"
                  key={c.id}
                  className="flex w-full items-center gap-2.5 px-3 py-2 text-left text-sm hover:bg-composer"
                  onClick={() => {
                    onChange([...selected, c.id]);
                    setOpen(false);
                    setSearch('');
                  }}
                >
                  {c.brand && <BrandLogo brand={c.brand} className="size-4" />}
                  {c.name}
                </button>
              ))}
          </div>
        )}
      </div>
    </section>
  );
}
