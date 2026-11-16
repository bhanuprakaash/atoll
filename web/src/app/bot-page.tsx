import { useEffect, useState, type ReactNode } from 'react';
import {
  Navigate,
  Link,
  Outlet,
  useLocation,
  useNavigate,
  useOutletContext,
  useParams,
  useSearchParams,
} from 'react-router';
import { Creature, type CreatureLook } from '@/components/creature';
import { Gone } from '@/components/gone';
import { useBots, type Bot } from '@/modules/bots/api/bots';
import { BotMemory } from '@/modules/memory/components/bot-memory';
import { BotSettings } from '@/modules/bots/components/bot-settings';
import { useConnections } from '@/modules/connections/api/connections';
import { ReconnectButton } from '@/modules/connections/components/reconnect-button';
import { useMcpServers } from '@/modules/mcp-servers/api/mcp-servers';
import { useProviders } from '@/modules/providers/api/providers';
import { ModelSelect } from '@/modules/providers/components/model-select';
import { useRepos } from '@/modules/repos/api/repos';
import { useSkills } from '@/modules/skills/api/skills';
import { useThread, useThreads, type Thread } from '@/modules/threads/api/threads';
import { HelperCreature } from '@/modules/threads/components/helper-creature';
import { ThreadPanel } from '@/modules/threads/components/thread-panel';
import {
  ApprovalDrop,
  FinishedNotice,
  NotificationList,
} from '@/modules/threads/components/notifications';
import type { Notifications } from '@/modules/threads/hooks/use-notifications';
import { helperState, isLiveHelper, type HelperState } from '@/modules/threads/utils/helper-state';

export function BotHeader({
  bot,
  thread,
  boss,
  helpers = [],
  waiting = 0,
  open = false,
  onNotifications,
  onSettings,
  details,
  children,
}: {
  bot?: Bot;
  thread?: Thread;
  boss?: { bot: Bot; threadId: string; title: string };
  helpers?: { id: string; name: string; look?: CreatureLook; state: HelperState }[];
  waiting?: number;
  open?: boolean;
  onNotifications?: () => void;
  onSettings?: () => void;
  details?: { open: boolean; toggle: () => void };
  children?: ReactNode;
}) {
  const title = thread?.title;
  return (
    <header
      data-tauri-drag-region="deep"
      className="@container relative z-10 grid h-18 flex-none grid-cols-[1fr_auto_1fr] items-start bg-bg pt-3.5"
    >
      <div
        data-island
        className={`relative z-30 col-start-2 flex flex-col overflow-hidden border border-composer-line bg-composer shadow-[0_12px_32px_rgba(0,0,0,.4)] transition-[min-width,max-height,border-radius] duration-420 ease-[cubic-bezier(.32,1.25,.5,1)] motion-reduce:transition-none ${open ? 'max-h-[60vh] min-w-[min(25rem,calc(100cqw-7rem))] max-w-[min(25rem,calc(100cqw-7rem))] rounded-[22px]' : 'max-h-10 min-w-0 rounded-[20px]'}`}
      >
        <div className="relative flex h-9.5 flex-none items-center justify-center gap-2.5 px-3 whitespace-nowrap">
          {boss ? (
            <nav
              aria-label="Breadcrumb"
              className="relative z-10 flex min-w-0 items-center gap-1.5 text-sm"
            >
              <Creature look={boss.bot.look} state={boss.bot.state} size={22} />
              <Link
                to={`/bots/${boss.bot.id}/threads/${boss.threadId}`}
                className="max-w-40 truncate text-muted hover:text-ink"
              >
                {boss.title}
              </Link>
              <span aria-hidden="true" className="text-label">
                ›
              </span>
              {bot && <Creature look={bot.look} state={bot.state} size={18} />}
              <h1>{bot?.name}</h1>
            </nav>
          ) : (
            <>
              {bot && <Creature look={bot.look} state={bot.state} size={22} />}
              <div className="flex min-w-0 items-center gap-1.5">
                <h1 className="text-sm">{bot?.name}</h1>
                {title && (
                  <>
                    <span aria-hidden="true" className="hidden text-label @min-[32rem]:inline">
                      ·
                    </span>
                    <h2 className="hidden max-w-50 truncate text-sm font-normal text-muted @min-[32rem]:block">
                      {title}
                    </h2>
                  </>
                )}
              </div>
            </>
          )}
          {helpers.length > 0 && (
            <div className="flex items-center gap-1">
              {helpers.map((h) => (
                <HelperCreature key={h.id} name={h.name} creature={h.look} state={h.state} />
              ))}
            </div>
          )}
          {thread?.approval ? (
            <span className="text-[13px] text-ink">Needs you</span>
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
            thread?.run_status === 'failed' &&
            thread.run_error_kind !== 'secret_store' && (
              <span className="text-[13px] text-muted">Run failed</span>
            )
          )}
          {waiting > 0 && (
            <span
              aria-hidden="true"
              className="flex items-center gap-1 rounded-full bg-tile px-2 py-px text-xs text-ink"
            >
              <span className="size-1.5 rounded-full bg-accent" />
              {waiting}
            </span>
          )}
          <button
            type="button"
            aria-label={waiting ? `Notifications, ${waiting} waiting` : 'Notifications'}
            aria-haspopup="dialog"
            onClick={onNotifications}
            className="absolute inset-0 hover:bg-ink/5"
          />
        </div>
        {children}
      </div>
      <div className="col-start-3 mr-4 flex items-center justify-self-end">
        {details && (
          <button
            type="button"
            aria-label="Thread details"
            aria-haspopup="dialog"
            aria-expanded={details.open}
            data-thread-details
            onClick={details.toggle}
            className="flex size-10 items-center justify-center rounded-full text-muted"
          >
            <svg
              aria-hidden="true"
              width="18"
              height="18"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              strokeWidth="1.8"
              strokeLinecap="round"
            >
              <path d="M9 6h11M9 12h11M9 18h11M4.5 6h.01M4.5 12h.01M4.5 18h.01" />
            </svg>
          </button>
        )}
        <button
          type="button"
          aria-label="Bot settings"
          onClick={onSettings}
          className="flex size-10 items-center justify-center rounded-full text-muted"
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
      </div>
      <span
        aria-hidden="true"
        className="pointer-events-none absolute inset-x-0 top-full h-6 bg-linear-to-b from-bg to-transparent"
      />
    </header>
  );
}

function BotFrame() {
  const { botId = '', threadId = '' } = useParams();
  const { data: bots = [] } = useBots();
  const { data: threads = [] } = useThreads(botId);
  const { data: repos = [] } = useRepos();
  const { data: skills = [] } = useSkills();
  const { data: mcpServers = [] } = useMcpServers();
  const { data: connections = [] } = useConnections();
  const { data: providers = [] } = useProviders();
  const [params, setParams] = useSearchParams();
  const settingsOpen = params.has('settings');
  const setSettingsOpen = (open: boolean) =>
    setParams(
      (prev) => {
        const next = new URLSearchParams(prev);
        if (open) next.set('settings', '');
        else next.delete('settings');
        return next;
      },
      { replace: true },
    );
  const [list, setList] = useState(false);
  const location = useLocation();
  const navigate = useNavigate();
  const { data: openThread, refetch, dataUpdatedAt } = useThread(threadId);
  const [opened, setOpened] = useState<{ id: string; at: number } | null>(null);
  const detailsOpen = !!openThread && opened?.id === threadId;
  const setDetailsFor = (id: string) => {
    setOpened(id ? { id, at: Date.now() } : null);
    if (id) void refetch();
  };
  useEffect(() => setOpened(null), [threadId]);
  useEffect(() => {
    if (!(location.state as { helpers?: boolean } | null)?.helpers) return;
    setDetailsFor(threadId);
    navigate(location.pathname + location.search, { replace: true, state: null });
  }, [location, threadId, navigate]);
  useEffect(() => {
    const icon = () => document.querySelector<HTMLElement>('[data-thread-details]');
    const onKey = (e: KeyboardEvent) => {
      if ((e.ctrlKey || e.metaKey) && e.key === '.') {
        e.preventDefault();
        setDetailsFor(opened?.id === threadId || !threadId ? '' : threadId);
      } else if (e.key === 'Escape' && detailsOpen && !e.defaultPrevented) {
        e.preventDefault();
        setDetailsFor('');
        icon()?.focus();
      }
    };
    const onDown = (e: PointerEvent) => {
      const target = e.target as Element;
      if (detailsOpen && !target.closest('[data-thread-panel], [data-thread-details]'))
        setDetailsFor('');
    };
    window.addEventListener('keydown', onKey);
    window.addEventListener('pointerdown', onDown);
    return () => {
      window.removeEventListener('keydown', onKey);
      window.removeEventListener('pointerdown', onDown);
    };
  }, [detailsOpen, threadId, opened]);
  const notifications = useOutletContext<Notifications>();
  const { drop, widened, dismiss, markSeen } = notifications;
  const expanded = list || !!drop;
  useEffect(() => (list ? markSeen : undefined), [list, markSeen]);
  useEffect(() => {
    if (!expanded) return;
    const shrink = () => {
      setList(false);
      dismiss();
    };
    const onKey = (e: KeyboardEvent) => e.key === 'Escape' && !e.defaultPrevented && shrink();
    const onDown = (e: PointerEvent) => !(e.target as Element).closest('[data-island]') && shrink();
    window.addEventListener('keydown', onKey);
    window.addEventListener('pointerdown', onDown);
    return () => {
      window.removeEventListener('keydown', onKey);
      window.removeEventListener('pointerdown', onDown);
    };
  }, [expanded, dismiss]);
  const bot = bots.find((item) => item.id === botId);
  const looks = Object.fromEntries(bots.map((item) => [item.id, item.look]));
  const current = threads.find((thread) => thread.id === threadId);
  const bossThread = current?.parent_thread_id
    ? bots
        .flatMap((b) => (b.threads ?? []).map((t) => ({ bot: b, thread: t })))
        .find(({ thread }) => thread.id === current.parent_thread_id)
    : undefined;
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
      <div className="relative flex min-h-0 min-w-0 flex-[999_1_400px] flex-col">
        <BotHeader
          bot={bot}
          onSettings={() => setSettingsOpen(true)}
          onNotifications={() => setList(!list)}
          details={
            openThread && {
              open: detailsOpen,
              toggle: () => setDetailsFor(detailsOpen ? '' : threadId),
            }
          }
          open={expanded || !!widened}
          thread={threads.find((thread) => thread.id === threadId)}
          boss={
            bossThread && {
              bot: bossThread.bot,
              threadId: bossThread.thread.id,
              title: bossThread.thread.title,
            }
          }
          helpers={helpers}
          waiting={notifications.needs.length}
        >
          {list ? (
            <NotificationList
              notifications={notifications}
              looks={looks}
              onClose={() => setList(false)}
            />
          ) : drop ? (
            <ApprovalDrop approval={drop} look={looks[drop.bot_id]} />
          ) : (
            widened && <FinishedNotice notice={widened} look={looks[widened.bot_id]} />
          )}
        </BotHeader>
        <Outlet />
        {detailsOpen && dataUpdatedAt >= opened.at && (
          <ThreadPanel
            thread={openThread}
            repoName={repos.find((r) => r.id === openThread.repo_id)?.name}
            bots={Object.fromEntries(bots.map((b) => [b.id, { name: b.name, look: b.look }]))}
          />
        )}
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
          reconnect={(connection) => <ReconnectButton connection={connection} />}
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

export function BotPage() {
  const { botId = '' } = useParams();
  const { data: bots } = useBots();
  if (!bots) return null;
  if (!bots.some((bot) => bot.id === botId))
    return <Gone title="This Bot is gone" to="/" link="Go home" />;
  return <BotFrame />;
}

export function LatestThread() {
  const { botId = '' } = useParams();
  const { data: threads } = useThreads(botId);
  const { search } = useLocation();
  if (!threads) return null;
  const latest = threads[0];
  return (
    <Navigate
      to={`${latest ? `/bots/${botId}/threads/${latest.id}` : `/bots/${botId}/new`}${search}`}
      replace
    />
  );
}
