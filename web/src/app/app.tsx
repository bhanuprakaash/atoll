import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { BrowserRouter, Route, Routes, useNavigate, useParams } from 'react-router';
import { BotPage, LatestThread } from '@/app/bot-page';
import { Shell } from '@/app/shell';
import { NewBotForm } from '@/modules/bots/components/new-bot-form';
import { ThreadView } from '@/modules/threads/components/thread-view';

const queryClient = new QueryClient();

function NewBotPage() {
  const navigate = useNavigate();
  return <NewBotForm onCreated={(bot) => navigate(`/bots/${bot.id}`)} />;
}

function ThreadPage() {
  const { threadId = '' } = useParams();
  return <ThreadView key={threadId} threadId={threadId} />;
}

export function App() {
  return (
    <QueryClientProvider client={queryClient}>
      <BrowserRouter>
        <Routes>
          <Route element={<Shell />}>
            <Route index element={null} />
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
