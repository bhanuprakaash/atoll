export type CreatureState = 'idle' | 'working' | 'needs_you' | 'asleep' | 'failed';
export type ToolKind =
  'none' | 'read' | 'search' | 'memory' | 'edit' | 'platform' | 'command' | 'work' | 'other';

type Paint = 'body' | 'belly' | 'lure' | 'tusk' | 'ridge' | 'eye';
type Trait = { d: string; motion?: string; fill?: Paint; stroke?: Paint; width?: number };
type Drawing = {
  colour: string;
  tilt: number;
  motion: string;
  eyes: readonly [number, number, number];
  back?: Trait;
  front?: Trait;
};

const pebble = 'M31 10 C44 9 55 19 56 33 C57 46 47 56 32 56 C17 56 7 47 8 34 C9 20 18 11 31 10 Z';

const fixed = {
  belly: 'var(--color-penguin-belly)',
  lure: 'var(--color-anglerfish-lure)',
  tusk: 'var(--color-narwhal-tusk)',
  ridge: 'var(--color-scallop-ridge)',
  eye: 'var(--color-eye)',
};
const asleep = {
  body: 'var(--color-sleep-body)',
  belly: 'var(--color-sleep-light)',
  lure: 'var(--color-sleep-accent)',
  tusk: 'var(--color-sleep-white)',
  ridge: fixed.ridge,
  eye: fixed.eye,
};

const drawings = {
  jellyfish: {
    colour: 'var(--color-jellyfish)',
    tilt: 0,
    motion: 'bob',
    eyes: [26, 37, 25],
    back: {
      d: 'M20 52 q-2 5 0 10 M32 54 q2 5 0 10 M44 52 q-2 5 0 10',
      stroke: 'body',
      width: 5,
      motion: 'sway',
    },
  },
  whale: {
    colour: 'var(--color-whale)',
    tilt: -6,
    motion: 'bob',
    eyes: [31, 42, 24],
    back: {
      d: 'M29 9 q-1 -5 -6 -6 M32 9 v-7 M35 9 q1 -5 6 -6',
      stroke: 'body',
      width: 3,
      motion: 'puff',
    },
  },
  shark: {
    colour: 'var(--color-shark)',
    tilt: 4,
    motion: 'swim',
    eyes: [23, 41, 26],
    back: { d: 'M24 14 Q32 -3 42 0 Q36 7 40 13 Z', fill: 'body', motion: 'fin' },
  },
  penguin: {
    colour: 'var(--color-penguin)',
    tilt: 8,
    motion: 'waddle',
    eyes: [21, 32, 24],
    front: { d: 'M19 43 a13 11 0 1 0 26 0 a13 11 0 1 0 -26 0', fill: 'belly' },
  },
  anglerfish: {
    colour: 'var(--color-anglerfish)',
    tilt: -4,
    motion: 'bob',
    eyes: [27, 38, 21],
    back: { d: 'M24 13 Q21 -1 36 2', stroke: 'body', width: 3 },
    front: { d: 'M35 2.5 a4.5 4.5 0 1 0 9 0 a4.5 4.5 0 1 0 -9 0', fill: 'lure', motion: 'glow' },
  },
  pufferfish: {
    colour: 'var(--color-pufferfish)',
    tilt: 0,
    motion: 'bob',
    eyes: [26, 37, 29],
    back: {
      d: 'M32 10 v-7 M47 15 l5 -5 M56 33 h7 M48 51 l5 5 M17 15 l-5 -5 M8 34 h-7 M16 51 l-5 5',
      stroke: 'body',
      width: 3,
      motion: 'spike',
    },
  },
  octopus: {
    colour: 'var(--color-octopus)',
    tilt: 6,
    motion: 'bob',
    eyes: [26, 37, 25],
    back: {
      d: 'M16 48 q-3 8 2 13 M27 53 q-1 6 1 10 M37 53 q1 6 -1 10 M48 48 q3 8 -2 13',
      stroke: 'body',
      width: 5,
      motion: 'sway',
    },
  },
  crab: {
    colour: 'var(--color-crab)',
    tilt: -8,
    motion: 'side',
    eyes: [31, 42, 24],
    back: { d: 'M5 15 a6.5 6.5 0 1 0 13 0 a6.5 6.5 0 1 0 -13 0', fill: 'body', motion: 'snipl' },
    front: { d: 'M46 15 a6.5 6.5 0 1 0 13 0 a6.5 6.5 0 1 0 -13 0', fill: 'body', motion: 'snipr' },
  },
  seal: {
    colour: 'var(--color-seal)',
    tilt: 2,
    motion: 'bob',
    eyes: [23, 41, 26],
    front: {
      d: 'M21 41 h-9 M21 44 l-8 4 M43 41 h9 M43 44 l8 4',
      stroke: 'eye',
      width: 1.6,
      motion: 'twitch',
    },
  },
  manta: {
    colour: 'var(--color-manta)',
    tilt: -2,
    motion: 'bob',
    eyes: [21, 32, 24],
    back: {
      d: 'M12 28 L-2 42 L14 46 Z M52 28 L66 42 L50 46 Z',
      fill: 'body',
      stroke: 'body',
      width: 2,
      motion: 'flap',
    },
  },
  scallop: {
    colour: 'var(--color-scallop)',
    tilt: 5,
    motion: 'bob',
    eyes: [27, 38, 21],
    front: {
      d: 'M32 55 V41 M32 55 L23 42 M32 55 L41 42 M32 55 L15 45 M32 55 L49 45',
      stroke: 'ridge',
      width: 2,
    },
  },
  narwhal: {
    colour: 'var(--color-narwhal)',
    tilt: -5,
    motion: 'bob',
    eyes: [26, 37, 29],
    back: { d: 'M40 12 L53 -5', stroke: 'tusk', width: 3 },
  },
} satisfies Record<string, Drawing>;

export type CreatureLook = keyof typeof drawings;
const lookName = (look: string) => look[0].toUpperCase() + look.slice(1);
export const creatureLooks = (Object.keys(drawings) as CreatureLook[]).map((look) => ({
  look,
  name: lookName(look),
  colour: drawings[look].colour,
}));

function eyePath(state: CreatureState, [a, b, y]: Drawing['eyes']) {
  if (state === 'failed') {
    return `M${a - 3} ${y + 5} L${a + 3} ${y + 1} M${b - 3} ${y + 1} L${b + 3} ${y + 5}`;
  }
  if (state === 'asleep') return `M${a - 3} ${y + 3} h6 M${b - 3} ${y + 3} h6`;
  return `M${a} ${y} L${a + 1} ${y + 6} M${b} ${y - 0.5} L${b + 1} ${y + 5.5}`;
}

export function Creature({
  look = 'jellyfish',
  state = 'idle',
  kind = 'none',
  size,
}: {
  look?: CreatureLook;
  state?: CreatureState;
  kind?: ToolKind;
  size: number;
}) {
  const drawing: Drawing = drawings[look] ?? drawings.jellyfish;
  const paints = { ...fixed, ...(state === 'asleep' ? asleep : { body: drawing.colour }) };
  const moving = state === 'working' || state === 'idle';
  const motion =
    state === 'working'
      ? ['read', 'search', 'memory'].includes(kind)
        ? 'scan'
        : ['edit', 'platform'].includes(kind)
          ? 'bounce'
          : kind === 'command'
            ? 'breathe'
            : 'circle'
      : state === 'idle'
        ? 'blink'
        : '';
  const trait = ({ d, motion: m, fill, stroke, width }: Trait) => (
    <path
      className={moving && m ? `creature-${m}` : undefined}
      d={d}
      fill={fill ? paints[fill] : 'none'}
      stroke={stroke ? paints[stroke] : 'none'}
      strokeWidth={width}
      strokeLinecap="round"
      strokeLinejoin="round"
    />
  );
  return (
    <svg
      width={size}
      height={size}
      viewBox="-4 -8 72 76"
      role="img"
      aria-label={`${lookName(look)}, ${state.replace('_', ' ')}`}
      className="shrink-0 overflow-visible"
    >
      <g transform={state === 'failed' ? undefined : `rotate(${drawing.tilt} 32 34)`}>
        <g className={moving ? `creature-${drawing.motion}` : undefined}>
          <g
            className={
              state === 'working' && ['edit', 'platform', 'command'].includes(kind)
                ? `creature-${motion}`
                : undefined
            }
          >
            {drawing.back && trait(drawing.back)}
            <path d={pebble} fill={paints.body} />
            {drawing.front && trait(drawing.front)}
            <g className={moving ? `creature-${motion}` : undefined}>
              <path
                d={eyePath(state, drawing.eyes)}
                fill="none"
                stroke={paints.eye}
                strokeWidth="4"
                strokeLinecap="round"
              />
            </g>
          </g>
        </g>
      </g>
      {state === 'needs_you' && (
        <circle
          className="creature-pulse"
          cx="56"
          cy="4"
          r="7"
          fill="var(--color-danger)"
          stroke="var(--color-bg)"
          strokeWidth="3"
        />
      )}
      {state === 'working' && (
        <g fill="none" stroke="var(--color-muted)" strokeWidth="2">
          <circle className="creature-bubble" cx="57" cy="8" r="3" />
          <circle className="creature-bubble creature-bubble-delay" cx="63" cy="2" r="2" />
        </g>
      )}
    </svg>
  );
}
