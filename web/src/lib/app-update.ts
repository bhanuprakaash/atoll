import { useMutation, useQuery } from '@tanstack/react-query';

export const inDesktopApp = '__TAURI_INTERNALS__' in window;

export type AppUpdate = { current: string; available: string | null };

async function invoke<T>(command: string) {
  const shell = (
    window as unknown as {
      __TAURI_INTERNALS__: { invoke: (command: string) => Promise<T> };
    }
  ).__TAURI_INTERNALS__;
  try {
    return await shell.invoke(command);
  } catch (err) {
    throw new Error(String(err));
  }
}

export function useAppUpdate() {
  return useQuery({
    queryKey: ['app-update'],
    queryFn: () => invoke<AppUpdate>('check_update'),
    enabled: inDesktopApp,
    retry: false,
    staleTime: Infinity,
    refetchOnWindowFocus: false,
  });
}

export function useInstallUpdate() {
  return useMutation({ mutationFn: () => invoke<void>('install_update') });
}
