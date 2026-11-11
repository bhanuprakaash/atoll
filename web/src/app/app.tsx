import { useState } from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import {
  createBrowserRouter,
  Navigate,
  Route,
  RouterProvider,
  Routes,
  useNavigate,
  useParams,
  useSearchParams,
} from 'react-router';
import { BotPage, LatestThread } from '@/app/bot-page';
import {
  ConnectionsPage,
  McpServersPage,
  MemoryPage,
  ProvidersPage,
  SettingsPage,
  SkillsPage,
} from '@/app/settings-page';
import { Shell } from '@/app/shell';
import { ReconnectingNotice } from '@/components/reconnecting-notice';
import { useBots, type Bot } from '@/modules/bots/api/bots';
import { useSetBotGroup } from '@/modules/bots/api/groups';
import { NewBotForm } from '@/modules/bots/components/new-bot-form';
import { useConnections, type Connection } from '@/modules/connections/api/connections';
import { ReconnectButton } from '@/modules/connections/components/reconnect-button';
import { FirstRun } from '@/modules/onboarding/components/first-run';
import {
  FirstRunProviders,
  SignInExpired,
} from '@/modules/providers/components/providers-settings';
import { useSaveSettings, useSettings } from '@/modules/settings/api/settings';
import { GeneralSettings } from '@/modules/settings/components/general-settings';
import { SavedMemory } from '@/modules/memory/components/saved-memory';
import { PlatformChanges } from '@/modules/platform/components/platform-changes';
import { useProviders } from '@/modules/providers/api/providers';
import { ModelSelect } from '@/modules/providers/components/model-select';
import { useRepos } from '@/modules/repos/api/repos';
import { ReposSettings } from '@/modules/repos/components/repos-settings';
import { useSkills } from '@/modules/skills/api/skills';
import { useCreateThread, useThread } from '@/modules/threads/api/threads';
import { DraftThread, ThreadView } from '@/modules/threads/components/thread-view';

const queryClient = new QueryClient();

const allowedRepos = <R extends { id: string }>(bot: Bot | undefined, repos: R[]) =>
  bot?.repo_access === 'all' ? repos : repos.filter((r) => bot?.repo_access.includes(r.id));

const withConnectionNames = <B extends { connections: string[] }>(
  bot: B | undefined,
  connections: Connection[],
) =>
  bot && {
    ...bot,
    connections: (bot.connections ?? []).flatMap((id) => {
      const kind = connections.find((c) => c.id === id)?.kind;
      return kind ? [kind === 'github' ? 'GitHub' : 'GitLab'] : [];
    }),
  };

function NewBotPage() {
  const navigate = useNavigate();
  const { data: repos = [] } = useRepos();
  const { data: connections = [] } = useConnections();
  const { data: skills = [] } = useSkills();
  const { data: providers = [] } = useProviders();
  const { data: bots = [] } = useBots();
  const [params] = useSearchParams();
  const setGroup = useSetBotGroup();
  return (
    <NewBotForm
      chiefId={bots.find((b) => b.can_manage_platform)?.id}
      modelControl={(provider, model, onChange) => (
        <ModelSelect providers={providers} provider={provider} model={model} onChange={onChange} />
      )}
      repos={repos}
      skills={skills}
      connections={connections}
      reconnect={(connection) => <ReconnectButton connection={connection} />}
      onCreated={(bot) => {
        const groupId = params.get('group');
        if (groupId) setGroup.mutate({ botId: bot.id, groupId });
        navigate(`/bots/${bot.id}`);
      }}
    />
  );
}

function ThreadPage() {
  const { threadId = '', botId = '' } = useParams();
  const { data: thread } = useThread(threadId);
  const { data: repos = [] } = useRepos();
  const { data: bots = [] } = useBots();
  const { data: connections = [] } = useConnections();
  const { data: skills = [] } = useSkills();
  const repoName = repos.find((r) => r.id === thread?.repo_id)?.name;
  const botNames = Object.fromEntries(bots.map((b) => [b.id, b.name]));
  const botLooks = Object.fromEntries(bots.map((b) => [b.id, b.look]));
  const botInfo = Object.fromEntries(bots.map((b) => [b.id, { name: b.name, look: b.look }]));
  const bot = bots.find((b) => b.id === botId);
  const allowed = allowedRepos(bot, repos);
  const lostRepo =
    thread?.repo_id && bot && !allowed.some((r) => r.id === thread.repo_id) ? repoName : undefined;
  return (
    <ThreadView
      key={threadId}
      threadId={threadId}
      botId={botId}
      botName={bots.find((b) => b.id === botId)?.name ?? ''}
      look={bots.find((b) => b.id === botId)?.look}
      bots={botInfo}
      repos={allowed}
      bot={withConnectionNames(bot, connections)}
      repoName={repoName}
      lostRepo={lostRepo}
      signedOut={(provider) => <SignInExpired provider={provider} />}
      afterRun={(runId) => (
        <>
          <PlatformChanges
            threadId={threadId}
            runId={runId}
            botId={botId}
            botNames={botNames}
            looks={botLooks}
            names={{ bots, skills, repos, connections }}
          />
          <SavedMemory runId={runId} botNames={botNames} />
        </>
      )}
    />
  );
}

function DraftThreadPage() {
  const { botId = '' } = useParams();
  const { data: repos = [] } = useRepos();
  const { data: bots = [] } = useBots();
  const { data: connections = [] } = useConnections();
  const bot = bots.find((b) => b.id === botId);
  return (
    <DraftThread
      key={botId}
      botId={botId}
      botName={bot?.name ?? ''}
      look={bot?.look}
      bot={withConnectionNames(bot, connections)}
      repos={allowedRepos(bot, repos)}
    />
  );
}

function StartPage() {
  const { data: bots } = useBots();
  if (!bots) return null;
  const latest = [...bots]
    .filter((bot) => bot.last_activity)
    .sort((a, b) => b.last_activity.localeCompare(a.last_activity))[0];
  return <Navigate to={`/bots/${latest?.id ?? 'chief'}`} replace />;
}

function AppRoutes() {
  const { data: settings, isError } = useSettings();
  const [onboarding, setOnboarding] = useState<boolean | null>(null);
  const save = useSaveSettings();
  const create = useCreateThread('chief');
  const navigate = useNavigate();
  if (!settings && !isError) return null;
  const show = onboarding ?? (settings ? !settings.user_name : false);
  if (show && settings)
    return (
      <FirstRun
        initialName={settings.user_name}
        providers={<FirstRunProviders />}
        onSave={async (name) => {
          setOnboarding(true);
          await save.mutateAsync({ ...settings, user_name: name });
        }}
        onOpen={(text) => {
          if (!text) {
            setOnboarding(false);
            navigate('/bots/chief');
            return;
          }
          void create.mutateAsync({ text }).then((thread) => {
            setOnboarding(false);
            navigate(`/bots/chief/threads/${thread.id}`);
          });
        }}
      />
    );
  return (
    <Routes>
      <Route element={<Shell />}>
        <Route index element={<StartPage />} />
        <Route path="settings" element={<SettingsPage />}>
          <Route index element={<Navigate to="general" replace />} />
          <Route path="general" element={<GeneralSettings />} />
          <Route path="providers" element={<ProvidersPage />} />
          <Route path="memory" element={<MemoryPage />} />
          <Route path="repos" element={<ReposSettings />} />
          <Route path="connections" element={<ConnectionsPage />} />
          <Route path="skills" element={<SkillsPage />} />
          <Route path="mcp-servers" element={<McpServersPage />} />
        </Route>
        <Route path="bots/new" element={<NewBotPage />} />
        <Route path="bots/:botId" element={<BotPage />}>
          <Route index element={<LatestThread />} />
          <Route path="new" element={<DraftThreadPage />} />
          <Route path="threads/:threadId" element={<ThreadPage />} />
        </Route>
      </Route>
    </Routes>
  );
}

const router = createBrowserRouter([{ path: '*', element: <AppRoutes /> }]);

export function App() {
  return (
    <QueryClientProvider client={queryClient}>
      <RouterProvider router={router} useTransitions={false} />
      <ReconnectingNotice />
    </QueryClientProvider>
  );
}
