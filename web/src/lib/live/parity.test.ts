// @vitest-environment node

// The live renderer, as built (tools/web/build.sh builds it before these
// tests run), draws every golden label dot for dot as the server does:
// internal/render's TestLiveParityFixture keeps the fixture's hashes those
// of the server's own drawing.

import { createHash } from 'node:crypto';
import { readFileSync } from 'node:fs';
import { fileURLToPath, pathToFileURL } from 'node:url';
import { beforeAll, describe, expect, it } from 'vitest';

interface Case {
  name: string;
  template: unknown;
  data: unknown;
  images?: Record<string, string>;
  /** What setIcons takes, for a label that draws icons. */
  icons?: unknown;
  /** The inks it is drawn in (docs/labels.md#inks); absent, the template's own. */
  palette?: string;
  width: number;
  height: number;
  sha256: string;
  /** The hash is of each dot's red, green and blue, not of its gray alone. */
  colors?: boolean;
}

interface Bamlive {
  addImage(id: string, bytes: Uint8Array): string | null;
  setIcons(json: string): string | null;
  render(
    template: string,
    data: string,
    placeholders: boolean,
    hidden: string,
    palette: string,
  ): { width: number; height: number; pixels: Uint8ClampedArray; problems: string | null };
}

const here = (path: string) => fileURLToPath(new URL(path, import.meta.url));
const cases: Case[] = JSON.parse(readFileSync(here('../../../../internal/render/testdata/live_parity.json'), 'utf8'));
let live: Bamlive;

beforeAll(async () => {
  // Go's loader, which sets globalThis.Go.
  await import(pathToFileURL(here('./generated/wasm_exec.js')).href);
  const go = new (globalThis as unknown as { Go: new () => { importObject: WebAssembly.Imports; run(i: WebAssembly.Instance): Promise<void> } }).Go();
  const { instance } = await WebAssembly.instantiate(readFileSync(here('./generated/bamlive.wasm')), go.importObject);
  void go.run(instance);
  live = (globalThis as unknown as { bamlive: Bamlive }).bamlive;
});

describe('the live renderer', () => {
  it('has every golden label to draw, icons among them, and one in each palette', () => {
    expect(cases.length).toBeGreaterThanOrEqual(9);
    expect(cases.some((c) => c.icons)).toBe(true);
    expect(new Set(cases.map((c) => c.palette ?? ''))).toEqual(new Set(['', 'bw', 'bwr', 'bwy', 'bwry', 'bwrygb']));
    expect(cases.some((c) => c.colors && !c.palette)).toBe(true);
  });

  it.each(cases.map((c) => [c.name, c] as const))('draws %s dot for dot as the server does', (_, c) => {
    for (const [id, b64] of Object.entries(c.images ?? {})) {
      expect(live.addImage(id, new Uint8Array(Buffer.from(b64, 'base64')))).toBeNull();
    }
    if (c.icons) expect(live.setIcons(JSON.stringify(c.icons))).toBeNull();
    const out = live.render(JSON.stringify(c.template), JSON.stringify(c.data), false, '[]', c.palette ?? '');
    expect(out.problems).toBeNull();
    expect([out.width, out.height]).toEqual([c.width, c.height]);
    const channels = c.colors ? 3 : 1;
    const dots = new Uint8Array(out.width * out.height * channels);
    for (let i = 0; i < out.width * out.height; i++) {
      for (let k = 0; k < channels; k++) dots[i * channels + k] = out.pixels[i * 4 + k]!;
    }
    expect(createHash('sha256').update(dots).digest('hex')).toBe(c.sha256);
  });
});
