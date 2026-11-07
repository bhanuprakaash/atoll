import { useState, type ReactNode } from 'react';
import { Navigate, Outlet, useNavigate, useParams } from 'react-router';
import { Creature, type CreatureLook } from '@/components/creature';
import { useBots, type Bot } from '@/modules/bots/api/bots';
import { BotMemory } from '@/modules/memory/components/bot-memory';
import { BotSettings } from '@/modules/bots/components/bot-settings';
import { useConnections } from '@/modules/connections/api/connections';
import { useMcpServers } from '@/modules/mcp-servers/api/mcp-servers';
import { useProviders } from '@/modules/providers/api/providers';
import { ModelSelect } from '@/modules/providers/components/model-select';
import { useRepos } from '@/modules/repos/api/repos';
import { useSkills } from '@/modules/skills/api/skills';
import { ThreadsPanel } from '@/modules/threads/components/threads-panel';
import { useThreads, type Thread } from '@/modules/threads/api/threads';
import { HelperCreature } from '@/modules/threads/components/helper-creature';
import { helperState, isLiveHelper, type HelperState } from '@/modules/threads/utils/helper-state';

export function BotHeader({
  bot,
  thread,
  helpers = [],
  waiting = 0,
  open = false,
  onThreads,
  onSettings,
  children,
}: {
  bot?: Bot;
  thread?: Thread;
  helpers?: { id: string; name: string; look?: CreatureLook; state: HelperState }[];
  waiting?: number;
  open?: boolean;
  onThreads?: () => void;
  onSettings?: () => void;
  children?: ReactNode;
}) {
  const title = thread?.title;
  return (
    <header
      data-tauri-drag-region="deep"
      className="grid h-18 flex-none grid-cols-[1fr_auto_1fr] items-start pt-3.5"
    >
      <div
        className={`relative z-30 col-start-2 flex flex-col overflow-hidden border border-composer-line bg-composer shadow-[0_12px_32px_rgba(0,0,0,.4)] transition-[min-width,max-height,border-radius] duration-420 ease-[cubic-bezier(.32,1.25,.5,1)] motion-reduce:transition-none ${open ? 'max-h-[60vh] min-w-100 rounded-[22px]' : 'max-h-10 min-w-0 rounded-[20px]'}`}
      >
        <div className="relative flex h-9.5 flex-none items-center justify-center gap-2.5 px-3 whitespace-nowrap">
          {bot && <Creature look={bot.look} state={bot.state} size={22} />}
          <div className="flex min-w-0 items-center gap-1.5">
            <h1 className="text-sm">{bot?.name}</h1>
            {title && (
              <>
                <span aria-hidden="true" className="text-label">
                  ·
                </span>
                <h2 className="max-w-50 truncate text-sm font-normal text-muted">{title}</h2>
              </>
            )}
          </div>
          {helpers.length > 0 && (
            <div className="flex items-center gap-1">
              {helpers.map((h) => (
                <HelperCreature key={h.id} name={h.name} creature={h.look} state={h.state} />
              ))}
            </div>
          )}
          {thread?.approval ? (
            <span className="text-[13px] text-needs-ink">Needs you</span>
          ) : thread?.run_status === 'running' ? (
            <span className="flex h-3.5 items-center gap-0.75">
              <span className="sr-only">Thinking</span>
              {[0, 1, 2].map((i) => (
                <i
                  key={i}
                  aria-hidden="true"
                  style={{ animationDelay: `${i * 0.15}s` }}
                  className="size-1.25 rounded-full bg-ink motion-safe:animate-wave motion-reduce:opacity-70"
                />
              ))}
            </span>
          ) : (
            thread?.run_status === 'failed' && (
              <span className="text-[13px] text-muted">Run failed</span>
            )
          )}
          {waiting > 0 && (
            <span
              aria-hidden="true"
              className="flex items-center gap-1 rounded-full bg-needs-badge px-2 py-px text-xs text-needs-ink"
            >
              <span className="size-1.5 rounded-full bg-danger" />
              {waiting}
            </span>
          )}
          <svg
            aria-hidden="true"
            width="12"
            height="12"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            strokeWidth="2.2"
            strokeLinecap="round"
            className={`text-muted ${open ? 'rotate-180' : ''}`}
          >
            <path d="M6 9l6 6 6-6" />
          </svg>
          <button
            type="button"
            aria-label={waiting ? `Threads, ${waiting} waiting` : 'Threads'}
            aria-expanded={open}
            onClick={onThreads}
            className="absolute inset-0 hover:bg-ink/5"
          />
        </div>
        {children}
      </div>
      <button
        type="button"
        aria-label="Bot settings"
        onClick={onSettings}
        className="col-start-3 mr-4 flex size-10 items-center justify-center justify-self-end rounded-full text-muted"
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
  const { data: mcpServers = [] } = useMcpServers();
  const { data: connections = [] } = useConnections();
  const { data: providers = [] } = useProviders();
  const [settingsOpen, setSettingsOpen] = useState(false);
  const [panel, setPanel] = useState(false);
  const navigate = useNavigate();
  const bot = bots.find((item) => item.id === botId);
  const helpers = (threads.find((thread) => thread.id === threadId)?.helpers ?? [])
    .filter(isLiveHelper)
    .map((helper) => {
      const helperBot = bots.find((item) => item.id === helper.bot_id);
      return {
        id: helper.id,
        name: helperBot?.name ?? 'Helper',
        look: helperBot?.look,
        state: helperState(helper),
      };
    });
  return (
    <div className="flex min-h-0 min-w-0 flex-1">
      <div className="flex min-h-0 min-w-0 flex-[999_1_400px] flex-col">
        <BotHeader
          bot={bot}
          onSettings={() => setSettingsOpen(true)}
          onThreads={() => setPanel(!panel)}
          open={panel}
          thread={threads.find((thread) => thread.id === threadId)}
          helpers={helpers}
          waiting={threads
            .filter((thread) => !thread.closed)
            .reduce(
              (count, thread) =>
                count +
                (thread.approval ? 1 : 0) +
                thread.helpers.filter((helper) => !helper.closed && helper.approval).length,
              0,
            )}
        >
          {panel && (
            <ThreadsPanel
              name={bot?.name ?? ''}
              botNames={Object.fromEntries(bots.map((item) => [item.id, item.name]))}
              threads={threads}
              repos={repos}
              currentId={threadId}
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
        </BotHeader>
        {panel && <div className="fixed inset-0 z-20" onClick={() => setPanel(false)} />}
        <Outlet />
      </div>
      {settingsOpen && bot && (
        <BotSettings
          key={bot.id}
          bot={bot}
          memory={<BotMemory botId={bot.id} />}
          onClose={() => setSettingsOpen(false)}
          repos={repos}
          bots={bots}
          skills={skills}
          connections={connections}
          mcpServers={mcpServers.filter((s) => s.kind !== 'connection')}
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
