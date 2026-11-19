import { Creature, type Appearance, type CreatureState } from '@/components/creature';
import { helperLabel, type HelperState } from '@/modules/threads/utils/helper-state';

const look: Record<HelperState, CreatureState> = {
  working: 'working',
  needs_you: 'needs_you',
  done: 'idle',
  failed: 'failed',
  stopped: 'asleep',
};

export function HelperCreature({
  name,
  creature,
  state,
}: {
  name: string;
  creature?: Appearance;
  state: HelperState;
}) {
  return (
    <span role="img" aria-label={helperLabel(name, state)}>
      <Creature {...creature} state={look[state]} size={20} />
    </span>
  );
}
