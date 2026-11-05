import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { BrowserRouter, Route, Routes, useNavigate, useParams } from 'react-router';
import { BotPage, LatestThread } from '@/app/bot-page';
import { SettingsPage } from '@/app/settings-page';
import { Shell } from '@/app/shell';
import { useBots } from '@/modules/bots/api/bots';
import { NewBotForm } from '@/modules/bots/components/new-bot-form';
import { useConnections } from '@/modules/connections/api/connections';
import { ConnectionsSettings } from '@/modules/connections/components/connections-settings';
import { SavedMemory } from '@/modules/memory/components/saved-memory';
import { PlatformChanges } from '@/modules/platform/components/platform-changes';
import { useRepos } from '@/modules/repos/api/repos';
import { ReposSettings } from '@/modules/repos/components/repos-settings';
import { useSkills } from '@/modules/skills/api/skills';
import { SkillsSettings } from '@/modules/skills/components/skills-settings';
import { useThread } from '@/modules/threads/api/threads';
import { ThreadView } from '@/modules/threads/components/thread-view';

const queryClient = new QueryClient();

function NewBotPage() {
  const navigate = useNavigate();
  const { data: repos = [] } = useRepos();
  const { data: connections = [] } = useConnections();
  const { data: skills = [] } = useSkills();
  return (
    <NewBotForm
      repos={repos}
      skills={skills}
      connections={connections.filter((c) => c.status === 'connected')}
      onCreated={(bot) => navigate(`/bots/${bot.id}`)}
    />
  );
}

function ThreadPage() {
  const { threadId = '' } = useParams();
  const { data: thread } = useThread(threadId);
  const { data: repos = [] } = useRepos();
  const { data: bots = [] } = useBots();
  const repoName = repos.find((r) => r.id === thread?.repo_id)?.name;
  const botNames = Object.fromEntries(bots.map((b) => [b.id, b.name]));
  return (
    <ThreadView
      key={threadId}
      threadId={threadId}
      repoName={repoName}
      memory={
        <>
          <SavedMemory threadId={threadId} botNames={botNames} />
          <PlatformChanges threadId={threadId} botNames={botNames} />
        </>
      }
    />
  );
}

export function App() {
  return (
    <QueryClientProvider client={queryClient}>
      <BrowserRouter>
        <Routes>
          <Route element={<Shell />}>
            <Route index element={null} />
            <Route path="settings" element={<SettingsPage />}>
              <Route path="repos" element={<ReposSettings />} />
              <Route path="connections" element={<ConnectionsSettings />} />
              <Route path="skills" element={<SkillsSettings />} />
            </Route>
            <Route path="bots/new" element={<NewBotPage />} />
            <Route path="bots/:botId" element={<BotPage />}>
              <Route index element={<LatestThread />} />
              <Route path="threads/:threadId" element={<ThreadPage />} />
            </Route>
          </Route>
        </Routes>
      </BrowserRouter>
    </QueryClientProvider>
  );
}
