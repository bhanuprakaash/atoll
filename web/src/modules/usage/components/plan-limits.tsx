import { useState } from 'react';
import { BrandLogo } from '@/components/brand-logo';
import { quiet } from '@/components/button';
import { ErrorNotice } from '@/components/error-notice';
import { type LimitWindow, type ProviderLimits, useLimits } from '@/modules/usage/api/usage';
import { messageTime } from '@/utils/time';

const names = { claude: 'Claude', codex: 'Codex' };
const weekMinutes = 7 * 24 * 60;

function Bar({ name, window }: { name: string; window: LimitWindow }) {
  const percent = Math.round(window.used_percent);
  return (
    <div className="grid grid-cols-[4.5rem_1fr_2.5rem_9rem] items-center gap-3 text-[13px] max-[599.98px]:grid-cols-[4.5rem_1fr_2.5rem]">
      <span className="text-muted">{name}</span>
      <div
        role="progressbar"
        aria-label={name}
        aria-valuemin={0}
        aria-valuemax={100}
        aria-valuenow={percent}
        className="h-1.5 overflow-hidden rounded-full bg-tile"
      >
        <div
          className={`h-full rounded-full ${percent >= 90 ? 'bg-blocked' : 'bg-ink/70'}`}
          style={{ width: `${Math.min(percent, 100)}%` }}
        />
      </div>
      <span className="text-right tabular-nums">{percent}%</span>
      <span className="text-muted max-[599.98px]:col-span-3">
        Resets {messageTime(window.resets_at)}
      </span>
    </div>
  );
}

function Failed({ error }: { error: string }) {
  const [open, setOpen] = useState(false);
  return (
    <div className="flex flex-col gap-1 text-[13px]">
      <div className="flex items-center gap-3">
        <span className="text-muted">Not available right now.</span>
        <button type="button" aria-expanded={open} onClick={() => setOpen(!open)} className={quiet}>
          Details
        </button>
      </div>
      {open && <p className="text-xs text-muted">{error}</p>}
    </div>
  );
}

function Row({ limits }: { limits: ProviderLimits }) {
  const name = names[limits.provider];
  const { session, weekly, error } = limits;
  return (
    <div role="group" aria-label={name} className="flex gap-4 px-4 py-3">
      <div className="flex w-24 flex-none items-start gap-2 text-sm">
        <span role="img" aria-label={name} className="mt-0.5 flex">
          <BrandLogo brand={limits.provider} className="size-4" />
        </span>
        {name}
      </div>
      <div className="flex min-w-0 flex-1 flex-col gap-2">
        {error !== undefined && <Failed error={error} />}
        {session && <Bar name="Session" window={session} />}
        {weekly && (
          <Bar
            name={
              weekly.window_minutes === weekMinutes
                ? 'Weekly'
                : `${Math.round(weekly.window_minutes / 1440)} days`
            }
            window={weekly}
          />
        )}
      </div>
    </div>
  );
}

export function PlanLimits() {
  const { data, error, refetch } = useLimits();
  if (error) return <ErrorNotice error={error} retry={() => refetch()} />;
  if (!data || data.length === 0) return null;
  return (
    <section className="flex flex-col gap-2">
      <h3 className="text-[15px] font-normal">Plan limits</h3>
      <div className="divide-y divide-line overflow-hidden rounded-[18px] border border-line bg-box">
        {data.map((limits) => (
          <Row key={limits.provider} limits={limits} />
        ))}
      </div>
    </section>
  );
}
