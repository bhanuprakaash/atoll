import { useState } from 'react';
import { Navigate, Outlet, useNavigate, useParams } from 'react-router';
import { Creature } from '@/components/creature';
import { useBots, type Bot } from '@/modules/bots/api/bots';
import { BotMemory } from '@/modules/memory/components/bot-memory';
import { BotSettings } from '@/modules/bots/components/bot-settings';
import { useConnections } from '@/modules/connections/api/connections';
import { useProviders } from '@/modules/providers/api/providers';
import { ModelSelect } from '@/modules/providers/components/model-select';
import { useRepos } from '@/modules/repos/api/repos';
import { useSkills } from '@/modules/skills/api/skills';
import { ThreadsPanel } from '@/modules/threads/components/threads-panel';
import { useThreads } from '@/modules/threads/api/threads';

export function BotHeader({
  bot,
  title,
  waiting = 0,
  onThreads,
  onSettings,
}: {
  bot?: Bot;
  title?: string;
  waiting?: number;
  onThreads?: () => void;
  onSettings?: () => void;
}) {
  return (
    <header
      data-tauri-drag-region
      className="flex min-h-16 items-center gap-3 border-b border-line px-5 py-2"
    >
      {bot && <Creature look={bot.look} state={bot.state} size={30} />}
      <div className="min-w-0 flex-1">
        <h1>{bot?.name}</h1>
        <div className="truncate text-xs text-muted">{title}</div>
      </div>
      <button
        type="button"
        aria-label={waiting ? `Threads, ${waiting} waiting` : 'Threads'}
        onClick={onThreads}
        className="flex h-8.5 items-center gap-2 rounded-full border border-outline pr-3 pl-2.5 text-[13.5px] hover:bg-line"
      >
        <svg
          aria-hidden="true"
          width="16"
          height="16"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          strokeWidth="1.8"
          strokeLinecap="round"
        >
          <path d="M4 6h16M4 12h16M4 18h10" />
        </svg>
        Threads
        {waiting > 0 && (
          <span
            aria-hidden="true"
            className="flex items-center gap-1 rounded-full bg-needs-badge px-2 py-px text-xs text-needs-ink"
          >
            <span className="size-1.5 rounded-full bg-danger" />
            {waiting}
          </span>
        )}
      </button>
      <button
        type="button"
        aria-label="Bot settings"
        onClick={onSettings}
        className="rounded-full p-2 text-muted"
      >
        <svg
          aria-hidden="true"
          width="18"
          height="18"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          strokeWidth="1.8"
        >
          <path d="M4 7h10M18 7h2M4 17h4M12 17h8" />
          <circle cx="16" cy="7" r="2" />
          <circle cx="10" cy="17" r="2" />
        </svg>
      </button>
    </header>
  );
}

export function BotPage() {
  const { botId = '', threadId = '' } = useParams();
  const { data: bots = [] } = useBots();
  const { data: threads = [] } = useThreads(botId);
  const { data: repos = [] } = useRepos();
  const { data: skills = [] } = useSkills();
  const { data: connections = [] } = useConnections();
  const { data: providers = [] } = useProviders();
  const [settingsOpen, setSettingsOpen] = useState(false);
  const [panel, setPanel] = useState(false);
  const navigate = useNavigate();
  const bot = bots.find((item) => item.id === botId);
  return (
    <div className="flex min-h-0 min-w-0 flex-1">
      <div className="flex min-h-0 min-w-0 flex-[999_1_400px] flex-col">
        <BotHeader
          bot={bot}
          onSettings={() => setSettingsOpen(true)}
          onThreads={() => setPanel(true)}
          title={threads.find((thread) => thread.id === threadId)?.title}
          waiting={threads.filter((thread) => !thread.closed && thread.approval).length}
        />
        <Outlet />
      </div>
      {settingsOpen && bot && (
        <BotSettings
          key={bot.id}
          bot={bot}
          memory={<BotMemory botId={bot.id} />}
          onClose={() => setSettingsOpen(false)}
          repos={repos}
          skills={skills}
          connections={connections}
          modelControl={(provider, model, onChange) => (
            <ModelSelect
              providers={providers}
              provider={provider}
              model={model}
              onChange={onChange}
            />
          )}
        />
      )}
      {panel && (
        <ThreadsPanel
          name={bot?.name ?? ''}
          threads={threads}
          repos={repos}
          currentId={threadId}
          onClose={() => setPanel(false)}
          onSelect={(id) => {
            setPanel(false);
            navigate(`/bots/${botId}/threads/${id}`);
          }}
          onNew={() => {
            setPanel(false);
            navigate(`/bots/${botId}/new`);
          }}
        />
      )}
    </div>
  );
}

export function LatestThread() {
  const { botId = '' } = useParams();
  const { data: threads } = useThreads(botId);
  if (!threads) return null;
  const latest = threads[0];
  return (
    <Navigate to={latest ? `/bots/${botId}/threads/${latest.id}` : `/bots/${botId}/new`} replace />
  );
}
