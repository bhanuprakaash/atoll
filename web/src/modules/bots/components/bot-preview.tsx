import type { CSSProperties } from 'react';
import { BrandLogo } from '@/components/brand-logo';
import { Creature, type CreatureLook } from '@/components/creature';
import type { Provider } from '@/modules/bots/api/bots';

type Named = { id: string; name: string };

const shown = 3;

function more(names: string[], chip: string) {
  const rest = names.slice(shown);
  return rest.length ? (
    <span className={`${chip} flex-none`} title={rest.join(', ')}>
      +{rest.length}
    </span>
  ) : null;
}

export function BotPreview({
  name,
  persona,
  provider,
  model,
  look,
  skills,
  connections,
  repos,
}: {
  name: string;
  persona: string;
  provider: Provider;
  model: string;
  look: CreatureLook;
  skills: Named[];
  connections: { id: string; instance_url: string }[];
  repos: Named[] | 'all';
}) {
  const chip =
    'flex max-w-full items-center gap-1.5 rounded-full bg-[color-mix(in_srgb,var(--tint)_18%,var(--color-tile))] px-2.5 py-0.5 text-ink/80';
  const none = (text: string) => <span className="text-label">{text}</span>;
  return (
    <div
      className="overflow-hidden rounded-[28px] border border-[color-mix(in_srgb,var(--tint)_30%,var(--color-selected))] bg-[linear-gradient(180deg,color-mix(in_srgb,var(--tint)_26%,var(--color-box))_0%,color-mix(in_srgb,var(--tint)_12%,var(--color-box))_45%,color-mix(in_srgb,var(--tint)_5%,var(--color-box))_100%)]"
      style={{ '--tint': `var(--color-${look})` } as CSSProperties}
    >
      <div className="flex h-40 items-center justify-center">
        <Creature look={look} size={104} />
      </div>
      <div className="flex flex-col items-center gap-2.5 px-6 pb-5 text-center">
        <h3 className={`text-2xl tracking-[-0.01em] ${name ? '' : 'text-muted'}`}>
          {name || 'Name your Bot'}
        </h3>
        {model ? (
          <p className="flex items-center gap-1.5 rounded-full bg-[color-mix(in_srgb,var(--tint)_18%,var(--color-tile))] px-2.5 py-1 text-xs text-ink/80">
            <BrandLogo brand={provider} className="size-3.5" />
            {provider === 'claude' ? 'Claude Code' : 'Codex'} · {model}
          </p>
        ) : (
          <p className="rounded-full border border-dashed border-outline px-2.5 py-1 text-xs text-label">
            Choose a model
          </p>
        )}
        <p className="line-clamp-3 w-full text-[13.5px] break-words whitespace-pre-wrap text-muted">
          {persona || 'Add a persona so it knows how to work.'}
        </p>
      </div>
      <dl className="grid grid-cols-[84px_minmax(0,1fr)] items-center gap-x-3 gap-y-2.5 border-t border-[color-mix(in_srgb,var(--tint)_22%,var(--color-line))] px-5 py-4 text-xs">
        <dt className="text-label">Skills</dt>
        <dd className="flex min-w-0 flex-wrap gap-1">
          {skills.length
            ? [
                ...skills.slice(0, shown).map((skill) => (
                  <span key={skill.id} className={`${chip} min-w-0 font-mono`}>
                    <span className="truncate">{skill.name}</span>
                  </span>
                )),
                more(
                  skills.map((skill) => skill.name),
                  chip,
                ),
              ]
            : none('None yet')}
        </dd>
        <dt className="text-label">Repos</dt>
        <dd className="flex min-w-0 flex-wrap gap-1">
          {repos === 'all' ? (
            <span className={chip}>All repos</span>
          ) : repos.length ? (
            [
              ...repos.slice(0, shown).map((repo) => (
                <span key={repo.id} className={`${chip} min-w-0 font-mono`}>
                  <span className="truncate">{repo.name}</span>
                </span>
              )),
              more(
                repos.map((repo) => repo.name),
                chip,
              ),
            ]
          ) : (
            none('Choose repos')
          )}
        </dd>
        <dt className="text-label">Connections</dt>
        <dd className="flex min-w-0 flex-wrap gap-1">
          {connections.length
            ? [
                ...connections.slice(0, shown).map((connection) => (
                  <span key={connection.id} className={`${chip} min-w-0`}>
                    <BrandLogo brand="gitlab" className="size-3.5 flex-none" />
                    <span className="truncate">
                      {connection.instance_url.replace(/^https?:\/\//, '')}
                    </span>
                  </span>
                )),
                more(
                  connections.map((connection) => connection.instance_url),
                  chip,
                ),
              ]
            : none('None')}
        </dd>
      </dl>
    </div>
  );
}

export function CreatureSidebar({ look, name }: { look: CreatureLook; name: string }) {
  return (
    <>
      <Creature look={look} size={44} />
      <span className="flex min-w-0 flex-1 flex-col gap-0.5">
        <span className="flex justify-between gap-2 text-[15px]">
          <span className={`truncate ${name ? '' : 'text-muted'}`}>{name || 'Name your Bot'}</span>
        </span>
        <span className="truncate text-[13px] text-muted">No activity yet</span>
      </span>
    </>
  );
}
