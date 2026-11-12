import { useServerLost } from '@/lib/api-client';

// The one notice for a server that is not answering, on every page, until it answers again.
export function ReconnectingNotice() {
  if (!useServerLost()) return null;
  return (
    <p
      role="status"
      className="fixed top-3 left-1/2 z-50 -translate-x-1/2 rounded-full border border-outline bg-popover px-4 py-1.5 text-[13px] text-muted shadow-sm"
    >
      Reconnecting…
    </p>
  );
}
