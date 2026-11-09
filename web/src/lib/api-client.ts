export class ApiError extends Error {
  constructor(
    readonly status: number,
    message: string,
  ) {
    super(message);
  }
}

async function apiError(res: Response) {
  const text = await res.text();
  try {
    const { error } = JSON.parse(text) as { error?: unknown };
    if (typeof error === 'string' && error) return new ApiError(res.status, error);
  } catch {
    // not JSON: show the text as sent
  }
  return new ApiError(res.status, text || res.statusText);
}

export async function api<T>(path: string, init: { method?: string; body?: unknown } = {}) {
  const res = await fetch(`/api${path}`, {
    method: init.method,
    headers: init.body === undefined ? undefined : { 'Content-Type': 'application/json' },
    body: init.body === undefined ? undefined : JSON.stringify(init.body),
  });
  if (!res.ok) throw await apiError(res);
  return (res.status === 204 ? undefined : await res.json()) as T;
}

export async function apiBlob(path: string) {
  const res = await fetch(`/api${path}`);
  if (!res.ok) throw await apiError(res);
  return res.blob();
}
