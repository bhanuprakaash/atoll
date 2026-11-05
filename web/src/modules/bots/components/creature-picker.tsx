import { Creature, creatureLooks, type CreatureLook } from '@/components/creature';

export function CreaturePicker({
  value,
  onChange,
}: {
  value: CreatureLook;
  onChange: (look: CreatureLook) => void;
}) {
  return (
    <fieldset role="radiogroup">
      <legend className="mb-2 text-sm font-medium">Creature</legend>
      <div className="grid grid-cols-5 gap-2">
        {creatureLooks.map(({ look, name }) => (
          <label
            key={look}
            className="flex cursor-pointer flex-col items-center gap-1 rounded-control border border-line p-2 has-checked:border-accent"
          >
            <input
              className="sr-only"
              type="radio"
              name="creature"
              value={look}
              checked={value === look}
              onChange={() => onChange(look)}
            />
            <span aria-hidden="true">
              <Creature look={look} size={36} />
            </span>
            <span className="text-xs">{name}</span>
          </label>
        ))}
      </div>
    </fieldset>
  );
}
