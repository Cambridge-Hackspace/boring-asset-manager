import { afterEach, describe, expect, it, vi } from 'vitest';
import { LimitError, contextFor, isForm, readLines, streamTurn, type TurnLine } from './assistant.svelte';
import { routes } from '../routes';

afterEach(() => vi.unstubAllGlobals());

/** A body that arrives in the chunks given, as a stream does. */
function chunked(...parts: string[]): ReadableStream<Uint8Array> {
  const enc = new TextEncoder();
  return new ReadableStream({
    start(c) {
      for (const p of parts) c.enqueue(enc.encode(p));
      c.close();
    },
  });
}

describe('readLines', () => {
  it('reads a JSON object a line, across chunks, skipping what is not JSON', async () => {
    const got: TurnLine[] = [];
    await readLines(chunked('{"type":"changed","to', 'ol":"create_asset"}\n\nnot json\n{"type":"error","error":"x"}'), async () => '', (l) => got.push(l));
    expect(got).toEqual([
      { type: 'changed', tool: 'create_asset' },
      { type: 'error', error: 'x' },
    ]);
  });

  it('reads the whole text where there is no stream', async () => {
    const got: TurnLine[] = [];
    await readLines(null, async () => '{"type":"progress","progress":{"1":"A saw"}}\n', (l) => got.push(l));
    expect(got).toEqual([{ type: 'progress', progress: { '1': 'A saw' } }]);
  });
});

describe('streamTurn', () => {
  it('posts the message and reads the turn', async () => {
    const fetchMock = vi.fn(async () => new Response('{"type":"done","conversation":{"id":"c1"}}\n', { status: 200 }));
    vi.stubGlobal('fetch', fetchMock);
    const got: TurnLine[] = [];
    await streamTurn('/assistant/conversations/c1/messages', { text: 'hi' }, (l) => got.push(l));
    expect(got[0]?.type).toBe('done');
    const [url, init] = fetchMock.mock.calls[0] as unknown as [string, RequestInit];
    expect(url).toBe('/api/v1/assistant/conversations/c1/messages');
    expect(init.method).toBe('POST');
    expect(JSON.parse(init.body as string)).toEqual({ text: 'hi' });
  });

  it('says when a limit resets', async () => {
    vi.stubGlobal('fetch', async () => new Response(JSON.stringify({ error: 'Limit reached', resets_at: '2026-11-10T00:00:00Z' }), { status: 429 }));
    const err = await streamTurn('/x', {}, () => {}).catch((e) => e);
    expect(err).toBeInstanceOf(LimitError);
    expect((err as LimitError).resetsAt).toBe('2026-11-10T00:00:00Z');
    expect((err as LimitError).message).toBe('Limit reached');
  });
});

describe('contextFor', () => {
  it('names what a page shows, by its route', () => {
    expect(contextFor(routes, '/assets/a1')).toEqual({ kind: 'asset', id: 'a1', label: 'Asset', path: '/assets/a1' });
    expect(contextFor(routes, '/categories/c1')).toMatchObject({ kind: 'category', id: 'c1' });
    expect(contextFor(routes, '/assets', '?q=saw')).toEqual({ kind: 'list', label: 'Assets', path: '/assets?q=saw' });
    expect(contextFor(routes, '/f/leave')).toMatchObject({ kind: 'flow', id: 'leave' });
    expect(contextFor(routes, '/assets/new')).toMatchObject({ kind: 'page', label: 'New asset' });
    expect(contextFor(routes, '/')).toBeNull();
    expect(contextFor(routes, '/account')).toBeNull();
    expect(contextFor(routes, '/no-such-page')).toBeNull();
  });

  it('tells forms apart', () => {
    expect(isForm('/assets/a1/edit')).toBe(true);
    expect(isForm('/assets/new')).toBe(true);
    expect(isForm('/assets/a1')).toBe(false);
  });
});
