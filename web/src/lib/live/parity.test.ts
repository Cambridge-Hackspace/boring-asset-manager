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
  width: number;
  height: number;
  sha256: string;
}

interface Bamlive {
  addImage(id: string, bytes: Uint8Array): string | null;
  render(template: string, data: string, placeholders: boolean): { width: number; height: number; pixels: Uint8ClampedArray; problems: string | null };
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
  it('has every golden label to draw', () => {
    expect(cases.length).toBeGreaterThanOrEqual(9);
  });

  it.each(cases.map((c) => [c.name, c] as const))('draws %s dot for dot as the server does', (_, c) => {
    for (const [id, b64] of Object.entries(c.images ?? {})) {
      expect(live.addImage(id, new Uint8Array(Buffer.from(b64, 'base64')))).toBeNull();
    }
    const out = live.render(JSON.stringify(c.template), JSON.stringify(c.data), false);
    expect(out.problems).toBeNull();
    expect([out.width, out.height]).toEqual([c.width, c.height]);
    const dots = new Uint8Array(out.width * out.height);
    for (let i = 0; i < dots.length; i++) dots[i] = out.pixels[i * 4]!;
    expect(createHash('sha256').update(dots).digest('hex')).toBe(c.sha256);
  });
});
