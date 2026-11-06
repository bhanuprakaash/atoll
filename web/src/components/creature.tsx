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
  octopus: {
    motion: 'c-bob',
    colors: { body: 'var(--color-octopus)', ...none },
    eyes: [
      [29, 20, 30, 27],
      [38, 19, 39, 26],
    ],
    shapes: (p) => (
      <>
        {[
          'M16 36 q-7 11 2 18',
          'M26 40 q-3 11 4 16',
          'M38 40 q3 11 -4 16',
          'M48 36 q7 11 -2 18',
        ].map((d, i) => (
          <path
            key={d}
            className={['c-sway', 'c-sway-1', 'c-sway-2', 'c-sway-1'][i]}
            d={d}
            fill="none"
            stroke={p.body}
            strokeWidth="6"
            strokeLinecap="round"
          />
        ))}
        <ellipse cx="32" cy="27" rx="21" ry="20" fill={p.body} />
      </>
    ),
  },
  turtle: {
    motion: 'c-peek',
    colors: {
      body: 'var(--color-turtle)',
      light: 'var(--color-turtle-light)',
      accent: '',
      white: '',
    },
    eyes: [
      [28, 11, 28.5, 17],
      [36, 11, 36.5, 17],
    ],
    shapes: (p) => (
      <>
        <g className="c-paddle">
          <ellipse cx="11" cy="34" rx="7" ry="4.5" transform="rotate(-30 11 34)" fill={p.body} />
        </g>
        <g className="c-paddle-1">
          <ellipse cx="53" cy="34" rx="7" ry="4.5" transform="rotate(30 53 34)" fill={p.body} />
        </g>
        <ellipse cx="14" cy="56" rx="6" ry="4" fill={p.body} />
        <ellipse cx="50" cy="56" rx="6" ry="4" fill={p.body} />
        <circle cx="32" cy="15" r="11" fill={p.body} />
        <ellipse cx="32" cy="42" rx="23" ry="18" fill={p.body} />
        <path d="M32 30 l8 5 v9 l-8 5 l-8 -5 v-9 z" fill={p.light} />
      </>
    ),
  },
  seahorse: {
    motion: 'c-hover',
    colors: { body: 'var(--color-seahorse)', ...none },
    eyes: [
      [36, 11, 36.5, 17],
      [43, 11, 43.5, 17],
    ],
    shapes: (p) => (
      <>
        <path
          d="M34 25 C22 31 24 45 34 49 C42 53 37 62 29 58"
          fill="none"
          stroke={p.body}
          strokeWidth="12"
          strokeLinecap="round"
        />
        <path
          className="c-flutter"
          d="M22 32 l-8 -4 l2 10 z"
          fill={p.body}
          stroke={p.body}
          strokeWidth="2"
          strokeLinejoin="round"
        />
        <rect x="42" y="14" width="14" height="7" rx="3.5" fill={p.body} />
        <circle cx="37" cy="16" r="12" fill={p.body} />
        <path d="M30 6 l3 -5 l3 5" fill={p.body} />
      </>
    ),
  },
  otter: {
    motion: 'c-rock',
    colors: {
      body: 'var(--color-otter)',
      light: 'var(--color-otter-light)',
      accent: 'var(--color-eye)',
      white: '',
    },
    eyes: [
      [24, 25, 24.5, 31],
      [40, 25, 40.5, 31],
    ],
    shapes: (p) => (
      <>
        <circle cx="13" cy="15" r="7" fill={p.body} />
        <circle cx="51" cy="15" r="7" fill={p.body} />
        <circle cx="32" cy="35" r="25" fill={p.body} />
        <ellipse cx="32" cy="44" rx="12" ry="9" fill={p.light} />
        <ellipse cx="32" cy="39.5" rx="3.5" ry="2.4" fill={p.accent} />
      </>
    ),
  },
  clownfish: {
    motion: 'c-swim',
    scale: 0.85,
    colors: { body: 'var(--color-danger)', light: '', accent: '', white: 'var(--color-white)' },
    eyes: [
      [45, 27, 45.5, 33],
      [51, 26, 51.5, 32],
    ],
    shapes: (p) => (
      <>
        <path
          className="c-tail"
          d="M12 34 L0 22 Q8 34 0 46 Z"
          fill={p.body}
          stroke={p.body}
          strokeWidth="3"
          strokeLinejoin="round"
        />
        <path d="M24 20 q8 -10 16 0 z" fill={p.body} />
        <ellipse cx="33" cy="35" rx="24" ry="17" fill={p.body} />
        <path
          d="M24 20 q-4 15 0 30 M38 18.5 q4 16 0 33"
          fill="none"
          stroke={p.white}
          strokeWidth="5"
        />
      </>
    ),
  },
  manta: {
    motion: 'c-glide',
    scale: 0.85,
    colors: { body: 'var(--color-manta)', ...none },
    eyes: [
      [26, 20, 26.5, 26],
      [38, 20, 38.5, 26],
    ],
    shapes: (p) => (
      <>
        <path
          className="c-tailwave"
          d="M33 46 q4 10 12 14"
          fill="none"
          stroke={p.body}
          strokeWidth="3"
          strokeLinecap="round"
        />
        <path
          className="c-wing"
          d="M2 30 Q18 10 32 14 Q46 10 62 30 Q46 34 38 44 Q32 50 26 44 Q18 34 2 30 Z"
          fill={p.body}
        />
        <path
          d="M24 15 q-2 -6 2 -9 M40 15 q2 -6 -2 -9"
          fill="none"
          stroke={p.body}
          strokeWidth="4"
          strokeLinecap="round"
        />
      </>
    ),
  },
} satisfies Record<string, Drawing>;

export type CreatureLook = keyof typeof drawings;
export const creatureLooks = (Object.keys(drawings) as CreatureLook[]).map((look) => ({
  look,
  name: look[0].toUpperCase() + look.slice(1),
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
      aria-label={`${look[0].toUpperCase() + look.slice(1)}, ${state.replace('_', ' ')}`}
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
                x1={x1}
                y1={state === 'failed' ? y1 + (i ? 3 : -3) : y1}
                x2={x2}
                y2={state === 'failed' ? y2 + (i ? -3 : 3) : y2}
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
