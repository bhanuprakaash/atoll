import { Creature, type CreatureLook, type CreatureState } from '@/components/creature';
import { helperLabel, type HelperState } from '@/modules/threads/utils/helper-state';

const look: Record<HelperState, { state: CreatureState; style: string }> = {
  working: { state: 'working', style: '' },
  needs_you: { state: 'needs_you', style: '' },
};

export function HelperCreature({
  name,
  creature,
  state,
}: {
  name: string;
  creature?: CreatureLook;
  state: HelperState;
}) {
  return (
    <span role="img" aria-label={helperLabel(name, state)} className={look[state].style}>
      <Creature look={creature} state={look[state].state} size={20} />
    </span>
  );
}
