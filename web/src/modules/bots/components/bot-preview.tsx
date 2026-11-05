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
  return (
    <div className="flex flex-col items-center gap-3.5 rounded-[28px] border border-selected bg-box px-6 pt-7 pb-6 text-center">
      <Creature look={look} size={112} />
      <h3 className={`text-2xl ${name ? '' : 'text-muted'}`}>{name || 'Name your Bot'}</h3>
      {model ? (
        <p className="rounded-full bg-tile px-3 py-1 text-xs">
          {provider === 'claude' ? 'Claude Code' : 'Codex'} · {model}
        </p>
      ) : (
        <p className="text-xs text-muted">Choose a model</p>
      )}
      <p className="line-clamp-3 w-full text-sm break-words whitespace-pre-wrap text-muted">
        {persona || 'Add a persona'}
      </p>
      <div className="flex flex-wrap justify-center gap-1.5 text-xs">
        {skills.map((skill) => (
          <span key={skill.id} className="rounded-full bg-tile px-3 py-1 font-mono">
            {skill.name}
          </span>
        ))}
        {connections.map((connection) => (
          <span
            key={connection.id}
            className="flex items-center gap-1.5 rounded-full bg-tile px-3 py-1"
          >
            <BrandLogo brand="gitlab" className="size-3.5" />
            GitLab · {connection.instance_url}
          </span>
        ))}
        {repos === 'all' ? (
          <span className="rounded-full bg-tile px-3 py-1">All repos</span>
        ) : repos.length ? (
          repos.map((repo) => (
            <span key={repo.id} className="rounded-full bg-tile px-3 py-1 font-mono">
              {repo.name}
            </span>
          ))
        ) : (
          <span className="text-muted">Choose repos</span>
        )}
      </div>
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
          <span className="text-xs text-muted">—</span>
        </span>
        <span className="truncate text-[13px] text-muted">No activity yet</span>
      </span>
    </>
  );
}
