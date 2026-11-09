export class ApiError extends Error {
  constructor(
    readonly status: number,
    message: string,
  ) {
    super(message);
  }
}

function errorText(text: string) {
  try {
    const { error } = JSON.parse(text) as { error?: unknown };
    return typeof error === 'string' && error ? error : text;
  } catch {
    return text;
  }
}

export async function api<T>(path: string, init: { method?: string; body?: unknown } = {}) {
  const res = await fetch(`/api${path}`, {
    method: init.method,
    headers: init.body === undefined ? undefined : { 'Content-Type': 'application/json' },
    body: init.body === undefined ? undefined : JSON.stringify(init.body),
  });
  if (!res.ok) throw new ApiError(res.status, errorText(await res.text()) || res.statusText);
  return (res.status === 204 ? undefined : await res.json()) as T;
}

export async function apiBlob(path: string) {
  const res = await fetch(`/api${path}`);
  if (!res.ok) throw new ApiError(res.status, errorText(await res.text()) || res.statusText);
  return res.blob();
}
