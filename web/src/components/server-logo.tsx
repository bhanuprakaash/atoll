import { BrandLogo, type Brand } from '@/components/brand-logo';

const brands: Record<string, Brand> = {
  github: 'github',
  slack: 'slack',
  notion: 'notion',
  figma: 'figma',
  cloudflare: 'cloudflare',
};

export function ServerLogo({ id, className = 'size-4' }: { id: string; className?: string }) {
  const brand = brands[id];
  if (brand) return <BrandLogo brand={brand} className={className} />;
  return (
    <svg
      viewBox="0 0 24 24"
      className={className}
      fill="none"
      stroke="var(--color-ink)"
      strokeWidth="1.8"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
    >
      <path d="M4 5h16v6H4zM4 13h16v6H4zM8 8h.01M8 16h.01" />
    </svg>
  );
}
