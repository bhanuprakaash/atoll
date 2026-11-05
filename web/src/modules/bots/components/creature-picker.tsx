import { Creature, creatureLooks, type CreatureLook } from '@/components/creature';
import { Segmented } from '@/components/segmented';

export function CreaturePicker({
  value,
  onChange,
  shuffle = false,
}: {
  value: CreatureLook;
  onChange: (look: CreatureLook) => void;
  shuffle?: boolean;
}) {
  return (
    <div>
      <div className="mb-2 flex items-center text-xs tracking-wide text-label">
        Look
        {shuffle && (
          <button
            type="button"
            className="ml-auto h-7 rounded-full border border-outline px-2.5 text-xs text-ink"
            onClick={() =>
              onChange(creatureLooks[Math.floor(Math.random() * creatureLooks.length)].look)
            }
          >
            Shuffle
          </button>
        )}
      </div>
      <Segmented
        label="Creature"
        value={value}
        onChange={(look) => onChange(look as CreatureLook)}
        className={shuffle ? 'grid max-w-[352px] grid-cols-5 gap-2' : 'grid grid-cols-5 gap-2'}
        choiceClass={(on) =>
          `flex flex-col items-center gap-1 rounded-2xl border bg-box text-xs ${shuffle ? 'h-16 justify-center p-1' : 'p-2'} ${on ? 'border-ink' : 'border-transparent'}`
        }
        choices={creatureLooks.map(({ look, name }) => ({
          value: look,
          label: name,
          content: (
            <>
              <Creature look={look} size={shuffle ? 32 : 36} />
              {name}
            </>
          ),
        }))}
      />
    </div>
  );
}

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
      <div className="flex w-full items-center justify-between">
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
