// The editor's live renderer: draws a template exactly as it will print, at
// every frame of a drag or a keystroke, with the server's own renderer
// compiled to WebAssembly and run in a worker (worker.ts). The server stays
// the authority on what prints; the editor falls back to its previews when
// the renderer cannot load.

import type { ProblemDetail } from '../api';
import type { TemplateDoc } from '../template';
import type { FromWorker, PreviewData, ToWorker } from './protocol';

export type { PreviewData } from './protocol';

/** One drawn label: its dots, as a thermal printer prints them, RGBA. */
export interface Frame {
  width: number;
  height: number;
  pixels: Uint8ClampedArray;
  /** As the server's JSON preview describes the label; null when it cannot be drawn. */
  description: { elements?: { index: number; code?: unknown }[]; warnings?: string[] } | null;
  /** Why it cannot be drawn, as the server's preview would say. */
  problems: ProblemDetail[];
}

export type LiveState = 'loading' | 'ready' | 'failed';

/** What the renderer talks to: a Worker running worker.ts, or a test's stand-in. */
export interface Port {
  postMessage(message: ToWorker, transfer?: Transferable[]): void;
  onmessage: ((event: MessageEvent<FromWorker>) => void) | null;
  onerror: ((event: ErrorEvent) => void) | null;
  terminate(): void;
}

interface Request {
  seq: number;
  template: string;
  data: string;
  placeholders: boolean;
  hidden: number[];
  resolve: (frame: Frame | null) => void;
}

export class LiveRenderer {
  state: LiveState = 'loading';
  /** Why it failed to load. */
  failure = '';
  /** Called when the state changes. */
  onstate: ((state: LiveState) => void) | null = null;

  private asked = 0;
  private queued = 0;
  private drawing: Request | null = null;
  private waiting: Request | null = null;
  private images = new Map<string, Promise<void>>();

  constructor(
    private port: Port,
    /** Fetches an uploaded image's bytes, as the server stores it. */
    private fetchImage: (id: string) => Promise<ArrayBuffer>,
  ) {
    port.onmessage = (event) => this.receive(event.data);
    port.onerror = (event) => this.fail(event.message || 'the renderer stopped');
  }

  /**
   * Draws a template with data. A request still waiting when a newer one
   * comes is dropped (resolving null); frames are never answered out of
   * order, so a frame is always newer than any shown before it. Null too
   * once the renderer has failed: the caller falls back to the server.
   */
  async render(doc: TemplateDoc, data: PreviewData, hidden: number[] = []): Promise<Frame | null> {
    if (this.state === 'failed') return null;
    const seq = ++this.asked;
    const ids = [...new Set(doc.elements.flatMap((e) => (e.type === 'static_image' && e.image_id ? [e.image_id] : [])))];
    await Promise.all(ids.map((id) => this.sendImage(id)));
    // Something newer was asked for while its images came, or it failed.
    if (seq < this.queued || this.failed()) return null;
    this.queued = seq;
    return new Promise((resolve) => {
      const req: Request = { seq, template: JSON.stringify(doc), data: JSON.stringify(data.data), placeholders: data.placeholders, hidden, resolve };
      if (this.state === 'ready' && !this.drawing) {
        this.post(req);
      } else {
        this.waiting?.resolve(null);
        this.waiting = req;
      }
    });
  }

  /**
   * Gives the renderer the icons labels draw (README §3.7): the build's,
   * the unknown icon and the palette's light colors, as live.Icons. They
   * reach it before anything asked for after.
   */
  setIcons(json: string) {
    if (!this.failed()) this.port.postMessage({ type: 'icons', json });
  }

  /** Stops the worker. */
  close() {
    this.port.terminate();
    this.fail('closed');
  }

  private sendImage(id: string): Promise<void> {
    const sent = this.images.get(id);
    if (sent) return sent;
    // An image that cannot be fetched is left out: the renderer then says it
    // is missing, as the server would, and it is fetched again next time.
    const sending = this.fetchImage(id).then(
      (bytes) => this.port.postMessage({ type: 'image', id, bytes }, [bytes]),
      () => {
        this.images.delete(id);
      },
    );
    this.images.set(id, sending);
    return sending;
  }

  private failed() {
    return this.state === 'failed';
  }

  private post(req: Request) {
    this.drawing = req;
    this.port.postMessage({ type: 'render', seq: req.seq, template: req.template, data: req.data, placeholders: req.placeholders, hidden: req.hidden });
  }

  private receive(msg: FromWorker) {
    switch (msg.type) {
      case 'ready':
        if (msg.fonts) addFonts(msg.fonts);
        this.setState('ready');
        this.next();
        return;
      case 'failed':
        this.fail(msg.message);
        return;
      case 'image-failed':
        return;
      case 'frame': {
        const req = this.drawing;
        if (!req || req.seq !== msg.seq) return;
        this.drawing = null;
        this.next();
        req.resolve({
          width: msg.width,
          height: msg.height,
          pixels: new Uint8ClampedArray(msg.pixels),
          description: msg.description ? JSON.parse(msg.description) : null,
          problems: msg.problems ? JSON.parse(msg.problems) : [],
        });
      }
    }
  }

  private next() {
    const req = this.waiting;
    if (!req || this.drawing || this.state !== 'ready') return;
    this.waiting = null;
    this.post(req);
  }

  private fail(message: string) {
    if (this.state === 'failed') return;
    this.failure = message;
    this.drawing?.resolve(null);
    this.waiting?.resolve(null);
    this.drawing = this.waiting = null;
    this.setState('failed');
  }

  private setState(state: LiveState) {
    this.state = state;
    this.onstate?.(state);
  }
}

/** The family the fonts labels print in are known by, in the page. */
export const PRINTED_FONT = { sans: 'BAM Printed Sans', mono: 'BAM Printed Mono' } as const;

/** Adds the fonts labels print in to the page, for text being typed. */
function addFonts(fonts: Record<string, ArrayBuffer>) {
  if (typeof FontFace === 'undefined' || typeof document === 'undefined') return;
  for (const [key, ttf] of Object.entries(fonts)) {
    const [family, weight] = key.split(' ') as ['sans' | 'mono', string];
    const face = new FontFace(PRINTED_FONT[family], ttf, { weight: weight === 'bold' ? '700' : '400' });
    document.fonts.add(face);
    void face.load().catch(() => {});
  }
}

/**
 * Starts the live renderer in a worker; it reports itself failed at once
 * where workers or WebAssembly are not to be had.
 */
export function startLiveRenderer(fetchImage: (id: string) => Promise<ArrayBuffer>): LiveRenderer {
  if (typeof Worker === 'undefined' || typeof WebAssembly === 'undefined') {
    const none: Port = { postMessage() {}, onmessage: null, onerror: null, terminate() {} };
    const live = new LiveRenderer(none, fetchImage);
    none.onmessage?.({ data: { type: 'failed', message: 'this browser cannot run it' } } as MessageEvent<FromWorker>);
    return live;
  }
  const worker = new Worker(new URL('./worker.ts', import.meta.url), { type: 'module' });
  return new LiveRenderer(worker as unknown as Port, fetchImage);
}
