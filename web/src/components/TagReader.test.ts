import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/svelte';
import TagReader from './TagReader.svelte';
import { fakeApi } from '../test/fetch';

type Reading = { serialNumber?: string; message?: { records: { recordType: string; data?: DataView }[] } };

class Reader {
  static all: Reader[] = [];
  listeners: Record<string, ((e: Reading) => void)[]> = {};
  signal: AbortSignal | undefined;
  constructor() {
    Reader.all.push(this);
  }
  scan(options?: { signal?: AbortSignal }) {
    this.signal = options?.signal;
    return Promise.resolve();
  }
  addEventListener(type: string, l: (e: Reading) => void) {
    (this.listeners[type] ??= []).push(l);
  }
  emit(e: Reading) {
    for (const l of this.listeners.reading ?? []) l(e);
  }
}

const link = (s: string) => ({ message: { records: [{ recordType: 'url', data: new DataView(new TextEncoder().encode(s).buffer) }] } });
const g = globalThis as { NDEFReader?: unknown };

function allowed(state: PermissionState) {
  vi.stubGlobal('navigator', { ...navigator, onLine: true, permissions: { query: vi.fn(async () => ({ state })) } });
}

beforeEach(() => {
  Reader.all = [];
  g.NDEFReader = Reader;
  allowed('prompt');
});
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  delete g.NDEFReader;
});

async function listening(onfound = vi.fn()) {
  render(TagReader, { onfound });
  await fireEvent.click(screen.getByRole('button', { name: 'Read NFC tags' }));
  await screen.findByText(/Listening for NFC tags/);
  return { onfound, reader: Reader.all.at(-1)! };
}

describe('TagReader', () => {
  it('isn’t offered where the browser can’t read tags', () => {
    delete g.NDEFReader;
    render(TagReader, { onfound: vi.fn() });
    expect(screen.queryByRole('button')).toBeNull();
    expect(screen.queryByRole('region', { name: 'NFC tags' })).toBeNull();
  });

  it('waits to be asked, then opens the asset a tag links to here', async () => {
    fakeApi({ 'GET /info': () => ({ base_url: 'https://bam.example.org' }) });
    render(TagReader, { onfound: vi.fn() });
    await Promise.resolve();
    expect(Reader.all).toHaveLength(0);
    cleanup();

    const { onfound, reader } = await listening();
    reader.emit({ serialNumber: '04:a2:3b:1c', ...link('https://bam.example.org/scan/a1') });
    await waitFor(() => expect(onfound).toHaveBeenCalledWith('/scan/a1'));
  });

  it('listens at once where NFC is already allowed', async () => {
    fakeApi({ 'GET /info': () => ({}) });
    allowed('granted');
    render(TagReader, { onfound: vi.fn() });
    expect(await screen.findByText(/Listening for NFC tags/)).toBeTruthy();
    expect(Reader.all).toHaveLength(1);
  });

  it('opens the asset whose code is the tag’s serial number', async () => {
    const { sent } = fakeApi({ 'GET /info': () => ({}), 'GET /scan/04%3AA2%3A3B%3A1C': () => ({ asset: { id: 'a1' } }) });
    const { onfound, reader } = await listening();
    reader.emit({ serialNumber: '04:a2:3b:1c' });
    await waitFor(() => expect(onfound).toHaveBeenCalledWith('/scan/04%3AA2%3A3B%3A1C'));
    expect(sent.map((s) => s.path)).toContain('/scan/04%3AA2%3A3B%3A1C');
  });

  it('says when a tag links elsewhere, or holds nothing known here', async () => {
    fakeApi({ 'GET /info': () => ({ base_url: 'https://bam.example.org' }), 'GET /scan/04%3AA2%3A3B%3A1C': () => ({ status: 404, body: { error: 'No asset has that code' } }) });
    const { onfound, reader } = await listening();
    reader.emit({ serialNumber: '04:a2:3b:1c', ...link('https://other.example.net/scan/a1') });
    expect(await screen.findByText('That tag links to somewhere other than this BAM!, and its serial number isn’t any asset’s code.')).toBeTruthy();
    reader.emit({});
    expect(await screen.findByText('That tag holds no link to an asset here.')).toBeTruthy();
    expect(onfound).not.toHaveBeenCalled();
  });

  it('stops listening when it goes', async () => {
    fakeApi({ 'GET /info': () => ({}) });
    const { reader } = await listening();
    cleanup();
    expect(reader.signal?.aborted).toBe(true);
  });
});
