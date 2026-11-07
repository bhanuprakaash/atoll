import type { Thread } from '@/modules/threads/api/threads';

export type HelperState = 'working' | 'needs_you';

export const helperState = (helper: Thread): HelperState =>
  helper.approval ? 'needs_you' : 'working';

export const helperLabel = (name: string, state: HelperState) =>
  `${name}: ${state.replace('_', ' ')}`;

export const isLiveHelper = (helper: Thread) =>
  !helper.closed && (helper.run_status === 'running' || helper.run_status === 'waiting');
