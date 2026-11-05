export function ReefLogo({ size = 20 }: { size?: number }) {
  return (
    <svg width={size} height={size} viewBox="0 0 64 64" aria-hidden="true" className="flex-none">
      <path d="M8 36 a24 24 0 0 1 48 0 q0 6 -6 6 H14 q-6 0 -6 -6 z" fill="var(--color-jellyfish)" />
      <path
        d="M18 44 q-4 6 0 13 M32 44 q4 6 0 13 M46 44 q-4 6 0 13"
        fill="none"
        stroke="var(--color-jellyfish)"
        strokeWidth="5.5"
        strokeLinecap="round"
      />
      <path
        d="M37 20 L38.5 28 M45 19 L46.5 27"
        stroke="var(--color-eye)"
        strokeWidth="4"
        strokeLinecap="round"
      />
    </svg>
  );
}
