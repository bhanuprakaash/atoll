import type { Thread } from '@/modules/threads/api/threads';

export type HelperState = 'working' | 'needs_you' | 'done' | 'failed' | 'stopped';

export function helperState(helper: Thread): HelperState {
  if (helper.approval) return 'needs_you';
  if (helper.run_status === 'done') return 'done';
  if (helper.run_status === 'failed') return 'failed';
  if (helper.run_status === 'stopped') return 'stopped';
  return 'working';
}

export const helperLabel = (name: string, state: HelperState) =>
  `${name}: ${state.replace('_', ' ')}`;

export const isLiveHelper = (helper: Thread) =>
  !helper.closed && (helper.run_status === 'running' || helper.run_status === 'waiting');
