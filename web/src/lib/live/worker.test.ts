import { afterEach, describe, expect, it, vi } from 'vitest';
import type { FromWorker, ToWorker } from './protocol';

// The worker itself, with the renderer it loads stood in for: one that
// traps as WebAssembly does when the Go inside it panics.
vi.mock('./generated/wasm_exec.js', () => ({}));
vi.mock('./generated/bamlive.wasm?url', () => ({ default: 'bamlive.wasm' }));

const g = globalThis as unknown as Record<string, unknown>;

afterEach(() => {
  vi.unstubAllGlobals();
  vi.resetModules();
  delete g.bamlive;
  delete g.Go;
});

describe('the live renderer’s worker', () => {
  it('says it has stopped when the renderer traps, rather than leaving the frame unanswered', async () => {
    const posted: FromWorker[] = [];
    let trap = false;
    g.bamlive = {
      addImage: () => null,
      font: () => null,
      render: () => {
        if (trap) throw new WebAssembly.RuntimeError('unreachable');
        return { width: 1, height: 1, pixels: new Uint8ClampedArray(4), description: null, problems: null };
      },
    };
    g.Go = class {
      importObject = {};
      run() {
        return new Promise(() => {});
      }
    };
    vi.stubGlobal('fetch', async () => new Response(new ArrayBuffer(8)));
    vi.spyOn(WebAssembly, 'instantiateStreaming').mockResolvedValue({ instance: {}, module: {} } as WebAssembly.WebAssemblyInstantiatedSource);
    const scope = self as unknown as { postMessage: (m: FromWorker) => void; onmessage: ((e: { data: ToWorker }) => Promise<void>) | null };
    vi.spyOn(scope, 'postMessage').mockImplementation((m) => void posted.push(m));

    await import('./worker');
    await vi.waitFor(() => expect(posted.map((m) => m.type)).toEqual(['ready']));
    const render = (seq: number) => scope.onmessage!({ data: { type: 'render', seq, template: '{}', data: '{}', placeholders: true, hidden: [] } });

    await render(1);
    expect(posted.map((m) => m.type)).toEqual(['ready', 'frame']);
    trap = true;
    await render(2);
    expect(posted.at(-1)).toEqual({ type: 'failed', message: 'unreachable' });
  });
});
