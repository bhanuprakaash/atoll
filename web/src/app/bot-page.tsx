import { useEffect, useState } from 'react';
import { Navigate, Outlet, useLocation, useParams, useSearchParams } from 'react-router';
import { ThreadTabs } from '@/app/thread-tabs';
import { Gone } from '@/components/gone';
import { useBots } from '@/modules/bots/api/bots';
import { helperBoss } from '@/modules/bots/utils/thread-tree';
import { BotMemory } from '@/modules/memory/components/bot-memory';
import { BotSettings } from '@/modules/bots/components/bot-settings';
import { useConnections } from '@/modules/connections/api/connections';
import { ReconnectButton } from '@/modules/connections/components/reconnect-button';
import { useMcpServers } from '@/modules/mcp-servers/api/mcp-servers';
import { useProviders } from '@/modules/providers/api/providers';
import { ModelSelect } from '@/modules/providers/components/model-select';
import { useRepos } from '@/modules/repos/api/repos';
import { useSkills } from '@/modules/skills/api/skills';
import { useThread, useThreads } from '@/modules/threads/api/threads';
import { FilePanel } from '@/modules/threads/components/file-panel';
import { PRPanel } from '@/modules/threads/components/pr-panel';
import { ThreadPanel } from '@/modules/threads/components/thread-panel';
import type { FileRef } from '@/utils/file-ref';

// OpenPR opens a GitHub pull request in the PR panel, read with the given thread's Bot.
export type OpenPR = (threadId: string, url: string) => void;
// OpenFile opens a file of the given thread's checkout in the file panel.
export type OpenFile = (threadId: string, file: FileRef) => void;

export function BotHeader({
  onSettings,
  details,
}: {
  onSettings?: () => void;
  details?: { open: boolean; toggle: () => void };
}) {
  return (
    <header
      data-tauri-drag-region="deep"
      className="relative z-10 grid h-18 flex-none grid-cols-[1fr_auto_1fr] items-start bg-bg pt-3.5"
    >
      <div className="col-start-3 mt-1.5 mr-4 flex items-center gap-1 justify-self-end">
        {details && (
          <button
            type="button"
            aria-label="Thread details"
            aria-haspopup="dialog"
            aria-expanded={details.open}
            data-thread-details
            onClick={details.toggle}
            className={`flex size-8 items-center justify-center rounded-lg ${details.open ? 'bg-selected text-ink' : 'text-muted'}`}
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
              <rect x="4" y="4" width="16" height="16" rx="3" />
              <path d="M8 9h8M8 12.5h8M8 16h5" />
            </svg>
          </button>
        )}
        <button
          type="button"
          aria-label="Bot settings"
          onClick={onSettings}
          className="flex size-8 items-center justify-center rounded-lg text-muted"
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
  const { data: openThread } = useThread(threadId);
  const [detailsFor, setDetailsFor] = useState('');
  const [opened, setOpened] = useState<
    { from: string; threadId: string } & ({ url: string } | { file: FileRef })
  >();
  const shown = opened?.from === threadId ? opened : undefined;
  const open = (panel: { threadId: string } & ({ url: string } | { file: FileRef })) => {
    setOpened({ from: threadId, ...panel });
    setDetailsFor('');
    if (settingsOpen) setSettingsOpen(false);
  };
  const openPR: OpenPR = (prThread, url) => open({ threadId: prThread, url });
  const openFile: OpenFile = (fileThread, file) => open({ threadId: fileThread, file });
  const detailsOpen = !!openThread && detailsFor === threadId;
  useEffect(() => setDetailsFor(''), [threadId]);
  useEffect(() => {
    const icon = () => document.querySelector<HTMLElement>('[data-thread-details]');
    const onKey = (e: KeyboardEvent) => {
      if ((e.ctrlKey || e.metaKey) && e.key === '.') {
        e.preventDefault();
        setDetailsFor((now) => (now === threadId || !threadId ? '' : threadId));
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
  }, [detailsOpen, threadId]);
  const bot = bots.find((item) => item.id === botId);
  const tabBoss = helperBoss(bots, threadId);
  const tabBot = tabBoss ? bots.find((item) => item.id === tabBoss.botId) : bot;
  return (
    <div className="flex min-h-0 min-w-0 flex-1">
      <div className="relative flex min-h-0 min-w-0 flex-[999_1_400px] flex-col">
        <BotHeader
          onSettings={() => {
            setOpened(undefined);
            setSettingsOpen(true);
          }}
          details={
            openThread && {
              open: detailsOpen,
              toggle: () => setDetailsFor(detailsOpen ? '' : threadId),
            }
          }
        />
        {tabBot && <ThreadTabs bot={tabBot} openThreadId={tabBoss ? tabBoss.threadId : threadId} />}
        <Outlet context={{ openPR, openFile }} />
        {detailsOpen && (
          <ThreadPanel
            thread={openThread}
            repoName={repos.find((r) => r.id === openThread.repo_id)?.name}
            bots={Object.fromEntries(bots.map((b) => [b.id, { name: b.name, look: b }]))}
            onOpenPR={openPR}
          />
        )}
      </div>
      {shown && 'url' in shown && (
        <PRPanel
          key={shown.threadId + shown.url}
          threadId={shown.threadId}
          url={shown.url}
          onClose={() => setOpened(undefined)}
        />
      )}
      {shown && 'file' in shown && (
        <FilePanel
          key={shown.threadId + JSON.stringify(shown.file)}
          threadId={shown.threadId}
          file={shown.file}
          onClose={() => setOpened(undefined)}
        />
      )}
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
