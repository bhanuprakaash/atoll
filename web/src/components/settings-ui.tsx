import type { ReactNode } from 'react';
import { Creature, type CreatureLook } from '@/components/creature';

export const outlineButton =
  'h-8 flex-none rounded-full border border-outline px-3.5 text-[13px] text-ink disabled:opacity-50';
export const solidButton =
  'h-8 flex-none rounded-full bg-accent px-3.5 text-[13px] text-accent-ink disabled:opacity-50';

export function SectionHead({
  title,
  lead,
  children,
}: {
  title: string;
  lead: string;
  children?: ReactNode;
}) {
  return (
    <div className="flex items-end gap-3">
      <div className="flex-1">
        <h2 className="text-[22px] font-normal tracking-tight">{title}</h2>
        <p className="mt-1 text-[13.5px] text-muted">{lead}</p>
      </div>
      {children}
    </div>
  );
}

export function Box({ children }: { children: ReactNode }) {
  return (
    <div className="divide-y divide-line overflow-hidden rounded-[18px] border border-line bg-box">
      {children}
    </div>
  );
}

export function Tile({
  className = 'bg-tile',
  children,
}: {
  className?: string;
  children: ReactNode;
}) {
  return (
    <span
      aria-hidden="true"
      className={`flex size-9 flex-none items-center justify-center rounded-[10px] ${className}`}
    >
      {children}
    </span>
  );
}

export function RowText({
  title,
  detail,
  mono,
  monoTitle,
  children,
}: {
  title: string;
  detail?: ReactNode;
  mono?: boolean;
  monoTitle?: boolean;
  children?: ReactNode;
}) {
  return (
    <span className="min-w-0 flex-1">
      <b className={`block text-[14.5px] font-normal ${monoTitle ? 'font-mono text-[13px]' : ''}`}>
        {title}
      </b>
      {detail !== undefined && detail !== '' && (
        <i
          className={`block truncate text-[12.5px] not-italic text-muted ${mono ? 'font-mono' : ''}`}
        >
          {detail}
        </i>
      )}
      {children}
    </span>
  );
}

export const rowClass = 'flex items-center gap-3 py-3 pl-3.5 pr-3';

export function Tag({
  tone = 'plain',
  children,
}: {
  tone?: 'ok' | 'warn' | 'plain';
  children: ReactNode;
}) {
  const colours = {
    ok: 'bg-ok-bg text-ok',
    warn: 'bg-warn-bg text-warn',
    plain: 'bg-tile text-muted',
  };
  return (
    <span className={`flex-none rounded-full px-[9px] py-0.5 text-xs ${colours[tone]}`}>
      {children}
    </span>
  );
}

type Bot = { id: string; name: string; look: CreatureLook };

export function UsedBy({ bots }: { bots: Bot[] }) {
  return (
    <span
      role="img"
      aria-label={`Used by ${bots.map((b) => b.name).join(', ')}`}
      className="mr-1 flex flex-none"
    >
      {bots.map((b) => (
        <span key={b.id} className="-ml-1.5 rounded-full bg-box">
          <Creature look={b.look} size={24} />
        </span>
      ))}
    </span>
  );
}
