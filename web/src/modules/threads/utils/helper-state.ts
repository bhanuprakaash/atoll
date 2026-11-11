import type { Thread } from '@/modules/threads/api/threads';

export type HelperState = 'working' | 'needs_you' | 'done' | 'failed' | 'stopped';

export const isLiveHelper = (helper: Thread) =>
  !helper.closed && (helper.run_status === 'running' || helper.run_status === 'waiting');

export const helperState = (helper: Thread): HelperState => {
  if (helper.approval) return 'needs_you';
  if (isLiveHelper(helper)) return 'working';
  return helper.run_status === 'failed' || helper.run_status === 'stopped'
    ? helper.run_status
    : 'done';
};

export const helperLabel = (name: string, state: HelperState) =>
  `${name}: ${state.replace('_', ' ')}`;

// ciWord is the word for an open PR's checks as the watcher last read them; none before a read.
export function ciWord(helper: Thread) {
  const ci = helper.mr_ci;
  if (helper.mr_state !== 'opened' || !ci) return '';
  if (ci.failed > 0) return 'failed';
  if (ci.pending > 0) return 'running';
  return ci.passed > 0 ? 'passed' : '';
}

// elapsed is "42s", "3m" or "1h 5m".
export function elapsed(since: string, now: number) {
  const seconds = Math.max(0, Math.floor((now - Date.parse(since)) / 1000));
  if (seconds < 60) return `${seconds}s`;
  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) return `${minutes}m`;
  return minutes % 60 ? `${Math.floor(minutes / 60)}h ${minutes % 60}m` : `${minutes / 60}h`;
}
