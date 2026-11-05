import { BrandLogo } from '@/components/brand-logo';
import { Creature, type CreatureLook } from '@/components/creature';
import type { Provider } from '@/modules/bots/api/bots';

type Named = { id: string; name: string };

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
  const chip = 'flex items-center gap-1.5 rounded-full bg-tile px-2.5 py-0.5 text-ink/80';
  const none = (text: string) => <span className="text-label">{text}</span>;
  return (
    <div className="overflow-hidden rounded-[28px] border border-selected bg-box">
      <div
        className="flex h-40 items-center justify-center"
        style={{
          background: `radial-gradient(closest-side at 50% 55%, color-mix(in srgb, var(--color-${look}) 26%, transparent), transparent)`,
        }}
      >
        <Creature look={look} size={104} />
      </div>
      <div className="flex flex-col items-center gap-2.5 px-6 pb-5 text-center">
        <h3 className={`text-2xl tracking-[-0.01em] ${name ? '' : 'text-muted'}`}>
          {name || 'Name your Bot'}
        </h3>
        {model ? (
          <p className="flex items-center gap-1.5 rounded-full bg-tile px-2.5 py-1 text-xs text-ink/80">
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
      <dl className="grid grid-cols-[84px_minmax(0,1fr)] items-center gap-x-3 gap-y-2.5 border-t border-line px-5 py-4 text-xs">
        <dt className="text-label">Skills</dt>
        <dd className="flex flex-wrap gap-1">
          {skills.length
            ? skills.map((skill) => (
                <span key={skill.id} className={`${chip} font-mono`}>
                  {skill.name}
                </span>
              ))
            : none('None yet')}
        </dd>
        <dt className="text-label">Repos</dt>
        <dd className="flex flex-wrap gap-1">
          {repos === 'all' ? (
            <span className={chip}>All repos</span>
          ) : repos.length ? (
            repos.map((repo) => (
              <span key={repo.id} className={`${chip} font-mono`}>
                {repo.name}
              </span>
            ))
          ) : (
            none('Choose repos')
          )}
        </dd>
        <dt className="text-label">Connections</dt>
        <dd className="flex flex-wrap gap-1">
          {connections.length
            ? connections.map((connection) => (
                <span key={connection.id} className={chip}>
                  <BrandLogo brand="gitlab" className="size-3.5" />
                  {connection.instance_url.replace(/^https?:\/\//, '')}
                </span>
              ))
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
