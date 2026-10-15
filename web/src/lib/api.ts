// The typed client for /api/v1. Every request goes through request(), which
// turns error responses into ApiError and reports 401s to the session.

/** One problem the server found in a request, located by path (templates). */
export interface ProblemDetail {
  path: string;
  message: string;
}

export class ApiError extends Error {
  constructor(
    readonly status: number,
    message: string,
    readonly details: ProblemDetail[] = [],
    /** The decoded error response, for errors that carry more than a list. */
    readonly body: unknown = undefined,
  ) {
    super(message);
    this.name = 'ApiError';
  }
}

type UnauthorizedHandler = () => void;
let onUnauthorized: UnauthorizedHandler = () => {};

/** Sets what happens when the server says the session is gone. */
export function setUnauthorizedHandler(handler: UnauthorizedHandler): void {
  onUnauthorized = handler;
}

export type Method = 'GET' | 'POST' | 'PUT' | 'PATCH' | 'DELETE';

async function send(method: Method, path: string, body?: unknown): Promise<Response> {
  const init: RequestInit = { method, credentials: 'same-origin', headers: { Accept: 'application/json' } };
  if (body instanceof FormData) {
    init.body = body;
  } else if (body !== undefined) {
    init.body = JSON.stringify(body);
    (init.headers as Record<string, string>)['Content-Type'] = 'application/json';
  }
  const res = await fetch('/api/v1' + path, init);
  if (!res.ok) {
    throw await errorFrom(res, path);
  }
  return res;
}

async function errorFrom(res: Response, path: string): Promise<ApiError> {
  let message = `${res.status} ${res.statusText}`.trim();
  let details: ProblemDetail[] = [];
  let body: unknown;
  try {
    const data = await res.json();
    body = data;
    if (typeof data?.error === 'string') message = data.error;
    if (Array.isArray(data?.details)) details = data.details;
  } catch {
    // Not JSON; keep the status line.
  }
  // A failed sign-in is not a lost session.
  if (res.status === 401 && path !== '/auth/login') onUnauthorized();
  return new ApiError(res.status, message, details, body);
}

/** Sends a JSON request and returns the decoded JSON response. */
export async function request<T>(method: Method, path: string, body?: unknown): Promise<T> {
  const res = await send(method, path, body);
  if (res.status === 204) return undefined as T;
  return (await res.json()) as T;
}

/** Sends a request whose response is a file, such as a label preview. */
export async function requestBlob(method: Method, path: string, body?: unknown): Promise<Blob> {
  const res = await send(method, path, body);
  return res.blob();
}

/**
 * Sends a request whose response is a file to save, and returns it with the
 * filename the server gave it, or fallback when it gave none.
 */
export async function requestDownload(method: Method, path: string, body: unknown, fallback: string): Promise<{ blob: Blob; filename: string }> {
  const res = await send(method, path, body);
  const named = /filename="([^"]+)"/.exec(res.headers.get('Content-Disposition') ?? '');
  return { blob: await res.blob(), filename: named?.[1] ?? fallback };
}

/** Encodes a value for use in a URL path segment. */
export function seg(value: string): string {
  return encodeURIComponent(value);
}
