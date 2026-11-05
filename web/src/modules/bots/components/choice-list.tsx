import { useState } from 'react';

export function ChoiceList({
  title,
  singular,
  choices,
  selected,
  onChange,
}: {
  title: string;
  singular: string;
  choices: { id: string; name: string }[];
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
              <span>{c.name}</span>
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
            <input
              aria-label={`Search ${title}`}
              placeholder={`Search ${title.toLowerCase()}`}
              className="w-full rounded-control bg-composer px-3 py-2 text-sm"
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
                  className="block w-full px-3 py-2 text-left text-sm hover:bg-composer"
                  onClick={() => {
                    onChange([...selected, c.id]);
                    setOpen(false);
                    setSearch('');
                  }}
                >
                  {c.name}
                </button>
              ))}
          </div>
        )}
      </div>
    </section>
  );
}
