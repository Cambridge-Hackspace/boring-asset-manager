// The live renderer's worker: loads the label renderer (cmd/bamlive, built
// by tools/web/build.sh) and draws labels off the page's thread, so the
// editor never stutters while one is drawn.

import './generated/wasm_exec.js';
import wasmURL from './generated/bamlive.wasm?url';
import type { FromWorker, ToWorker } from './protocol';

interface Bamlive {
  addImage(id: string, bytes: Uint8Array): string | null;
  render(
    template: string,
    data: string,
    placeholders: boolean,
  ): { width: number; height: number; pixels: Uint8ClampedArray; description: string | null; problems: string | null };
}

interface GoRuntime {
  importObject: WebAssembly.Imports;
  run(instance: WebAssembly.Instance): Promise<void>;
}

const scope = self as unknown as {
  postMessage(message: FromWorker, transfer?: Transferable[]): void;
  onmessage: ((event: MessageEvent<ToWorker>) => void) | null;
};

async function start(): Promise<Bamlive> {
  const go = new (globalThis as unknown as { Go: new () => GoRuntime }).Go();
  // Compiled while it downloads, where the server says it is WebAssembly.
  const { instance } = await WebAssembly.instantiateStreaming(fetch(wasmURL), go.importObject).catch(async () => {
    const bytes = await (await fetch(wasmURL)).arrayBuffer();
    return WebAssembly.instantiate(bytes, go.importObject);
  });
  void go.run(instance);
  const live = (globalThis as unknown as { bamlive?: Bamlive }).bamlive;
  if (!live) throw new Error('the renderer did not start');
  return live;
}

const ready = start();
ready.then(
  () => scope.postMessage({ type: 'ready' }),
  (err: unknown) => scope.postMessage({ type: 'failed', message: err instanceof Error ? err.message : String(err) }),
);

scope.onmessage = async (event) => {
  const msg = event.data;
  const live = await ready;
  if (msg.type === 'image') {
    const failure = live.addImage(msg.id, new Uint8Array(msg.bytes));
    if (failure) scope.postMessage({ type: 'image-failed', id: msg.id, message: failure });
    return;
  }
  const out = live.render(msg.template, msg.data, msg.placeholders);
  const pixels = out.pixels.buffer as ArrayBuffer;
  scope.postMessage(
    { type: 'frame', seq: msg.seq, width: out.width, height: out.height, pixels, description: out.description, problems: out.problems },
    [pixels],
  );
};
