import { api } from '@/lib/api-client';
import type { Run } from '@/modules/threads/api/messages';

export const getRun = (runId: string) => api<Run>(`/runs/${runId}`);
