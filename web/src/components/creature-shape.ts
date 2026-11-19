// Geometry for the Bot creatures. Each body is a radius for every angle around a centre (angle 0
// is straight up, right is positive); limbs are chains of segments hanging from the body. The
// same functions draw the still SVG and every frame of the live one.

export const looks = [
  'jellyfish',
  'octopus',
  'whale',
  'starfish',
  'sponge',
  'coral',
  'turtle',
  'clam',
  'seal',
] as const;
export type CreatureLook = (typeof looks)[number];

export const colours = [
  'brown',
  'orange',
  'amber',
  'green',
  'teal',
  'sky',
  'blue',
  'purple',
  'pink',
] as const;
export type BotColour = (typeof colours)[number];

type Point = [number, number];
export type Limb = {
  x: number;
  y: number;
  seg: number;
  w0: number;
  w1: number;
  side: -1 | 1;
  rest: number[];
  raise?: number[];
  sway: number;
  ph: number;
  fluke?: { rx: number; ry: number; out: number; lean: number };
  working?: boolean;
};
export type Kind = {
  colour: BotColour;
  cx: number;
  cy: number;
  eyeX: number;
  eyeY: number;
  gap: number;
  pulse: number;
  travel: number;
  shape: (a: number, t: number) => number;
  limbs: Limb[];
};

const PI = Math.PI;
const clamp = (v: number, a: number, b: number) => Math.max(a, Math.min(b, v));
export const wrap = (a: number) => Math.atan2(Math.sin(a), Math.cos(a));
export const bump = (a: number, c: number, w: number) => {
  const d = wrap(a - c) / w;
  return Math.exp(-d * d);
};
const pair = (a: number, c: number, w: number) => bump(a, c, w) + bump(a, -c, w);
const ell = (a: number, rx: number, ry: number) =>
  1 / Math.sqrt((Math.sin(a) / rx) ** 2 + (Math.cos(a) / ry) ** 2);
const smooth = (e0: number, e1: number, x: number) => {
  const t = clamp((x - e0) / (e1 - e0), 0, 1);
  return t * t * (3 - 2 * t);
};
const rounded = (a: number, h: number) => h / (Math.cos(a) ** 4 + Math.sin(a) ** 4) ** 0.25;
const smin = (a: number, b: number, k: number) =>
  -Math.log(Math.exp(-k * a) + Math.exp(-k * b)) / k;
// flat cuts the body off at distance d below the centre, with a soft corner.
const flat = (a: number, r: number, d: number, k: number) => {
  const down = -Math.cos(a);
  return smin(r, down > 0.05 ? d / down : 1000, k);
};
const deg = (d: number) => (d * PI) / 180;
const limb = (l: Omit<Limb, 'rest' | 'raise'> & { rest: number[]; raise?: number[] }): Limb => ({
  ...l,
  rest: l.rest.map(deg),
  raise: l.raise?.map(deg),
});

export const kinds: Record<CreatureLook, Kind> = {
  jellyfish: {
    colour: 'orange',
    cx: 32,
    cy: 30,
    eyeX: 0,
    eyeY: -4,
    gap: 4.8,
    pulse: 1,
    travel: 1,
    shape: (a) => {
      const c = Math.cos(a);
      return flat(a, -6 * c + Math.sqrt(36 * c * c + 540), 12, 0.45);
    },
    limbs: [-14, 0, 14].map((x, i) =>
      limb({
        x,
        y: 9,
        seg: 3.5,
        w0: 5.8,
        w1: 4.6,
        side: x < 0 ? -1 : 1,
        rest: [0, 0, 0, 0, 0],
        sway: 0.55,
        ph: i * 1.9,
      }),
    ),
  },
  octopus: {
    colour: 'purple',
    cx: 32,
    cy: 27,
    eyeX: 0,
    eyeY: 3,
    gap: 5.3,
    pulse: 0.35,
    travel: 1,
    shape: (a) => ell(a, 19.5, 19) + 1.6 * pair(a, 2.05, 0.5) - 2.5 * bump(a, PI, 0.5),
    limbs: [
      limb({
        x: -12,
        y: 11,
        seg: 3.4,
        w0: 7.4,
        w1: 3,
        side: -1,
        rest: [40, 50, 65, 90, 125],
        sway: 0.28,
        ph: 0,
      }),
      limb({
        x: 12,
        y: 11,
        seg: 3.4,
        w0: 7.4,
        w1: 3,
        side: 1,
        rest: [40, 50, 65, 90, 125],
        raise: [60, 100, 130, 150, 165],
        sway: 0.28,
        ph: 2.1,
      }),
      limb({
        x: -4,
        y: 12,
        seg: 3.4,
        w0: 7.4,
        w1: 3,
        side: -1,
        rest: [12, 18, 30, 55, 95],
        sway: 0.3,
        ph: 1.1,
      }),
      limb({
        x: 4,
        y: 12,
        seg: 3.4,
        w0: 7.4,
        w1: 3,
        side: 1,
        rest: [12, 18, 30, 55, 95],
        sway: 0.3,
        ph: 3.3,
      }),
    ],
  },
  whale: {
    colour: 'blue',
    cx: 34,
    cy: 33,
    eyeX: 5,
    eyeY: -2,
    gap: 4.4,
    pulse: 0.3,
    travel: 0.5,
    shape: (a) => ell(a, 21.5, 20.5) + 3.5 * bump(a, -1.25, 0.34),
    limbs: [
      limb({
        x: -19,
        y: -8,
        seg: 3,
        w0: 7.5,
        w1: 5,
        side: -1,
        rest: [120, 140],
        sway: 0.35,
        ph: 0,
        fluke: { rx: 4.8, ry: 2.8, out: 3.2, lean: 0.25 },
      }),
      limb({
        x: 2,
        y: -17.5,
        seg: 3.4,
        w0: 3.2,
        w1: 2.6,
        side: -1,
        rest: [165, 125],
        sway: 0.3,
        ph: 1,
        working: true,
      }),
      limb({
        x: 4,
        y: -17.5,
        seg: 3.4,
        w0: 3.2,
        w1: 2.6,
        side: 1,
        rest: [165, 125],
        sway: 0.3,
        ph: 2,
        working: true,
      }),
    ],
  },
  starfish: {
    colour: 'amber',
    cx: 32,
    cy: 32.5,
    eyeX: 0,
    eyeY: -1,
    gap: 4.4,
    pulse: 0.25,
    travel: 0.9,
    shape: (a, t) => 18.5 + 6 * (0.5 + 0.5 * Math.cos(5 * a + 0.1 * Math.sin(t * 0.8))),
    limbs: [],
  },
  sponge: {
    colour: 'brown',
    cx: 32,
    cy: 33,
    eyeX: 0,
    eyeY: -1,
    gap: 4.6,
    pulse: 0.3,
    travel: 0.9,
    shape: (a) => rounded(a, 18.5) + bump(a, 2.3, 0.8),
    limbs: [],
  },
  coral: {
    colour: 'pink',
    cx: 32,
    cy: 32,
    eyeX: 0,
    eyeY: 0,
    gap: 4.6,
    pulse: 0.2,
    travel: 0.9,
    shape: (a) =>
      flat(
        a,
        17 + 5.2 * bump(a, 0, 0.38) + 4.2 * pair(a, 0.98, 0.36) + 2 * pair(a, 1.8, 0.4),
        15.5,
        0.5,
      ),
    limbs: [],
  },
  turtle: {
    colour: 'teal',
    cx: 31,
    cy: 33,
    eyeX: 3,
    eyeY: -1,
    gap: 4.6,
    pulse: 0.2,
    travel: 0.6,
    shape: (a) => {
      const side = PI / 3;
      const phi = ((((a - PI / 2) % side) + side) % side) - PI / 6;
      return 11 + (9.9 * Math.cos(PI / 6)) / Math.cos(phi) + 3.5 * bump(a, PI / 2, 0.3);
    },
    limbs: [],
  },
  clam: {
    colour: 'green',
    cx: 32,
    cy: 31,
    eyeX: 0,
    eyeY: 0,
    gap: 4.6,
    pulse: 0.2,
    travel: 0.9,
    shape: (a) => {
      const u = Math.abs(wrap(a)) / PI;
      return (
        18 +
        4.5 * (1 - smooth(0.3, 0.75, u)) -
        5.5 * smooth(0.55, 1, u) +
        3.6 * pair(a, 0.95, 0.5) -
        2 * bump(a, 0, 0.5)
      );
    },
    limbs: [],
  },
  seal: {
    colour: 'sky',
    cx: 32,
    cy: 33,
    eyeX: 0,
    eyeY: -4,
    gap: 4.6,
    pulse: 0.3,
    travel: 0.7,
    shape: (a) =>
      1 / (Math.abs(Math.sin(a) / 17) ** 3 + Math.abs(Math.cos(a) / 18.5) ** 3) ** (1 / 3),
    limbs: [
      limb({ x: -14, y: 7, seg: 3.5, w0: 7.2, w1: 5, side: -1, rest: [50, 70], sway: 0.5, ph: 0 }),
      limb({ x: 14, y: 7, seg: 3.5, w0: 7.2, w1: 5, side: 1, rest: [50, 70], sway: 0.5, ph: 1.6 }),
    ],
  },
};

export const RING = 64;
export const ringAngles = Array.from({ length: RING }, (_, i) => wrap((i / RING) * PI * 2));

const f1 = (n: number) => n.toFixed(1);

// spline is a closed smooth path through pts, always wound the same way so overlapping parts of
// one path never cut holes in each other.
export function spline(pts: Point[]): string {
  let area = 0;
  for (let i = 0; i < pts.length; i++) {
    const [px, py] = pts[i];
    const [qx, qy] = pts[(i + 1) % pts.length];
    area += px * qy - qx * py;
  }
  const p = area < 0 ? [...pts].reverse() : pts;
  const n = p.length;
  let d = `M${f1(p[0][0])},${f1(p[0][1])}`;
  for (let i = 0; i < n; i++) {
    const p0 = p[(i + n - 1) % n];
    const p1 = p[i];
    const p2 = p[(i + 1) % n];
    const p3 = p[(i + 2) % n];
    d +=
      `C${f1(p1[0] + (p2[0] - p0[0]) / 6)},${f1(p1[1] + (p2[1] - p0[1]) / 6)} ` +
      `${f1(p2[0] - (p3[0] - p1[0]) / 6)},${f1(p2[1] - (p3[1] - p1[1]) / 6)} ${f1(p2[0])},${f1(p2[1])}`;
  }
  return d + 'Z';
}

export type Frame = {
  ox: number;
  oy: number;
  sx: number;
  sy: number;
  t: number;
  dents?: Float32Array;
};

export function bodyPath(kind: Kind, f: Frame): string {
  const pts: Point[] = ringAngles.map((a, i) => {
    const r = kind.shape(a, f.t) + (f.dents?.[i] ?? 0);
    return [f.ox + Math.sin(a) * r * f.sx, f.oy - Math.cos(a) * r * f.sy];
  });
  return spline(pts);
}

// limbAngle is where segment j of l points: 0 is straight down, outward is positive. curl
// straightens limbs below 1; raise lifts the arms that have a raised pose.
export function limbAngle(l: Limb, j: number, curl: number, raise: number) {
  const rest = l.raise ? l.rest[j] + (l.raise[j] - l.rest[j]) * raise : l.rest[j];
  return l.side * rest * curl;
}

// limbOutline is the filled shape around chain, the points down a limb from its root.
export function limbOutline(l: Limb, chain: Point[]): string {
  const m = chain.length;
  const left: Point[] = [];
  const right: Point[] = [];
  for (let j = 0; j < m; j++) {
    const a = chain[Math.max(0, j - 1)];
    const b = chain[Math.min(m - 1, j + 1)];
    let nx = -(b[1] - a[1]);
    let ny = b[0] - a[0];
    const len = Math.hypot(nx, ny) || 1;
    nx /= len;
    ny /= len;
    const w = (l.w0 + (l.w1 - l.w0) * (j / (m - 1))) / 2;
    left.push([chain[j][0] + nx * w, chain[j][1] + ny * w]);
    right.push([chain[j][0] - nx * w, chain[j][1] - ny * w]);
    if (j === m - 1) left.push([chain[j][0] + ny * w, chain[j][1] - nx * w]);
  }
  let d = spline([...left, ...right.reverse()]);
  if (l.fluke) {
    const fl = l.fluke;
    const [a, b] = [chain[m - 2], chain[m - 1]];
    let tx = b[0] - a[0];
    let ty = b[1] - a[1];
    const tl = Math.hypot(tx, ty) || 1;
    tx /= tl;
    ty /= tl;
    for (const s of [-1, 1]) {
      let ax = -ty * s + tx * fl.lean;
      let ay = tx * s + ty * fl.lean;
      const al = Math.hypot(ax, ay);
      ax /= al;
      ay /= al;
      const cx = b[0] + ax * fl.out;
      const cy = b[1] + ay * fl.out;
      const lobe: Point[] = [];
      for (let q = 0; q < 10; q++) {
        const u = (q / 10) * PI * 2;
        lobe.push([
          cx + ax * fl.rx * Math.cos(u) - ay * fl.ry * Math.sin(u),
          cy + ay * fl.rx * Math.cos(u) + ax * fl.ry * Math.sin(u),
        ]);
      }
      d += spline(lobe);
    }
  }
  return d;
}

// restChain is the still pose of l on a body drawn by f.
export function restChain(l: Limb, f: Frame, curl: number, raise: number): Point[] {
  const rootX = f.ox + l.x * f.sx;
  const rootY = f.oy + l.y * f.sy;
  const first = limbAngle(l, 0, curl, raise);
  const chain: Point[] = [
    [rootX - Math.sin(first) * 3, rootY - Math.cos(first) * 3],
    [rootX, rootY],
  ];
  let [x, y] = [rootX, rootY];
  l.rest.forEach((_, j) => {
    const th = limbAngle(l, j, curl, raise);
    x += Math.sin(th) * l.seg;
    y += Math.cos(th) * l.seg;
    chain.push([x, y]);
  });
  return chain;
}

// eyesPath draws two pills of half-length hl, leaning by left and right radians.
export function eyesPath(
  cx: number,
  cy: number,
  gap: number,
  hl: number,
  left: number,
  right: number,
) {
  const eye = (x: number, a: number) =>
    `M${f1(x - Math.sin(a) * hl)},${(cy + Math.cos(a) * hl).toFixed(2)}` +
    `L${f1(x + Math.sin(a) * hl)},${(cy - Math.cos(a) * hl).toFixed(2)}`;
  return eye(cx - gap, left) + eye(cx + gap, right);
}
