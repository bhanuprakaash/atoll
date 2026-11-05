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
      <div className="mb-2 flex items-center text-xs tracking-wide text-muted">
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
