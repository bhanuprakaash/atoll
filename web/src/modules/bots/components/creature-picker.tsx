import { Creature, creatureLooks, type CreatureLook } from '@/components/creature';
import { Segmented } from '@/components/segmented';

function Arrow({ label, d, onClick }: { label: string; d: string; onClick: () => void }) {
  return (
    <button
      type="button"
      aria-label={label}
      onClick={onClick}
      className="flex size-9 items-center justify-center rounded-full border border-outline text-muted hover:bg-tile hover:text-ink"
    >
      <svg
        width="16"
        height="16"
        viewBox="0 0 24 24"
        fill="none"
        stroke="currentColor"
        strokeWidth="2"
        strokeLinecap="round"
        strokeLinejoin="round"
        aria-hidden="true"
      >
        <path d={d} />
      </svg>
    </button>
  );
}

export function CreatureCarousel({
  value,
  onChange,
}: {
  value: CreatureLook;
  onChange: (look: CreatureLook) => void;
}) {
  const i = Math.max(
    0,
    creatureLooks.findIndex((c) => c.look === value),
  );
  const step = (by: number) =>
    onChange(creatureLooks[(i + by + creatureLooks.length) % creatureLooks.length].look);
  return (
    <div className="flex flex-col items-center gap-3 py-2">
      <div className="flex w-full max-w-80 items-center justify-between">
        <Arrow label="Previous look" d="M15 6l-6 6 6 6" onClick={() => step(-1)} />
        <Creature look={value} size={104} />
        <Arrow label="Next look" d="M9 6l6 6-6 6" onClick={() => step(1)} />
      </div>
      <span className="text-[13px] text-muted">{creatureLooks[i].name}</span>
      <Segmented
        label="Creature"
        value={value}
        onChange={(look) => onChange(look as CreatureLook)}
        className="flex gap-1.5"
        choiceClass={(on) =>
          `h-1.5 rounded-full transition-[width] ${on ? 'w-5 bg-ink' : 'w-1.5 bg-segment hover:bg-muted'}`
        }
        choices={creatureLooks.map(({ look, name }) => ({
          value: look,
          label: name,
          content: <span className="sr-only">{name}</span>,
        }))}
      />
    </div>
  );
}
