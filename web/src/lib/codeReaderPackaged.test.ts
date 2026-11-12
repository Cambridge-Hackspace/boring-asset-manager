// @vitest-environment node

// The packaged code reader reads every code in the barcode goldens: the
// module matrices BAM!'s own encoders are tested to draw, from Python's
// segno for QR and Micro QR (internal/generators/*/testdata), drawn as
// pixels with their quiet zones and read through the polyfill, as the
// camera's frames are.

import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { beforeAll, describe, expect, it } from 'vitest';
import { prepareZXingModule } from 'barcode-detector/ponyfill';
import { PackagedDetector } from './codeReaderPackaged';
import { scanFormats, type Detector } from './codeReader';

const here = (p: string) => fileURLToPath(new URL(p, import.meta.url));
const golden = <T>(p: string): T[] => JSON.parse(readFileSync(here(`../../../internal/generators/${p}`), 'utf8'));

// What a page has and Node does not: the polyfill takes ImageData, and
// answers with DOMRectReadOnly.
class ImageDataStandIn {
  readonly colorSpace = 'srgb';
  constructor(
    readonly data: Uint8ClampedArray,
    readonly width: number,
    readonly height: number,
  ) {}
}
class DOMRectStandIn {
  constructor(
    readonly x = 0,
    readonly y = 0,
    readonly width = 0,
    readonly height = 0,
  ) {}
  static fromRect(r: { x?: number; y?: number; width?: number; height?: number } = {}) {
    return new DOMRectStandIn(r.x, r.y, r.width, r.height);
  }
}

/** rows of modules, each '1' dark, drawn scale pixels a module within quiet modules of white. */
function draw(rows: string[], scale: number, quiet: number): ImageData {
  const width = (rows[0]!.length + 2 * quiet) * scale;
  const height = (rows.length + 2 * quiet) * scale;
  const data = new Uint8ClampedArray(width * height * 4).fill(255);
  rows.forEach((row, y) => {
    [...row].forEach((m, x) => {
      if (m !== '1') return;
      for (let dy = 0; dy < scale; dy++) {
        for (let dx = 0; dx < scale; dx++) {
          const i = (((y + quiet) * scale + dy) * width + (x + quiet) * scale + dx) * 4;
          data[i] = data[i + 1] = data[i + 2] = 0;
        }
      }
    });
  });
  return new ImageData(data, width, height);
}

let detector: Detector;

beforeAll(async () => {
  const g = globalThis as Record<string, unknown>;
  g.ImageData ??= ImageDataStandIn;
  g.DOMRectReadOnly ??= DOMRectStandIn;
  // The WebAssembly from the package, as the build serves it; here read
  // from disk, as Node has no server to fetch it from.
  const wasm = readFileSync(here('../../node_modules/zxing-wasm/dist/reader/zxing_reader.wasm'));
  await prepareZXingModule({ overrides: { wasmBinary: wasm.buffer.slice(wasm.byteOffset, wasm.byteOffset + wasm.byteLength) }, fireImmediately: true });
  detector = new PackagedDetector({ formats: scanFormats });
});

async function read(image: ImageData): Promise<string | undefined> {
  return (await detector.detect(image as unknown as HTMLVideoElement))[0]?.rawValue;
}

describe('the packaged code reader', () => {
  it('reads every format BAM! prints', async () => {
    expect(await PackagedDetector.getSupportedFormats()).toEqual(expect.arrayContaining(scanFormats));
  });

  it('reads every QR code golden', async () => {
    const cases = golden<{ name: string; payload: string; rows: string[] }>('qr/testdata/segno.json');
    expect(cases.length).toBeGreaterThan(0);
    for (const c of cases) expect(await read(draw(c.rows, 4, 4)), c.name).toBe(c.payload);
  });

  it('reads every Micro QR golden', async () => {
    const cases = golden<{ name: string; payload: string; rows: string[] }>('microqr/testdata/segno.json');
    expect(cases.length).toBeGreaterThan(0);
    for (const c of cases) expect(await read(draw(c.rows, 4, 2)), c.name).toBe(c.payload);
  });

  it('reads every Code 128 golden', async () => {
    const cases = golden<{ payload: string; modules: string }>('code128/testdata/golden.json');
    expect(cases.length).toBeGreaterThan(0);
    for (const c of cases) expect(await read(draw(Array(30).fill(c.modules), 3, 10)), c.payload).toBe(c.payload);
  });
});
