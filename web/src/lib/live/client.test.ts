import { describe, expect, it, vi } from 'vitest';
import { LiveRenderer, type Frame, type Port } from './client';
import type { FromWorker, ToWorker } from './protocol';
import type { TemplateDoc } from '../template';

// A stand-in for the worker: it records what is posted, and answers when told.
function fakePort() {
  const posted: ToWorker[] = [];
  const port: Port = {
    postMessage: (m) => posted.push(m),
    onmessage: null,
    onerror: null,
    terminate: vi.fn(),
  };
  const send = (m: FromWorker) => port.onmessage?.({ data: m } as MessageEvent<FromWorker>);
  const renders = () => posted.filter((m): m is Extract<ToWorker, { type: 'render' }> => m.type === 'render');
  const answer = (seq: number, extra: Partial<Extract<FromWorker, { type: 'frame' }>> = {}) =>
    send({ type: 'frame', seq, width: 1, height: 1, pixels: new Uint8ClampedArray([0, 0, 0, 255]).buffer, description: '{"elements":[]}', problems: null, ...extra });
  return { port, posted, send, renders, answer };
}

const doc = (x: number, image?: string): TemplateDoc => ({
  id: '',
  name: '',
  units: 'mm',
  width: 20,
  height: 10,
  dpi: 300,
  elements: [
    { type: 'text', x, y: 1, width: 10, height: 5, font_size: 10, content_binding: 'A' },
    ...(image ? [{ type: 'static_image' as const, x: 0, y: 0, width: 5, height: 5, image_id: image }] : []),
  ],
});
const data = { data: { asset_name: 'Sample' }, placeholders: true };
const flush = () => new Promise((r) => setTimeout(r, 0));

describe('LiveRenderer', () => {
  it('draws only the newest of what was asked while it loaded', async () => {
    const w = fakePort();
    const live = new LiveRenderer(w.port, async () => new ArrayBuffer(0));
    const asked = [live.render(doc(1), data), live.render(doc(2), data), live.render(doc(3), data)];
    await flush();
    expect(w.renders()).toHaveLength(0);

    w.send({ type: 'ready' });
    expect(live.state).toBe('ready');
    expect(w.renders().map((r) => JSON.parse(r.template).elements[0].x)).toEqual([3]);
    w.answer(w.renders()[0]!.seq);
    const [a, b, c] = await Promise.all(asked);
    expect(a).toBeNull();
    expect(b).toBeNull();
    expect(c).toMatchObject({ width: 1, height: 1, problems: [], description: { elements: [] } });
  });

  it('never answers a frame after a newer one: one draws at a time, and the newest waits', async () => {
    const w = fakePort();
    const live = new LiveRenderer(w.port, async () => new ArrayBuffer(0));
    w.send({ type: 'ready' });
    const shown: number[] = [];
    const ask = (x: number) => live.render(doc(x), data).then((f) => f && shown.push(x));

    const first = ask(1);
    await flush();
    const second = ask(2);
    const third = ask(3);
    await flush();
    // The first draws; the second was replaced by the third while waiting.
    expect(w.renders().map((r) => JSON.parse(r.template).elements[0].x)).toEqual([1]);
    w.answer(w.renders()[0]!.seq);
    await flush();
    expect(w.renders().map((r) => JSON.parse(r.template).elements[0].x)).toEqual([1, 3]);
    w.answer(w.renders()[1]!.seq);
    await Promise.all([first, second, third]);
    expect(shown).toEqual([1, 3]);
  });

  it('drops a request overtaken while its images were fetched, and sends each image once', async () => {
    const w = fakePort();
    let release = () => {};
    const fetchImage = vi.fn(() => new Promise<ArrayBuffer>((r) => (release = () => r(new ArrayBuffer(4)))));
    const live = new LiveRenderer(w.port, fetchImage);
    w.send({ type: 'ready' });

    const withImage = live.render(doc(1, 'img-1'), data);
    await flush();
    const later = live.render(doc(2), data);
    await flush();
    w.answer(w.renders()[0]!.seq);
    expect(await later).not.toBeNull();
    release();
    expect(await withImage).toBeNull();

    // Drawn again with the image, it is not fetched or sent again.
    const again = live.render(doc(3, 'img-1'), data);
    await flush();
    w.answer(w.renders().at(-1)!.seq);
    expect(await again).not.toBeNull();
    expect(fetchImage).toHaveBeenCalledTimes(1);
    expect(w.posted.filter((m) => m.type === 'image')).toHaveLength(1);
  });

  it('passes on why a label cannot be drawn, as the server would say it', async () => {
    const w = fakePort();
    const live = new LiveRenderer(w.port, async () => new ArrayBuffer(0));
    w.send({ type: 'ready' });
    const asked = live.render(doc(1), data);
    await flush();
    w.answer(w.renders()[0]!.seq, { width: 0, height: 0, description: null, problems: '[{"path":"dpi","message":"must be at least 150"}]' });
    const frame = (await asked) as Frame;
    expect(frame.problems).toEqual([{ path: 'dpi', message: 'must be at least 150' }]);
    expect(frame.description).toBeNull();
  });

  it('fails over to the server when it cannot load, letting go of what was asked', async () => {
    const w = fakePort();
    const live = new LiveRenderer(w.port, async () => new ArrayBuffer(0));
    const states: string[] = [];
    live.onstate = (s) => states.push(s);
    const asked = live.render(doc(1), data);
    await flush();
    w.send({ type: 'failed', message: 'WebAssembly is not allowed' });
    expect(await asked).toBeNull();
    expect(live.state).toBe('failed');
    expect(live.failure).toBe('WebAssembly is not allowed');
    expect(states).toEqual(['failed']);
    expect(await live.render(doc(2), data)).toBeNull();
    expect(w.renders()).toHaveLength(0);
  });
});
