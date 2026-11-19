import { useEffect, useMemo, useRef, useState } from 'react';
import {
  bodyPath,
  bump,
  colours,
  eyesPath,
  kinds,
  limbAngle,
  limbOutline,
  looks,
  restChain,
  RING,
  ringAngles,
  type BotColour,
  type CreatureLook,
  type Frame,
  type Kind,
} from '@/components/creature-shape';

export type { BotColour, CreatureLook };
export type Appearance = { look?: CreatureLook; colour?: string };
export type CreatureState = 'idle' | 'working' | 'needs_you' | 'asleep' | 'failed';
export type ToolKind =
  'none' | 'read' | 'search' | 'memory' | 'edit' | 'platform' | 'command' | 'work' | 'other';

const lookName = (look: string) => look[0].toUpperCase() + look.slice(1);
export const creatureLooks = looks.map((look) => ({ look, name: lookName(look) }));
export const botColours = colours;
// Each token is written out in full so the stylesheet keeps it.
export const colourVars: Record<BotColour, string> = {
  brown: 'var(--color-bot-brown)',
  orange: 'var(--color-bot-orange)',
  amber: 'var(--color-bot-amber)',
  green: 'var(--color-bot-green)',
  teal: 'var(--color-bot-teal)',
  sky: 'var(--color-bot-sky)',
  blue: 'var(--color-bot-blue)',
  purple: 'var(--color-bot-purple)',
  pink: 'var(--color-bot-pink)',
};
export const colourOf = (look: CreatureLook, colour?: string): BotColour =>
  colours.includes(colour as BotColour) ? (colour as BotColour) : kinds[look].colour;

// Below this size a creature turns toward the threads and keeps still; from it up it is the live
// jelly that follows the pointer.
const LIVE = 64;

type Pose = { hl: number; left: number; right: number; curl: number; raise: number };
const poses: Record<CreatureState, Pose> = {
  idle: { hl: 3.7, left: 0, right: 0, curl: 1, raise: 0 },
  working: { hl: 3.1, left: 0, right: 0, curl: 1, raise: 0 },
  needs_you: { hl: 4.3, left: 0, right: 0, curl: 1, raise: 1 },
  asleep: { hl: 2.5, left: Math.PI / 2, right: Math.PI / 2, curl: 0.7, raise: 0 },
  failed: { hl: 3.3, left: Math.PI / 6, right: -Math.PI / 6, curl: 0.3, raise: 0 },
};

const rest = (kind: Kind): Frame => ({ ox: kind.cx, oy: kind.cy, sx: 1, sy: 1, t: 0 });

function limbsAt(kind: Kind, state: CreatureState, f: Frame) {
  const pose = poses[state];
  return kind.limbs
    .filter((l) => !l.working || state === 'working')
    .map((l) => limbOutline(l, restChain(l, f, pose.curl, pose.raise)))
    .join('');
}

function workMotion(kind: ToolKind) {
  if (['read', 'search', 'memory'].includes(kind)) return 'scan';
  if (['edit', 'platform'].includes(kind)) return 'bounce';
  return kind === 'command' ? 'breathe' : 'circle';
}

export function Creature({
  look = 'jellyfish',
  colour,
  state = 'idle',
  kind = 'none',
  size,
}: {
  look?: CreatureLook;
  colour?: string;
  state?: CreatureState;
  kind?: ToolKind;
  size: number;
}) {
  const known: CreatureLook = kinds[look] ? look : 'jellyfish';
  const shape = kinds[known];
  const fill = state === 'asleep' ? 'var(--color-sleep-body)' : colourVars[colourOf(known, colour)];
  const live = size >= LIVE;
  const still = state === 'failed' || state === 'asleep';
  const pose = poses[state];
  const drawing = useMemo(() => {
    const f = rest(shape);
    const side = !live;
    const ex = shape.cx + shape.eyeX + (side ? 7 * shape.travel : 0);
    const ey = shape.cy + shape.eyeY - (side ? 1 : 0);
    const gap = shape.gap * (side ? 0.88 : 1);
    return {
      body: bodyPath(shape, f),
      limbs: limbsAt(shape, state, f),
      eyes: eyesPath(ex, ey, gap, pose.hl, pose.left, pose.right),
    };
  }, [shape, state, live, pose]);
  const [poke, setPoke] = useState(0);
  const motion = state === 'working' ? workMotion(kind) : state === 'idle' ? 'blink' : '';
  const body = state === 'working' && ['edit', 'platform', 'command'].includes(kind);
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 64 64"
      role="img"
      aria-label={`${lookName(known)}, ${state.replace('_', ' ')}`}
      className="shrink-0 overflow-visible"
      onClick={() => !live && setPoke((n) => n + 1)}
    >
      {live && !still ? (
        <Jelly key={known} kind={shape} state={state} fill={fill} drawing={drawing} />
      ) : (
        <g key={poke} className={poke ? 'creature-poke' : undefined}>
          <g className={still ? undefined : 'creature-bob'}>
            <g className={body ? `creature-${motion}` : undefined}>
              <path className={still ? undefined : 'creature-sway'} d={drawing.limbs} fill={fill} />
              <path d={drawing.body} fill={fill} />
              <g className={motion && !body ? `creature-${motion}` : undefined}>
                <path
                  d={drawing.eyes}
                  fill="none"
                  stroke="var(--color-eye)"
                  strokeWidth={4.3}
                  strokeLinecap="round"
                />
              </g>
            </g>
          </g>
        </g>
      )}
      {state === 'needs_you' && (
        <circle
          className="creature-pulse"
          cx="54"
          cy="9"
          r="5.5"
          fill="var(--color-accent)"
          stroke="var(--color-bg)"
          strokeWidth="2.5"
        />
      )}
      {state === 'failed' && (
        <circle
          cx="54"
          cy="9"
          r="5.5"
          fill="var(--color-danger)"
          stroke="var(--color-bg)"
          strokeWidth="2.5"
        />
      )}
    </svg>
  );
}

class Spring {
  vel = 0;
  constructor(
    public val: number,
    public target: number = val,
    private k = 210,
    private c = 16,
  ) {}
  step(dt: number) {
    this.vel += (-this.k * (this.val - this.target) - this.c * this.vel) * dt;
    this.val += this.vel * dt;
    return this.val;
  }
}

const pointer = { x: 0, y: 0, t: -1e9 };
if (typeof window !== 'undefined') {
  window.addEventListener(
    'pointermove',
    (e) => Object.assign(pointer, { x: e.clientX, y: e.clientY, t: performance.now() }),
    { passive: true },
  );
}

// Jelly is the live creature: a ring of springs for the body, limbs that trail behind it, a
// pulse, a blink and eyes that follow the pointer. It draws by writing path data on each frame.
function Jelly({
  kind,
  state,
  fill,
  drawing,
}: {
  kind: Kind;
  state: CreatureState;
  fill: string;
  drawing: { body: string; limbs: string; eyes: string };
}) {
  const body = useRef<SVGPathElement>(null);
  const limbs = useRef<SVGPathElement>(null);
  const eyes = useRef<SVGPathElement>(null);
  const sim = useRef<ReturnType<typeof makeSim> | null>(null);
  sim.current ??= makeSim(kind);
  sim.current.state = state;

  useEffect(() => {
    if (matchMedia('(prefers-reduced-motion: reduce)').matches) return;
    let last = performance.now();
    let frame = requestAnimationFrame(function tick(now) {
      const dt = Math.min(0.032, Math.max(0.001, (now - last) / 1000));
      last = now;
      const svg = body.current?.ownerSVGElement;
      const d = sim.current && svg ? sim.current.step(dt, now, svg.getBoundingClientRect()) : null;
      if (d) {
        body.current?.setAttribute('d', d.body);
        limbs.current?.setAttribute('d', d.limbs);
        eyes.current?.setAttribute('d', d.eyes);
      }
      frame = requestAnimationFrame(tick);
    });
    return () => cancelAnimationFrame(frame);
  }, []);

  const onClick = (e: React.MouseEvent) => {
    const r = body.current?.ownerSVGElement?.getBoundingClientRect();
    if (!r || !sim.current) return;
    sim.current.poke(
      Math.atan2(e.clientX - (r.left + r.width / 2), -(e.clientY - (r.top + r.height * 0.45))),
    );
  };
  return (
    <g onClick={onClick}>
      <path ref={limbs} d={drawing.limbs} fill={fill} />
      <path ref={body} d={drawing.body} fill={fill} />
      <path
        ref={eyes}
        d={drawing.eyes}
        fill="none"
        stroke="var(--color-eye)"
        strokeWidth={4.3}
        strokeLinecap="round"
      />
    </g>
  );
}

function makeSim(kind: Kind) {
  const s = {
    state: 'idle' as CreatureState,
    phase: Math.random() * 6,
    beat: Math.random(),
    nextBlink: performance.now() + 1500,
    blinkStart: -1e9,
    px: new Spring(0, 0, 200, 14),
    py: new Spring(0, 0, 200, 13),
    sx: new Spring(1, 1, 260, 13),
    sy: new Spring(1, 1, 260, 13),
    lx: new Spring(0, 0, 170, 18),
    ly: new Spring(0, 0, 170, 18),
    hl: new Spring(3.7, 3.7, 260, 20),
    raise: new Spring(0, 0, 90, 12),
    spout: new Spring(0, 0, 120, 12),
    dents: new Float32Array(RING),
    vel: new Float32Array(RING),
    chains: kind.limbs.map((l) => l.rest.map(() => ({ x: 0, y: 0, vx: 0, vy: 0, set: false }))),
    poke(angle: number) {
      for (let i = 0; i < RING; i++) s.vel[i] += -105 * bump(ringAngles[i], angle, 0.6) + 22;
      s.sx.vel += 2.8;
      s.sy.vel -= 2.8;
      s.py.vel += -55 * Math.cos(angle) - 25;
      s.px.vel += -55 * Math.sin(angle);
    },
    step(dt: number, now: number, rect: DOMRect) {
      const working = s.state === 'working';
      const needs = s.state === 'needs_you';
      s.phase += dt * 3 * (working ? 2 : 1);
      s.beat = (s.beat + dt * 0.42 * (working ? 2 : 1)) % 1;
      const b = s.beat;
      const con =
        (b < 0.3
          ? Math.sin((b / 0.3) * (Math.PI / 2))
          : Math.cos(((b - 0.3) / 0.7) * (Math.PI / 2))) * kind.pulse;
      const hop = Math.abs(Math.sin(s.phase * 0.75));
      s.py.target = needs ? -2.5 - hop * 2.6 : -2.4 * con + Math.sin(s.phase * 0.5) * 0.7;
      s.sx.target = needs ? 0.97 : 1 - 0.08 * con;
      s.sy.target = needs ? 1.04 : 1 + 0.07 * con;
      let hl = poses[s.state].hl;
      if (now >= s.nextBlink) {
        s.blinkStart = now;
        s.nextBlink = now + 2200 + Math.random() * 3600;
      }
      const blink = now - s.blinkStart;
      if (blink >= 0 && blink <= 150) hl *= 1 - Math.sin((blink / 150) * Math.PI) * 0.9;
      s.hl.target = hl;
      let lookX = 0;
      let lookY = 0;
      if (working) {
        lookX = Math.sin(s.phase * 0.9) * 5;
        lookY = 1;
      } else if (now - pointer.t < 4000) {
        const dx = pointer.x - (rect.left + rect.width / 2);
        const dy = pointer.y - (rect.top + rect.height * 0.42);
        const dist = Math.hypot(dx, dy) || 1;
        const f = Math.min(1, dist / 200) * (needs ? 0.4 : 1);
        lookX = (dx / dist) * 7 * f;
        lookY = (dy / dist) * 4 * f - (needs ? 1 : 0);
      } else {
        lookX = 5;
        lookY = -2.5;
      }
      s.lx.target = Math.max(-7.5, Math.min(7.5, lookX)) * kind.travel;
      s.ly.target = Math.max(-4.5, Math.min(4.5, lookY));
      s.raise.target = needs ? 1 : 0;
      s.spout.target = working ? 1 : 0;
      const px = s.px.step(dt);
      const py = s.py.step(dt);
      const sx = s.sx.step(dt);
      const sy = s.sy.step(dt);
      const ex = s.lx.step(dt);
      const ey = s.ly.step(dt);
      const eh = Math.max(0.15, s.hl.step(dt));
      const raise = Math.max(0, Math.min(1, s.raise.step(dt)));
      const spout = Math.max(0, Math.min(1.2, s.spout.step(dt)));

      const h = dt / 2;
      for (let pass = 0; pass < 2; pass++) {
        for (let i = 0; i < RING; i++) {
          const a = ringAngles[i];
          const lean = Math.sin(a) * ex * 0.22 - Math.cos(a) * ey * 0.15;
          const force =
            -170 * (s.dents[i] - lean) -
            7.5 * s.vel[i] +
            900 * (s.dents[(i + RING - 1) % RING] + s.dents[(i + 1) % RING] - 2 * s.dents[i]);
          s.vel[i] += (force + (s.py.vel * Math.cos(a) - s.px.vel * Math.sin(a)) * 1.4) * h;
        }
        for (let i = 0; i < RING; i++)
          s.dents[i] = Math.max(-9, Math.min(10, s.dents[i] + s.vel[i] * h));
      }
      const f: Frame = {
        ox: kind.cx + px,
        oy: kind.cy + py + (1 - sy) * 12,
        sx,
        sy,
        t: s.phase,
        dents: s.dents,
      };

      let limbsD = '';
      kind.limbs.forEach((l, li) => {
        const seg = l.seg * (l.working ? spout : 1);
        if (l.working && spout < 0.05) return;
        const rootX = f.ox + l.x * sx;
        const rootY = f.oy + l.y * sy;
        const chain: [number, number][] = [
          [rootX, rootY - 3],
          [rootX, rootY],
        ];
        let [prevX, prevY] = [rootX, rootY];
        s.chains[li].forEach((p, j) => {
          const wave =
            l.sway *
            Math.sin(s.phase * 0.9 - j * 0.8 + l.ph) *
            ((j + 1) / l.rest.length) *
            (l.raise ? 1 + raise * 1.2 : 1);
          const th = limbAngle(l, j, 1, raise) + wave;
          if (j === 0) chain[0] = [rootX - Math.sin(th) * 3, rootY - Math.cos(th) * 3];
          const tx = prevX + Math.sin(th) * seg;
          const ty = prevY + Math.cos(th) * seg;
          if (!p.set) Object.assign(p, { x: tx, y: ty, set: true });
          const k = 150 - j * 14;
          p.vx += ((tx - p.x) * k - p.vx * 11) * dt;
          p.vy += ((ty - p.y) * k - p.vy * 11) * dt;
          p.x += p.vx * dt;
          p.y += p.vy * dt;
          chain.push([p.x, p.y]);
          prevX = p.x;
          prevY = p.y;
        });
        limbsD += limbOutline(l, chain);
      });
      const pose = poses[s.state];
      return {
        body: bodyPath(kind, f),
        limbs: limbsD,
        eyes: eyesPath(
          f.ox + kind.eyeX + ex,
          f.oy + kind.eyeY + ey,
          kind.gap,
          eh,
          pose.left,
          pose.right,
        ),
      };
    },
  };
  return s;
}
