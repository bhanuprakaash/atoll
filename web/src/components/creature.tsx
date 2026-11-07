import type { ReactNode } from 'react';

export type CreatureState = 'idle' | 'working' | 'needs_you' | 'asleep' | 'failed';
export type ToolKind =
  'none' | 'read' | 'search' | 'memory' | 'edit' | 'platform' | 'command' | 'work' | 'other';

type Palette = { body: string; light: string; accent: string; white: string };
type Eye = readonly [number, number, number, number];
type Drawing = {
  motion: string;
  colors: Palette;
  eyes: readonly [Eye, Eye];
  scale?: number;
  shapes: (p: Palette) => ReactNode;
};

const asleep: Palette = {
  body: 'var(--color-sleep-body)',
  light: 'var(--color-sleep-light)',
  accent: 'var(--color-sleep-accent)',
  white: 'var(--color-sleep-white)',
};
const none = { light: '', accent: '', white: '' };

const drawings = {
  jellyfish: {
    motion: 'c-bob',
    colors: { body: 'var(--color-jellyfish)', ...none },
    eyes: [
      [36, 19, 37.5, 26],
      [44, 18, 45.5, 25],
    ],
    shapes: (p) => (
      <>
        <path d="M8 34 a24 24 0 0 1 48 0 q0 6 -6 6 H14 q-6 0 -6 -6 z" fill={p.body} />
        {['M18 44 q-4 6 0 13', 'M32 44 q4 6 0 13', 'M46 44 q-4 6 0 13'].map((d, i) => (
          <path
            key={d}
            className={['c-sway', 'c-sway-1', 'c-sway-2'][i]}
            d={d}
            fill="none"
            stroke={p.body}
            strokeWidth="5.5"
            strokeLinecap="round"
          />
        ))}
      </>
    ),
  },
  whale: {
    motion: 'c-bob',
    scale: 0.85,
    colors: { body: 'var(--color-whale)', ...none },
    eyes: [
      [18, 28, 19.5, 35],
      [26, 27, 27.5, 34],
    ],
    shapes: (p) => (
      <>
        <path
          className="c-flick"
          d="M46 36 q9 -2 13 -13 q2 9 -3 15 q6 1 8 8 q-9 -3 -16 -2 z"
          fill={p.body}
        />
        <ellipse cx="27" cy="38" rx="24" ry="19" fill={p.body} />
        <path
          className="c-puff"
          d="M24 16 v-6 M24 11 q-4 -4 -8 -3 M24 11 q4 -4 8 -3"
          fill="none"
          stroke={p.body}
          strokeWidth="3.5"
          strokeLinecap="round"
        />
      </>
    ),
  },
  penguin: {
    motion: 'c-waddle',
    colors: {
      body: 'var(--color-penguin)',
      light: 'var(--color-penguin-light)',
      accent: 'var(--color-penguin-accent)',
      white: '',
    },
    eyes: [
      [26, 15, 26.5, 21],
      [37.5, 15, 38, 21],
    ],
    shapes: (p) => (
      <>
        <g className="c-flap">
          <ellipse cx="10" cy="40" rx="5" ry="12" transform="rotate(22 10 40)" fill={p.body} />
        </g>
        <g className="c-flap-1">
          <ellipse cx="54" cy="40" rx="5" ry="12" transform="rotate(-22 54 40)" fill={p.body} />
        </g>
        <ellipse cx="25" cy="61" rx="6" ry="2.6" fill={p.accent} />
        <ellipse cx="39" cy="61" rx="6" ry="2.6" fill={p.accent} />
        <ellipse cx="32" cy="34" rx="22" ry="26" fill={p.body} />
        <ellipse cx="32" cy="42" rx="14" ry="17" fill={p.light} />
        <path
          d="M28.5 25 h7 l-3.5 4.5 z"
          fill={p.accent}
          stroke={p.accent}
          strokeWidth="1.5"
          strokeLinejoin="round"
        />
      </>
    ),
  },
  shark: {
    motion: 'c-swim',
    scale: 0.85,
    colors: {
      body: 'var(--color-shark)',
      light: 'var(--color-shark-light)',
      accent: '',
      white: 'var(--color-white)',
    },
    eyes: [
      [45, 24, 46, 30],
      [52, 23, 53, 29],
    ],
    shapes: (p) => (
      <>
        <path
          className="c-tail"
          d="M12 34 L1 21 Q9 34 1 47 Z"
          fill={p.body}
          stroke={p.body}
          strokeWidth="3"
          strokeLinejoin="round"
        />
        <path d="M27 17 Q33 4 41 2 Q38 10 41 17 Z" fill={p.body} />
        <ellipse cx="35" cy="35" rx="26" ry="19" fill={p.body} />
        <path d="M13 40 Q35 57 59 39 Q36 49 13 40 Z" fill={p.light} />
        <path
          d="M41 42 l2.2 2.4 l2.2 -2.4 l2.2 2.4 l2.2 -2.4"
          fill="none"
          stroke={p.white}
          strokeWidth="1.8"
          strokeLinecap="round"
          strokeLinejoin="round"
        />
      </>
    ),
  },
  starfish: {
    motion: 'c-bob',
    colors: { body: 'var(--color-starfish)', ...none },
    eyes: [
      [28, 29, 29, 35],
      [36, 29, 37, 35],
    ],
    shapes: (p) => (
      <polygon
        points="32,10 38.5,27.1 56.7,28 42.5,39.4 47.3,57 32,47 16.7,57 21.5,39.4 7.3,28 25.5,27.1"
        fill={p.body}
        stroke={p.body}
        strokeWidth="6"
        strokeLinejoin="round"
      />
    ),
  },
  anglerfish: {
    motion: 'c-swim',
    scale: 0.85,
    colors: {
      body: 'var(--color-anglerfish)',
      light: '',
      accent: 'var(--color-sun)',
      white: 'var(--color-white)',
    },
    eyes: [
      [36, 27, 37, 33],
      [43, 27, 44, 33],
    ],
    shapes: (p) => (
      <>
        <path
          className="c-tail"
          d="M10 38 L1 28 Q4 38 1 48 Z"
          fill={p.body}
          stroke={p.body}
          strokeWidth="3"
          strokeLinejoin="round"
        />
        <path
          d="M30 21 Q32 5 48 7"
          fill="none"
          stroke={p.body}
          strokeWidth="3"
          strokeLinecap="round"
        />
        <circle cx="50" cy="9" r="5" fill={p.accent} />
        <ellipse cx="31" cy="38" rx="22" ry="18" fill={p.body} />
        <path
          d="M38 45 l2 3 l2 -3 l2 3 l2 -3 l2 3"
          fill="none"
          stroke={p.white}
          strokeWidth="1.8"
          strokeLinecap="round"
          strokeLinejoin="round"
        />
      </>
    ),
  },
  sand_dollar: {
    motion: 'c-bob',
    colors: {
      body: 'var(--color-sand-dollar)',
      light: '',
      accent: 'var(--color-sand-dollar-accent)',
      white: '',
    },
    eyes: [
      [27, 39, 28, 45],
      [36, 39, 37, 45],
    ],
    shapes: (p) => (
      <>
        <circle cx="32" cy="34" r="25" fill={p.body} />
        {[0, 72, 144, 216, 288].map((deg) => (
          <ellipse
            key={deg}
            cx="32"
            cy="16"
            rx="2.6"
            ry="6"
            transform={`rotate(${deg} 32 23)`}
            fill={p.accent}
          />
        ))}
      </>
    ),
  },
  sponge: {
    motion: 'c-bob',
    colors: {
      body: 'var(--color-sponge)',
      light: '',
      accent: 'var(--color-sponge-accent)',
      white: '',
    },
    eyes: [
      [29, 31, 30, 37],
      [36, 31, 37, 37],
    ],
    shapes: (p) => (
      <>
        <rect x="7" y="24" width="15" height="35" rx="7.5" fill={p.body} />
        <rect x="24" y="9" width="17" height="50" rx="8.5" fill={p.body} />
        <rect x="43" y="19" width="15" height="40" rx="7.5" fill={p.body} />
        <ellipse cx="14.5" cy="28" rx="4" ry="2.5" fill={p.accent} />
        <ellipse cx="32.5" cy="14" rx="5" ry="3" fill={p.accent} />
        <ellipse cx="50.5" cy="23" rx="4" ry="2.5" fill={p.accent} />
      </>
    ),
  },
  kelp: {
    motion: 'c-sway',
    colors: { body: 'var(--color-kelp)', ...none },
    eyes: [
      [29, 10, 30, 16],
      [37, 10, 38, 16],
    ],
    shapes: (p) => (
      <>
        <path
          d="M32 62 C28 46 38 34 32 20"
          fill="none"
          stroke={p.body}
          strokeWidth="5"
          strokeLinecap="round"
        />
        <ellipse cx="23" cy="48" rx="10" ry="4.5" transform="rotate(-30 23 48)" fill={p.body} />
        <ellipse cx="42" cy="38" rx="10" ry="4.5" transform="rotate(30 42 38)" fill={p.body} />
        <ellipse cx="24" cy="30" rx="8" ry="4" transform="rotate(-35 24 30)" fill={p.body} />
        <circle cx="33" cy="14" r="11" fill={p.body} />
      </>
    ),
  },
  shell: {
    motion: 'c-bob',
    colors: {
      body: 'var(--color-shell)',
      light: '',
      accent: 'var(--color-shell-accent)',
      white: '',
    },
    eyes: [
      [26.75, 28, 27.75, 34],
      [36.75, 28, 37.75, 34],
    ],
    shapes: (p) => (
      <>
        <path
          d="M32 58 L8 30 Q10 10 32 8 Q54 10 56 30 Z"
          fill={p.body}
          stroke={p.body}
          strokeWidth="3"
          strokeLinejoin="round"
        />
        <path
          d="M32 54 L32 10 M32 54 L18 14 M32 54 L46 14 M32 54 L10 28 M32 54 L54 28"
          fill="none"
          stroke={p.accent}
          strokeWidth="2.5"
          strokeLinecap="round"
        />
        <rect x="25" y="53" width="14" height="7" rx="2" fill={p.body} />
      </>
    ),
  },
  wave: {
    motion: 'c-bob',
    colors: { body: 'var(--color-wave)', ...none },
    eyes: [
      [21, 32, 22, 38],
      [29, 31, 30, 37],
    ],
    shapes: (p) => (
      <path
        d="M4 56 C4 34 18 18 38 18 C52 18 60 28 58 38 C54 30 44 30 42 38 C40 46 50 50 56 48 L60 56 Z"
        fill={p.body}
        stroke={p.body}
        strokeWidth="3"
        strokeLinejoin="round"
      />
    ),
  },
  island: {
    motion: 'c-bob',
    colors: {
      body: 'var(--color-island)',
      light: 'var(--color-kelp)',
      accent: 'var(--color-wood)',
      white: '',
    },
    eyes: [
      [20, 46, 21, 52],
      [28, 45, 29, 51],
    ],
    shapes: (p) => (
      <>
        <path
          d="M35 46 Q30 30 36 16"
          fill="none"
          stroke={p.accent}
          strokeWidth="5"
          strokeLinecap="round"
        />
        <path
          d="M36 16 q-10 -6 -20 2 M36 16 q10 -8 20 0 M36 16 q-4 -10 -14 -12 M36 16 q6 -10 16 -10"
          fill="none"
          stroke={p.light}
          strokeWidth="5"
          strokeLinecap="round"
        />
        <path d="M2 58 Q32 22 62 58 Z" fill={p.body} />
      </>
    ),
  },
  sun: {
    motion: 'c-bob',
    colors: { body: 'var(--color-sun)', ...none },
    eyes: [
      [27, 27, 28, 33],
      [36, 27, 37, 33],
    ],
    shapes: (p) => (
      <>
        <path
          d="M32 5 v5 M32 54 v5 M5 32 h5 M54 32 h5 M13 13 l4 4 M47 47 l4 4 M51 13 l-4 4 M13 51 l4 -4"
          fill="none"
          stroke={p.body}
          strokeWidth="5"
          strokeLinecap="round"
        />
        <circle cx="32" cy="32" r="17" fill={p.body} />
      </>
    ),
  },
  sailboat: {
    motion: 'c-rock',
    colors: {
      body: 'var(--color-sailboat)',
      light: '',
      accent: 'var(--color-wood)',
      white: '',
    },
    eyes: [
      [27.75, 47, 28.75, 53],
      [35.75, 47, 36.75, 53],
    ],
    shapes: (p) => (
      <>
        <path d="M32 6 V44" stroke={p.body} strokeWidth="3" strokeLinecap="round" />
        <path
          d="M34 8 L54 40 H34 Z M30 16 L14 40 H30 Z"
          fill={p.body}
          stroke={p.body}
          strokeWidth="2"
          strokeLinejoin="round"
        />
        <path d="M6 43 H58 Q52 57 41 57 H23 Q12 57 6 43 Z" fill={p.accent} />
      </>
    ),
  },
} satisfies Record<string, Drawing>;

export type CreatureLook = keyof typeof drawings;
const lookName = (look: string) => look[0].toUpperCase() + look.slice(1).replace('_', ' ');
export const creatureLooks = (Object.keys(drawings) as CreatureLook[]).map((look) => ({
  look,
  name: lookName(look),
  colour: drawings[look].colors.body,
}));

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
  const palette = state === 'asleep' ? asleep : drawing.colors;
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
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 64 64"
      role="img"
      aria-label={`${lookName(look)}, ${state.replace('_', ' ')}`}
      className="shrink-0 overflow-visible"
    >
      <g
        transform={
          drawing.scale ? `translate(32 32) scale(${drawing.scale}) translate(-32 -32)` : undefined
        }
      >
        <g
          className={
            state === 'working' && ['edit', 'platform', 'command'].includes(kind)
              ? `creature-${motion}`
              : undefined
          }
        >
          {drawing.shapes(palette)}
          <g className={state === 'working' || state === 'idle' ? `creature-${motion}` : undefined}>
            {drawing.eyes.map(([x1, y1, x2, y2], i) => (
              <line
                key={x1}
                x1={state === 'failed' ? (x1 + x2) / 2 + (i ? -2 : 2) : x1}
                y1={y1}
                x2={state === 'failed' ? (x1 + x2) / 2 + (i ? 2 : -2) : x2}
                y2={y2}
                stroke="var(--color-eye)"
                strokeWidth="4"
                strokeLinecap="round"
              />
            ))}
          </g>
        </g>
      </g>
      {state === 'needs_you' && (
        <circle
          className="creature-pulse"
          cx="56"
          cy="8"
          r="7"
          fill="var(--color-danger)"
          stroke="var(--color-bg)"
          strokeWidth="3"
        />
      )}
      {state === 'working' && (
        <g fill="none" stroke="var(--color-muted)" strokeWidth="2">
          <circle className="creature-bubble" cx="55" cy="10" r="3" />
          <circle className="creature-bubble creature-bubble-delay" cx="61" cy="5" r="2" />
        </g>
      )}
    </svg>
  );
}
