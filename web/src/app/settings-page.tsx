import { NavLink, Outlet } from 'react-router';
import { useBots } from '@/modules/bots/api/bots';
import { ConnectionsSettings } from '@/modules/connections/components/connections-settings';
import { useMemory } from '@/modules/memory/api/memory';
import { MemorySettings } from '@/modules/memory/components/memory-settings';
import { useProviders } from '@/modules/providers/api/providers';
import { ProvidersSettings } from '@/modules/providers/components/providers-settings';
import { useRepos } from '@/modules/repos/api/repos';
import { useSkills } from '@/modules/skills/api/skills';
import { SkillsSettings } from '@/modules/skills/components/skills-settings';

export function ProvidersPage() {
  const { data: bots = [] } = useBots();
  return <ProvidersSettings bots={bots} />;
}

export function ConnectionsPage() {
  const { data: bots = [] } = useBots();
  return <ConnectionsSettings bots={bots} />;
}

export function SkillsPage() {
  const { data: bots = [] } = useBots();
  return <SkillsSettings bots={bots} />;
}

export function MemoryPage() {
  const { data: bots = [] } = useBots();
  const { data: repos = [] } = useRepos();
  return (
    <MemorySettings
      botNames={Object.fromEntries(bots.map((b) => [b.id, b.name]))}
      repoNames={Object.fromEntries(repos.map((r) => [r.id, r.name]))}
    />
  );
}

export function SettingsPage() {
  const { data: providers = [] } = useProviders();
  const { data: skills = [] } = useSkills();
  const { data: repos = [] } = useRepos();
  const { data: memory = [] } = useMemory();
  const toSignIn = providers.filter((p) => p.found && !p.signed_in).length;
  const sections = [
    ['general', 'General', ''],
    ['providers', 'Providers', toSignIn ? `${toSignIn} to sign in` : ''],
    ['connections', 'Connections', ''],
    ['skills', 'Skills', skills.length || ''],
    ['repos', 'Repos', repos.length || ''],
    ['memory', 'Memory', memory.filter((m) => m.status !== 'retired').length || ''],
  ] as const;
  return (
    <div className="flex min-h-0 flex-1">
      <nav
        aria-label="Settings"
        className="flex w-55 shrink-0 flex-col gap-0.5 border-r border-line px-3 py-6"
      >
        <h1 data-tauri-drag-region="deep" className="px-3 pb-3 text-base font-normal">
          Settings
        </h1>
        {sections.map(([path, label, note]) => (
          <NavLink
            key={path}
            to={`/settings/${path}`}
            className={({ isActive }) =>
              `flex h-[38px] items-center gap-2.5 rounded-xl px-3 text-sm ${isActive ? 'bg-line text-ink' : 'text-ink/80'}`
            }
          >
            {label}
            <span
              className={`ml-auto text-xs ${path === 'providers' ? 'text-blocked' : 'text-muted'}`}
            >
              {note}
            </span>
          </NavLink>
        ))}
      </nav>
      <div className="min-w-0 flex-1 overflow-auto px-10 pb-12 pt-8">
        <div className="mx-auto flex max-w-180 flex-col gap-4.5">
          <Outlet />
        </div>
      </div>
    </div>
  );
}
