// A fake fetch for component tests: routes requests to handlers by method and
// path, and records what was sent.

import { vi } from 'vitest';

export interface SentRequest {
  method: string;
  path: string;
  body: unknown;
}

type Handler = (body: unknown, path: string) => { status?: number; body?: unknown } | unknown;

export function fakeApi(routes: Record<string, Handler>) {
  const sent: SentRequest[] = [];
  const fetchMock = vi.fn(async (url: string, init: RequestInit = {}) => {
    const method = init.method ?? 'GET';
    const path = url.replace(/^\/api\/v1/, '').split('?')[0]!;
    const body = typeof init.body === 'string' ? JSON.parse(init.body) : init.body;
    sent.push({ method, path, body });
    const handler = routes[`${method} ${path}`];
    if (!handler) {
      return new Response(JSON.stringify({ error: `no fake for ${method} ${path}` }), { status: 404 });
    }
    const result = handler(body, path) as { status?: number; body?: unknown } | undefined;
    const isEnvelope = result !== null && typeof result === 'object' && ('status' in result || 'body' in result);
    const status = isEnvelope ? (result.status ?? 200) : 200;
    const payload = isEnvelope ? result.body : result;
    return new Response(payload === undefined ? null : JSON.stringify(payload), {
      status,
      headers: { 'Content-Type': 'application/json' },
    });
  });
  vi.stubGlobal('fetch', fetchMock);
  return { sent, fetchMock };
}
