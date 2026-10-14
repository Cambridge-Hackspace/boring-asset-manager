import { afterEach, describe, expect, it, vi } from 'vitest';
import { ApiError, request, setUnauthorizedHandler } from './api';

function respond(status: number, body: unknown, contentType = 'application/json') {
  const text = typeof body === 'string' ? body : JSON.stringify(body);
  return vi.fn().mockResolvedValue(new Response(status === 204 ? null : text, { status, headers: { 'Content-Type': contentType } }));
}

afterEach(() => {
  vi.unstubAllGlobals();
  setUnauthorizedHandler(() => {});
});

describe('request', () => {
  it('sends JSON and decodes the answer', async () => {
    const fetchMock = respond(200, { ok: true });
    vi.stubGlobal('fetch', fetchMock);
    expect(await request('POST', '/things', { a: 1 })).toEqual({ ok: true });
    const [url, init] = fetchMock.mock.calls[0]!;
    expect(url).toBe('/api/v1/things');
    expect(init.method).toBe('POST');
    expect(init.body).toBe('{"a":1}');
    expect(init.headers['Content-Type']).toBe('application/json');
    expect(init.credentials).toBe('same-origin');
  });

  it('turns error responses into ApiError with details', async () => {
    vi.stubGlobal('fetch', respond(400, { error: 'Invalid template', details: [{ path: 'elements[0].x', message: 'too big' }] }));
    const err = (await request('PUT', '/templates/x', {}).catch((e) => e)) as ApiError;
    expect(err).toBeInstanceOf(ApiError);
    expect(err.status).toBe(400);
    expect(err.message).toBe('Invalid template');
    expect(err.details).toEqual([{ path: 'elements[0].x', message: 'too big' }]);
  });

  it('keeps the status line when the error is not JSON', async () => {
    vi.stubGlobal('fetch', respond(502, 'upstream down', 'text/plain'));
    const err = (await request('GET', '/x').catch((e) => e)) as ApiError;
    expect(err.status).toBe(502);
    expect(err.message).toMatch(/^502/);
  });

  it('reports lost sessions, but not failed sign-ins', async () => {
    const lost = vi.fn();
    setUnauthorizedHandler(lost);
    vi.stubGlobal('fetch', respond(401, { error: 'Authentication required' }));
    await request('GET', '/assets').catch(() => {});
    expect(lost).toHaveBeenCalledTimes(1);
    await request('POST', '/auth/login', {}).catch(() => {});
    expect(lost).toHaveBeenCalledTimes(1);
  });
});
