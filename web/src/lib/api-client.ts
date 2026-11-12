import { useSyncExternalStore } from 'react';

const GENERIC = 'Something went wrong on our side.';

// code is the server's stable id for the cause; offline (status 0) is the server not answering.
export class ApiError extends Error {
  constructor(
    readonly status: number,
    readonly code: string,
    message: string,
    readonly method: string,
    readonly path: string,
  ) {
    super(message);
  }
}

let lost = false;
const listeners = new Set<() => void>();

export function setLost(next: boolean) {
  if (lost === next) return;
  lost = next;
  listeners.forEach((listener) => listener());
}

// useServerLost is true from a request the server never answered until the next one it does.
export function useServerLost() {
  return useSyncExternalStore(
    (listener) => {
      listeners.add(listener);
      return () => listeners.delete(listener);
    },
    () => lost,
  );
}

function failure(status: number, text: string, method: string, path: string) {
  try {
    const { error, code } = JSON.parse(text) as { error?: unknown; code?: unknown };
    if (typeof error === 'string' && error && typeof code === 'string' && code)
      return new ApiError(status, code, error, method, path);
  } catch {
    // not the server's reply: a proxy's page or an empty body
  }
  return new ApiError(status, 'internal', GENERIC, method, path);
}

async function request(path: string, init: { method?: string; body?: unknown } = {}) {
  const method = init.method ?? 'GET';
  let res: Response;
  try {
    res = await fetch(`/api${path}`, {
      method: init.method,
      headers: init.body === undefined ? undefined : { 'Content-Type': 'application/json' },
      body: init.body === undefined ? undefined : JSON.stringify(init.body),
    });
  } catch {
    setLost(true);
    throw new ApiError(0, 'offline', 'Reef is not answering.', method, `/api${path}`);
  }
  setLost(false);
  if (!res.ok) throw failure(res.status, await res.text(), method, `/api${path}`);
  return res;
}

export async function api<T>(path: string, init: { method?: string; body?: unknown } = {}) {
  const res = await request(path, init);
  return (res.status === 204 ? undefined : await res.json()) as T;
}

export async function apiBlob(path: string) {
  return (await request(path)).blob();
}
