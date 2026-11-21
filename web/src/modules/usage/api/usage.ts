import { keepPreviousData, useQuery } from '@tanstack/react-query';
import { api } from '@/lib/api-client';

export type UsageRange = '24h' | '7d' | '30d' | 'all';

export type Usage = {
  total: { cost_usd: number | null; tokens: number; runs: number };
  days: { day: string; cost_usd: number | null; tokens: number }[];
  bots: { id: string; name: string; runs: number; tokens: number; cost_usd: number | null }[];
  models: { model: string; tokens: number; cost_usd: number | null }[];
};

export type LimitWindow = { used_percent: number; resets_at: string; window_minutes: number };

// A provider's entry has either its windows or an error.
export type ProviderLimits = {
  provider: 'claude' | 'codex';
  session?: LimitWindow | null;
  weekly?: LimitWindow | null;
  error?: string;
};

export function useLimits() {
  return useQuery({
    queryKey: ['usage-limits'],
    queryFn: () => api<ProviderLimits[]>('/usage/limits'),
    refetchOnWindowFocus: false,
  });
}

export function useUsage(range: UsageRange) {
  const offset = -new Date().getTimezoneOffset();
  return useQuery({
    queryKey: ['usage', range, offset],
    queryFn: () => api<Usage>(`/usage?range=${range}&tz_offset=${offset}`),
    placeholderData: keepPreviousData,
  });
}
